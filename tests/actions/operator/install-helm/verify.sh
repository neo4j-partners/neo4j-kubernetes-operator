#!/usr/bin/env bash
# operator/install-helm — verify the chart delivered a working multi-namespace scope:
# the declared scope, the granted scope, and the fact that the controller actually started.
#
# The last check is the one that matters and it is not cosmetic. When a watched namespace has no
# Role, the reflectors there fail to list, the cache never syncs, and the manager stops before
# "Starting workers" — no namespace is reconciled any more, not even the ones with permissions,
# while the pod stays Ready and the Deployment Available. So readiness proves nothing on its own;
# "Starting workers" is the first line that proves every watched namespace is readable.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

RELEASE_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
DEPLOYMENT="${OPERATOR_DEPLOYMENT:-neo4j-operator-controller-manager}"
ROLE="${OPERATOR_ROLE:-neo4j-operator-manager-role}"
WATCHED="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
TIMEOUT="${E2E_OPERATOR_TIMEOUT:-180s}"
SELECTOR="${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}"

kubectl_wait_deployment "${RELEASE_NS}" "${DEPLOYMENT}" "${TIMEOUT}"

# 1. Declared scope: the chart keeps WATCH_NAMESPACE and watchNamespaces in the same order.
declared="$(kubectl get deployment "${DEPLOYMENT}" -n "${RELEASE_NS}" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="WATCH_NAMESPACE")].value}')"
[[ "${declared}" == "${WATCHED}" ]] \
  || die "WATCH_NAMESPACE is ${declared:-empty}, expected ${WATCHED}"
log "WATCH_NAMESPACE=${declared}"

# 2. Granted scope: one Role and one RoleBinding per watched namespace.
IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  kubectl get role "${ROLE}" -n "${ns}" >/dev/null 2>&1 \
    || die "chart rendered no Role ${ROLE} in watched namespace ${ns}"
  kubectl get rolebinding "${ROLE/-role/-rolebinding}" -n "${ns}" >/dev/null 2>&1 \
    || die "chart rendered no RoleBinding in watched namespace ${ns}"
  log "Role and RoleBinding present in ${ns}"
done

# 3. The controller reached its work loop, which only happens once every watched namespace
#    synced. Give it a budget: the lease has to be acquired first.
deadline=$((SECONDS + 120))
until kubectl logs -n "${RELEASE_NS}" -l "${SELECTOR}" --tail=-1 2>/dev/null \
  | grep -q "Starting workers"; do
  if [[ "${SECONDS}" -ge "${deadline}" ]]; then
    kubectl logs -n "${RELEASE_NS}" -l "${SELECTOR}" --tail=-1 >&2 2>/dev/null || true
    die "controller never reached 'Starting workers' — a watched namespace in ${WATCHED} is not readable, so nothing is reconciled anywhere"
  fi
  sleep 3
done

log "Controller started workers with ${#watched_list[@]} watched namespaces (OP-2-001-SCOPE-02)"
