#!/usr/bin/env bash
# assert/reconcile-error — the operator refuses the CR at reconcile time and names the cause
# with a stable reason from the error oracle:
#   - Error = True with reason = EXPECT_REASON
#   - a Warning Event carries the SAME reason (what a user sees in `kubectl describe`)
#   - nothing is deployed: no StatefulSet for this instance, and the CR never becomes Ready
#
# Assert on Reason, never on the message (free-form and allowed to change). Reasons come from
# src/internal/oracle/catalog.go, which also generates
# docs/user-guide/05-reference/errors.md and tests/lib/oracle.sh — so the reason below is checked
# against the operator's contract before the wait starts.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, EXPECT_REASON, RECONCILE_ERROR_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

: "${EXPECT_REASON:?EXPECT_REASON required (a reason catalogued in internal/oracle)}"
oracle_require Error "${EXPECT_REASON}"

RES="neo4j/${NEO4J_CR_NAME}"
# Time we allow the operator to observe the CR and write the refusal on the Error condition.
TIMEOUT="${RECONCILE_ERROR_TIMEOUT:-120}"

condition() {
  kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || true
}

log "Expecting ${RES} to be refused with Error=True/${EXPECT_REASON} within ${TIMEOUT}s"

err_status="" err_reason=""
deadline=$((SECONDS + TIMEOUT))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  err_status="$(condition Error status)"
  err_reason="$(condition Error reason)"
  if [[ "${err_status}" == "True" && "${err_reason}" == "${EXPECT_REASON}" ]]; then
    break
  fi
  sleep 5
done

if [[ "${err_status}" != "True" || "${err_reason}" != "${EXPECT_REASON}" ]]; then
  kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status}' >&2 2>/dev/null || true
  echo >&2
  die "expected Error=True/${EXPECT_REASON} within ${TIMEOUT}s, got status='${err_status:-<none>}' reason='${err_reason:-<none>}'"
fi

log "Error condition reports ${EXPECT_REASON}: $(condition Error message)"

# The refusal must land before any operand exists — that is the security property, not the text.
sts="$(kubectl get statefulset -n "${NEO4J_NAMESPACE}" \
  -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o name 2>/dev/null || true)"
[[ -z "${sts}" ]] \
  || die "operator created ${sts} despite refusing the Secret (must stop before rendering operands)"

[[ "$(condition Ready status)" != "True" ]] \
  || die "operator reported Ready despite refusing the CR"

# Same reason on a Warning Event. The Event is emitted just before the status write, but the
# broadcaster is async, so allow a short grace period.
ev_type=""
ev_deadline=$((SECONDS + 30))
while [[ "${SECONDS}" -lt "${ev_deadline}" ]]; do
  ev_type="$(kubectl get events -n "${NEO4J_NAMESPACE}" \
    --field-selector "involvedObject.name=${NEO4J_CR_NAME},reason=${EXPECT_REASON}" \
    -o jsonpath='{.items[0].type}' 2>/dev/null || true)"
  [[ -n "${ev_type}" ]] && break
  sleep 3
done

[[ "${ev_type}" == "Warning" ]] \
  || die "expected a Warning Event with reason ${EXPECT_REASON} on ${RES}, got type='${ev_type:-<none>}'"

log "Operator refused ${RES} with reason ${EXPECT_REASON} on both the Error condition and a Warning Event, and deployed nothing"
