#!/usr/bin/env bash
# assert/connectivity — probe Neo4j connectors from the Neo4j pod itself (localhost).
# Validates the connector matrix (bolt/neo4j/http succeed, https fails without TLS).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

POD="${NEO4J_STS_NAME}-0"

kubectl get pod "${POD}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "Neo4j pod ${POD} not found — connectivity needs a running Neo4j (E2E_ASSERT_NEO4J_READY=true)"

# Run each probe snippet inside the Neo4j container over its localhost interface.
conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

conn_assert_matrix "localhost" "from-pod"
