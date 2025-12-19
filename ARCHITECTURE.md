# Hugo Static Site Architecture Reference

This document describes the architecture for deploying Hugo static sites with optional Go API backends to a VPS using Docker and GitHub Actions.

## Architecture Overview

```
Internet → Outer Caddy (HTTPS/TLS) → Docker Container (HTTP:8082)
                                          ├── Inner Caddy (static files + reverse proxy)
                                          └── Go API (optional, for forms/dynamic features)
```

## Project Structure

```
project/
├── .github/
│   └── workflows/
│       └── deploy.yml          # GitHub Actions CI/CD
├── api/                        # Go API (optional)
│   ├── main.go
│   └── go.mod
├── assets/
│   └── css/
│       └── main.css            # Site styles
├── content/                    # Hugo content (markdown)
├── layouts/                    # Hugo templates
│   ├── _default/
│   │   ├── baseof.html         # Base template
│   │   └── single.html         # Single page template
│   ├── partials/
│   │   ├── header.html
│   │   └── footer.html
│   └── index.html              # Homepage
├── static/                     # Static assets (images, fonts)
├── .env.example                # Environment template
├── Caddyfile                   # Inner Caddy config
├── docker-compose.yml          # Docker orchestration
├── docker-entrypoint.sh        # Container startup script
├── Dockerfile                  # Multi-stage Docker build
├── hugo.toml                   # Hugo configuration
└── DEPLOYMENT.md               # Deployment instructions
```

## Key Configuration Files

### hugo.toml

```toml
baseURL = 'https://www.example.com/'
languageCode = 'en-us'
title = 'Site Title'

[params]
  description = 'Site description for SEO'
  phone = '555-555-5555'
  # Add Turnstile site key for spam protection
  turnstileSiteKey = 'your-site-key'

[params.social]
  facebook = 'https://facebook.com/yourpage'
  instagram = 'https://instagram.com/yourpage'
```

### Dockerfile (Multi-stage build)

```dockerfile
# Stage 1: Build Hugo site
FROM hugomods/hugo:exts AS hugo-builder
WORKDIR /src
COPY . .
RUN hugo --gc --minify

# Stage 2: Build Go API (optional)
FROM golang:1.21-alpine AS go-builder
WORKDIR /app
COPY api/go.mod ./
RUN go mod download
COPY api/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o contact-api .

# Stage 3: Final image with Caddy
FROM caddy:2-alpine

# Copy Hugo static site
COPY --from=hugo-builder /src/public /srv

# Copy Go API binary (optional)
COPY --from=go-builder /app/contact-api /usr/local/bin/contact-api

# Copy Caddyfile
COPY Caddyfile /etc/caddy/Caddyfile

# Copy entrypoint script
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]
```

### Caddyfile (Inner Caddy)

```caddyfile
{
    admin off
    auto_https off
}

:{$PORT:80} {
    root * /srv
    file_server

    # Proxy API requests to Go backend
    handle /api/* {
        reverse_proxy localhost:8080
    }

    try_files {path} {path}/ /index.html

    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
    }

    encode gzip zstd

    log {
        output stdout
        format console
    }
}
```

### docker-entrypoint.sh

```bash
#!/bin/sh
set -e

# Start the Go API in the background (if exists)
if [ -f /usr/local/bin/contact-api ]; then
    echo "Starting contact API..."
    /usr/local/bin/contact-api &
    sleep 1
fi

# Start Caddy in the foreground
echo "Starting Caddy..."
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
```

### docker-compose.yml

```yaml
services:
  web:
    image: ${DOCKER_IMAGE:-username/project:latest}
    ports:
      - "${LISTEN_PORT:-8082}:80"
    environment:
      - PORT=80
      - POSTMARK_TOKEN=${POSTMARK_TOKEN}
      - FROM_EMAIL=${FROM_EMAIL}
      - TO_EMAIL=${TO_EMAIL}
      - ALLOWED_ORIGIN=${ALLOWED_ORIGIN}
      - TURNSTILE_SECRET=${TURNSTILE_SECRET}
    restart: unless-stopped
```

### .github/workflows/deploy.yml

```yaml
name: Build and Deploy

on:
  push:
    branches:
      - master

env:
  IMAGE_NAME: project-name

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Build and push Docker image
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: |
            ${{ secrets.DOCKERHUB_USERNAME }}/${{ env.IMAGE_NAME }}:latest
            ${{ secrets.DOCKERHUB_USERNAME }}/${{ env.IMAGE_NAME }}:${{ github.sha }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Deploy to VPS
        uses: appleboy/ssh-action@v1.0.3
        with:
          host: ${{ secrets.VPS_HOST }}
          username: ${{ secrets.VPS_USER }}
          key: ${{ secrets.VPS_SSH_KEY }}
          script: |
            cd /opt/project-name
            docker compose pull
            docker compose up -d
            docker image prune -f
```

