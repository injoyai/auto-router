# ── Stage 1: Build frontend ──
FROM node:22-alpine AS web-builder
ARG HTTP_PROXY="http://127.0.0.1:1080"
ARG HTTPS_PROXY="http://127.0.0.1:1080"
ENV HTTP_PROXY=${HTTP_PROXY} HTTPS_PROXY=${HTTPS_PROXY}
WORKDIR /root
COPY web/package.json web/package-lock.json ./
RUN npm ci --legacy-peer-deps
COPY web/ ./
RUN npm run build

# ── Stage 2: Build backend (embeds frontend dist) ──
FROM golang:1.25-alpine AS go-builder
ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=""
ENV GOPROXY=${GOPROXY}
WORKDIR /root
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /root/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X auto-router/internal/version.Version=${VERSION}" -o auto-router ./cmd/server

# ── Stage 3: Runtime ──
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /root
COPY --from=go-builder /root/auto-router .
RUN mkdir -p data/database config
VOLUME ["/root/data", "/root/config"]
EXPOSE 9090
ENTRYPOINT ["./auto-router"]
