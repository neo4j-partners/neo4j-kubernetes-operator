#!/usr/bin/env bash
# assert/restore-chain — the record-based, overwrite chain restore completes end to end:
#   - Neo4jRestore reaches status.phase=Succeeded with RestoreReady=True/RestoreSucceeded
#   - the restored database `neo4j` is online (SHOW DATABASES)
#   - probes #1 and #2 (in the full → incremental chain) are present, proving the incremental's
#     data was carried, not just the full snapshot
#   - probe #3 (written AFTER the last backup) is ABSENT, proving the overwrite replaced the live
#     store with the backup chain rather than merging into it.
# Contract sources: src/internal/controller/neo4jrestore/reconciler.go (seedURIFromArtifact →
# file:/backups/<path>; overwrite → CreateOrReplaceDatabaseWithSeed), reasons via
# tests/lib/oracle.sh (RestoreReady/RestoreSucceeded).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
TARGET_DB="neo4j"
META_ROLE="e2ebrefrole"
POD="${NEO4J_STS_NAME}-0"
RES="neo4jrestore/${RESTORE_NAME}"
TIMEOUT="${RESTORE_ASSERT_TIMEOUT:-600}"
EXPECT_REASON="${RESTORE_EXPECT_REASON:-RestoreSucceeded}"
oracle_require RestoreReady "${EXPECT_REASON}"

kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${RES} not found — did restore/backupref-chain apply the Neo4jRestore?"

log "Waiting for ${RES} phase=Succeeded / RestoreReady=True/${EXPECT_REASON} (timeout ${TIMEOUT}s)"
phase="" status="" reason=""
deadline=$((SECONDS + TIMEOUT))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  status="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="RestoreReady")].status}' 2>/dev/null || true)"
  reason="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="RestoreReady")].reason}' 2>/dev/null || true)"
  [[ "${phase}" == "Succeeded" && "${status}" == "True" && "${reason}" == "${EXPECT_REASON}" ]] && break
  [[ "${phase}" == "Failed" ]] && break
  sleep 5
done

if [[ "${phase}" != "Succeeded" || "${status}" != "True" || "${reason}" != "${EXPECT_REASON}" ]]; then
  kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o yaml >&2 || true
  kubectl logs -n "${OPERATOR_NAMESPACE:-neo4j-operator-system}" \
    -l "${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}" --tail=-1 >&2 || true
  die "expected ${RES} phase=Succeeded RestoreReady=True/${EXPECT_REASON}, got phase='${phase:-<none>}' status='${status:-<none>}' reason='${reason:-<none>}'"
fi

password="$(neo4j_password)"

log "Asserting database ${TARGET_DB} is online"
online=""
deadline=$((SECONDS + 120))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  online="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
     \"SHOW DATABASES YIELD name, currentStatus WHERE name = '${TARGET_DB}' RETURN DISTINCT currentStatus;\"" 2>&1 || true)"
  grep -qi 'online' <<<"${online}" && break
  sleep 5
done
grep -qi 'online' <<<"${online}" \
  || die "database ${TARGET_DB} not online after restore; last SHOW DATABASES: ${online:-<none>}"

# count RestoreProbe rows in ${TARGET_DB} matching the given id predicate.
probe_count() {
  local pred="$1" out
  out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d ${TARGET_DB} -u neo4j -p '${password}' --format plain \
     \"MATCH (n:RestoreProbe) WHERE ${pred} RETURN count(n);\"" 2>&1 || true)"
  grep -Eo '[0-9]+' <<<"${out}" | tail -n 1
}

log "Asserting BOTH chain probes present in ${TARGET_DB} (full + incremental)"
chain="$(probe_count "n.id IN ['e2e-bref-1','e2e-bref-2']")"
[[ "${chain}" == "2" ]] \
  || die "expected 2 chain RestoreProbe rows in ${TARGET_DB} (full + incremental), got '${chain:-0}'"

log "Asserting the post-backup probe is ABSENT (overwrite replaced the live store)"
post="$(probe_count "n.id = 'e2e-bref-post'")"
[[ "${post}" == "0" ]] \
  || die "expected 0 post-backup RestoreProbe in ${TARGET_DB} (overwrite should drop it), got '${post:-?}'"

# The role was dropped before the restore; spec.restoreMetadata must have recreated it.
log "Asserting role ${META_ROLE} was recreated by the metadata apply (SHOW ROLES)"
roles="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
   \"SHOW ROLES YIELD role WHERE role = '${META_ROLE}' RETURN role;\"" 2>&1 || true)"
grep -q "${META_ROLE}" <<<"${roles}" \
  || die "role ${META_ROLE} missing after restore; spec.restoreMetadata did not reapply it. SHOW ROLES: ${roles:-<none>}"

log "Neo4jRestore ${RESTORE_NAME} Succeeded; ${TARGET_DB} online with both chain probes, no post-backup probe, and role ${META_ROLE} restored"
