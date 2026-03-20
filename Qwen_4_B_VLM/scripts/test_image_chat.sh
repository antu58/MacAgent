#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

ENDPOINT="${1:-http://127.0.0.1:${PORT}/v1/chat/completions}"
IMAGE_URL="${2:-https://images.cocodataset.org/val2017/000000039769.jpg}"

PYTHON_BIN="${VENV_DIR}/bin/python"
if [[ ! -x "$PYTHON_BIN" ]]; then
  echo "Error: Python runtime not found at $PYTHON_BIN" >&2
  echo "Run ./scripts/setup_mlx_service.sh first." >&2
  exit 1
fi

exec "$PYTHON_BIN" - <<'PY' "$ENDPOINT" "$MODEL_ID" "$IMAGE_URL"
import json
import sys
import urllib.request

endpoint = sys.argv[1]
model_id = sys.argv[2]
image_url = sys.argv[3]

payload = {
    "model": model_id,
    "messages": [
        {
            "role": "user",
            "content": [
                {"type": "text", "text": "请用中文简要描述这张图里有什么。"},
                {"type": "image_url", "image_url": {"url": image_url}},
            ],
        }
    ],
    "max_tokens": 128,
}

data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
req = urllib.request.Request(
    endpoint,
    data=data,
    headers={"Content-Type": "application/json"},
    method="POST",
)

with urllib.request.urlopen(req, timeout=180) as resp:
    print(resp.read().decode("utf-8", errors="replace"))
PY
