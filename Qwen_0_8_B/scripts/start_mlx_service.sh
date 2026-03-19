#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

if [[ ! -x "$VENV_DIR/bin/mlx_lm.server" ]]; then
  echo "Error: mlx_lm.server not found. Run scripts/setup_mlx_service.sh first." >&2
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
  --model "$MODEL_ID"
  --host "$HOST"
  --port "$PORT"
  --max-tokens "$MAX_TOKENS"
  --log-level "$LOG_LEVEL"
)

if [[ "$TRUST_REMOTE_CODE" == "1" ]]; then
  ARGS+=(--trust-remote-code)
fi

echo "Starting MLX OpenAI-compatible server"
echo "Model: $MODEL_ID"
echo "Endpoint: http://$HOST:$PORT/v1"
echo "Cache root: $MLX_SERVICE_HOME"
echo "Venv: $VENV_DIR"

exec "$VENV_DIR/bin/mlx_lm.server" "${ARGS[@]}"
