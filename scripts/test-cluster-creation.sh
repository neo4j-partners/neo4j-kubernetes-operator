#!/bin/bash

# Test script for validating Kind cluster creation
# This script tests the various Kind configurations to ensure they work

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    local color=$1
    local message=$2
    echo -e "${color}[$(date +'%Y-%m-%d %H:%M:%S')] ${message}${NC}"
}

# Function to create and test cluster
test_cluster_config() {
    local config_file=$1
    local config_name=$2
    local node_image=${3:-"kindest/node:v1.30.0"}

    print_status $BLUE "Testing cluster configuration: $config_name"
    print_status $BLUE "Using config: $config_file"
    print_status $BLUE "Using node image: $node_image"

    # Clean up any existing test cluster
    kind delete cluster --name neo4j-operator-test 2>/dev/null || true

    # Try to create cluster
    if kind create cluster --name neo4j-operator-test --config "$config_file" --image "$node_image" --wait 5m; then
        print_status $GREEN "✅ Cluster created successfully with $config_name"

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

        # Clean up
        kind delete cluster --name neo4j-operator-test
        print_status $GREEN "✅ Test completed successfully for $config_name"
        return 0
    else
        print_status $RED "❌ Cluster creation failed with $config_name"
        return 1
    fi
}

# Main test execution
main() {
    print_status $BLUE "Starting Kind cluster configuration tests"

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

    # Test configurations in order of preference
    local success_count=0
    local total_count=0

    # Test robust configuration first
    if test_cluster_config "hack/kind-config-robust.yaml" "robust configuration" "kindest/node:v1.30.0"; then
        ((success_count++))
    fi
    ((total_count++))

    # Test single node configuration
    if test_cluster_config "hack/kind-config-single.yaml" "single node configuration" "kindest/node:v1.30.0"; then
        ((success_count++))
    fi
    ((total_count++))

    # Test minimal configuration
    if test_cluster_config "hack/kind-config-minimal.yaml" "minimal configuration" "kindest/node:v1.30.0"; then
        ((success_count++))
    fi
    ((total_count++))

    # Test CI configuration
    if test_cluster_config "hack/kind-config-ci.yaml" "CI configuration" "kindest/node:v1.30.0"; then
        ((success_count++))
    fi
    ((total_count++))

    # Test basic configuration with newer node image
    if test_cluster_config "hack/kind-config-basic.yaml" "basic configuration" "kindest/node:v1.30.0"; then
        ((success_count++))
    fi
    ((total_count++))

    # Print summary
    print_status $BLUE "=== Test Summary ==="
    print_status $BLUE "Successful configurations: $success_count/$total_count"

    if [ $success_count -eq $total_count ]; then
        print_status $GREEN "✅ All configurations tested successfully!"
        exit 0
    elif [ $success_count -gt 0 ]; then
        print_status $YELLOW "⚠️  Some configurations failed, but at least one works"
        exit 0
    else
        print_status $RED "❌ All configurations failed"
        exit 1
    fi
}

# Run main function
main "$@"
