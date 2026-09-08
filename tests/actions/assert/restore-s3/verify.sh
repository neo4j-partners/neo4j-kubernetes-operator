#!/usr/bin/env bash
# assert/restore-s3 — the record-based, overwrite restore from an OBJECT-STORE (MinIO) backup
# completes end to end:
#   - Neo4jRestore reaches status.phase=Succeeded with RestoreReady=True/RestoreSucceeded
#   - the restored database `neo4j` is online (SHOW DATABASES)
#   - probe #1 (in the backup) is present, proving the server seeded straight from s3:// via
#     CloudSeedProvider using spec.security.cloudIdentity.staticKeySecret (ADR-016 read side)
#   - probe #post (written AFTER the backup) is ABSENT, proving the overwrite replaced the live
#     store with the backup rather than merging into it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
TARGET_DB="neo4j"
POD="${NEO4J_STS_NAME}-0"
RES="neo4jrestore/${RESTORE_NAME}"
TIMEOUT="${RESTORE_ASSERT_TIMEOUT:-600}"
EXPECT_REASON="${RESTORE_EXPECT_REASON:-RestoreSucceeded}"
oracle_require RestoreReady "${EXPECT_REASON}"

kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${RES} not found — did backup/s3-roundtrip apply the Neo4jRestore?"

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

log "Asserting the backed-up probe is present (server seeded from s3:// via CloudSeedProvider)"
present="$(probe_count "n.id = 'e2e-s3-1'")"
[[ "${present}" == "1" ]] \
  || die "expected 1 backed-up RestoreProbe in ${TARGET_DB} (seeded from MinIO), got '${present:-0}'"

log "Asserting the post-backup probe is ABSENT (overwrite replaced the live store)"
post="$(probe_count "n.id = 'e2e-s3-post'")"
[[ "${post}" == "0" ]] \
  || die "expected 0 post-backup RestoreProbe in ${TARGET_DB} (overwrite should drop it), got '${post:-?}'"

log "Neo4jRestore ${RESTORE_NAME} Succeeded; ${TARGET_DB} online, seeded from MinIO, post-backup probe dropped"
