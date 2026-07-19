#!/usr/bin/env bash
# upgrade.sh — rolling upgrade for ppt-gen
#
# Usage:
#   ./upgrade.sh --artifact /path/to/ppt-gen
#   ./upgrade.sh --artifact /path/to/ppt-gen_1.2.3_linux_amd64.tar.gz
#   ./upgrade.sh --dir /opt/ppt-gen --artifact ./ppt-gen --no-restart
#   ./upgrade.sh --artifact ./ppt-gen --rollback
#
# Behavior:
#   1) stop service (optional)
#   2) backup current binary (+ optional data snapshot)
#   3) install new binary
#   4) restart + health check
#   5) rollback automatically if health check fails
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/ppt-gen}"
SERVICE_NAME="${SERVICE_NAME:-ppt-gen}"
ARTIFACT=""
RESTART=1
BACKUP_DATA=0
ROLLBACK_ONLY=0
HEALTH_URL=""
HEALTH_TIMEOUT=30

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_root() {
  [[ "$(id -u)" -eq 0 ]] || die "please run as root (or sudo)"
}

usage() { sed -n '1,35p' "$0"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) APP_DIR="${2:-}"; shift 2 ;;
    --artifact) ARTIFACT="${2:-}"; shift 2 ;;
    --service) SERVICE_NAME="${2:-}"; shift 2 ;;
    --no-restart) RESTART=0; shift ;;
    --backup-data) BACKUP_DATA=1; shift ;;
    --rollback) ROLLBACK_ONLY=1; shift ;;
    --health-url) HEALTH_URL="${2:-}"; shift 2 ;;
    --health-timeout) HEALTH_TIMEOUT="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) die "unknown arg: $1" ;;
  esac
done

need_root
[[ -d "$APP_DIR" ]] || die "app dir not found: $APP_DIR"
BIN_PATH="$APP_DIR/ppt-gen"
BACKUP_DIR="$APP_DIR/backups"
mkdir -p "$BACKUP_DIR"

has_systemd=0
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q "^${SERVICE_NAME}\\.service"; then
  has_systemd=1
fi

service_stop() {
  if [[ "$has_systemd" == "1" ]]; then
    log "Stopping ${SERVICE_NAME}"
    systemctl stop "$SERVICE_NAME" || true
  fi
}

service_start() {
  if [[ "$has_systemd" == "1" ]]; then
    log "Starting ${SERVICE_NAME}"
    systemctl start "$SERVICE_NAME"
  elif [[ -x "$APP_DIR/run.sh" ]]; then
    warn "no systemd unit; please start manually: $APP_DIR/run.sh"
  fi
}

service_restart() {
  if [[ "$has_systemd" == "1" ]]; then
    log "Restarting ${SERVICE_NAME}"
    systemctl restart "$SERVICE_NAME"
  else
    service_start
  fi
}

health_check() {
  local url="$1"
  local timeout="$2"
  local i
  for ((i=1; i<=timeout; i++)); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      log "Health check passed: $url"
      return 0
    fi
    sleep 1
  done
  return 1
}

do_rollback() {
  local latest
  latest="$(ls -1t "$BACKUP_DIR"/ppt-gen.bak.* 2>/dev/null | head -n1 || true)"
  [[ -n "$latest" ]] || die "no backup binary found in $BACKUP_DIR"
  log "Rolling back to $latest"
  service_stop
  install -m 0755 "$latest" "$BIN_PATH"
  if id pptgen >/dev/null 2>&1; then
    chown pptgen:pptgen "$BIN_PATH" || true
  fi
  service_start
  log "Rollback complete"
}

if [[ "$ROLLBACK_ONLY" == "1" ]]; then
  do_rollback
  exit 0
fi

[[ -n "$ARTIFACT" ]] || die "--artifact is required"
[[ -e "$ARTIFACT" ]] || die "artifact not found: $ARTIFACT"

TMP_EXTRACT=""
cleanup() { [[ -n "$TMP_EXTRACT" && -d "$TMP_EXTRACT" ]] && rm -rf "$TMP_EXTRACT"; }
trap cleanup EXIT

NEW_BIN=""
if [[ -f "$ARTIFACT" && "$ARTIFACT" == *.tar.gz ]]; then
  log "Extracting archive $ARTIFACT"
  TMP_EXTRACT="$(mktemp -d)"
  tar -xzf "$ARTIFACT" -C "$TMP_EXTRACT"
  NEW_BIN="$(find "$TMP_EXTRACT" -type f -name 'ppt-gen' | head -n1 || true)"
  [[ -n "$NEW_BIN" ]] || die "ppt-gen binary not found inside archive"
elif [[ -f "$ARTIFACT" ]]; then
  NEW_BIN="$ARTIFACT"
else
  die "unsupported artifact: $ARTIFACT"
fi
[[ -f "$NEW_BIN" ]] || die "invalid binary: $NEW_BIN"
chmod +x "$NEW_BIN" || true

stamp="$(date +%Y%m%d-%H%M%S)"
if [[ -f "$BIN_PATH" ]]; then
  bak="$BACKUP_DIR/ppt-gen.bak.$stamp"
  log "Backing up current binary -> $bak"
  cp -a "$BIN_PATH" "$bak"
fi

if [[ "$BACKUP_DATA" == "1" && -d "$APP_DIR/data" ]]; then
  data_bak="$BACKUP_DIR/data-$stamp.tgz"
  log "Backing up data -> $data_bak"
  if [[ -f "$APP_DIR/config.yml" || -f "$APP_DIR/env" ]]; then
    tar -czf "$data_bak" -C "$APP_DIR" data config.yml env 2>/dev/null || tar -czf "$data_bak" -C "$APP_DIR" data
  else
    tar -czf "$data_bak" -C "$APP_DIR" data
  fi
fi

log "Installing new binary"
if [[ "$RESTART" == "1" ]]; then
  service_stop
fi
install -m 0755 "$NEW_BIN" "$BIN_PATH"
owner="$(stat -c '%U:%G' "$APP_DIR" 2>/dev/null || true)"
if [[ -n "$owner" ]]; then
  chown "$owner" "$BIN_PATH" || true
fi

if [[ "$RESTART" == "1" ]]; then
  service_restart
  if [[ -z "$HEALTH_URL" ]]; then
    port="18080"
    if [[ -f "$APP_DIR/env" ]]; then
      port="$(grep -E '^PORT=' "$APP_DIR/env" | tail -n1 | cut -d= -f2 | tr -d '"' || true)"
      [[ -n "$port" ]] || port="18080"
    fi
    HEALTH_URL="http://127.0.0.1:${port}/"
  fi
  if ! health_check "$HEALTH_URL" "$HEALTH_TIMEOUT"; then
    warn "Health check failed; rolling back"
    do_rollback
    if ! health_check "$HEALTH_URL" "$HEALTH_TIMEOUT"; then
      die "rollback health check also failed"
    fi
    die "upgrade failed and was rolled back"
  fi
fi

log "Upgrade succeeded"
if [[ "$has_systemd" == "1" ]]; then
  systemctl --no-pager --full status "$SERVICE_NAME" || true
fi
