#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

ENDPOINT="${1:-http://127.0.0.1:18080/v1}"
PYTHON_BIN="${VENV_DIR}/bin/python"

if [[ ! -x "$PYTHON_BIN" ]]; then
  echo "Error: Python runtime not found at $PYTHON_BIN" >&2
  echo "Run ./scripts/setup_mlx_service.sh first." >&2
  exit 1
fi

exec "$PYTHON_BIN" "$SCRIPT_DIR/tool_call_benchmark.py" "$ENDPOINT"
