#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
PID_FILE="$RUN_DIR/qwen35-mlx.pid"

if [[ ! -f "$PID_FILE" ]]; then
  echo "No PID file found. Service is likely not running."
  exit 0
fi

PID="$(cat "$PID_FILE" || true)"
if [[ -z "${PID:-}" ]]; then
  rm -f "$PID_FILE"
  echo "Empty PID file removed."
  exit 0
fi

if ! kill -0 "$PID" >/dev/null 2>&1; then
  rm -f "$PID_FILE"
  echo "Process $PID not running. Stale PID file removed."
  exit 0
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
