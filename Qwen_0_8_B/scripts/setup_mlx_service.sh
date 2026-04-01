#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "Error: MLX native service requires macOS (Darwin)." >&2
  exit 1
fi

if [[ "$(uname -m)" != "arm64" ]]; then
  echo "Warning: Apple Silicon (arm64) is recommended for MLX performance." >&2
fi

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

pick_python() {
  if command -v python3.12 >/dev/null 2>&1; then
    echo "python3.12"
    return 0
  fi
  if command -v python3.11 >/dev/null 2>&1; then
    echo "python3.11"
    return 0
  fi
  if [[ -x "/opt/homebrew/bin/python3.12" ]]; then
    echo "/opt/homebrew/bin/python3.12"
    return 0
  fi
  if [[ -x "/opt/homebrew/bin/python3.11" ]]; then
    echo "/opt/homebrew/bin/python3.11"
    return 0
  fi
  if command -v python3 >/dev/null 2>&1; then
    echo "python3"
    return 0
  fi
  return 1
}

PYTHON_BIN="$(pick_python || true)"
if [[ -z "$PYTHON_BIN" ]]; then
  echo "Error: No python3 found. Install python@3.11 first." >&2
  exit 1
fi

PYTHON_MAJOR_MINOR="$("$PYTHON_BIN" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"
PYTHON_MAJOR="${PYTHON_MAJOR_MINOR%%.*}"
PYTHON_MINOR="${PYTHON_MAJOR_MINOR##*.}"
if (( PYTHON_MAJOR < 3 || (PYTHON_MAJOR == 3 && PYTHON_MINOR < 11) )); then
  echo "Error: $PYTHON_BIN is Python $PYTHON_MAJOR_MINOR. Python >= 3.11 is required for Qwen3.5 multimodal support in mlx-vlm." >&2
  exit 1
fi

mkdir -p \
  "$MLX_SERVICE_HOME" \
  "$HF_HOME" \
  "$HUGGINGFACE_HUB_CACHE" \
  "$TRANSFORMERS_CACHE" \
  "$XDG_CACHE_HOME"

if [[ -x "$VENV_DIR/bin/python" ]]; then
  VENV_VER="$("$VENV_DIR/bin/python" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"
  VENV_MAJOR="${VENV_VER%%.*}"
  VENV_MINOR="${VENV_VER##*.}"
  if (( VENV_MAJOR < 3 || (VENV_MAJOR == 3 && VENV_MINOR < 11) )); then
    echo "Existing VENV uses Python $VENV_VER, recreating with $PYTHON_BIN ..."
    rm -rf "$VENV_DIR"
  fi
fi

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  rm -rf "$VENV_DIR"
  "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

source "$VENV_DIR/bin/activate"
python -m pip install --upgrade pip wheel
python -m pip install --upgrade "mlx-vlm[torch]" huggingface_hub

# Pre-download model weights to persistent cache to avoid repeated downloads.
python - <<'PY'
import os
from huggingface_hub import snapshot_download

model_id = os.environ["MODEL_ID"]
cache_dir = os.environ["HUGGINGFACE_HUB_CACHE"]

print(f"Downloading (or reusing cache): {model_id}")
path = snapshot_download(repo_id=model_id, cache_dir=cache_dir)
print(f"Model cache ready at: {path}")
PY

cat <<EOF
Setup complete.
Model: $MODEL_ID
Persistent cache root: $MLX_SERVICE_HOME
Start service with:
  $ROOT_DIR/scripts/start_mlx_service.sh
EOF
