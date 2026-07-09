#!/usr/bin/env bash
# Prepare a local kind cluster for e2e (CI and laptop).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"
# shellcheck source=../config/reconcile.sh
source "${REPO_ROOT}/tests/config/reconcile.sh"
load_cloud_config local-kind

require_cmd kind docker kubectl

if ! kind get clusters 2>/dev/null | grep -qx "${KIND_CLUSTER_NAME}"; then
  log "Creating kind cluster ${KIND_CLUSTER_NAME}"
  kind create cluster --name "${KIND_CLUSTER_NAME}"
else
  log "Reusing kind cluster ${KIND_CLUSTER_NAME}"
fi

kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}"

cd "${REPO_ROOT}"
docker build -t "${OPERATOR_IMAGE}" .
kind load docker-image "${OPERATOR_IMAGE}" --name "${KIND_CLUSTER_NAME}"

log "kind cluster ready"
