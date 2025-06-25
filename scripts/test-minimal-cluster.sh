#!/bin/bash

# Minimal test script for Kind cluster creation
# This script creates the simplest possible Kind cluster to test basic functionality

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
    print_status $BLUE "Testing minimal Kind cluster creation"

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

    # Create minimal cluster without any configuration
    print_status $BLUE "Creating minimal cluster without configuration..."

    if kind create cluster --name neo4j-operator-test --image kindest/node:v1.30.0 --wait 5m; then
        print_status $GREEN "✅ Minimal cluster created successfully!"

        # Test basic functionality
        print_status $BLUE "Testing cluster functionality..."

        # Wait for nodes to be ready
        kubectl wait --for=condition=ready nodes --all --timeout=60s || {
            print_status $YELLOW "⚠️  Nodes not ready within timeout"
        }

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

        print_status $GREEN "✅ Minimal test completed successfully!"
        exit 0
    else
        print_status $RED "❌ Minimal cluster creation failed"

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
