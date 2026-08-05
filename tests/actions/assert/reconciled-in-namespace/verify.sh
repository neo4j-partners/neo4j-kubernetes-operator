#!/usr/bin/env bash
# assert/reconciled-in-namespace — AC-OP-SCOPE-SINGLE-002 (positive control):
# a CR in a WATCHED namespace IS reconciled — the operator renders operands and
# sets the Installed condition. This is the counterpart to assert/scope-ignored:
# together they prove the operator acts inside its scope and only inside it.
#
# Inputs:
#   NEO4J_NAMESPACE          — watched namespace (default "default")
#   E2E_SCOPE_WATCHED_CR     — CR name (default e2e-scope-watched)
#   E2E_ASSERT_TIMEOUT       — wait budget
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${NEO4J_NAMESPACE:-default}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
NEO4J_RESOURCE="neo4j/${CR}"

log "Waiting for ${NEO4J_RESOURCE} Installed condition in watched ns ${NS}"
if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NS}" --timeout="${TIMEOUT}" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NS}" >&2 || true
  die "CR ${CR} in watched ns ${NS} was NOT reconciled (Installed not True) — scope too narrow"
fi

# Operands must exist (the operator did real work, not just set a condition).
sts="$(kubectl get statefulset -n "${NS}" \
  -l "app.kubernetes.io/instance=${CR}" -o name 2>/dev/null || true)"
[[ -n "${sts}" ]] \
  || die "no StatefulSet created for reconciled CR ${CR} in ${NS}"

log "CR ${CR} in watched ns ${NS} reconciled (Installed=True, ${sts} created) (AC-OP-SCOPE-SINGLE-002)"
