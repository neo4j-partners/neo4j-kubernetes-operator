#!/usr/bin/env bash
# assert/restore-s3-cluster — the record-based, overwrite restore from an OBJECT-STORE (MinIO) backup
# completes on a 3-primary cluster:
#   - Neo4jRestore reaches status.phase=Succeeded with RestoreReady=True/RestoreSucceeded
#   - `neo4j` is allocated + online on ALL THREE primaries — the cluster restore contract: the
#     operator issued CREATE OR REPLACE ... TOPOLOGY 3 PRIMARIES and each member seeded from s3://
#     independently via CloudSeedProvider (spec.security.cloudIdentity.staticKeySecret, ADR-016)
#   - probe #1 (in the backup) is present and probe #post (written after the backup) is ABSENT,
#     proving the overwrite replaced the live store cluster-wide rather than merging into it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
TARGET_DB="neo4j"
WANT_MEMBERS="${CLUSTER_EXPECTED_DB_PRIMARIES:-3}"
RES="neo4jrestore/${RESTORE_NAME}"
TIMEOUT="${RESTORE_ASSERT_TIMEOUT:-600}"
EXPECT_REASON="${RESTORE_EXPECT_REASON:-RestoreSucceeded}"
oracle_require RestoreReady "${EXPECT_REASON}"

# Exec into primary member 0 directly (StatefulSet <cr>-primary). A label lookup on
# app.kubernetes.io/instance would ALSO match the completed backup Job pod (shared CommonLabels),
# which sorts first and can't be exec'd — that is what made the earlier run measure 0 online.
POD="${NEO4J_CR_NAME}-primary-0"

kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${RES} not found — did backup/s3-roundtrip-cluster apply the Neo4jRestore?"

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

# cypher runs a statement on the given database from the resolved pod. Reads are fine over a direct
# bolt:// session — every primary hosts `neo4j` (TOPOLOGY 3) and the system database is everywhere.
cypher() {
  local db="$1" stmt="$2"
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d ${db} -u neo4j -p '${password}' --format plain \"${stmt}\"" 2>&1 || true
}

# last_int extracts the trailing integer from stdin; no digits (a transient cypher error or a
# not-yet-online database) yields empty, NOT a non-zero exit — otherwise `set -o pipefail` would abort
# the whole script mid-poll instead of letting the caller treat it as 0 and retry.
last_int() { grep -Eo '[0-9]+' | tail -n 1 || true; }

# The cluster restore contract: SHOW DATABASES yields one row per (database, server) allocation, so
# an online-count of WANT_MEMBERS proves every primary seeded its own copy from s3://. The members
# download from MinIO independently after the seed is issued, so this polls rather than checks once.
log "Asserting ${TARGET_DB} is online on all ${WANT_MEMBERS} primaries (per-member seed)"
allocs=0 dbs=""
deadline=$((SECONDS + 300))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  dbs="$(cypher system "SHOW DATABASES YIELD name, currentStatus WHERE name = '${TARGET_DB}' AND currentStatus = 'online' RETURN count(*) AS n;")"
  allocs="$(last_int <<<"${dbs}")"
  [[ "${allocs:-0}" -ge "${WANT_MEMBERS}" ]] && break
  sleep 10
done
if [[ "${allocs:-0}" -lt "${WANT_MEMBERS}" ]]; then
  cypher system "SHOW DATABASES YIELD name, currentStatus, address WHERE name = '${TARGET_DB}';" >&2 || true
  kubectl logs -n "${OPERATOR_NAMESPACE:-neo4j-operator-system}" \
    -l "${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}" --tail=-1 >&2 || true
  die "expected ${TARGET_DB} online on ${WANT_MEMBERS} primaries after restore, got ${allocs:-0} — per-member seed did not complete"
fi

# count RestoreProbe rows in ${TARGET_DB} matching the given id predicate.
probe_count() {
  local pred="$1"
  cypher "${TARGET_DB}" "MATCH (n:RestoreProbe) WHERE ${pred} RETURN count(n);" | last_int
}

log "Asserting the backed-up probe is present (members seeded from s3:// via CloudSeedProvider)"
present="$(probe_count "n.id = 'e2e-s3c-1'")"
[[ "${present}" == "1" ]] \
  || die "expected 1 backed-up RestoreProbe in ${TARGET_DB} (seeded from MinIO), got '${present:-0}'"

log "Asserting the post-backup probe is ABSENT (overwrite replaced the live store cluster-wide)"
post="$(probe_count "n.id = 'e2e-s3c-post'")"
[[ "${post}" == "0" ]] \
  || die "expected 0 post-backup RestoreProbe in ${TARGET_DB} (overwrite should drop it), got '${post:-?}'"

log "Neo4jRestore ${RESTORE_NAME} Succeeded; ${TARGET_DB} seeded from MinIO on ${allocs} primaries, post-backup probe dropped"
