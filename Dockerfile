# ── Stage 1: Build frontend ──
FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --legacy-peer-deps
COPY web/ ./
RUN npm run build

# ── Stage 2: Build backend (embeds frontend dist) ──
FROM golang:1.25-alpine AS go-builder
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o auto-router ./cmd

# ── Stage 3: Runtime ──
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /app/auto-router .
RUN mkdir -p data/database config
VOLUME ["/app/data", "/app/config"]
EXPOSE 8080
ENTRYPOINT ["./auto-router"]
