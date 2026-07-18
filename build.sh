#!/usr/bin/env bash
# Build the single-binary PPT generator:
#   1) lint / typecheck / export the Next frontend
#   2) embed static assets into internal/web/dist
#   3) run Go tests
#   4) produce bin/ppt-gen
#
# Usage:
#   ./build.sh
#   SKIP_FRONTEND=1 ./build.sh          # reuse existing embed assets
#   SKIP_TESTS=1 ./build.sh             # skip go test
#   SKIP_LINT=1 ./build.sh              # skip frontend lint/typecheck
#   GO_FLAGS="-trimpath" ./build.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$ROOT_DIR/fronted"
FRONTEND_OUT="$FRONTEND_DIR/out"
EMBED_DIR="$ROOT_DIR/internal/web/dist"
BIN_DIR="$ROOT_DIR/bin"
BIN_PATH="$BIN_DIR/ppt-gen"

SKIP_FRONTEND="${SKIP_FRONTEND:-0}"
SKIP_TESTS="${SKIP_TESTS:-0}"
SKIP_LINT="${SKIP_LINT:-0}"
GO_FLAGS="${GO_FLAGS:-}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

require_cmd go
if [[ "$SKIP_FRONTEND" != "1" ]]; then
  require_cmd npm
fi

started_at="$(date +%s)"

if [[ "$SKIP_FRONTEND" != "1" ]]; then
  log "Building frontend"
  cd "$FRONTEND_DIR"

  if [[ ! -d node_modules ]]; then
    log "Installing frontend dependencies"
    if [[ -f package-lock.json ]]; then
      npm ci
    else
      npm install
    fi
  fi

  if [[ "$SKIP_LINT" != "1" ]]; then
    npm run lint
    npm run typecheck
  else
    log "Skipping frontend lint/typecheck (SKIP_LINT=1)"
  fi

  # Optional node tests when present (navigation helpers, etc.)
  if compgen -G "lib/*.test.ts" >/dev/null 2>&1; then
    log "Running frontend unit tests"
    node --experimental-strip-types --test lib/*.test.ts
  fi

  npm run build

  [[ -d "$FRONTEND_OUT" ]] || die "frontend export missing: $FRONTEND_OUT"
  [[ -f "$FRONTEND_OUT/index.html" ]] || die "frontend export incomplete: index.html not found"

  log "Embedding frontend assets into internal/web/dist"
  mkdir -p "$EMBED_DIR"
  # Preserve .gitkeep while replacing exported assets.
  find "$EMBED_DIR" -mindepth 1 ! -name '.gitkeep' -exec rm -rf {} +
  cp -a "$FRONTEND_OUT"/. "$EMBED_DIR"/
else
  log "Skipping frontend build (SKIP_FRONTEND=1)"
  [[ -f "$EMBED_DIR/index.html" ]] || die "embed assets missing; run without SKIP_FRONTEND=1 first"
fi

cd "$ROOT_DIR"

if [[ "$SKIP_TESTS" != "1" ]]; then
  log "Testing backend"
  # Keep scope to app packages only — never walk fronted/node_modules.
  # shellcheck disable=SC2086
  go test $GO_FLAGS ./cmd/server ./internal/...
else
  log "Skipping backend tests (SKIP_TESTS=1)"
fi

log "Building single binary"
mkdir -p "$BIN_DIR"
# shellcheck disable=SC2086
go build $GO_FLAGS -o "$BIN_PATH" ./cmd/server

elapsed="$(( $(date +%s) - started_at ))"
log "Build completed successfully in ${elapsed}s"
printf 'Binary: %s\n' "$BIN_PATH"
printf 'Run:    ADDR=:8080 %s\n' "$BIN_PATH"
printf 'Debug:  DEBUG=1 ADDR=:8080 %s\n' "$BIN_PATH"
