#!/usr/bin/env bash
# operator/install-helm — install the operator from the Helm chart with more than one watched
# namespace (OP-2-001-SCOPE-02, chart install OP-2-001-PKG-02).
#
# Two reasons this exists next to operator/install rather than replacing it:
#   - the kustomize manifests hard-code WATCH_NAMESPACE=default and ship a single Role, so a
#     multi-namespace scope can only be expressed through the chart's watchNamespaces value;
#   - the chart is the install path the user guide recommends, and nothing else in the harness
#     exercises it.
#
# Inputs:
#   OPERATOR_IMAGE              — repo:tag of the controller image (per cloud case)
#   OPERATOR_IMAGE_PULL_POLICY  — Never on kind (image pre-loaded), IfNotPresent on a registry
#   OPERATOR_HELM_RELEASE       — release name
#   OPERATOR_HELM_NAMESPACE     — release namespace, dedicated to this suite
#   E2E_SCOPE_NAMESPACES        — comma-separated watched namespaces
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

require_cmd helm

cd "${REPO_ROOT}"

RELEASE="${OPERATOR_HELM_RELEASE:-neo4j-operator}"
RELEASE_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
CHART="${OPERATOR_HELM_CHART:-charts/neo4j-operator}"
WATCHED="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
IMAGE="${OPERATOR_IMAGE:?OPERATOR_IMAGE is required}"

# The chart takes repository and tag apart, the harness carries one reference.
IMAGE_REPO="${IMAGE%:*}"
IMAGE_TAG="${IMAGE##*:}"
[[ "${IMAGE_REPO}" != "${IMAGE}" ]] || die "OPERATOR_IMAGE must be repo:tag, got ${IMAGE}"

log "Installing CRD (server-side)"
make install

# Helm creates its release namespace but never a namespace it is asked to put a Role in: a
# rendered Namespace object that already exists fails the install on ownership metadata, so the
# chart cannot own these. Creating them here is what a platform team does before installing.
IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  log "Ensuring watched namespace ${ns}"
  kubectl create namespace "${ns}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
done

log "Installing chart ${CHART} as ${RELEASE} in ${RELEASE_NS} (watching ${WATCHED}, image ${IMAGE})"
helm upgrade --install "${RELEASE}" "${CHART}" \
  --namespace "${RELEASE_NS}" --create-namespace \
  --set image.repository="${IMAGE_REPO}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy="${OPERATOR_IMAGE_PULL_POLICY:-IfNotPresent}" \
  --set "watchNamespaces={${WATCHED}}" \
  --wait --timeout "${E2E_OPERATOR_TIMEOUT:-180s}"

log "Chart installed"
