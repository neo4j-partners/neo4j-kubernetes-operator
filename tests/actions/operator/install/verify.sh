#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

TIMEOUT="${E2E_OPERATOR_TIMEOUT}"

kubectl get crd "${OPERATOR_CRD}" >/dev/null \
  || die "CRD ${OPERATOR_CRD} not found (apiVersion ${NEO4J_API_VERSION})"

log "Waiting for Deployment/${OPERATOR_DEPLOYMENT} in ${OPERATOR_NAMESPACE} (timeout ${TIMEOUT})"

if ! kubectl rollout status "deployment/${OPERATOR_DEPLOYMENT}" \
  -n "${OPERATOR_NAMESPACE}" --timeout="${TIMEOUT}"; then
  log "Operator deployment not ready — diagnostics:"
  kubectl get deployment,pods -n "${OPERATOR_NAMESPACE}" -l "${OPERATOR_LABEL_SELECTOR}" -o wide >&2 || true
  kubectl describe deployment "${OPERATOR_DEPLOYMENT}" -n "${OPERATOR_NAMESPACE}" >&2 || true
  kubectl describe pods -n "${OPERATOR_NAMESPACE}" -l "${OPERATOR_LABEL_SELECTOR}" >&2 || true
  kubectl logs -n "${OPERATOR_NAMESPACE}" -l "${OPERATOR_LABEL_SELECTOR}" --tail=80 >&2 || true
  die "operator Deployment/${OPERATOR_DEPLOYMENT} failed to become ready"
fi

ready=$(kubectl get deployment "${OPERATOR_DEPLOYMENT}" -n "${OPERATOR_NAMESPACE}" \
  -o jsonpath='{.status.readyReplicas}')
[[ "${ready:-0}" -ge 1 ]] || die "operator deployment has no ready replicas"

log "Operator is ready (${NEO4J_API_VERSION} controller in ${OPERATOR_NAMESPACE})"
