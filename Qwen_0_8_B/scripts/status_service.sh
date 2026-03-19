#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
PID_FILE="$RUN_DIR/qwen35-mlx.pid"
LOG_FILE="$RUN_DIR/qwen35-mlx.log"

if [[ ! -f "$PID_FILE" ]]; then
  echo "Service status: stopped"
  echo "PID file: $PID_FILE (not found)"
  exit 0
fi

PID="$(cat "$PID_FILE" || true)"
if [[ -n "${PID:-}" ]] && kill -0 "$PID" >/dev/null 2>&1; then
  echo "Service status: running"
  echo "PID: $PID"
  echo "Endpoint: http://127.0.0.1:18080/v1"
  echo "Log: $LOG_FILE"
else
  echo "Service status: stopped (stale PID file)"
  echo "PID file: $PID_FILE"
  exit 1
fi
