#!/usr/bin/env bash
# assert/cluster-routing — AC-NEO-CLUSTER-003: cluster roles and routing are usable
# through the Neo4j driver. Connects over the *routing* scheme (neo4j://) via the
# client Service — the path a real application uses — and performs a write plus a
# read-back, which only succeeds if a leader exists and routing resolves to it.
#
# A write (not just RETURN 1) is what distinguishes a formed, writable cluster from
# one that merely accepts connections.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_CLIENT_SVC,
#         NEO4J_AUTH_SECRET, E2E_CLUSTER_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

POD="${NEO4J_STS_NAME}-0"
HOST="${NEO4J_CLIENT_SVC}.${NEO4J_NAMESPACE}.svc"
TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"
MARKER="e2e-routing-${SUITE_CASE_ID:-case}"

password="$(neo4j_password)"

# Run cypher from inside a member pod, but address the cluster through the client
# Service over neo4j:// so the driver performs real routing (not a direct bolt hop).
_cypher() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a 'neo4j://${HOST}:7687' -u neo4j -p '${password}' --format plain \"$1\"" 2>&1
}

# 1. Write through the routing driver — requires a leader and write routing.
log "Writing a node via neo4j://${HOST}:7687 (routing, requires leader)"
deadline=$((SECONDS + TIMEOUT_SECS))
wrote=0
out=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  if out="$(_cypher "CREATE (n:E2ERouting {id: '${MARKER}'}) RETURN n.id;")"; then
    wrote=1
    break
  fi
  sleep 10
done
[[ "${wrote}" -eq 1 ]] \
  || { printf '%s\n' "${out}" >&2; die "routed write failed within ${TIMEOUT_SECS}s — no leader or routing broken"; }
log "routed write succeeded"

# 2. Read it back through routing — proves the write committed and is visible.
out="$(_cypher "MATCH (n:E2ERouting {id: '${MARKER}'}) RETURN count(n) AS c;")" \
  || { printf '%s\n' "${out}" >&2; die "routed read failed"; }
grep -qE '(^|[^0-9])1([^0-9]|$)' <<<"${out}" \
  || { printf '%s\n' "${out}" >&2; die "read-back did not return the written node"; }
log "routed read-back returned the written node"

# 3. Clean up the marker node so re-runs stay idempotent.
_cypher "MATCH (n:E2ERouting {id: '${MARKER}'}) DELETE n;" >/dev/null || true

log "Cluster routing verified: write + read-back over neo4j:// (AC-NEO-CLUSTER-003)"
