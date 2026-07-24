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

# Pre-pull the Neo4j image once and load it into the node, so the FIRST Neo4j pod does not
# pay a cold Docker Hub pull (often rate-limited on CI runners) that can exceed the Ready
# wait. The image tag is not "latest", so pods use imagePullPolicy=IfNotPresent and reuse
# the cached node image. Best-effort: on failure, pods fall back to pulling on demand.
NEO4J_VERSION="${NEO4J_VERSION:-2026.05.0}"
NEO4J_EDITION="${NEO4J_EDITION:-enterprise}"
if [[ "${NEO4J_EDITION}" == "enterprise" ]]; then
  NEO4J_IMAGE="neo4j:${NEO4J_VERSION}-enterprise"
else
  NEO4J_IMAGE="neo4j:${NEO4J_VERSION}"
fi
log "Pre-loading Neo4j image ${NEO4J_IMAGE} into kind (avoids per-pod Docker Hub pulls)"
if docker pull "${NEO4J_IMAGE}"; then
  kind load docker-image "${NEO4J_IMAGE}" --name "${KIND_CLUSTER_NAME}"
else
  log "WARN: could not pre-pull ${NEO4J_IMAGE}; Neo4j pods will pull it on demand"
fi

log "kind cluster ready"
