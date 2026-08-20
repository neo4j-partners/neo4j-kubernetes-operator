#!/usr/bin/env bash
# assert/rbac-namespaced — AC-OP-SCOPE-MULTI-002 (and AC-OP-SCOPE-SINGLE-004 as its
# single-namespace case): the operator's operand permissions are namespace-scoped — one Role per
# watched namespace, all granting the same rules — and it holds NO cluster-wide grant. Widening
# the scope must add Roles, never a ClusterRole. Cluster-scoped access is limited to the CRD
# itself, which is installed separately (make install), not granted to the running operator.
#
# Inputs:
#   OPERATOR_HELM_NAMESPACE      — namespace holding the controller and its ServiceAccount
#   E2E_SCOPE_WATCHED_NAMESPACES — comma-separated watched namespaces
#   OPERATOR_ROLE / OPERATOR_SA  — names rendered by the chart
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

OP_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
WATCHED="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
ROLE="${OPERATOR_ROLE:-neo4j-operator-manager-role}"
SA="${OPERATOR_SA:-neo4j-operator-controller-manager}"

# 1. One Role per watched namespace, with the same rules everywhere: a namespace granted less than
#    the others would reconcile until the first missing verb.
reference=""
IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  rules="$(kubectl get role "${ROLE}" -n "${ns}" -o jsonpath='{.rules}' 2>/dev/null || true)"
  [[ -n "${rules}" ]] || die "namespaced Role ${ROLE} missing in ${ns} — operator not scoped as expected"
  if [[ -z "${reference}" ]]; then
    reference="${rules}"
  elif [[ "${rules}" != "${reference}" ]]; then
    die "Role ${ROLE} in ${ns} does not grant the same rules as the other watched namespaces"
  fi
  log "Role ${ROLE} present in ${ns} with the common rule set"
done

# 2. Never in the operator's own namespace (NEO-016). Leader election has its own Role there.
if kubectl get role "${ROLE}" -n "${OP_NS}" >/dev/null 2>&1; then
  die "manager Role ${ROLE} must not exist in operator namespace ${OP_NS} (NEO-016)"
fi
log "Role ${ROLE} absent from operator namespace ${OP_NS} (NEO-016)"

# 3. No ClusterRoleBinding may name the operator ServiceAccount, however many namespaces are
#    watched. metrics.enabled (NEO-017) is the documented exception and e2e leaves metrics off.
#    Flatten every CRB's subjects to "<crb> ServiceAccount/<ns>/<name>" lines and look for our SA
#    — plain jsonpath plus grep, no jq dependency.
subject_needle="ServiceAccount/${OP_NS}/${SA}"
crb_lines="$(kubectl get clusterrolebindings -o jsonpath='{range .items[*]}{.metadata.name}{" "}{range .subjects[*]}{.kind}/{.namespace}/{.name}{"\n"}{end}{end}' 2>/dev/null || true)"
if grep -qF -- "${subject_needle}" <<<"${crb_lines}"; then
  offending="$(grep -F -- "${subject_needle}" <<<"${crb_lines}" || true)"
  die "operator SA ${OP_NS}/${SA} is bound cluster-wide via ClusterRoleBinding subject(s): ${offending}"
fi

log "RBAC is one Role per watched namespace, no cluster-wide grant (AC-OP-SCOPE-MULTI-002)"
