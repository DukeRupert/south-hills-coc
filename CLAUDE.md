# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Hugo static site for South Hills Church of Christ (Helena, MT) with a Go API backend for the contact form. Deployed via Docker to a VPS using GitHub Actions. Uses a two-Caddy architecture: outer Caddy on host for HTTPS/TLS, inner Caddy in container for static serving and API proxying.

## Build Commands

```bash
# Hugo development
hugo server -D                    # Dev server with drafts at localhost:1313
hugo --gc --minify               # Production build (outputs to public/)

# Docker
docker build -t south-hills-coc . # Build container
docker compose up -d              # Run locally at localhost:8082
docker compose logs               # View logs

# Go API
cd api && go build -o contact-api # Build API binary
cd api && go run .                # Run API directly (for testing)
```

## Architecture

```
Internet → Outer Caddy (HTTPS) → Docker Container (HTTP:8082)
                                      ├── Inner Caddy (static files + /api/* proxy)
                                      └── Go API (localhost:8080)
```

### Key Files
- `hugo.toml` - Site config, params (phone, email, address, service times), Turnstile site key
- `Caddyfile` - Inner Caddy: static serving from `/srv`, reverse proxy `/api/*` to Go API
- `Dockerfile` - Three-stage build: Hugo → Go → Caddy final image
- `docker-entrypoint.sh` - Starts Go API in background, then Caddy
- `docker-compose.yml` - Container config with env var passthrough

### Hugo Template Hierarchy
- `layouts/_default/baseof.html` - Base template with SEO meta, Schema.org structured data
- `layouts/index.html` - Homepage
- `layouts/page/contact.html` - Contact form with Turnstile
- `layouts/page/visit.html` - Visit page
- `layouts/about/leadership.html` - Leadership page using `data/leadership.yaml`
- `layouts/ministries/list.html` - Ministries list using `data/ministries.yaml`
- `layouts/partials/` - Shared header, footer, schema

### Data Files
- `data/leadership.yaml` - Church leadership info (elders, deacons, ministers)
- `data/ministries.yaml` - Ministry descriptions

## API Endpoints

- `POST /api/contact` - Contact form submission (validates Turnstile, sends email via Postmark)
- `GET /api/health` - Health check

## Environment Variables

Required for Go API:
- `POSTMARK_TOKEN` - Postmark API key for sending emails
- `FROM_EMAIL` - Sender email (must be verified in Postmark)
- `TO_EMAIL` - Recipient email
- `ALLOWED_ORIGIN` - CORS origin (must match production domain exactly, e.g., `https://www.southhillscoc.com`)
- `TURNSTILE_SECRET` - Cloudflare Turnstile secret key

Turnstile test keys for localhost development:
- Site Key: `1x00000000000000000000AA` (put in hugo.toml params.turnstileSiteKey)
- Secret: `1x0000000000000000000000000000000AA` (put in TURNSTILE_SECRET env var)

## Deployment

Pushes to `master` trigger GitHub Actions: builds Docker image → pushes to Docker Hub → SSHs to VPS → pulls and restarts container.

Required GitHub secrets: `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`, `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`

VPS location: `/opt/south-hills-coc/` with `docker-compose.yml` and `.env`

## Local Development with Contact Form

To test the contact form locally:

1. Update `hugo.toml` with test Turnstile site key
2. Run Hugo: `hugo server -D`
3. In another terminal, run the API with test env vars:
   ```bash
   cd api
   TURNSTILE_SECRET="1x0000000000000000000000000000000AA" \
   ALLOWED_ORIGIN="http://localhost:1313" \
   go run .
   ```
4. Note: Emails won't send without valid Postmark credentials
