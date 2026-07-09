#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

cd "${REPO_ROOT}"

log "Installing CRD"
make install

log "Applying operator namespace and RBAC"
kubectl apply -f "${REPO_ROOT}/${OPERATOR_NAMESPACE_MANIFEST}"
kubectl apply -k "${REPO_ROOT}/${OPERATOR_RBAC_KUSTOMIZE}"

log "Deploying operator (image: ${OPERATOR_IMAGE}, cloud: ${CLOUD_ID:-unset})"

manifest="$(kubectl kustomize "${REPO_ROOT}/${OPERATOR_MANAGER_KUSTOMIZE}")"
manifest="$(printf '%s\n' "${manifest}" | sed \
  -e "s|image: ${OPERATOR_MANAGER_IMAGE}|image: ${OPERATOR_IMAGE}|g" \
  -e "s|imagePullPolicy: IfNotPresent|imagePullPolicy: ${OPERATOR_IMAGE_PULL_POLICY:-IfNotPresent}|g")"

if [[ "${OPERATOR_LEADER_ELECT:-false}" == "false" ]]; then
  manifest="$(printf '%s\n' "${manifest}" | sed 's|- --leader-elect|- --leader-elect=false|g')"
fi

printf '%s\n' "${manifest}" | kubectl apply -f -
