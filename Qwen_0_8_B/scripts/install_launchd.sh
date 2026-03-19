#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/common.env"

PLIST_ID="com.local.qwen35.mlx"
PLIST_TARGET="$HOME/Library/LaunchAgents/${PLIST_ID}.plist"

if [[ ! -x "$VENV_DIR/bin/mlx_lm.server" ]]; then
  echo "Error: $VENV_DIR/bin/mlx_lm.server not found. Run scripts/setup_mlx_service.sh first." >&2
  exit 1
fi

mkdir -p "$HOME/Library/LaunchAgents"

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
  </dict>

  <key>ProgramArguments</key>
  <array>
    <string>${VENV_DIR}/bin/mlx_lm.server</string>
    <string>--model</string>
    <string>${MODEL_ID}</string>
    <string>--host</string>
    <string>${HOST}</string>
    <string>--port</string>
    <string>${PORT}</string>
    <string>--max-tokens</string>
    <string>${MAX_TOKENS}</string>
    <string>--log-level</string>
    <string>${LOG_LEVEL}</string>
EOF

if [[ "$TRUST_REMOTE_CODE" == "1" ]]; then
cat >> "$PLIST_TARGET" <<EOF
    <string>--trust-remote-code</string>
EOF
fi

cat >> "$PLIST_TARGET" <<EOF
  </array>

  <key>WorkingDirectory</key>
  <string>${MLX_SERVICE_HOME}</string>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>/tmp/qwen35-mlx.out.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/qwen35-mlx.err.log</string>
</dict>
</plist>
EOF

launchctl unload "$PLIST_TARGET" >/dev/null 2>&1 || true
launchctl load "$PLIST_TARGET"
launchctl start "$PLIST_ID"

echo "launchd service installed: $PLIST_ID"
echo "plist: $PLIST_TARGET"
echo "status: launchctl list | grep $PLIST_ID"
