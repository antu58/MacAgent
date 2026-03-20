#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"

docker compose \
  --env-file "${ENV_FILE}" \
  -f "${SCRIPT_DIR}/docker-compose.yml" \
  down
