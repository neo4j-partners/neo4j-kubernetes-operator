#!/usr/bin/env bash
# assert/storage-error — DESIRED contract (not yet implemented): when the data PVC can
# never be created/bound (missing StorageClass, or claimName pointing at a missing PVC),
# the operator must TIME OUT and mark the CR as failed:
#   - status.phase = Failed (or an Error condition with status=True)
#   - the failure message explains the problem and mentions the PVC ("pvc")
#
# EXPECTED TO FAIL for now: the operator currently stays in Bootstrapping forever
# (StorageReady=False/PVCPending, Error=False/NoError) with no storage timeout. This test
# encodes the target behavior; do NOT patch operator code to make it pass — that work is
# tracked separately.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

RES="neo4j/${NEO4J_CR_NAME}"
# Window we allow the operator to detect the stuck PVC and give up. Should be >= the
# operator's (future) storage timeout; override with STORAGE_ERROR_TIMEOUT.
TIMEOUT="${STORAGE_ERROR_TIMEOUT:-180}"

log "Expecting the operator to time out and mark ${RES} Failed (PVC cannot be created) within ${TIMEOUT}s"

phase=""
err_status=""
deadline=$((SECONDS + TIMEOUT))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  err_status="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.conditions[?(@.type=="Error")].status}' 2>/dev/null || true)"
  if [[ "${phase}" == "Failed" || "${err_status}" == "True" ]]; then
    break
  fi
  sleep 5
done

if [[ "${phase}" != "Failed" && "${err_status}" != "True" ]]; then
  kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status}' >&2 2>/dev/null || true
  echo >&2
  die "expected the operator to time out and set a Failed/Error status within ${TIMEOUT}s, but phase='${phase:-unknown}' Error='${err_status:-False}' (storage failure timeout not implemented yet)"
fi

# Pull the failure message from the Error condition, falling back to the Ready condition.
err_msg="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="Error")].message}' 2>/dev/null || true)"
[[ -n "${err_msg}" ]] || err_msg="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || true)"

log "Operator reported failure (phase='${phase}', Error='${err_status}'): ${err_msg}"

grep -qi 'pvc' <<<"${err_msg}" \
  || die "failure message must explain the PVC problem (expected 'pvc' in: '${err_msg}')"

log "Operator timed out and reported a PVC failure with an explanatory message, as required"
