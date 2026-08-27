#!/usr/bin/env bash
# assert/storage-error — when the data PVC cannot bind (missing StorageClass, or a
# claimName pointing at a missing PVC), the operator keeps the CR PENDING (not Failed)
# and surfaces the cause on the StorageReady condition:
#   - StorageReady = False, reason = PVCPending
#   - the message explains the problem and mentions the PVC ("pvc")
#   - status.phase is NOT Failed and the CR never becomes Ready
#
# NOTE: PVCPending lives ONLY on the StorageReady condition — the catalog declares its surface
# as `condition`, so there is deliberately no Event to look for. Source of truth:
# src/internal/oracle/catalog.go, checked below through tests/lib/oracle.sh so a rename fails
# here rather than after the full timeout.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

RES="neo4j/${NEO4J_CR_NAME}"
# Time we allow the operator to observe the stuck PVC and set StorageReady=False/PVCPending.
TIMEOUT="${STORAGE_ERROR_TIMEOUT:-120}"
EXPECT_REASON="${STORAGE_ERROR_REASON:-PVCPending}"
oracle_require StorageReady "${EXPECT_REASON}"

log "Expecting ${RES} to stay Pending with StorageReady=False/${EXPECT_REASON} (PVC cannot bind) within ${TIMEOUT}s"

sr_status="" sr_reason=""
deadline=$((SECONDS + TIMEOUT))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  sr_status="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.conditions[?(@.type=="StorageReady")].status}' 2>/dev/null || true)"
  sr_reason="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.conditions[?(@.type=="StorageReady")].reason}' 2>/dev/null || true)"
  if [[ "${sr_status}" == "False" && "${sr_reason}" == "${EXPECT_REASON}" ]]; then
    break
  fi
  sleep 5
done

phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.phase}' 2>/dev/null || true)"
ready_status="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"

if [[ "${sr_status}" != "False" || "${sr_reason}" != "${EXPECT_REASON}" ]]; then
  kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status}' >&2 2>/dev/null || true
  echo >&2
  die "expected StorageReady=False/${EXPECT_REASON} within ${TIMEOUT}s, got status='${sr_status:-<none>}' reason='${sr_reason:-<none>}'"
fi

# Decision: a stuck PVC keeps the CR Pending — it must NOT be marked Failed, and must NOT
# report Ready.
[[ "${phase}" != "Failed" ]] \
  || die "operator marked the CR Failed; the decision is to stay Pending on a stuck PVC (phase='${phase}')"
[[ "${ready_status}" != "True" ]] \
  || die "operator reported Ready despite an unbound PVC"

sr_msg="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="StorageReady")].message}' 2>/dev/null || true)"

log "Operator kept ${RES} Pending (phase='${phase:-unknown}', Ready='${ready_status:-False}') with StorageReady message: ${sr_msg}"

grep -qi 'pvc' <<<"${sr_msg}" \
  || die "StorageReady message must explain the PVC problem (expected 'pvc' in: '${sr_msg}')"

log "Operator surfaced the PVC-pending cause on StorageReady while staying Pending, as required"
