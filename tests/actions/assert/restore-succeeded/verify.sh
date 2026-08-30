#!/usr/bin/env bash
# assert/restore-succeeded — the Neo4jRestore completes end to end:
#   - Neo4jRestore reaches status.phase=Succeeded with RestoreReady=True/RestoreSucceeded
#   - the seeded database `restored` is online (SHOW DATABASES)
#   - the probe row written before backup is present in `restored` — proving the seed carried
#     real data, not just that a database was created.
# Contract sources: src/internal/controller/neo4jrestore/reconciler.go,
# reasons via tests/lib/oracle.sh (RestoreReady/RestoreSucceeded).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
RESTORED_DB="restored"
PROBE_ID="e2e-restore"
POD="${NEO4J_STS_NAME}-0"
RES="neo4jrestore/${RESTORE_NAME}"
TIMEOUT="${RESTORE_ASSERT_TIMEOUT:-600}"
EXPECT_REASON="${RESTORE_EXPECT_REASON:-RestoreSucceeded}"
oracle_require RestoreReady "${EXPECT_REASON}"

kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${RES} not found — did restore/seed apply the Neo4jRestore?"

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
  kubectl logs -n "${OPERATOR_NAMESPACE}" -l "${OPERATOR_LABEL_SELECTOR}" --tail=-1 >&2 || true
  die "expected ${RES} phase=Succeeded RestoreReady=True/${EXPECT_REASON}, got phase='${phase:-<none>}' status='${status:-<none>}' reason='${reason:-<none>}'"
fi

password="$(neo4j_password)"

log "Asserting database ${RESTORED_DB} is online"
online=""
deadline=$((SECONDS + 120))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  online="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
     \"SHOW DATABASES YIELD name, currentStatus WHERE name = '${RESTORED_DB}' RETURN DISTINCT currentStatus;\"" 2>&1 || true)"
  grep -qi 'online' <<<"${online}" && break
  sleep 5
done
grep -qi 'online' <<<"${online}" \
  || die "database ${RESTORED_DB} not online after restore; last SHOW DATABASES: ${online:-<none>}"

log "Asserting the probe row seeded into ${RESTORED_DB}"
count="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "cypher-shell -a bolt://localhost:7687 -d ${RESTORED_DB} -u neo4j -p '${password}' --format plain \
   \"MATCH (n:RestoreProbe {id:'${PROBE_ID}'}) RETURN count(n);\"" 2>&1 || true)"
probe="$(grep -Eo '[0-9]+' <<<"${count}" | tail -n 1 || true)"
[[ "${probe}" == "1" ]] \
  || die "expected 1 RestoreProbe in ${RESTORED_DB} (seed carried data), got '${probe:-0}'; output: ${count:-<none>}"

log "Neo4jRestore ${RESTORE_NAME} Succeeded; ${RESTORED_DB} online with seeded probe row"
