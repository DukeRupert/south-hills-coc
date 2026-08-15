# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go web server for South Hills Church of Christ (Helena, MT) using html/template, htmx, and Tailwind CSS. Contact form with Turnstile CAPTCHA and Postmark email. Double opt-in newsletter list backed by embedded SQLite. Deployed via Docker to a Hetzner VPS using GitHub Actions. Outer Caddy on host handles HTTPS/TLS, Go server in container handles everything else.

## Build Commands

```bash
# Go development server (with template hot-reload)
APP_ENV=development go run ./cmd/server

# Build CSS (Tailwind)
npm run css:dev                  # Watch mode
npm run css:build                # Production build (minified)

# Build Go binary
go build -o server ./cmd/server

# Docker
docker build -t south-hills-coc . # Build container
docker compose up -d              # Run locally at localhost:8082
docker compose logs               # View logs
```

## Architecture

```
Internet → Outer Caddy (HTTPS) → Docker Container (HTTP:8082)
                                      └── Go Server (localhost:8080)
                                            ├── Page routes (html/template)
                                            ├── Static files (/static/*)
                                            └── API endpoints (/api/*)
```

### Key Files
- `cmd/server/main.go` - Entry point, routes, server start
- `internal/config/config.go` - Env-based config (site params, API keys)
- `internal/handlers/handlers.go` - Template parsing, render helper
- `internal/handlers/pages.go` - Page handler funcs (Home, About, etc.)
- `internal/handlers/contact.go` - Contact form API (Turnstile + Postmark)
- `internal/data/data.go` - Leadership + ministries YAML data (go:embed)
- `internal/newsletter/` - Subscriber store (SQLite, embedded migrations)
- `internal/mailer/` - Mailer interface, Postmark client, test fake
- `internal/handlers/newsletter.go` - Signup/confirm/unsubscribe handlers, bot mitigation
- `Dockerfile` - Three-stage build: Tailwind CSS → Go → Alpine final image
- `docker-compose.yml` - Container config with env var passthrough

### Template Hierarchy
- `templates/base.html` - Base layout with SEO meta, Schema.org, nav, footer blocks
- `templates/partials/header.html` - Navigation with mobile hamburger menu
- `templates/partials/footer.html` - Footer with service times, contact, socials
- `templates/partials/schema.html` - Schema.org structured data (Church, WebSite)
- `templates/pages/*.html` - Page-specific content blocks

### Data Files
- `internal/data/leadership.yaml` - Church leadership (staff, elders, deacons)
- `internal/data/ministries.yaml` - Ministry descriptions by category

### CSS / Tailwind
- `static/css/input.css` - Tailwind v4 config + existing site CSS
- `static/css/main.css` - Built output (gitignored)
- Design tokens defined in `@theme` block in input.css
- Existing pages use custom CSS classes; new features should use Tailwind utilities

## Routes

| Path | Handler | Template |
|------|---------|----------|
| `/` | Home | home.html |
| `/visit/` | Visit | visit.html |
| `/about/` | About | about.html |
| `/about/leadership/` | Leadership | about-leadership.html |
| `/about/doctrine/` | Doctrine | about-doctrine.html |
| `/ministries/` | Ministries | ministries.html |
| `/events/` | Events | events.html |
| `/contact/` | Contact | contact.html |
| `/newsletter/` | Newsletter | newsletter.html |
| `POST /newsletter/subscribe` | HandleNewsletterSubscribe | newsletter-sent.html |
| `GET /newsletter/confirm` | NewsletterConfirm | newsletter-confirm.html |
| `POST /newsletter/confirm` | HandleNewsletterConfirm | newsletter-confirmed.html |
| `GET /newsletter/unsubscribe` | NewsletterUnsubscribe | newsletter-unsubscribe.html |
| `POST /newsletter/unsubscribe` | HandleNewsletterUnsubscribe | newsletter-unsubscribed.html |
| `POST /newsletter/unsubscribe/one-click` | HandleNewsletterOneClick | - |
| `POST /api/contact` | HandleContact | - |
| `GET /api/health` | HandleHealth | - |

## Environment Variables

```bash
PORT=8080                    # Server port
APP_ENV=development          # "development" enables template hot-reload

# Postmark (email)
POSTMARK_TOKEN=              # Postmark API key
FROM_EMAIL=                  # Verified sender email
TO_EMAIL=                    # Recipient email

# Security
ALLOWED_ORIGIN=              # CORS origin (e.g., https://www.southhillscoc.org)
TURNSTILE_SECRET=            # Cloudflare Turnstile secret key
TURNSTILE_SITE_KEY=          # Cloudflare Turnstile site key (has default)

# Newsletter
SITE_BASE_URL=               # Absolute, no trailing slash. All email links built from it.
NEWSLETTER_DB_PATH=          # SQLite file (default data/newsletter.db)
POSTMARK_SERVER_TOKEN=       # Falls back to POSTMARK_TOKEN
POSTMARK_STREAM_TRANSACTIONAL=  # Default "outbound" — confirmation + welcome
POSTMARK_STREAM_BROADCAST=      # Default "broadcast" — newsletters only
NEWSLETTER_FROM_NAME=
NEWSLETTER_FROM_ADDRESS=     # Falls back to FROM_EMAIL
FORM_HMAC_SECRET=            # openssl rand -hex 32. Ephemeral if unset.
TRUSTED_PROXY_COUNT=         # Default 2 (Cloudflare + Caddy). Picks the real IP from X-Forwarded-For.
TEMPLATE_DIR=                # Default "templates". Overridable for tests.
STATIC_DIR=                  # Default "static". Serving root + fingerprint source.
```

