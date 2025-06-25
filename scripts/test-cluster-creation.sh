#!/bin/bash

set -e

echo "=== Testing Kind Cluster Creation ==="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    local status=$1
    local message=$2
    case $status in
        "SUCCESS")
            echo -e "${GREEN}✅ $message${NC}"
            ;;
        "FAILURE")
            echo -e "${RED}❌ $message${NC}"
            ;;
        "WARNING")
            echo -e "${YELLOW}⚠️  $message${NC}"
            ;;
        "INFO")
            echo -e "ℹ️  $message"
            ;;
    esac
}

# Clean up any existing clusters
print_status "INFO" "Cleaning up any existing clusters..."
kind delete cluster --name neo4j-operator-test 2>/dev/null || true

# Test 1: Simple configuration (our primary method)
print_status "INFO" "Testing simple configuration..."
if kind create cluster --name neo4j-operator-test --config hack/kind-config-simple.yaml --image kindest/node:v1.30.0 --wait 10m; then
    print_status "SUCCESS" "Simple configuration works!"

    # Verify cluster is working
    print_status "INFO" "Verifying cluster functionality..."
    kubectl cluster-info --context kind-neo4j-operator-test
    kubectl get nodes -o wide

    # Clean up
    kind delete cluster --name neo4j-operator-test
    print_status "SUCCESS" "Simple configuration test completed successfully"
else
    print_status "FAILURE" "Simple configuration failed"
    exit 1
fi

# Test 2: Minimal cluster without configuration (fallback)
print_status "INFO" "Testing minimal cluster without configuration..."
if kind create cluster --name neo4j-operator-test --image kindest/node:v1.30.0 --wait 10m; then
    print_status "SUCCESS" "Minimal cluster without configuration works!"

    # Verify cluster is working
    print_status "INFO" "Verifying cluster functionality..."
    kubectl cluster-info --context kind-neo4j-operator-test
    kubectl get nodes -o wide

    # Clean up
    kind delete cluster --name neo4j-operator-test
    print_status "SUCCESS" "Minimal cluster test completed successfully"
else
    print_status "FAILURE" "Minimal cluster without configuration failed"
    exit 1
fi

print_status "SUCCESS" "All cluster creation tests passed!"
echo "=== Cluster Creation Tests Completed ==="
