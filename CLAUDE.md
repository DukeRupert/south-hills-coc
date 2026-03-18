# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go web server for South Hills Church of Christ (Helena, MT) using html/template, htmx, and Tailwind CSS. Contact form with Turnstile CAPTCHA and Postmark email. Deployed via Docker to a Hetzner VPS using GitHub Actions. Outer Caddy on host handles HTTPS/TLS, Go server in container handles everything else.

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
```

Turnstile test keys for localhost:
- Site Key: `1x00000000000000000000AA`
- Secret: `1x0000000000000000000000000000000AA`

## Deployment

Pushes to `master` trigger GitHub Actions: builds Docker image → pushes to Docker Hub → SSHs to VPS → pulls and restarts container.

Required GitHub secrets: `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`, `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`

VPS location: `/opt/south-hills-coc/` with `docker-compose.yml` and `.env`

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
Warm, welcoming, genuine. Like a friend inviting you to something they love — not a corporation, not a sales pitch, not performative. The voice is honest, approachable, and assumes no church background. Montana-grounded: earthy, unpretentious, community-oriented.

### Aesthetic Direction
- **Palette:** Warm earth tones anchored by terracotta (#975849). No cold grays, no bright blues, no neons. Every neutral has a warm undertone. Alternating sections use warm cream (#FAF7F4), never gray-100.
- **Typography:** Inter for everything. Optional Lora serif for scripture only. Generous line-height (leading-relaxed). Headings in logo charcoal (#3F3D3C).
- **Imagery:** Real congregation photos only — no stock photography. Warm color grading, natural light. Candid over posed.
- **Layout:** Generous spacing (py-16 md:py-24). Max-w-6xl content. Breathing room everywhere. Photography-forward sections.
- **Components:** Rounded-lg corners. Warm shadows. Terracotta accents on buttons, borders, icons. Scripture in serif with left border or centered block treatment.
- **Theme:** Light mode only.
- **Icons:** Lucide, stroke-width 1.5, terracotta on light backgrounds, white on dark.

### Design Principles

1. **Visitor-first hierarchy.** Service times, address, and "Plan Your Visit" visible in the first viewport on every device. Information architecture serves the nervous first-timer, not the longtime member.

2. **Warmth in every detail.** No cold grays, no sharp edges, no clinical spacing. Every color, radius, and gap should feel like a warm Montana afternoon — approachable and inviting. When in doubt, add warmth.

3. **Scripture as design element.** Bible verses are visual anchors — set apart in serif, with terracotta accents and generous spacing. They create rhythm between sections, never crammed inline. 2-3 per page max.

4. **Earned trust, not claimed.** Real photos of real people. Honest copy that doesn't over-promise. Celebrate Recovery featured openly. No stock photography, no "All are welcome" platitudes. Show, don't tell.

5. **Polish the details.** The direction is right — elevate execution. Consistent spacing, smooth transitions, aligned elements, intentional hover states. The difference between good and great is in the margins, the timing, and the care.

### Accessibility
- Target: WCAG AA compliance
- Already implemented: skip links, focus-visible states, prefers-reduced-motion, sr-only utility, semantic HTML
- Contrast: white text on terracotta meets AA (~5.5:1). Body text #4B4544 on white meets AA.
- All images require descriptive alt text
- Form inputs have visible labels and focus indicators
