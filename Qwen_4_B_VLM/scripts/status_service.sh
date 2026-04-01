#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
PID_FILE="$RUN_DIR/qwen35-4b-vlm.pid"
LOG_FILE="$RUN_DIR/qwen35-4b-vlm.log"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

is_our_vlm_process() {
  local cmd="$1"
  [[ "$cmd" == *"mlx_vlm.server"* ]] || \
  [[ "$cmd" == *"Qwen_4_B_VLM/scripts/run_vlm_server.py"* ]] || \
  [[ "$cmd" == *"/scripts/run_vlm_server.py"* ]]
}

resolve_service_pid() {
  if ! command -v lsof >/dev/null 2>&1; then
    return 1
  fi

  local pids pid cmd
  pids="$(lsof -t -iTCP:"$PORT" -sTCP:LISTEN -n -P 2>/dev/null | sort -u || true)"
  for pid in $pids; do
    cmd="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    if is_our_vlm_process "$cmd"; then
      printf '%s\n' "$pid"
      return 0
    fi
  done
  return 1
}

if [[ ! -f "$PID_FILE" ]]; then
  PID="$(resolve_service_pid || true)"
  if [[ -n "${PID:-}" ]] && kill -0 "$PID" >/dev/null 2>&1; then
    echo "$PID" >"$PID_FILE"
    echo "Service status: running"
    echo "PID: $PID"
    echo "Endpoint: http://127.0.0.1:$PORT/v1"
    echo "Log: $LOG_FILE"
    exit 0
  fi
  echo "Service status: stopped"
  echo "PID file: $PID_FILE (not found)"
  exit 0
fi

PID="$(cat "$PID_FILE" || true)"
if [[ -n "${PID:-}" ]] && kill -0 "$PID" >/dev/null 2>&1; then
  echo "Service status: running"
  echo "PID: $PID"
  echo "Endpoint: http://127.0.0.1:$PORT/v1"
  echo "Log: $LOG_FILE"
else
  PID="$(resolve_service_pid || true)"
  if [[ -n "${PID:-}" ]] && kill -0 "$PID" >/dev/null 2>&1; then
    echo "$PID" >"$PID_FILE"
    echo "Service status: running"
    echo "PID: $PID"
    echo "Endpoint: http://127.0.0.1:$PORT/v1"
    echo "Log: $LOG_FILE"
    exit 0
  fi
  echo "Service status: stopped (stale PID file)"
  echo "PID file: $PID_FILE"
  exit 1
fi
