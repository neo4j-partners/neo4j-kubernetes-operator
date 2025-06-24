#!/bin/bash

# Cluster Availability Check Script
# This script checks if a Kubernetes cluster is available and ready for testing

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
CLUSTER_TYPE="auto"
VERBOSE=false
TIMEOUT=30

# Function to print colored output
print_status() {
    local color=$1
    local message=$2
    echo -e "${color}[$(date +'%Y-%m-%d %H:%M:%S')] ${message}${NC}"
}

# Function to print usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Cluster Availability Check Script

OPTIONS:
    -t, --type TYPE        Cluster type to check (kind, openshift, remote, auto)
    -v, --verbose          Enable verbose output
    --timeout SECONDS      Timeout for cluster checks (default: 30)
    -h, --help             Show this help message

EXIT CODES:
    0 - Cluster is available and ready
    1 - Cluster is not available or not ready
    2 - Invalid arguments or configuration

EXAMPLES:
    # Check any available cluster
    $0 --verbose

    # Check specific cluster type
    $0 --type kind --verbose

    # Use in scripts
    if $0 --type kind; then
        echo "Cluster is ready"
    else
        echo "Cluster is not ready"
    fi

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--type)
            CLUSTER_TYPE="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        --timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            print_status $RED "Unknown option: $1"
            usage
            exit 2
            ;;
    esac
done

# Function to detect cluster type
detect_cluster_type() {
    if command -v oc &> /dev/null && [ -n "${OPENSHIFT_SERVER:-}" ] && [ -n "${OPENSHIFT_TOKEN:-}" ]; then
        echo "openshift"
    elif command -v kind &> /dev/null && kind get clusters | grep -q "neo4j-operator"; then
        echo "kind"
    elif command -v kubectl &> /dev/null && kubectl cluster-info &> /dev/null; then
        echo "remote"
    else
        echo "none"
    fi
}

# Function to check kubectl availability
check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        print_status $RED "kubectl is not installed or not in PATH"
        return 1
    fi

    if [ "$VERBOSE" = true ]; then
        # Use a more compatible version check
        kubectl_version=$(kubectl version --client 2>/dev/null | grep -o 'GitVersion:"[^"]*"' | cut -d'"' -f2 || echo "unknown")
        print_status $GREEN "kubectl is available: $kubectl_version"
    fi
    return 0
}

# Function to check kind cluster
check_kind_cluster() {
    if ! command -v kind &> /dev/null; then
        print_status $RED "kind is not installed"
        return 1
    fi

    local clusters=$(kind get clusters 2>/dev/null | grep "neo4j-operator" || true)
    if [ -z "$clusters" ]; then
        print_status $YELLOW "No Neo4j operator Kind clusters found"
        return 1
    fi

    if [ "$VERBOSE" = true ]; then
        print_status $BLUE "Found Kind clusters: $clusters"
    fi

    # Check if any cluster is ready
    for cluster in $clusters; do
        if kubectl config use-context "kind-$cluster" &> /dev/null; then
            if kubectl cluster-info &> /dev/null; then
                if [ "$VERBOSE" = true ]; then
                    print_status $GREEN "Kind cluster '$cluster' is available"
                fi
                return 0
            fi
        fi
    done

    print_status $RED "No Kind clusters are ready"
    return 1
}

# Function to check OpenShift cluster
check_openshift_cluster() {
    if ! command -v oc &> /dev/null; then
        print_status $RED "OpenShift CLI (oc) is not installed"
        return 1
    fi

    if [ -z "${OPENSHIFT_SERVER:-}" ] || [ -z "${OPENSHIFT_TOKEN:-}" ]; then
        print_status $YELLOW "OpenShift credentials not configured"
        return 1
    fi

    if [ "$VERBOSE" = true ]; then
        print_status $BLUE "Checking OpenShift cluster connectivity..."
    fi

    # Test connectivity without actually logging in
    if oc login --token="$OPENSHIFT_TOKEN" --server="$OPENSHIFT_SERVER" --dry-run &> /dev/null; then
        if [ "$VERBOSE" = true ]; then
            print_status $GREEN "OpenShift cluster is accessible"
        fi
        return 0
    else
        print_status $RED "OpenShift cluster is not accessible"
        return 1
    fi
}

# Function to check remote cluster
check_remote_cluster() {
    if ! kubectl cluster-info &> /dev/null; then
        print_status $RED "Remote cluster is not accessible"
        return 1
    fi

    if [ "$VERBOSE" = true ]; then
        print_status $BLUE "Checking remote cluster readiness..."
        kubectl cluster-info
    fi

    # Check if cluster is ready
    if kubectl wait --for=condition=ready nodes --all --timeout=${TIMEOUT}s &> /dev/null; then
        if [ "$VERBOSE" = true ]; then
            print_status $GREEN "Remote cluster is ready"
        fi
        return 0
    else
        print_status $RED "Remote cluster is not ready"
        return 1
    fi
}

# Function to check cluster health
check_cluster_health() {
    local cluster_type=$1

    if [ "$VERBOSE" = true ]; then
        print_status $BLUE "Checking cluster health..."
    fi

    case $cluster_type in
        "kind"|"remote")
            # Check API server health
            if ! kubectl get --raw /healthz &> /dev/null; then
                print_status $RED "API server is not healthy"
                return 1
            fi

            # Check if there are any nodes
            local node_count=$(kubectl get nodes --no-headers 2>/dev/null | wc -l || echo "0")
            if [ "$node_count" -eq 0 ]; then
                print_status $RED "No nodes found in cluster"
                return 1
            fi

            if [ "$VERBOSE" = true ]; then
                print_status $GREEN "Cluster health check passed ($node_count nodes)"
            fi
            ;;
        "openshift")
            # For OpenShift, we just check if we can access the API
            if [ "$VERBOSE" = true ]; then
                print_status $GREEN "OpenShift cluster health check passed"
            fi
            ;;
    esac

    return 0
}

# Main execution
main() {
    if [ "$VERBOSE" = true ]; then
        print_status $BLUE "Starting cluster availability check"
    fi

    # Check kubectl availability
    if ! check_kubectl; then
        exit 1
    fi

    # Auto-detect cluster type if not specified
    if [ "$CLUSTER_TYPE" = "auto" ]; then
        CLUSTER_TYPE=$(detect_cluster_type)
        if [ "$VERBOSE" = true ]; then
            print_status $BLUE "Auto-detected cluster type: $CLUSTER_TYPE"
        fi
    fi

    # Check cluster based on type
    case $CLUSTER_TYPE in
        "kind")
            if ! check_kind_cluster; then
                exit 1
            fi
            if ! check_cluster_health "kind"; then
                exit 1
            fi
            ;;
        "openshift")
            if ! check_openshift_cluster; then
                exit 1
            fi
            if ! check_cluster_health "openshift"; then
                exit 1
            fi
            ;;
        "remote")
            if ! check_remote_cluster; then
                exit 1
            fi
            if ! check_cluster_health "remote"; then
                exit 1
            fi
            ;;
        "none")
            print_status $YELLOW "No cluster detected"
            exit 1
            ;;
        *)
            print_status $RED "Unsupported cluster type: $CLUSTER_TYPE"
            exit 2
            ;;
    esac

    if [ "$VERBOSE" = true ]; then
        print_status $GREEN "Cluster availability check completed successfully!"
    fi
    exit 0
}

# Run main function
main "$@"
