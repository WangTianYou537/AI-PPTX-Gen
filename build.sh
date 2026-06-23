#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$ROOT_DIR/fronted"
EMBED_DIR="$ROOT_DIR/internal/web/dist"
BIN_DIR="$ROOT_DIR/bin"

rm -rf "$EMBED_DIR"
mkdir -p "$EMBED_DIR"

echo "==> Building frontend"
cd "$FRONTEND_DIR"
npm run lint
npm run typecheck
npm run build

echo "==> Embedding frontend assets"
cp -a "$FRONTEND_DIR/out/." "$EMBED_DIR/"

echo "==> Testing backend"
cd "$ROOT_DIR"
go test ./cmd/server ./internal/...

echo "==> Building single binary"
mkdir -p "$BIN_DIR"
go build -o "$BIN_DIR/ppt-gen" ./cmd/server

echo "==> Build completed successfully"
echo "Run: ADDR=:8080 $BIN_DIR/ppt-gen"
