# Stage 1: Build Hugo site
FROM hugomods/hugo:exts AS hugo-builder
WORKDIR /src
COPY . .
RUN hugo --gc --minify

# Stage 2: Build Go API
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

# Copy Go API binary
COPY --from=go-builder /app/contact-api /usr/local/bin/contact-api

# Copy Caddyfile
COPY Caddyfile /etc/caddy/Caddyfile

# Copy entrypoint script
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]