## VPS Setup

### Outer Caddy Configuration

Add to `/etc/caddy/Caddyfile`:

```caddyfile
www.example.com {
    reverse_proxy localhost:8082
}

example.com {
    redir https://www.example.com{uri} permanent
}
```

### Deploy User Setup (Fedora/RHEL)

```bash
# Create deploy user
sudo useradd -r -s /bin/bash -m -d /home/deploy-user deploy-user
sudo usermod -aG docker deploy-user

# Setup SSH key
sudo mkdir -p /home/deploy-user/.ssh
sudo ssh-keygen -t ed25519 -f /home/deploy-user/.ssh/github_deploy -N "" -C "deploy@github-actions"
sudo cat /home/deploy-user/.ssh/github_deploy.pub | sudo tee /home/deploy-user/.ssh/authorized_keys
sudo chmod 700 /home/deploy-user/.ssh
sudo chmod 600 /home/deploy-user/.ssh/authorized_keys
sudo chown -R deploy-user:deploy-user /home/deploy-user/.ssh
sudo restorecon -R /home/deploy-user/.ssh  # SELinux

# Create project directory
sudo mkdir -p /opt/project-name
sudo chown -R deploy-user:deploy-user /opt/project-name
```

### Required Files on VPS

Only two files needed in `/opt/project-name/`:

1. `docker-compose.yml`
2. `.env` (with secrets)

### GitHub Secrets Required

| Secret | Description |
|--------|-------------|
| `DOCKERHUB_USERNAME` | Docker Hub username |
| `DOCKERHUB_TOKEN` | Docker Hub access token |
| `VPS_HOST` | VPS IP address |
| `VPS_USER` | SSH username (deploy user) |
| `VPS_SSH_KEY` | Private SSH key |

## Common Patterns

### Contact Form with Spam Protection

1. **Frontend** (Hugo template):
   - Cloudflare Turnstile widget
   - Honeypot field (hidden)
   - JavaScript to POST JSON to `/api/contact`

2. **Backend** (Go API):
   - Validate Turnstile token with Cloudflare API
   - Check honeypot field (reject silently if filled)
   - Send email via Postmark/SendGrid/etc.

3. **Environment Variables**:
   - `TURNSTILE_SECRET` - Cloudflare Turnstile secret key
   - `POSTMARK_TOKEN` - Email service API key
   - `FROM_EMAIL` - Sender email address
   - `TO_EMAIL` - Recipient email address
   - `ALLOWED_ORIGIN` - CORS origin (production domain)

### Turnstile Test Keys (for localhost)

- Site Key: `1x00000000000000000000AA`
- Secret: `1x0000000000000000000000000000000AA`

## Deployment Checklist

### New Site Setup

- [ ] Create Hugo project structure
- [ ] Create Dockerfile, Caddyfile, docker-entrypoint.sh
- [ ] Create docker-compose.yml
- [ ] Create .env.example
- [ ] Create GitHub Actions workflow
- [ ] Set up GitHub secrets
- [ ] Create deploy user on VPS
- [ ] Configure outer Caddy on VPS
- [ ] rsync docker-compose.yml and .env to VPS
- [ ] Push to trigger first deployment

### Pre-Deployment Checks

- [ ] Production Turnstile keys in hugo.toml
- [ ] Production secrets in VPS .env
- [ ] ALLOWED_ORIGIN matches production domain
- [ ] DNS configured for domain

## Troubleshooting

### Port Conflicts

- Outer Caddy: Uses host ports (80, 443)
- Container: Exposes on LISTEN_PORT (default 8082)
- Inner Caddy: Port 80 inside container
- Go API: Port 8080 inside container (use API_PORT env var)

### Container Won't Start

```bash
docker compose logs
```

### API Not Receiving Requests

1. Check CORS: `ALLOWED_ORIGIN` must match exactly
2. Check Caddy proxy: `/api/*` routes to localhost:8080
3. Check API is running: logs should show "Server starting on port 8080"

### Turnstile Errors

- "Invalid domain": Add domain to Turnstile widget in Cloudflare, or use test keys for localhost
- 403 errors: Check TURNSTILE_SECRET is correct

### Email Not Sending

1. Check email service API token
2. Verify FROM_EMAIL is authorized sender
3. Check container logs for Postmark/SendGrid errors
