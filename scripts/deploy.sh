#!/usr/bin/env bash
# deploy.sh — first-time deploy for ppt-gen single binary
#
# Usage:
#   ./deploy.sh
#   ./deploy.sh --port 18080 --dir /opt/ppt-gen
#   ./deploy.sh --bin ./ppt-gen --user pptgen --systemd
#   ./deploy.sh --store sqlite --data data/app.db
#
# Environment overrides:
#   APP_DIR, APP_USER, APP_PORT, APP_BIN, SYSTEMD=1, DEBUG=1
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/ppt-gen}"
APP_USER="${APP_USER:-pptgen}"
APP_PORT="${APP_PORT:-18080}"
APP_BIN="${APP_BIN:-}"
SYSTEMD="${SYSTEMD:-1}"
DEBUG="${DEBUG:-0}"
STORE_KIND="${STORE_KIND:-}"
DATA_PATH="${DATA_PATH:-}"
DSN="${DSN:-}"
SERVICE_NAME="ppt-gen"
SERVICE_FILE="ppt-gen.service"

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    die "please run as root (or sudo) for system deploy"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) APP_DIR="${2:-}"; shift 2 ;;
    --user) APP_USER="${2:-}"; shift 2 ;;
    --port) APP_PORT="${2:-}"; shift 2 ;;
    --bin) APP_BIN="${2:-}"; shift 2 ;;
    --systemd) SYSTEMD=1; shift ;;
    --no-systemd) SYSTEMD=0; shift ;;
    --debug) DEBUG=1; shift ;;
    --store) STORE_KIND="${2:-}"; shift 2 ;;
    --data) DATA_PATH="${2:-}"; shift 2 ;;
    --dsn) DSN="${2:-}"; shift 2 ;;
    --help|-h)
      sed -n '1,30p' "$0"
      exit 0
      ;;
    *) die "unknown arg: $1" ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CANDIDATES=()
[[ -n "$APP_BIN" ]] && CANDIDATES+=("$APP_BIN")
CANDIDATES+=("$SCRIPT_DIR/ppt-gen" "$SCRIPT_DIR/../bin/ppt-gen" "./ppt-gen" "./bin/ppt-gen")
BIN_SRC=""
for c in "${CANDIDATES[@]}"; do
  if [[ -f "$c" ]]; then
    BIN_SRC="$(cd "$(dirname "$c")" && pwd)/$(basename "$c")"
    break
  fi
done
[[ -n "$BIN_SRC" ]] || die "ppt-gen binary not found; pass --bin /path/to/ppt-gen"

need_root

log "Deploying ppt-gen"
log "  dir : $APP_DIR"
log "  user: $APP_USER"
log "  port: $APP_PORT"
log "  bin : $BIN_SRC"

if ! id "$APP_USER" >/dev/null 2>&1; then
  log "Creating user $APP_USER"
  useradd --system --home "$APP_DIR" --shell /usr/sbin/nologin "$APP_USER" 2>/dev/null \
    || useradd --system --home "$APP_DIR" --shell /sbin/nologin "$APP_USER"
fi

mkdir -p "$APP_DIR" "$APP_DIR/data" "$APP_DIR/data/uploads" "$APP_DIR/data/export-debug" "$APP_DIR/logs" "$APP_DIR/backups"
install -m 0755 "$BIN_SRC" "$APP_DIR/ppt-gen"

if [[ ! -f "$APP_DIR/config.yml" ]]; then
  if [[ -f "$SCRIPT_DIR/config.example.yml" ]]; then
    install -m 0644 "$SCRIPT_DIR/config.example.yml" "$APP_DIR/config.yml"
  elif [[ -f "$SCRIPT_DIR/../config.yml" ]]; then
    install -m 0644 "$SCRIPT_DIR/../config.yml" "$APP_DIR/config.yml"
  else
    printf '%s\n' \
      'storage:' \
      '  kind: "json"' \
      '  path: "data/app.json"' \
      '  dsn: ""' > "$APP_DIR/config.yml"
    chmod 0644 "$APP_DIR/config.yml"
  fi
