#!/usr/bin/env bash
# assert/rbac-multi-namespaced — AC-OP-SCOPE-MULTI-002: widening the scope adds one Role and one
# RoleBinding per watched namespace and nothing else. Watching several namespaces must not turn
# into a ClusterRole, and the manager Role must still be absent from the operator's own namespace
# (NEO-016).
#
# Inputs:
#   OPERATOR_HELM_NAMESPACE — release namespace holding the controller and its ServiceAccount
#   E2E_SCOPE_NAMESPACES    — comma-separated watched namespaces
#   OPERATOR_ROLE / OPERATOR_SA — names rendered by the chart
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

OP_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
WATCHED="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
ROLE="${OPERATOR_ROLE:-neo4j-operator-manager-role}"
SA="${OPERATOR_SA:-neo4j-operator-controller-manager}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"

# 1. One Role per watched namespace, and the same rules everywhere: a namespace granted less than
#    the others would reconcile until the first missing verb.
reference=""
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  rules="$(kubectl get role "${ROLE}" -n "${ns}" -o jsonpath='{.rules}' 2>/dev/null || true)"
  [[ -n "${rules}" ]] || die "no Role ${ROLE} in watched namespace ${ns}"
  if [[ -z "${reference}" ]]; then
    reference="${rules}"
  elif [[ "${rules}" != "${reference}" ]]; then
    die "Role ${ROLE} in ${ns} does not grant the same rules as the other watched namespaces"
  fi
  log "Role ${ROLE} present in ${ns} with the common rule set"
done

# 2. Never in the operator namespace (NEO-016). Leader election has its own Role there.
if kubectl get role "${ROLE}" -n "${OP_NS}" >/dev/null 2>&1; then
  die "manager Role ${ROLE} must not exist in operator namespace ${OP_NS} (NEO-016)"
fi
log "Role ${ROLE} absent from operator namespace ${OP_NS} (NEO-016)"

# 3. No cluster-wide grant, however many namespaces are watched.
assert_no_cluster_wide_grant "${OP_NS}" "${SA}"

log "RBAC is one Role per watched namespace, no cluster-wide grant (AC-OP-SCOPE-MULTI-002)"
