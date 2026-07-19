#!/usr/bin/env bash
# package-release.sh — build multi-arch binaries and release archives
#
# Usage:
#   ./scripts/package-release.sh --version 1.2.3
#   ./scripts/package-release.sh --version 1.2.3 --commit abcdef --outdir dist
#   SKIP_FRONTEND=1 ./scripts/package-release.sh --version 1.2.3
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION=""
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
OUTDIR="$ROOT_DIR/dist"
SKIP_FRONTEND="${SKIP_FRONTEND:-0}"
SKIP_TESTS="${SKIP_TESTS:-0}"
SKIP_LINT="${SKIP_LINT:-0}"

TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:-}"; shift 2 ;;
    --commit) COMMIT="${2:-}"; shift 2 ;;
    --outdir) OUTDIR="${2:-}"; shift 2 ;;
    --skip-frontend) SKIP_FRONTEND=1; shift ;;
    --skip-tests) SKIP_TESTS=1; shift ;;
    --help|-h)
      sed -n '1,20p' "$0"
      exit 0
      ;;
    *) die "unknown arg: $1" ;;
  esac
done

[[ -n "$VERSION" ]] || die "--version is required (e.g. 1.2.3)"
VERSION="${VERSION#v}"

command -v go >/dev/null || die "go not found"
if [[ "$SKIP_FRONTEND" != "1" ]]; then
  command -v npm >/dev/null || die "npm not found"
fi

log "Packaging ppt-gen v${VERSION} (commit ${COMMIT})"
mkdir -p "$OUTDIR"
rm -rf "${OUTDIR:?}/"*

# 1) Build/embed frontend once
if [[ "$SKIP_FRONTEND" != "1" ]]; then
  log "Building frontend + embed assets"
  SKIP_TESTS=1 SKIP_LINT="$SKIP_LINT" ./build.sh
else
  log "Reusing existing embed assets (SKIP_FRONTEND=1)"
  [[ -f internal/web/dist/index.html ]] || die "internal/web/dist missing; build frontend first"
fi

# 2) Backend tests once (optional)
if [[ "$SKIP_TESTS" != "1" ]]; then
  log "Running backend tests"
  go test ./cmd/server ./internal/...
else
  log "Skipping backend tests"
fi

# 3) Cross-compile binaries
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS=(
  "-s" "-w"
  "-X" "main.version=${VERSION}"
  "-X" "main.commit=${COMMIT}"
  "-X" "main.buildTime=${BUILD_TIME}"
)
# main package currently has no version vars; keep ldflags harmless.

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

for target in "${TARGETS[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  name="ppt-gen_${VERSION}_${goos}_${goarch}"
  bin_name="ppt-gen"
  [[ "$goos" == "windows" ]] && bin_name="ppt-gen.exe"

  log "Building ${goos}/${goarch}"
  out_bin="$tmpdir/$name/$bin_name"
  mkdir -p "$tmpdir/$name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "${LDFLAGS[*]}" -o "$out_bin" ./cmd/server

  # package files
  cp "$ROOT_DIR/scripts/deploy.sh" "$tmpdir/$name/deploy.sh"
  cp "$ROOT_DIR/scripts/upgrade.sh" "$tmpdir/$name/upgrade.sh"
  cp "$ROOT_DIR/deploy/ppt-gen.service" "$tmpdir/$name/ppt-gen.service"
  cp "$ROOT_DIR/deploy/env.example" "$tmpdir/$name/env.example"
  if [[ -f "$ROOT_DIR/config.yml" ]]; then
    cp "$ROOT_DIR/config.yml" "$tmpdir/$name/config.example.yml"
  else
    cat > "$tmpdir/$name/config.example.yml" <<'YML'
storage:
  kind: "json"
  path: "data/app.json"
  dsn: ""
YML
  fi
  cat > "$tmpdir/$name/README-RELEASE.txt" <<TXT
ppt-gen v${VERSION}
commit: ${COMMIT}
built:  ${BUILD_TIME}
target: ${goos}/${goarch}

Quick start:
  1) mkdir -p /opt/ppt-gen && cp -a . /opt/ppt-gen/
  2) cd /opt/ppt-gen && ./deploy.sh --port 18080
  3) open http://SERVER:18080

Upgrade:
  ./upgrade.sh --artifact /path/to/new/ppt-gen

Notes:
  - Binary is self-contained (frontend embedded)
  - Runtime data in ./data
  - officecli is required on host for PPTX export
TXT
  chmod +x "$tmpdir/$name/deploy.sh" "$tmpdir/$name/upgrade.sh" "$out_bin"

  archive="$OUTDIR/${name}.tar.gz"
  tar -C "$tmpdir" -czf "$archive" "$name"
  log "Wrote $archive"
done

# Also publish deploy scripts standalone for convenience
cp "$ROOT_DIR/scripts/deploy.sh" "$OUTDIR/deploy.sh"
cp "$ROOT_DIR/scripts/upgrade.sh" "$OUTDIR/upgrade.sh"
cp "$ROOT_DIR/deploy/ppt-gen.service" "$OUTDIR/ppt-gen.service"
cp "$ROOT_DIR/deploy/env.example" "$OUTDIR/env.example"
chmod +x "$OUTDIR/deploy.sh" "$OUTDIR/upgrade.sh"

log "Release artifacts in: $OUTDIR"
ls -lh "$OUTDIR"
