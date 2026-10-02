#!/usr/bin/env bash
# operator/install-helm-clusterwide — verify the chart delivered a working cluster-wide scope:
# the declared scope (WATCH_ALL_NAMESPACES, empty WATCH_NAMESPACE), the cluster-scoped grant
# (ClusterRole + ClusterRoleBinding), and the fact that the controller actually started.
#
# As with the namespaced install, "Starting workers" is the check that matters: a cluster-wide
# cache that cannot list some resource never syncs and the manager stops before its work loop,
# while the pod stays Ready. Readiness proves nothing on its own.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

RELEASE_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
DEPLOYMENT="${OPERATOR_DEPLOYMENT:-neo4j-operator-controller-manager}"
ROLE="${OPERATOR_ROLE:-neo4j-operator-manager-role}"
TIMEOUT="${E2E_OPERATOR_TIMEOUT:-180s}"
SELECTOR="${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}"

kubectl_wait_deployment "${RELEASE_NS}" "${DEPLOYMENT}" "${TIMEOUT}"

# 1. Declared scope: WATCH_ALL_NAMESPACES=true and WATCH_NAMESPACE must be absent/empty.
env_json="$(kubectl get deployment "${DEPLOYMENT}" -n "${RELEASE_NS}" \
  -o jsonpath='{.spec.template.spec.containers[0].env[*].name}')"
all_ns="$(kubectl get deployment "${DEPLOYMENT}" -n "${RELEASE_NS}" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="WATCH_ALL_NAMESPACES")].value}')"
[[ "${all_ns}" == "true" ]] || die "WATCH_ALL_NAMESPACES is ${all_ns:-empty}, expected true"
if grep -qw "WATCH_NAMESPACE" <<<"${env_json}"; then
  die "WATCH_NAMESPACE must not be set in cluster-wide mode"
fi
log "WATCH_ALL_NAMESPACES=true, WATCH_NAMESPACE unset"

# 2. Granted scope: a cluster-scoped ClusterRole + ClusterRoleBinding, not per-namespace Roles.
kubectl get clusterrole "${ROLE}" >/dev/null 2>&1 \
  || die "chart rendered no ClusterRole ${ROLE} in cluster-wide mode"
kubectl get clusterrolebinding "${ROLE/-role/-rolebinding}" >/dev/null 2>&1 \
  || die "chart rendered no ClusterRoleBinding in cluster-wide mode"
log "ClusterRole and ClusterRoleBinding ${ROLE} present"

# 3. The controller reached its work loop, which only happens once the cluster-wide cache synced.
deadline=$((SECONDS + 120))
until kubectl logs -n "${RELEASE_NS}" -l "${SELECTOR}" --tail=-1 2>/dev/null \
  | grep -q "Starting workers"; do
  if [[ "${SECONDS}" -ge "${deadline}" ]]; then
    kubectl logs -n "${RELEASE_NS}" -l "${SELECTOR}" --tail=-1 >&2 2>/dev/null || true
    die "controller never reached 'Starting workers' — the cluster-wide cache is not syncing"
  fi
  sleep 3
done

log "Controller started workers in cluster-wide scope (OP-2-001-SCOPE-03)"
