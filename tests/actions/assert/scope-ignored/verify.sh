#!/usr/bin/env bash
# assert/scope-ignored — OP-2-001-SCOPE-01 (AC-OP-SCOPE-SINGLE):
# a Neo4j CR in a namespace outside WATCH_NAMESPACE must be left completely alone —
# the namespace-scoped operator must not reconcile it.
#
# This is an ABSENCE assertion, so it is time-bounded: we give the operator a grace
# window to (incorrectly) act, and PASS only if nothing appears. Two signals, either
# of which is a failure:
#   1. a StatefulSet (or any operand) shows up in the unwatched namespace, or
#   2. the CR's status gains an Installed / Ready condition set by the operator.
#
# Inputs:
#   E2E_SCOPE_NAMESPACE — unwatched namespace          (default e2e-unwatched)
#   E2E_SCOPE_CR        — CR name inside that namespace (default e2e-scope)
#   E2E_SCOPE_GRACE     — seconds to wait for (wrong) reconciliation (default 30)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${E2E_SCOPE_NAMESPACE:-e2e-unwatched}"
CR="${E2E_SCOPE_CR:-e2e-scope}"
GRACE="${E2E_SCOPE_GRACE:-30}"
INSTANCE_SELECTOR="app.kubernetes.io/instance=${CR}"

log "Watching ${GRACE}s for any (incorrect) reconciliation of ${CR} in unwatched ns ${NS}"

deadline=$((SECONDS + GRACE))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  # 1. No operands may be created in the unwatched namespace.
  operands="$(kubectl get statefulset,deployment,configmap,secret,svc,pvc \
    -n "${NS}" -l "${INSTANCE_SELECTOR}" \
    -o name 2>/dev/null || true)"
  if [[ -n "${operands}" ]]; then
    die "operator reconciled a CR in unwatched ns ${NS} — created:"$'\n'"${operands}"
  fi

  # 2. The operator must not have written status conditions onto the CR.
  installed="$(kubectl get neo4j "${CR}" -n "${NS}" \
    -o jsonpath='{.status.conditions[?(@.type=="Installed")].status}' 2>/dev/null || true)"
  if [[ "${installed}" == "True" ]]; then
    die "operator set Installed=True on a CR in unwatched ns ${NS} — scope not enforced"
  fi

  sleep 3
done

log "No operands and no Installed condition in ${NS} after ${GRACE}s — CR correctly ignored (OP-2-001-SCOPE-01)"
