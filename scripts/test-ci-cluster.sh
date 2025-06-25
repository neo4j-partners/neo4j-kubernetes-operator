#!/bin/bash

# Test script for CI cluster creation
# This script tests the cluster creation with the fixes applied

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    local color=$1
    local message=$2
    echo -e "${color}[$(date +'%Y-%m-%d %H:%M:%S')] ${message}${NC}"
}

main() {
    print_status $BLUE "Testing CI cluster creation with fixes"

    # Check prerequisites
    if ! command -v kind &> /dev/null; then
        print_status $RED "Kind is not installed"
        exit 1
    fi

    if ! command -v kubectl &> /dev/null; then
        print_status $RED "kubectl is not installed"
        exit 1
    fi

    if ! command -v docker &> /dev/null; then
        print_status $RED "Docker is not installed"
        exit 1
    fi

    print_status $GREEN "Prerequisites check passed"

    # Clean up any existing cluster
    print_status $BLUE "Cleaning up any existing test cluster..."
    kind delete cluster --name neo4j-operator-test 2>/dev/null || true

    # Test the robust configuration first (most likely to work)
    print_status $BLUE "Testing robust configuration with v1.30.0 node image..."

    if kind create cluster --name neo4j-operator-test --config hack/kind-config-robust.yaml --image kindest/node:v1.30.0 --wait 10m; then
        print_status $GREEN "✅ Cluster created successfully!"

        # Test basic functionality
        print_status $BLUE "Testing cluster functionality..."

        # Wait for nodes to be ready
        kubectl wait --for=condition=ready nodes --all --timeout=120s || {
            print_status $YELLOW "⚠️  Nodes not ready within timeout"
        }

        # Check kubelet health
        if docker exec neo4j-operator-test-control-plane curl -s http://localhost:10248/healthz >/dev/null 2>&1; then
            print_status $GREEN "✅ Kubelet is healthy"
        else
            print_status $YELLOW "⚠️  Kubelet health check failed"
        fi

        # Test API server
        if kubectl get nodes >/dev/null 2>&1; then
            print_status $GREEN "✅ API server is responding"
        else
            print_status $YELLOW "⚠️  API server not responding"
        fi

        # Show cluster info
        print_status $BLUE "Cluster information:"
        kubectl cluster-info
        kubectl get nodes -o wide

        # Clean up
        print_status $BLUE "Cleaning up test cluster..."
        kind delete cluster --name neo4j-operator-test

        print_status $GREEN "✅ Test completed successfully!"
        exit 0
    else
        print_status $RED "❌ Cluster creation failed"

        # Show debugging information
        print_status $BLUE "=== Debugging information ==="
        echo "Docker containers:"
        docker ps -a
        echo "Docker system info:"
        docker system df
        echo "Available memory:"
        free -h || echo "free command not available"
        echo "Cgroups information:"
        ls -la /sys/fs/cgroup/ || echo "Cannot access cgroups"
        echo "=== End debugging ==="

        exit 1
    fi
}

main "$@"
