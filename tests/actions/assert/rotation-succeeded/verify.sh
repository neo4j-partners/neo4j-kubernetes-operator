#!/usr/bin/env bash
# assert/rotation-succeeded — the Neo4jPasswordRotation completes end to end (BDR-017 / ADR-019):
#   - Neo4jPasswordRotation reaches status.phase=Succeeded with RotationReady=True/RotationSucceeded
#   - the live auth Secret <cr>-auth now holds the NEW password (the operator synced it)
#   - the NEW password authenticates a bolt query, and the OLD password is rejected — proving the
#     ALTER actually landed in the database, not just that the Secret was rewritten.
# Contract sources: src/internal/controller/neo4jpasswordrotation/reconciler.go,
# reasons via tests/lib/oracle.sh (RotationReady/RotationSucceeded).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

ROTATION_NAME="${NEO4J_CR_NAME}-run"
OLDPW_SECRET="${NEO4J_CR_NAME}-e2e-oldpw"
POD="${NEO4J_STS_NAME}-0"
RES="neo4jpasswordrotation/${ROTATION_NAME}"
TIMEOUT="${ROTATION_ASSERT_TIMEOUT:-300}"
EXPECT_REASON="${ROTATION_EXPECT_REASON:-RotationSucceeded}"
NEW_PASSWORD="Rotated-${NEO4J_CR_NAME}-pw1"   # must match rotation/apply
oracle_require RotationReady "${EXPECT_REASON}"

kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${RES} not found — did rotation/apply file the Neo4jPasswordRotation?"

log "Waiting for ${RES} phase=Succeeded / RotationReady=True/${EXPECT_REASON} (timeout ${TIMEOUT}s)"
phase="" status="" reason=""
deadline=$((SECONDS + TIMEOUT))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  status="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="RotationReady")].status}' 2>/dev/null || true)"
  reason="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="RotationReady")].reason}' 2>/dev/null || true)"
  [[ "${phase}" == "Succeeded" && "${status}" == "True" && "${reason}" == "${EXPECT_REASON}" ]] && break
  [[ "${phase}" == "Failed" ]] && break
  sleep 5
done

if [[ "${phase}" != "Succeeded" || "${status}" != "True" || "${reason}" != "${EXPECT_REASON}" ]]; then
  kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o yaml >&2 || true
  kubectl logs -n "${OPERATOR_NAMESPACE}" -l "${OPERATOR_LABEL_SELECTOR}" --tail=-1 >&2 || true
  die "expected ${RES} phase=Succeeded RotationReady=True/${EXPECT_REASON}, got phase='${phase:-<none>}' status='${status:-<none>}' reason='${reason:-<none>}'"
fi

# The operator must have synced <cr>-auth to the new value (neo4j_password reads NEO4J_AUTH_SECRET).
synced="$(neo4j_password)"
[[ "${synced}" == "${NEW_PASSWORD}" ]] \
  || die "live auth Secret ${NEO4J_AUTH_SECRET} was not synced: holds a different password than the rotated one"
log "Live auth Secret ${NEO4J_AUTH_SECRET} now holds the rotated password"

old_password="$(kubectl get secret "${OLDPW_SECRET}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || true)"
[[ -n "${old_password}" ]] || die "could not read stashed old password from ${OLDPW_SECRET}"

conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

# The new password must authenticate; the old must now be rejected — the ALTER really changed the DB.
conn_assert_one bolt success localhost "${NEW_PASSWORD}" "rotation-new"
conn_assert_one bolt failure localhost "${old_password}" "rotation-old"

log "Neo4jPasswordRotation ${ROTATION_NAME} Succeeded; new password active, old password rejected"
