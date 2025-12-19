# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Hugo static site with optional Go API backend, deployed via Docker to a VPS using GitHub Actions. The architecture uses a two-Caddy setup: an outer Caddy on the host for HTTPS/TLS termination, and an inner Caddy in the container for static file serving and API proxying.

## Build Commands

```bash
# Local Hugo development
hugo server -D                    # Start dev server with drafts
hugo --gc --minify               # Production build (outputs to public/)

# Docker
docker build -t south-hills-coc . # Build container
docker compose up -d              # Run with docker-compose
docker compose logs               # View container logs

# Go API (if using api/ directory)
cd api && go mod download         # Install Go dependencies
cd api && go build -o contact-api # Build API binary
```

## Architecture

```
Internet → Outer Caddy (HTTPS) → Docker Container (HTTP:8082)
                                      ├── Inner Caddy (static files + /api/* proxy)
                                      └── Go API (localhost:8080)
```

### Key Files
- `hugo.toml` - Hugo configuration (baseURL, params, Turnstile site key)
- `Caddyfile` - Inner Caddy config (static serving, API reverse proxy)
- `Dockerfile` - Multi-stage build (Hugo → Go → Caddy final image)
- `docker-entrypoint.sh` - Starts Go API (if present) then Caddy
- `docker-compose.yml` - Container orchestration with env vars

### Directory Structure
- `content/` - Markdown content files (about/, ministries/, events/)
- `layouts/` - Hugo templates (`_default/`, `partials/`, `about/`, `ministries/`, `page/`)
- `assets/css/` - Stylesheets (main.css)
- `static/images/` - Static images (leadership/, worship/, logo)
- `data/` - YAML data files (leadership.yaml, ministries.yaml)
- `api/` - Go API for contact form

## Environment Variables

For the Go API backend:
- `POSTMARK_TOKEN` - Email service API key
- `FROM_EMAIL` / `TO_EMAIL` - Email addresses for contact form
- `ALLOWED_ORIGIN` - CORS origin (must match production domain exactly)
- `TURNSTILE_SECRET` - Cloudflare Turnstile secret key

Turnstile test keys for localhost:
- Site Key: `1x00000000000000000000AA`
- Secret: `1x0000000000000000000000000000000AA`

## Deployment

Pushes to `master` trigger GitHub Actions which builds and pushes a Docker image, then SSHs to the VPS to pull and restart the container. Required GitHub secrets: `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`, `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`.
