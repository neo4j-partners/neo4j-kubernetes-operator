#!/usr/bin/env bash
# cleanup/operator-helm — remove the Helm release installed by operator/install-helm, then the
# namespaces it was given. The CRD is left in place: it is installed separately (make install),
# other suites depend on it, and the chart never owned it.
#
# Namespace deletes do not wait. A namespace only finishes terminating once every finalizer in it
# is released, and the controller that releases the Neo4j finalizer is the one being uninstalled
# here — blocking on that would trade a leftover namespace on a disposable cluster for a hung
# teardown.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

RELEASE="${OPERATOR_HELM_RELEASE:-neo4j-operator}"
RELEASE_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
WATCHED="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"

if command -v helm >/dev/null 2>&1; then
  log "Uninstalling release ${RELEASE} from ${RELEASE_NS}"
  helm uninstall "${RELEASE}" --namespace "${RELEASE_NS}" --wait --timeout 120s || true
fi

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  kubectl delete namespace "${ns}" --ignore-not-found --wait=false || true
done
kubectl delete namespace "${RELEASE_NS}" --ignore-not-found --wait=false || true

log "Helm operator cleanup done"
