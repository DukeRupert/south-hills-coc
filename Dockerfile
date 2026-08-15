# Stage 1: Build Tailwind CSS
FROM node:22-alpine AS css-builder
WORKDIR /app
COPY package.json ./
RUN npm install
COPY static/css/input.css static/css/input.css
COPY templates/ templates/
RUN npx @tailwindcss/cli -i ./static/css/input.css -o ./static/css/main.css --minify

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# Stage 3: Final image
FROM alpine:latest
WORKDIR /app

# Copy Go binary
COPY --from=go-builder /app/server .

# Copy templates
COPY templates/ templates/

# Copy static assets (images, favicon, robots.txt)
COPY static/ static/

# Copy built CSS from css-builder (overrides input.css with compiled output)
COPY --from=css-builder /app/static/css/main.css static/css/main.css

EXPOSE 8080
CMD ["./server"]
