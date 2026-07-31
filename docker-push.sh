#!/bin/bash
set -e

# ── 配置（按需修改） ──
REGISTRY="ghcr.io"          # Docker Hub 改为 docker.io
REPO="injoyai/auto-router"  # 镜像名（GHCR 格式: 用户名/仓库名）

# ── 自动标签 ──
TAG_LATEST="latest"
TAG_SHA=$(git rev-parse --short HEAD)

IMAGE="$REGISTRY/$REPO"

echo "==> Building $IMAGE:$TAG_LATEST ($TAG_SHA)"
docker build \
  -t "$IMAGE:$TAG_LATEST" \
  -t "$IMAGE:$TAG_SHA" \
  .

echo "==> Pushing"
docker push "$IMAGE:$TAG_LATEST"
docker push "$IMAGE:$TAG_SHA"

echo "==> Done: $IMAGE:$TAG_LATEST / $IMAGE:$TAG_SHA"
