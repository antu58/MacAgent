#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  echo "Error: Python runtime not found in $VENV_DIR. Run scripts/setup_mlx_service.sh first." >&2
  exit 1
fi

if [[ ! -f "$SCRIPT_DIR/run_vlm_server.py" ]]; then
  echo "Error: run_vlm_server.py not found in $SCRIPT_DIR." >&2
  exit 1
fi

mkdir -p \
  "$MLX_SERVICE_HOME" \
  "$HF_HOME" \
  "$HUGGINGFACE_HUB_CACHE" \
  "$TRANSFORMERS_CACHE" \
  "$XDG_CACHE_HOME" \
  "$VENV_DIR"

ARGS=(
  --host "$HOST"
  --port "$PORT"
)

if [[ "$TRUST_REMOTE_CODE" == "1" ]]; then
  ARGS+=(--trust-remote-code)
fi

echo "Starting MLX VLM OpenAI-compatible server"
echo "Endpoint: http://$HOST:$PORT/v1"
echo "Default model for requests: $MODEL_ID"
echo "Default context window: ${MAX_KV_SIZE} tokens"
echo "Cache root: $MLX_SERVICE_HOME"
echo "Venv: $VENV_DIR"

exec "$VENV_DIR/bin/python" "$SCRIPT_DIR/run_vlm_server.py" "${ARGS[@]}"
