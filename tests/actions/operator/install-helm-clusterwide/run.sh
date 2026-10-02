#!/usr/bin/env bash
# operator/install-helm-clusterwide — install the operator from the Helm chart in cluster-wide
# scope (OP-2-001-SCOPE-03, BDR-003 amendment): clusterWide=true renders a ClusterRole instead of
# per-namespace Roles and sets WATCH_ALL_NAMESPACES=true. No watchNamespaces list is passed.
#
# The workload namespaces are created here as PLAIN namespaces (no Role): the whole point of
# cluster-wide is that a namespace the operator was never told about is still reconciled. Applying
# CRs into them later therefore proves coverage the namespaced install cannot express.
#
# Inputs:
#   OPERATOR_IMAGE              — repo:tag of the controller image (per cloud case)
#   OPERATOR_IMAGE_PULL_POLICY  — Never on kind (image pre-loaded), IfNotPresent on a registry
#   OPERATOR_HELM_RELEASE       — release name
#   OPERATOR_HELM_NAMESPACE     — release namespace, dedicated to this suite
#   E2E_SCOPE_WATCHED_NAMESPACES — comma-separated workload namespaces to create (not passed to the chart)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

require_cmd helm

cd "${REPO_ROOT}"

RELEASE="${OPERATOR_HELM_RELEASE:-neo4j-operator}"
RELEASE_NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
CHART="${OPERATOR_HELM_CHART:-charts/neo4j-operator}"
WORKLOAD="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
IMAGE="${OPERATOR_IMAGE:?OPERATOR_IMAGE is required}"

# The chart takes repository and tag apart, the harness carries one reference.
IMAGE_REPO="${IMAGE%:*}"
IMAGE_TAG="${IMAGE##*:}"
[[ "${IMAGE_REPO}" != "${IMAGE}" ]] || die "OPERATOR_IMAGE must be repo:tag, got ${IMAGE}"

log "Installing CRD (server-side)"
make install

# Plain workload namespaces — no Role. Cluster-wide covers them by watching everything.
IFS=',' read -r -a workload_list <<<"${WORKLOAD}"
for ns in "${workload_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  log "Ensuring workload namespace ${ns}"
  kubectl create namespace "${ns}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
done

log "Installing chart ${CHART} as ${RELEASE} in ${RELEASE_NS} (clusterWide, image ${IMAGE})"
helm upgrade --install "${RELEASE}" "${CHART}" \
  --namespace "${RELEASE_NS}" --create-namespace \
  --set image.repository="${IMAGE_REPO}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy="${OPERATOR_IMAGE_PULL_POLICY:-IfNotPresent}" \
  --set clusterWide=true \
  --wait --timeout "${E2E_OPERATOR_TIMEOUT:-180s}"

log "Chart installed (cluster-wide)"
