#!/usr/bin/env bash
# Process controller for the Blog Service. This intentionally does not use
# systemd; Jenkins deploys the binary and invokes this script over SSH.
set -Eeuo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
APP_DIR=${APP_DIR:-$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)}
BIN=${BIN:-$APP_DIR/current/blog-service}
ENV_FILE=${ENV_FILE:-$APP_DIR/shared/blog.env}
LOGFILE=${LOGFILE:-$APP_DIR/logs/blog-service.log}
PIDFILE=${PIDFILE:-$APP_DIR/run/blog-service.pid}
START_TIMEOUT=${START_TIMEOUT:-60}
STOP_TIMEOUT=${STOP_TIMEOUT:-30}
POLL_INTERVAL=${POLL_INTERVAL:-1}

log() { printf '%s %s\n' "[$(date '+%Y-%m-%d %H:%M:%S')]" "$*"; }
die() { log "ERROR: $*" >&2; exit 1; }

load_env() {
  if [[ -f "$ENV_FILE" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
  else
    log "no env file at $ENV_FILE — relying on inherited environment"
  fi
}

health_url() {
  local addr=${BLOG_HTTP_ADDR:-:8080}
  local port=${addr##*:}
  printf 'http://127.0.0.1:%s/healthz' "$port"
}

running_pid() {
  [[ -f "$PIDFILE" ]] || return 1
  local pid
  pid=$(<"$PIDFILE")
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  kill -0 "$pid" 2>/dev/null && printf '%s' "$pid"
}

probe_health() {
  curl --fail --silent --show-error --max-time 5 "$(health_url)" >/dev/null 2>&1
}

cmd_start() {
  load_env
  if local pid; pid=$(running_pid); then
    log "already running (pid $pid)"
    return 0
  fi
  [[ -x "$BIN" ]] || die "binary not found or not executable: $BIN"
  mkdir -p "$(dirname -- "$LOGFILE")" "$(dirname -- "$PIDFILE")"
  log "starting $BIN"
  nohup "$BIN" >>"$LOGFILE" 2>&1 &
  local pid=$!
  printf '%s\n' "$pid" >"$PIDFILE"
  local waited=0
  while (( waited < START_TIMEOUT )); do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PIDFILE"
      tail -n 30 "$LOGFILE" >&2 || true
      die "server failed to start"
    fi
    if probe_health; then
      log "started (pid $pid)"
      return 0
    fi
    sleep "$POLL_INTERVAL"
    waited=$((waited + POLL_INTERVAL))
  done
  kill -TERM "$pid" 2>/dev/null || true
  rm -f "$PIDFILE"
  die "server did not become healthy in ${START_TIMEOUT}s"
}

cmd_stop() {
  local pid
  if ! pid=$(running_pid); then
    rm -f "$PIDFILE"
    log "not running"
    return 0
  fi
  log "stopping (pid $pid)"
  kill -TERM "$pid" 2>/dev/null || true
  local waited=0
  while (( waited < STOP_TIMEOUT )); do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PIDFILE"
      log "stopped"
      return 0
    fi
    sleep "$POLL_INTERVAL"
    waited=$((waited + POLL_INTERVAL))
  done
  kill -KILL "$pid" 2>/dev/null || true
  rm -f "$PIDFILE"
  log "stopped after SIGKILL"
}

cmd_status() {
  local pid
  if ! pid=$(running_pid); then
    log "stopped"
    return 3
  fi
  load_env
  if probe_health; then
    log "running (pid $pid) — healthy"
  else
    log "running (pid $pid) — unhealthy"
    return 1
  fi
}

case "${1:-}" in
  start) cmd_start ;;
  stop) cmd_stop ;;
  restart) cmd_stop; cmd_start ;;
  status) cmd_status ;;
  *) echo "Usage: $0 {start|stop|restart|status}" >&2; exit 2 ;;
esac