fi

ENV_FILE="$APP_DIR/env"
if [[ ! -f "$ENV_FILE" ]]; then
  {
    echo "PORT=${APP_PORT}"
    echo "DEBUG=${DEBUG}"
    echo "# Optional overrides:"
    echo "# ADDR=:${APP_PORT}"
    echo "# STORE=json"
    echo "# DATA=data/app.json"
    echo "# DSN="
  } > "$ENV_FILE"
  chmod 0640 "$ENV_FILE"
fi

# helper runner used by systemd and manual start
{
  echo '#!/usr/bin/env bash'
  echo 'set -euo pipefail'
  echo "cd \"$APP_DIR\""
  echo 'set -a'
  echo "# shellcheck disable=SC1091"
  echo "source \"$ENV_FILE\""
  echo 'set +a'
  echo "ARGS=(--port \"\${PORT:-$APP_PORT}\" --storage-config \"$APP_DIR/config.yml\")"
  echo 'if [[ "${DEBUG:-0}" == "1" || "${DEBUG:-false}" == "true" ]]; then'
  echo '  ARGS+=(--debug)'
  echo 'fi'
  echo 'if [[ -n "${STORE:-}" ]]; then ARGS+=(--store "$STORE"); fi'
  echo 'if [[ -n "${DATA:-}" ]]; then ARGS+=(--data "$DATA"); fi'
  echo 'if [[ -n "${DSN:-}" ]]; then ARGS+=(--dsn "$DSN"); fi'
  echo "exec \"$APP_DIR/ppt-gen\" \"\${ARGS[@]}\""
} > "$APP_DIR/run.sh"
chmod 0755 "$APP_DIR/run.sh"

set_env_key() {
  local key="$1" value="$2"
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  else
    echo "${key}=${value}" >> "$ENV_FILE"
  fi
}

[[ -n "$STORE_KIND" ]] && set_env_key STORE "$STORE_KIND"
[[ -n "$DATA_PATH" ]] && set_env_key DATA "$DATA_PATH"
[[ -n "$DSN" ]] && set_env_key DSN "$DSN"
set_env_key PORT "$APP_PORT"
set_env_key DEBUG "$DEBUG"

chown -R "$APP_USER:$APP_USER" "$APP_DIR"

if ! command -v officecli >/dev/null 2>&1; then
  warn "officecli not found in PATH. PPTX export will fail until installed."
fi

if [[ "$SYSTEMD" == "1" ]]; then
  if ! command -v systemctl >/dev/null 2>&1; then
    warn "systemctl not found; skipping systemd install. Use: $APP_DIR/run.sh"
  else
    UNIT_SRC=""
    for c in "$SCRIPT_DIR/$SERVICE_FILE" "$SCRIPT_DIR/../deploy/$SERVICE_FILE" "./$SERVICE_FILE"; do
      if [[ -f "$c" ]]; then UNIT_SRC="$c"; break; fi
    done
    [[ -n "$UNIT_SRC" ]] || die "missing $SERVICE_FILE"

    tmp_unit="$(mktemp)"
    sed \
      -e "s|__APP_DIR__|$APP_DIR|g" \
      -e "s|__APP_USER__|$APP_USER|g" \
      -e "s|__APP_PORT__|$APP_PORT|g" \
      "$UNIT_SRC" > "$tmp_unit"
    install -m 0644 "$tmp_unit" "/etc/systemd/system/${SERVICE_NAME}.service"
    rm -f "$tmp_unit"

    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    systemctl restart "$SERVICE_NAME"
    sleep 1
    systemctl --no-pager --full status "$SERVICE_NAME" || true
    log "Deployed via systemd: systemctl status ${SERVICE_NAME}"
  fi
else
  log "Systemd disabled. Start manually:"
  log "  sudo -u $APP_USER $APP_DIR/run.sh"
fi

log "Done. Open: http://<server>:${APP_PORT}"
