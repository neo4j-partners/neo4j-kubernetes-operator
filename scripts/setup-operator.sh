#!/bin/bash
# Simple operator setup script for Neo4j Operator

set -euo pipefail

CLUSTER_NAME="neo4j-operator-test"
NAMESPACE="neo4j-operator-system"
OPERATOR_IMAGE="neo4j-operator:test"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

setup() {
    log "Setting up operator..."

    # Check if test cluster exists
    if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log "Test cluster not found. Creating..."
        make test-cluster
    fi

    # Export kubeconfig
    kind export kubeconfig --name "${CLUSTER_NAME}"

    # Build and load operator image
    log "Building operator image..."
    make docker-build IMG="${OPERATOR_IMAGE}"
    kind load docker-image "${OPERATOR_IMAGE}" --name "${CLUSTER_NAME}"

    # Deploy operator
    log "Deploying operator..."
    make deploy IMG="${OPERATOR_IMAGE}"

    # Wait for operator to be ready
    log "Waiting for operator to be ready..."
    kubectl wait --for=condition=available deployment/neo4j-operator-controller-manager -n "${NAMESPACE}" --timeout=300s

    log "Operator setup complete!"
}

status() {
    log "Checking operator status..."

    echo "=== Pods ==="
    kubectl get pods -n "${NAMESPACE}" || echo "No pods found"
    echo ""
    echo "=== Deployments ==="
    kubectl get deployments -n "${NAMESPACE}" || echo "No deployments found"
    echo ""
    echo "=== Services ==="
    kubectl get services -n "${NAMESPACE}" || echo "No services found"
}

logs() {
    log "Following operator logs..."
    kubectl logs -n "${NAMESPACE}" deployment/neo4j-operator-controller-manager -f
}

case "${1:-}" in
    setup)
        setup
        ;;
    status)
        status
        ;;
    logs)
        logs
        ;;
    *)
        echo "Usage: $0 {setup|status|logs}"
        exit 1
        ;;
esac
