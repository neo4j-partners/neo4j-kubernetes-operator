#!/usr/bin/env bash
# assert/rbac-clusterwide — AC-OP-SCOPE-CLUSTER (BDR-003 amendment): cluster-wide scope grants the
# operand permissions through ONE ClusterRole + ClusterRoleBinding naming the operator
# ServiceAccount, and renders NO per-namespace manager Role. Leader election keeps its own
# namespaced Role in the operator namespace (NEO-016). The mirror of assert/rbac-namespaced.
#
# Inputs:
#   OPERATOR_HELM_NAMESPACE      — namespace holding the controller and its ServiceAccount
#   E2E_SCOPE_WATCHED_NAMESPACES — workload namespaces that must NOT hold a manager Role
#   OPERATOR_ROLE / OPERATOR_SA  — names rendered by the chart
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

OP_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
WORKLOAD="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
ROLE="${OPERATOR_ROLE:-neo4j-operator-manager-role}"
CRB="${ROLE/-role/-rolebinding}"
SA="${OPERATOR_SA:-neo4j-operator-controller-manager}"
LEADER_ROLE="${OPERATOR_LEADER_ROLE:-neo4j-operator-leader-election-role}"

# 1. Exactly one cluster-scoped manager grant, bound to the operator ServiceAccount.
kubectl get clusterrole "${ROLE}" >/dev/null 2>&1 \
  || die "cluster-wide scope must render ClusterRole ${ROLE}"
subject_needle="ServiceAccount/${OP_NS}/${SA}"
crb_subjects="$(kubectl get clusterrolebinding "${CRB}" \
  -o jsonpath='{range .subjects[*]}{.kind}/{.namespace}/{.name}{"\n"}{end}' 2>/dev/null || true)"
grep -qF -- "${subject_needle}" <<<"${crb_subjects}" \
  || die "ClusterRoleBinding ${CRB} does not bind the operator SA ${subject_needle}"
log "ClusterRole ${ROLE} bound to ${subject_needle}"

# 2. No per-namespace manager Role anywhere — cluster-wide replaces them, it does not add to them.
kubectl get role "${ROLE}" -n "${OP_NS}" >/dev/null 2>&1 \
  && die "manager Role ${ROLE} must not exist in operator namespace ${OP_NS} (NEO-016)"
IFS=',' read -r -a workload_list <<<"${WORKLOAD}"
for ns in "${workload_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  kubectl get role "${ROLE}" -n "${ns}" >/dev/null 2>&1 \
    && die "cluster-wide scope must not render a per-namespace manager Role, found one in ${ns}"
done
log "No per-namespace manager Role in ${OP_NS} or ${WORKLOAD}"

# 3. Leader election stays namespaced in the operator namespace (not folded into the ClusterRole).
kubectl get role "${LEADER_ROLE}" -n "${OP_NS}" >/dev/null 2>&1 \
  || die "leader-election Role ${LEADER_ROLE} must remain namespaced in ${OP_NS}"
log "Leader-election Role ${LEADER_ROLE} still namespaced in ${OP_NS}"

log "RBAC is one ClusterRole + ClusterRoleBinding, no per-namespace manager Role (AC-OP-SCOPE-CLUSTER)"
