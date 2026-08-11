#!/usr/bin/env bash
# assert/status-conditions — OP-1-003 / OP-2-003-STATUS-01 (AC-OP-STATUS): the operator
# exposes the full basic condition catalog — Ready, Reconciling, Error, Installed.
#
# Previously only `Ready` was covered (implicitly, by waiting on it). The requirement is
# the whole catalog, so this checks all four are present with the reasons the status
# writer documents. A condition silently dropped from `internal/status` — the contract
# drift NEO3-23 is about — is invisible to a `kubectl wait` on Ready alone.
#
# This asserts the catalog is *exposed*, not that the workload is healthy: Ready may be
# False here because the suite does not always wait for a booted Neo4j
# (E2E_ASSERT_NEO4J_READY defaults to false). What is pinned is that Ready exists and
# carries a reason the writer can actually produce — so Ready=True can never be reported
# with a reason that does not mean "all members ready".
#
# Deliberately out of scope: status.phase and observedGeneration. Both have open defects
# (NEO3-59, NEO3-58) and phase semantics are still an unaccepted ADR (NEO3-23); pinning
# them here would encode behaviour the project has not decided on.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"

kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} not found — cannot check status conditions"

cond() {  # cond <type> <field>
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || true
}

# Reconciling is True for the duration of a pass and settles to False/Completed. The
# operator requeues, so a read can land mid-pass — poll for the settled value instead of
# racing it. Bounded: a pass that never completes is itself the failure.
log "Waiting for the reconcile pass to settle (Reconciling=False)"
for _ in $(seq 1 30); do
  [[ "$(cond Reconciling status)" == "False" ]] && break
  sleep 2
done

failures=0
expect_cond() {  # expect_cond <type> <expected-status> <expected-reason>
  local type=$1 want_status=$2 want_reason=$3 status reason
  status="$(cond "${type}" status)"
  reason="$(cond "${type}" reason)"
  if [[ -z "${status}" ]]; then
    log "FAIL ${type}: condition absent from .status.conditions"
    failures=$((failures + 1))
    return
  fi
  [[ "${status}" == "${want_status}" ]] \
    || { log "FAIL ${type}: status=${status}, expected ${want_status}"; failures=$((failures + 1)); }
  [[ "${reason}" == "${want_reason}" ]] \
    || { log "FAIL ${type}: reason=${reason:-unset}, expected ${want_reason}"; failures=$((failures + 1)); }
  log "${type}=${status} (${reason})"
}

# Steady state after a completed reconcile — src/internal/status/writer.go.
expect_cond Installed True ObjectsCreated
expect_cond Reconciling False Completed
expect_cond Error False NoError

# Ready must exist and carry a reason the writer can produce. True is only legitimate
# with AllMembersReady, so a Ready=True paired with any other reason is a defect.
ready_status="$(cond Ready status)"
ready_reason="$(cond Ready reason)"
if [[ -z "${ready_status}" ]]; then
  log "FAIL Ready: condition absent from .status.conditions"
  failures=$((failures + 1))
else
  # readyReason() emits the first three; OfflineMaintenance and ReconcileError are set
  # directly by the writer (maintenance mode and MarkPipelineError).
  case "${ready_reason}" in
    AllMembersReady|MembersNotReady|TLSNotReady|OfflineMaintenance|ReconcileError) ;;
    *)
      log "FAIL Ready: unknown reason ${ready_reason:-unset} (writer emits AllMembersReady/MembersNotReady/TLSNotReady/OfflineMaintenance/ReconcileError)"
      failures=$((failures + 1))
      ;;
  esac
  if [[ "${ready_status}" == "True" && "${ready_reason}" != "AllMembersReady" ]]; then
    log "FAIL Ready: status=True with reason ${ready_reason}, expected AllMembersReady"
    failures=$((failures + 1))
  fi
  log "Ready=${ready_status} (${ready_reason})"
fi

if [[ "${failures}" -ne 0 ]]; then
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{range .status.conditions[*]}{.type}={.status} reason={.reason} message={.message}{"\n"}{end}' >&2 || true
  die "status condition catalog assertions failed (${failures} mismatch(es)) — OP-2-003-STATUS-01"
fi

log "Condition catalog exposed: Ready, Reconciling, Error, Installed (OP-2-003-STATUS-01)"
