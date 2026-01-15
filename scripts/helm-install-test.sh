#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

RELEASE_NAME="${RELEASE_NAME:-neo4j-operator}"
NAMESPACE="${NAMESPACE:-neo4j-operator-system}"
CHART_DIR="${CHART_DIR:-${PROJECT_ROOT}/charts/neo4j-operator}"
KIND_CLUSTER="${KIND_CLUSTER:-neo4j-operator-test}"
SKIP_CLEANUP="${SKIP_CLEANUP:-false}"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

cleanup() {
    if [[ "${SKIP_CLEANUP}" == "true" ]]; then
        log "Skipping cleanup (SKIP_CLEANUP=true)"
        return
    fi

    log "Cleaning up Helm release..."
    helm uninstall "${RELEASE_NAME}" --namespace "${NAMESPACE}" >/dev/null 2>&1 || true
    kubectl delete namespace "${NAMESPACE}" --ignore-not-found=true --timeout=120s >/dev/null 2>&1 || true
}

trap cleanup EXIT

for cmd in helm kubectl kind; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "Required command not found: ${cmd}" >&2
        exit 1
    fi
done

if ! kind get clusters | grep -q "^${KIND_CLUSTER}$"; then
    log "Kind cluster ${KIND_CLUSTER} not found; creating..."
    make test-cluster
fi

log "Switching to Kind cluster ${KIND_CLUSTER}"
kind export kubeconfig --name "${KIND_CLUSTER}"

log "Linting Helm chart"
helm lint "${CHART_DIR}"

log "Installing Helm chart"
helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" \
    --namespace "${NAMESPACE}" \
    --create-namespace

log "Waiting for operator deployment"
kubectl wait --for=condition=available deployment \
    -l app.kubernetes.io/instance="${RELEASE_NAME}" \
    -n "${NAMESPACE}" \
    --timeout=300s

log "Verifying CRDs"
crds=(
    neo4jbackups.neo4j.neo4j.com
    neo4jdatabases.neo4j.neo4j.com
    neo4jenterpriseclusters.neo4j.neo4j.com
    neo4jenterprisestandalones.neo4j.neo4j.com
    neo4jplugins.neo4j.neo4j.com
    neo4jrestores.neo4j.neo4j.com
    neo4jshardeddatabases.neo4j.neo4j.com
)

for crd in "${crds[@]}"; do
    kubectl get crd "${crd}" >/dev/null
    log "Found CRD: ${crd}"
done

log "Helm chart installation verified"
