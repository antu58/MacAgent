#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
PID_FILE="$RUN_DIR/qwen35-mlx.pid"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

is_our_vlm_process() {
  local cmd="$1"
  [[ "$cmd" == *"mlx_vlm.server"* ]] || \
  [[ "$cmd" == *"mlx_lm.server"* ]] || \
  [[ "$cmd" == *"Qwen_0_8_B/scripts/run_vlm_server.py"* ]] || \
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

if [[ -f "$SCRIPT_DIR/uninstall_launchd.sh" ]]; then
  "$SCRIPT_DIR/uninstall_launchd.sh" >/dev/null 2>&1 || true
fi

if [[ ! -f "$PID_FILE" ]]; then
  PID="$(resolve_service_pid || true)"
  if [[ -z "${PID:-}" ]]; then
    echo "No PID file found. Service is likely not running."
    exit 0
  fi
else
  PID="$(cat "$PID_FILE" || true)"
fi

if [[ -z "${PID:-}" ]]; then
  rm -f "$PID_FILE"
  echo "Empty PID file removed."
  exit 0
fi

if ! kill -0 "$PID" >/dev/null 2>&1; then
  PID="$(resolve_service_pid || true)"
  if [[ -z "${PID:-}" ]]; then
    rm -f "$PID_FILE"
    echo "Process not running. Stale PID file removed."
    exit 0
  fi
fi

kill "$PID"

for _ in {1..20}; do
  if kill -0 "$PID" >/dev/null 2>&1; then
    sleep 0.5
  else
    rm -f "$PID_FILE"
    echo "Service stopped."
    exit 0
  fi
done

kill -9 "$PID" >/dev/null 2>&1 || true
rm -f "$PID_FILE"
echo "Service force stopped."
