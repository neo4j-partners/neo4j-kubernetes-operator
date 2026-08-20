#!/usr/bin/env bash
# assert/rbac-namespaced — AC-OP-SCOPE-SINGLE-004:
# the operator's operand permissions are namespace-scoped (a Role in each watched
# namespace) and it holds NO cluster-wide grant — no ClusterRoleBinding references
# the operator ServiceAccount. Cluster-scoped access is limited to the CRD itself,
# which is installed separately (make install), not granted to the running operator.
#
# Inputs (defaults match config/rbac/):
#   OPERATOR_NAMESPACE       — operator namespace (default neo4j-operator-system)
#   NEO4J_NAMESPACE          — a watched workload namespace (default "default")
#   OPERATOR_ROLE            — namespaced manager Role name
#   OPERATOR_SA              — operator ServiceAccount name
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

OP_NS="${OPERATOR_NAMESPACE:-neo4j-operator-system}"
WATCH_NS="${NEO4J_NAMESPACE:-default}"
ROLE="${OPERATOR_ROLE:-neo4j-operator-manager-role}"
SA="${OPERATOR_SA:-neo4j-operator-controller-manager}"

# 1. Namespaced manager Role must exist in each watched workload namespace,
#    and must NOT exist in the operator namespace (NEO-016).
kubectl get role "${ROLE}" -n "${WATCH_NS}" >/dev/null 2>&1 \
  || die "namespaced Role ${ROLE} missing in ${WATCH_NS} — operator not scoped as expected"
log "Role ${ROLE} present in ${WATCH_NS}"
if kubectl get role "${ROLE}" -n "${OP_NS}" >/dev/null 2>&1; then
  die "manager Role ${ROLE} must not exist in operator namespace ${OP_NS} (NEO-016)"
fi
log "Role ${ROLE} absent from operator namespace ${OP_NS} (NEO-016)"

# 2. No ClusterRoleBinding may grant the operator ServiceAccount cluster-wide access
#    on a default install.
assert_no_cluster_wide_grant "${OP_NS}" "${SA}"

log "No ClusterRoleBinding grants ${OP_NS}/${SA} — RBAC is namespace-scoped (AC-OP-SCOPE-SINGLE-004)"
