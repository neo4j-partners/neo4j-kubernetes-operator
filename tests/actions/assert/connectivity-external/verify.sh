#!/usr/bin/env bash
# assert/connectivity-external — probe Neo4j connectors from a separate client pod,
# reaching Neo4j through the client Service DNS. The client pod reuses the Neo4j
# image (bash + cypher-shell already present and cached on the node).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

SERVER_POD="${NEO4J_STS_NAME}-0"
CLIENT_POD="conn-client-${SUITE_CASE_ID:-case}"

cleanup_client_pod() {
  kubectl delete pod "${CLIENT_POD}" -n "${NEO4J_NAMESPACE}" \
    --ignore-not-found --wait=false >/dev/null 2>&1 || true
}
trap cleanup_client_pod EXIT

image="$(kubectl get pod "${SERVER_POD}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.containers[?(@.name=="neo4j")].image}' 2>/dev/null)"
[[ -n "${image}" ]] || die "could not resolve Neo4j image from pod ${SERVER_POD}"

log "Launching client pod ${CLIENT_POD} (image ${image})"
cleanup_client_pod
kubectl run "${CLIENT_POD}" -n "${NEO4J_NAMESPACE}" \
  --image="${image}" --image-pull-policy=IfNotPresent --restart=Never \
  --command -- sleep 3600 >/dev/null

if ! kubectl wait --for=condition=Ready "pod/${CLIENT_POD}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1; then
  kubectl describe pod "${CLIENT_POD}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "client pod ${CLIENT_POD} did not become Ready"
fi

# Run each probe snippet inside the client pod, targeting the client Service DNS.
conn_exec_clientpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${CLIENT_POD}" -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_clientpod

conn_assert_matrix "${NEO4J_CLIENT_SVC}" "from-client-pod"
