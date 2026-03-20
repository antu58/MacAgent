#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [[ ! -f "${ENV_FILE}" ]]; then
  cp "${SCRIPT_DIR}/.env.example" "${ENV_FILE}"
  echo "Created ${ENV_FILE} from .env.example"
  echo "Please edit DEEP_MODEL_API_KEY in ${ENV_FILE} if needed, then rerun."
  exit 1
fi

set -a
source "${ENV_FILE}"
set +a

if [[ "${ENABLE_PROXY:-1}" == "1" ]]; then
  PROXY_URL="${PROXY_URL:-http://host.docker.internal:7897}"
  export HTTP_PROXY="${HTTP_PROXY:-${PROXY_URL}}"
  export HTTPS_PROXY="${HTTPS_PROXY:-${PROXY_URL}}"
  export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,host.docker.internal}"
  export http_proxy="${HTTP_PROXY}"
  export https_proxy="${HTTPS_PROXY}"
  export no_proxy="${NO_PROXY}"
  echo "Proxy enabled: ${PROXY_URL}"
else
  echo "Proxy disabled."
fi

if [[ -z "${DEEP_MODEL_API_KEY:-}" ]]; then
  echo "DEEP_MODEL_API_KEY is empty, please set it in ${ENV_FILE}"
  exit 1
fi

export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1

echo "Building linux/amd64 gateway binary with local Go..."
mkdir -p "${ROOT_DIR}/build"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 /opt/homebrew/bin/go build -o "${ROOT_DIR}/build/gateway" "${ROOT_DIR}/cmd/server"

docker compose \
  --env-file "${ENV_FILE}" \
  -f "${SCRIPT_DIR}/docker-compose.yml" \
  up -d --build

echo "Gateway deployed."
echo "Health check: curl http://127.0.0.1:${HOST_PORT:-18081}/healthz"
