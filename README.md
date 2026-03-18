# South Hills Church of Christ

Website for South Hills Church of Christ in Helena, Montana.

Built with Go, html/template, htmx, and Tailwind CSS v4.

## Local Development

```bash
# Terminal 1: Watch CSS
npm run css:dev

# Terminal 2: Run server with template hot-reload
APP_ENV=development go run ./cmd/server
# http://localhost:8080
```

## Docker

```bash
docker build -t south-hills-coc .
docker compose up -d
# http://localhost:8082
```

## Deployment

Pushes to `master` trigger GitHub Actions: builds Docker image, pushes to Docker Hub, deploys to VPS via SSH.

## License

All rights reserved.
