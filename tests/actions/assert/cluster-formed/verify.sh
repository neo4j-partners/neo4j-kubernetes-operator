#!/usr/bin/env bash
# assert/cluster-formed — AC-NEO-CLUSTER-002: the cluster actually forms and reaches
# the expected minimum size. Pods being Running is NOT the same as a formed cluster,
# so this asks Neo4j itself via `SHOW SERVERS` on the system database and requires every
# expected member to be both Enabled (admitted to the cluster) and Available (serving).
#
# Also asserts the operator's own view agrees: the CR reports Ready=True with
# AllMembersReady, i.e. status is honest about formation.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET,
#         CLUSTER_EXPECTED_MEMBERS, E2E_CLUSTER_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

WANT="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"
# Cluster formation (discovery + quorum) is slower than a single-server boot.
TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"

# 1. The operator must report the cluster Ready — its own formation verdict.
log "Waiting up to ${TIMEOUT_SECS}s for ${NEO4J_RESOURCE} Ready (cluster formation)"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "${NEO4J_RESOURCE} did not reach Ready within ${TIMEOUT_SECS}s — cluster did not form"
fi

reason="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}' 2>/dev/null || true)"
message="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || true)"
log "operator reports Ready (reason=${reason:-?}, message=${message:-none})"

# 1b. ClusterFormed is the operator's dedicated formation verdict (set by
#     domain/formation): True/Formed once every desired server is enabled. Ready can
#     go True on member health alone, so assert the formation-specific condition too.
#     Reasons it may hold instead: WaitingQuorum, EnablingServer,
#     UnsupportedSystemScaleUp, UnsupportedSinglePrimary.
formed_status="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="ClusterFormed")].status}' 2>/dev/null || true)"
formed_reason="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="ClusterFormed")].reason}' 2>/dev/null || true)"
[[ "${formed_status}" == "True" ]] \
  || die "ClusterFormed=${formed_status:-<absent>} (reason=${formed_reason:-none}) — operator does not consider the cluster formed"
log "ClusterFormed=True (reason=${formed_reason:-?})"

# 2. Ask Neo4j directly — the authoritative check. Every expected member must appear
#    as Enabled + Available. Retried: servers are admitted progressively.
password="$(neo4j_password)"
log "Querying SHOW SERVERS from ${POD} — expecting ${WANT} Enabled+Available member(s)"

deadline=$((SECONDS + TIMEOUT_SECS))
ok=0
servers_out=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  # -d system is mandatory: a direct bolt:// session defaults to the `neo4j` database,
  # which topology.defaultPrimariesCount may host on a subset of the primaries. On a pod
  # that does not host it the session is refused and this loop sees no output at all —
  # reported as "cluster not formed" while the DBMS is healthy. SHOW SERVERS is a
  # system-database command, so pin the session there.
  servers_out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
     'SHOW SERVERS YIELD name,address,state,health;'" 2>&1 || true)"
  # Count rows that are both Enabled and Available (header line never matches both).
  count="$(grep -c '"Enabled".*"Available"' <<<"${servers_out}" || true)"
  if [[ "${count:-0}" -ge "${WANT}" ]]; then
    ok=1
    break
  fi
  sleep 10
done

if [[ "${ok}" -ne 1 ]]; then
  # stdout+stderr: an empty dump used to hide the cypher-shell error behind a bare count.
  log "last SHOW SERVERS attempt (stdout+stderr) was:"
  printf '%s\n' "${servers_out:-<no output at all — cypher-shell did not run or was refused>}" >&2
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "only ${count:-0}/${WANT} server(s) Enabled+Available within ${TIMEOUT_SECS}s — cluster not formed"
fi

log "SHOW SERVERS: ${count}/${WANT} member(s) Enabled+Available — cluster formed (AC-NEO-CLUSTER-002)"
