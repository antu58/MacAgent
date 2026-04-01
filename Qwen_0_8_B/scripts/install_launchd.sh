#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_DIR="$ROOT_DIR/run"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

PLIST_ID="com.local.qwen35.mlx"
PLIST_TARGET="$HOME/Library/LaunchAgents/${PLIST_ID}.plist"
LOG_FILE="$RUN_DIR/qwen35-mlx.log"
ERR_FILE="$RUN_DIR/qwen35-mlx.err.log"

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  echo "Error: $VENV_DIR/bin/python not found. Run scripts/setup_mlx_service.sh first." >&2
  exit 1
fi

if [[ ! -f "$SCRIPT_DIR/run_vlm_server.py" ]]; then
  echo "Error: $SCRIPT_DIR/run_vlm_server.py not found." >&2
  exit 1
fi

mkdir -p "$HOME/Library/LaunchAgents"
mkdir -p "$RUN_DIR"

cat > "$PLIST_TARGET" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${PLIST_ID}</string>

  <key>EnvironmentVariables</key>
  <dict>
    <key>MLX_SERVICE_HOME</key>
    <string>${MLX_SERVICE_HOME}</string>
    <key>HF_HOME</key>
    <string>${HF_HOME}</string>
    <key>HUGGINGFACE_HUB_CACHE</key>
    <string>${HUGGINGFACE_HUB_CACHE}</string>
    <key>TRANSFORMERS_CACHE</key>
    <string>${TRANSFORMERS_CACHE}</string>
    <key>XDG_CACHE_HOME</key>
    <string>${XDG_CACHE_HOME}</string>
    <key>MAX_KV_SIZE</key>
    <string>${MAX_KV_SIZE}</string>
    <key>MAX_TOKENS</key>
    <string>${MAX_TOKENS}</string>
  </dict>

  <key>ProgramArguments</key>
  <array>
    <string>${VENV_DIR}/bin/python</string>
    <string>${SCRIPT_DIR}/run_vlm_server.py</string>
    <string>--host</string>
    <string>${HOST}</string>
    <string>--port</string>
    <string>${PORT}</string>
EOF

if [[ "$TRUST_REMOTE_CODE" == "1" ]]; then
cat >> "$PLIST_TARGET" <<EOF
    <string>--trust-remote-code</string>
EOF
fi

cat >> "$PLIST_TARGET" <<EOF
  </array>

  <key>WorkingDirectory</key>
  <string>${ROOT_DIR}</string>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>${LOG_FILE}</string>
  <key>StandardErrorPath</key>
  <string>${ERR_FILE}</string>
</dict>
</plist>
EOF

launchctl unload "$PLIST_TARGET" >/dev/null 2>&1 || true
launchctl load "$PLIST_TARGET"
launchctl start "$PLIST_ID" >/dev/null 2>&1 || true

echo "launchd service installed: $PLIST_ID"
echo "plist: $PLIST_TARGET"
echo "status: launchctl list | grep $PLIST_ID"
