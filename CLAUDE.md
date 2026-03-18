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
