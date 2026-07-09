#!/usr/bin/env bash
# Build and push the operator image to ACR (after ensure-aks.sh).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"

require_cmd az docker make

: "${AZURE_ACR_NAME:?AZURE_ACR_NAME required}"
: "${OPERATOR_IMAGE:?OPERATOR_IMAGE required}"

az acr login --name "${AZURE_ACR_NAME}"
cd "${REPO_ROOT}"
DOCKER_PLATFORM=linux/amd64 make docker-build IMG="${OPERATOR_IMAGE}"
docker push "${OPERATOR_IMAGE}"

log "Pushed ${OPERATOR_IMAGE}"
