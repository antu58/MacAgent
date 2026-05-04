#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
PID_FILE="$RUN_DIR/qwen35-4b-vlm.pid"
LOG_FILE="$RUN_DIR/qwen35-4b-vlm.log"

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

echo "Starting service in background..."
echo "Endpoint: http://127.0.0.1:$PORT/v1"
echo "Log: $LOG_FILE"

: >"$LOG_FILE"

(
  cd "$ROOT_DIR"
  exec nohup ./scripts/start_mlx_service.sh >"$LOG_FILE" 2>&1
) &
START_PID="$!"
disown "$START_PID" >/dev/null 2>&1 || true
echo "$START_PID" >"$PID_FILE"

for _ in {1..90}; do
  sleep 1

  ACTUAL_PID="$(resolve_service_pid || true)"
  if [[ -n "${ACTUAL_PID:-}" ]] && kill -0 "$ACTUAL_PID" >/dev/null 2>&1; then
    echo "$ACTUAL_PID" >"$PID_FILE"
    echo "Service started, PID=$ACTUAL_PID"
    echo "Endpoint: http://127.0.0.1:$PORT/v1"
    echo "Log: $LOG_FILE"
    exit 0
  fi

  if ! kill -0 "$START_PID" >/dev/null 2>&1; then
    rm -f "$PID_FILE"
    echo "Service failed to start. Last log lines:" >&2
    tail -n 80 "$LOG_FILE" >&2 || true
    exit 1
  fi
done

echo "Service process started, PID=$START_PID"
echo "Endpoint is not listening yet; model may still be loading."
echo "Run status: $SCRIPT_DIR/status_service.sh"
echo "Follow log: tail -f $LOG_FILE"
exit 0
