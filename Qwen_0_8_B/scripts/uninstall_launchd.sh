#!/usr/bin/env bash
set -euo pipefail

PLIST_ID="com.local.qwen35.mlx"
PLIST_TARGET="$HOME/Library/LaunchAgents/${PLIST_ID}.plist"

launchctl stop "$PLIST_ID" >/dev/null 2>&1 || true
launchctl unload "$PLIST_TARGET" >/dev/null 2>&1 || true
rm -f "$PLIST_TARGET"

echo "launchd service removed: $PLIST_ID"
