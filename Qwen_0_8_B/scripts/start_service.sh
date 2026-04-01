#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
PID_FILE="$RUN_DIR/qwen35-mlx.pid"
LOG_FILE="$RUN_DIR/qwen35-mlx.log"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

stop_pid_gracefully() {
  local pid="$1"
  if [[ -z "${pid:-}" ]]; then
    return 0
  fi

  if ! kill -0 "$pid" >/dev/null 2>&1; then
    return 0
  fi

  kill "$pid" >/dev/null 2>&1 || true
  for _ in {1..20}; do
    if kill -0 "$pid" >/dev/null 2>&1; then
      sleep 0.5
    else
      return 0
    fi
  done

  kill -9 "$pid" >/dev/null 2>&1 || true
}

is_our_vlm_process() {
  local cmd="$1"
  [[ "$cmd" == *"mlx_vlm.server"* ]] || \
  [[ "$cmd" == *"mlx_lm.server"* ]] || \
  [[ "$cmd" == *"Qwen_0_8_B/scripts/run_vlm_server.py"* ]] || \
  [[ "$cmd" == *"/scripts/run_vlm_server.py"* ]]
}

mkdir -p "$RUN_DIR"

if [[ -f "$PID_FILE" ]]; then
  OLD_PID="$(cat "$PID_FILE" || true)"
  if [[ -n "${OLD_PID:-}" ]] && kill -0 "$OLD_PID" >/dev/null 2>&1; then
    echo "Found old service PID=$OLD_PID, stopping it first..."
    stop_pid_gracefully "$OLD_PID"
  fi
  rm -f "$PID_FILE"
fi

# Fallback: stop old MLX VLM server still occupying the target port.
if command -v lsof >/dev/null 2>&1; then
  PORT_PIDS="$(lsof -t -iTCP:"$PORT" -sTCP:LISTEN -n -P 2>/dev/null | sort -u || true)"
  if [[ -n "${PORT_PIDS:-}" ]]; then
    for pid in $PORT_PIDS; do
      cmd="$(ps -p "$pid" -o command= 2>/dev/null || true)"
      if is_our_vlm_process "$cmd"; then
        echo "Found old MLX VLM process on port $PORT (PID=$pid), stopping it..."
        stop_pid_gracefully "$pid"
      else
        echo "Port $PORT is occupied by non-MLX-VLM process (PID=$pid)." >&2
        echo "Please free the port or change PORT in scripts/common.env." >&2
        exit 1
      fi
    done
  fi
fi

nohup "$SCRIPT_DIR/start_mlx_service.sh" >"$LOG_FILE" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" >"$PID_FILE"

sleep 2
if kill -0 "$NEW_PID" >/dev/null 2>&1; then
  echo "Service started, PID=$NEW_PID"
  echo "Endpoint: http://127.0.0.1:$PORT/v1"
  echo "Log: $LOG_FILE"
else
  echo "Service failed to start. Check log: $LOG_FILE" >&2
  exit 1
fi
