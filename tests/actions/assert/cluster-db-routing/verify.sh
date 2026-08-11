#!/usr/bin/env bash
# assert/cluster-db-routing — the default database must be reachable from every member
# through the routing scheme, including from members that do not host it.
#
# When topology.defaultPrimariesCount is lower than primaries.members, the default
# database is allocated on a subset of the primaries. A direct bolt:// session opened on
# a member that does not host it is refused: that is expected Neo4j behaviour, not a
# failure, so it is only reported. What must always hold is that neo4j:// through the
# client Service routes the session to a member that does host the database.
#
# This is the trap that made assert/cluster-formed report a phantom "cluster not formed":
# it queried a healthy DBMS over a direct bolt session bound to the default database.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_CLIENT_SVC,
#         NEO4J_AUTH_SECRET, CLUSTER_EXPECTED_MEMBERS
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

MEMBERS="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set — cluster cases must declare it}"
HOST="${NEO4J_CLIENT_SVC}.${NEO4J_NAMESPACE}.svc"

password="$(neo4j_password)"

# conn_probe runs its snippet through CONN_EXEC_FN; point it at the pod under test so
# both schemes are probed from the same member.
PROBE_POD=""
_exec_in_probe_pod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${PROBE_POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=_exec_in_probe_pod

log "Probing bolt:// (local, may be refused) vs neo4j:// (routed, must work) from ${MEMBERS} member(s)"

refused=0
for ((i = 0; i < MEMBERS; i++)); do
  PROBE_POD="${NEO4J_STS_NAME}-${i}"
  if conn_probe bolt localhost "${password}" >/dev/null 2>&1; then
    log "[${PROBE_POD}] direct bolt:// to the default database works — this member hosts it"
  else
    refused=$((refused + 1))
    log "[${PROBE_POD}] direct bolt:// refused — this member does not host the default database (allowed)"
  fi
  # Same pod, routing scheme: this one is not allowed to fail.
  conn_assert_one neo4j success "${HOST}" "${password}" "${PROBE_POD} via neo4j://"
done

if [[ "${refused}" -gt 0 ]]; then
  log "Routing reached the default database from ${refused}/${MEMBERS} member(s) that do not host it"
else
  log "Every member hosts the default database; routing verified from all ${MEMBERS}"
fi
