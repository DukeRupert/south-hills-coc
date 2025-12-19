#!/bin/sh
set -e

# Start the Go API in the background
if [ -f /usr/local/bin/contact-api ]; then
    echo "Starting contact API..."
    /usr/local/bin/contact-api &
    sleep 1
fi

# Start Caddy in the foreground
echo "Starting Caddy..."
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
