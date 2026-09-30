#!/usr/bin/env bash
# assert/operator-ns-ignored — NEO-016 under cluster-wide scope: a Neo4j CR in the operator's own
# namespace must be left completely alone, even though the operator watches every other namespace.
# The cache field selector (metadata.namespace != POD_NAMESPACE) and the reconciler guard both
# enforce this. ABSENCE assertion, so time-bounded: give the operator a grace window to (wrongly)
# act and PASS only if nothing appears.
#
# Inputs:
#   OPERATOR_HELM_NAMESPACE  — operator namespace (default neo4j-operator-scope)
#   E2E_SCOPE_OPERATOR_NS_CR — CR name in that namespace (default e2e-scope-operatorns)
#   E2E_SCOPE_GRACE          — seconds to wait for (wrong) reconciliation (default 30)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
CR="${E2E_SCOPE_OPERATOR_NS_CR:-e2e-scope-operatorns}"
GRACE="${E2E_SCOPE_GRACE:-30}"
INSTANCE_SELECTOR="app.kubernetes.io/instance=${CR}"

log "Watching ${GRACE}s for any (incorrect) reconciliation of ${CR} in operator ns ${NS}"

deadline=$((SECONDS + GRACE))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  # 1. No operands may be created in the operator namespace for this CR.
  operands="$(kubectl get statefulset,deployment,configmap,secret,svc,pvc \
    -n "${NS}" -l "${INSTANCE_SELECTOR}" \
    -o name 2>/dev/null || true)"
  if [[ -n "${operands}" ]]; then
    die "operator reconciled a CR in its own namespace ${NS} (NEO-016 breach) — created:"$'\n'"${operands}"
  fi

  # 2. The operator must not have written status conditions onto the CR.
  installed="$(kubectl get neo4j "${CR}" -n "${NS}" \
    -o jsonpath='{.status.conditions[?(@.type=="Installed")].status}' 2>/dev/null || true)"
  if [[ "${installed}" == "True" ]]; then
    die "operator set Installed=True on a CR in its own namespace ${NS} — NEO-016 not enforced"
  fi

  sleep 3
done

log "No operands and no Installed condition in ${NS} after ${GRACE}s — operator namespace correctly excluded (NEO-016)"
