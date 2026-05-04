#!/usr/bin/env bash
set -euo pipefail

PLIST_ID="com.local.qwen35.9b.mlx"
PLIST_TARGET="$HOME/Library/LaunchAgents/${PLIST_ID}.plist"

LAUNCHD_DOMAIN="gui/$(id -u)"

launchctl bootout "$LAUNCHD_DOMAIN" "$PLIST_TARGET" >/dev/null 2>&1 || true
launchctl remove "$PLIST_ID" >/dev/null 2>&1 || true
rm -f "$PLIST_TARGET"

echo "launchd service removed: $PLIST_ID"
