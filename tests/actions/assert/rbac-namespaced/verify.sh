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

# 1. Namespaced Role must exist in each watched namespace.
for ns in "${OP_NS}" "${WATCH_NS}"; do
  kubectl get role "${ROLE}" -n "${ns}" >/dev/null 2>&1 \
    || die "namespaced Role ${ROLE} missing in ${ns} — operator not scoped as expected"
  log "Role ${ROLE} present in ${ns}"
done

# 2. No ClusterRoleBinding may grant the operator ServiceAccount cluster-wide access.
#    (A namespaced operator must not appear as a subject in any ClusterRoleBinding.)
#    Flatten every CRB's subjects to "<crb> ServiceAccount/<ns>/<name>" lines and
#    look for our SA — plain jsonpath + grep, no jq dependency.
subject_needle="ServiceAccount/${OP_NS}/${SA}"
crb_lines="$(kubectl get clusterrolebindings -o jsonpath='{range .items[*]}{.metadata.name}{" "}{range .subjects[*]}{.kind}/{.namespace}/{.name}{"\n"}{end}{end}' 2>/dev/null || true)"
if grep -qF -- "${subject_needle}" <<<"${crb_lines}"; then
  offending="$(grep -F -- "${subject_needle}" <<<"${crb_lines}" || true)"
  die "operator SA ${OP_NS}/${SA} is bound cluster-wide via ClusterRoleBinding subject(s): ${offending}"
fi

log "No ClusterRoleBinding grants ${OP_NS}/${SA} — RBAC is namespace-scoped (AC-OP-SCOPE-SINGLE-004)"
