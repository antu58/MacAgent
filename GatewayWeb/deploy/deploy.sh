#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"

if [[ ! -f "${ENV_FILE}" ]]; then
  cp "${SCRIPT_DIR}/.env.example" "${ENV_FILE}"
  echo "Created ${ENV_FILE} from .env.example"
fi

set -a
source "${ENV_FILE}"
set +a

echo "Building gateway-web binary..."
mkdir -p "${ROOT_DIR}/build"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 /opt/homebrew/bin/go build -o "${ROOT_DIR}/build/gateway-web" "${ROOT_DIR}/cmd/web"

docker compose \
  --env-file "${ENV_FILE}" \
  -f "${SCRIPT_DIR}/docker-compose.yml" \
  up -d --build

echo "GatewayWeb deployed."
echo "Open: http://127.0.0.1:${HOST_PORT:-19091}"