## Static Assets

Asset URLs are content-fingerprinted: `main.css` is served as
`/static/css/main.ba010de3.css`, where the hash is the first 8 hex digits of
the file's SHA-256. The index is built once at startup by walking `STATIC_DIR`
(`internal/handlers/assets.go`).

This exists because the site sits behind Cloudflare. With a stable filename an
edge cache keeps serving the previous build for its full TTL, so a CSS fix can
be invisible to visitors for hours after a green deploy. Fingerprinting makes
a deploy self-invalidating — new bytes, new URL, no manual purge.

- **Templates must use `{{asset "css/main.css"}}`**, never a literal
  `/static/...` path. A literal path still works but drops to a 5-minute TTL
  and goes stale behind the CDN. `asset` accepts a leading `/` and a `static/`
  prefix, so `{{asset .Image}}` works on YAML-sourced paths.
- **Cache-Control** is `public, max-age=31536000, immutable` for a hashed name,
  `public, max-age=300` for a plain one. Both names serve the same file, so an
  HTML page held in a browser tab across a deploy keeps working.
- **HTML is `no-cache`.** The HTML names the hashed assets, so caching it would
  pin visitors to the previous build's URLs.
- `/favicon.ico` and `/robots.txt` are requested at fixed well-known paths and
  cannot be fingerprinted.
- Assets are read from disk, not `embed.FS` — Tailwind builds `main.css` in a
  Docker stage after the Go build, so the file does not exist at compile time.

## Newsletter

Double opt-in list, under 100 subscribers by design — no queue, no worker pool.
Rules that are easy to break by accident:

- **GET never mutates.** Mail-provider link scanners prefetch URLs found in
  email bodies. `/newsletter/confirm` and `/newsletter/unsubscribe` render a
  page on GET and act only on POST.
- **Responses never vary by subscriber state.** A new address, a subscribed
  one, an unsubscribed one, and a honeypot-tripped bot all get the same bytes
  back from the signup form, or it becomes an enumeration oracle.
- **`MessageStream` is set explicitly on every send.** Omitting it silently
  falls back to the transactional stream.
- **`complained` is permanent.** The public form can never resurrect it.
- **One-click unsubscribe carries no CSRF token** — it is POSTed by the
  recipient's mail provider. Do not put it behind CSRF middleware.

The SQLite file lives on the `newsletter-data` Docker volume. Migrations in
`internal/newsletter/migrations/` run automatically at startup.

Turnstile test keys for localhost:
- Site Key: `1x00000000000000000000AA`
- Secret: `1x0000000000000000000000000000000AA`

## Deployment

Pushes to `master` trigger GitHub Actions: builds Docker image → pushes to Docker Hub → SSHs to VPS → pulls and restarts container.

Required GitHub secrets: `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`, `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`

VPS location: `/opt/south-hills-coc/` with `docker-compose.yml` and `.env`

The newsletter database persists on the `newsletter-data` named volume. After
adding it, `docker-compose.yml` and `.env` on the VPS must be updated by hand —
the deploy workflow only pulls a new image.

## Local Development

```bash
# Terminal 1: Watch CSS
npm run css:dev

# Terminal 2: Run server with template hot-reload
APP_ENV=development go run ./cmd/server
# Server at http://localhost:8080

# To test contact form with Turnstile:
TURNSTILE_SECRET="1x0000000000000000000000000000000AA" \
APP_ENV=development go run ./cmd/server
```

Note: Emails won't send without valid Postmark credentials.

## Design Context

### Users
First-time church visitors deciding whether to show up this Sunday. They're likely anxious, unfamiliar with church culture, and searching on mobile. They need service times, location, and reassurance within seconds. Secondary: current members staying connected, and families evaluating children's programs.

### Brand Personality
Warm, welcoming, genuine. Like a friend inviting you to something they love — not a corporation, not a sales pitch, not performative. The voice is honest, approachable, and assumes no church background. Montana-grounded: earthy, unpretentious, community-oriented. Three words: **warm, grounded, literary.**

### Anti-References
- **No mega-church polish.** Avoid slick, corporate aesthetics (Elevation, Life.Church style). No concert lighting photos, no "experience" language, no startup energy.
- **No generic templates.** Avoid anything that looks like a Squarespace/Wix church template. No stock photos, no cookie-cutter centered-hero-with-three-cards layouts.
- **This should feel handcrafted and community-scale.** Like a well-designed independent bookshop, not a chain store.

