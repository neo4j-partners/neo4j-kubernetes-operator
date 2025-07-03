#!/bin/bash
# Simple test environment manager for Neo4j Operator

set -euo pipefail

CLUSTER_NAME="neo4j-operator-test"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

cluster() {
    log "Setting up test cluster..."

    # Clean up any existing cluster
    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log "Deleting existing cluster: ${CLUSTER_NAME}"
        kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
    fi

    # Create new cluster
    log "Creating cluster: ${CLUSTER_NAME}"
    kind create cluster --name "${CLUSTER_NAME}" --wait 10m

    # Export kubeconfig
    kind export kubeconfig --name "${CLUSTER_NAME}"

    # Install cert-manager
    log "Installing cert-manager..."
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s

    log "Test cluster ready!"
}

setup() {
    log "Setting up test environment..."

    # Create directories
    mkdir -p "${PROJECT_ROOT}/test-results"
    mkdir -p "${PROJECT_ROOT}/coverage"
    mkdir -p "${PROJECT_ROOT}/logs"

    # Generate manifests
    cd "${PROJECT_ROOT}"
    make manifests

    log "Test environment setup complete!"
}

cleanup() {
    log "Cleaning up test environment..."

    # Clean up directories
    rm -rf "${PROJECT_ROOT}/test-results"
    rm -rf "${PROJECT_ROOT}/coverage"
    rm -rf "${PROJECT_ROOT}/logs"
    rm -rf "${PROJECT_ROOT}/tmp"

    # Clean up files
    rm -f "${PROJECT_ROOT}/test-output.log"
    rm -f "${PROJECT_ROOT}/coverage-*.out"
    rm -f "${PROJECT_ROOT}/coverage-*.html"

    # Delete test cluster
    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log "Deleting test cluster: ${CLUSTER_NAME}"
        kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
    fi

    log "Test environment cleanup complete!"
}

clean_cluster() {
    log "Cleaning test cluster resources..."

    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log "Switching to test cluster context..."
        kind export kubeconfig --name "${CLUSTER_NAME}"

        log "Removing operator deployment..."
        kubectl delete namespace neo4j-operator-system --ignore-not-found=true --timeout=60s

        log "Removing test resources..."
        kubectl delete namespace neo4j --ignore-not-found=true --timeout=60s

        log "Removing CRDs..."
        kubectl delete crd --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true

        log "Test cluster resources cleaned!"
    else
        log "Test cluster not found, skipping cleanup"
    fi
}

case "${1:-}" in
    cluster)
        cluster
        ;;
    setup)
        setup
        ;;
    cleanup)
        cleanup
        ;;
    clean-cluster)
        clean_cluster
        ;;
    *)
        echo "Usage: $0 {cluster|setup|cleanup|clean-cluster}"
        exit 1
        ;;
esac