### Aesthetic Direction (Brand Guide is Canonical)
The authoritative design direction is defined in `south-hills-ui-guide.html`. All new work must follow this guide.

- **Palette:**
  - Rust `#975849` — brand mark, detail only. Ghost button borders, text links, pills. Never as a background larger than a badge.
  - Rust Dark `#7A4639` — hover/pressed states
  - Rust Pale `#FFDAD1` — tags, type-on-rust backgrounds
  - Sage `#48967D` — eyebrows, action color, series tags, info card left-borders. The "look here" color.
  - Sage Dark `#35413D` — nav bar, sidebar panels, primary button background, H1/H2 color. The heaviest visual weight.
  - Brown `#57443F` — body text, meta text
  - Stone `#F4F0EB` — page background (replaces white)
  - Stone Mid `#EAE5DE` — cards, info panels
  - Ink `#2B2B28` — darkest text
  - No cold grays, no bright blues, no neons. Every neutral has a warm undertone.

- **Typography:**
  - **Headings:** Cormorant Garamond, weight 300 (light). H1: 42px/1.05, H2: 28px/1.1, color: Sage Dark.
  - **H3 / Card titles:** Cormorant Garamond 400 italic, 20px/1.2, color: Brown.
  - **Eyebrows:** Jost 500, 10px, letter-spacing 0.14em, uppercase, color: Sage. Preceded by an 18px horizontal line.
  - **Body:** Jost 300, 13.5px/1.8, color: Brown.
  - **Small/meta:** Jost 300, 11px, letter-spacing 0.04em, color: Brown.
  - **Nav links:** Jost 300, 11px, letter-spacing 0.06em, color: #7A9B90.
  - **Scripture:** Cormorant Garamond 300 italic, generous sizing, color: Sage Dark or Rust.

- **Imagery:** Real congregation photos only — no stock photography. Warm color grading, natural light. Candid over posed.

- **Layout:** Generous spacing. Max-w-900px for content-heavy pages. Breathing room everywhere. Asymmetric layouts preferred over centered-grid monotony.

- **Components:**
  - **Nav:** Sage Dark background, rounded-8px, rust-pale circle mark, two-line wordmark, muted sage links.
  - **Buttons:** Rounded-3px (not rounded-lg). 11px Jost, letter-spacing 0.08em. Primary: sage-dark bg. Ghost: rust border. Sage: sage bg. Text: sage color with arrow.
  - **Cards:** Stone background with 0.5px stone-dark border, rounded-8px. Eyebrow tags with leading line. Cormorant Garamond titles. Rust-colored "Listen/Learn more" links.
  - **Info panels:** Stone-mid background, sage left-border, rounded 0-4px-4px-0.
  - **Sidebar:** Sage-dark background, rounded-8px. Labels in tiny uppercase sage. Values in light text. Rust pill for location.
  - **Tags:** Rounded-2px, 10px text. Rust-pale/sage/stone-mid variants.

- **Theme:** Light mode only. Stone (#F4F0EB) is the base, not white.
- **Icons:** Lucide, stroke-width 1.5, rust on light backgrounds, rust-pale on dark.

### Design Principles

1. **Visitor-first hierarchy.** Service times, address, and "Plan Your Visit" visible in the first viewport on every device. Information architecture serves the nervous first-timer, not the longtime member.

2. **Warmth in every detail.** No cold grays, no sharp edges, no clinical spacing. Every color, radius, and gap should feel like a warm Montana afternoon — stone surfaces, sage structure, rust accents. When in doubt, add warmth.

3. **Scripture as design element.** Bible verses are visual anchors — set in Cormorant Garamond italic, with sage or rust accents and generous spacing. They create rhythm between sections, never crammed inline. 2-3 per page max.

4. **Earned trust, not claimed.** Real photos of real people. Honest copy that doesn't over-promise. Celebrate Recovery featured openly. No stock photography, no "All are welcome" platitudes. Show, don't tell.

5. **Break the grid.** Vary section layouts — asymmetric splits, sidebar panels, eyebrow-labeled sections, editorial spacing. Never three identical cards in a row. Each section should feel intentionally composed, not stamped from a template.

6. **Restraint is confidence.** Light font weights (300), small type sizes, generous letter-spacing, minimal border radii (2-3px). The design should whisper, not shout. Rust is detail-only — never a background field larger than a pill.

### Accessibility
- Target: WCAG AA compliance
- Already implemented: skip links, focus-visible states, prefers-reduced-motion, sr-only utility, semantic HTML
- Contrast: Ink (#2B2B28) on Stone (#F4F0EB) exceeds AA. Brown (#57443F) on Stone meets AA. Rust-pale (#FFDAD1) on Sage Dark (#35413D) must be verified.
- All images require descriptive alt text
- Form inputs have visible labels and focus indicators
