#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT"

log_info "Setting up test environment for Neo4j Kubernetes Operator"
log_info "Project root: $PROJECT_ROOT"

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is required but not installed"
        exit 1
    fi

    # Check kind
    if ! command -v kind &> /dev/null; then
        log_warning "kind is not installed - some tests may fail"
    fi

    # Check make
    if ! command -v make &> /dev/null; then
        log_error "make is required but not installed"
        exit 1
    fi

    log_success "Prerequisites check completed"
}

# Detect cluster type and name
detect_cluster() {
    log_info "Detecting Kubernetes cluster..."

    # Check if we're in a kind cluster
    if command -v kind &> /dev/null; then
        CLUSTERS=$(kind get clusters 2>/dev/null || echo "")
        if [ -n "$CLUSTERS" ]; then
            CLUSTER_NAME=$(echo "$CLUSTERS" | head -n1)
            log_info "Found kind cluster: $CLUSTER_NAME"
            export KIND_CLUSTER="$CLUSTER_NAME"
            return 0
        fi
    fi

    # Check if we're in a different Kubernetes environment
    if kubectl cluster-info &> /dev/null; then
        log_info "Connected to Kubernetes cluster"
        kubectl cluster-info | head -n1
        return 0
    fi

    log_warning "No accessible Kubernetes cluster found"
    return 1
}

# Install CRDs
install_crds() {
    log_info "Installing Custom Resource Definitions..."

    # Check if CRDs are already installed
    if kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com &> /dev/null; then
        log_info "CRDs already installed"
        return 0
    fi

    # Install CRDs
    if make install &> /dev/null; then
        log_success "CRDs installed successfully"

        # Wait for CRDs to be ready
        log_info "Waiting for CRDs to be established..."
        kubectl wait --for=condition=established --timeout=60s crd/neo4jenterpriseclusters.neo4j.neo4j.com || true
        kubectl wait --for=condition=established --timeout=60s crd/neo4jbackups.neo4j.neo4j.com || true
        kubectl wait --for=condition=established --timeout=60s crd/neo4jrestores.neo4j.neo4j.com || true

        return 0
    else
        log_error "Failed to install CRDs"
        return 1
    fi
}

# Create test namespace
create_test_namespace() {
    local namespace="${1:-neo4j-operator-system}"

    log_info "Creating test namespace: $namespace"

    if kubectl get namespace "$namespace" &> /dev/null; then
        log_info "Namespace $namespace already exists"
        return 0
    fi

    kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
    log_success "Namespace $namespace created"
}

# Verify cluster health
verify_cluster_health() {
    log_info "Verifying cluster health..."

    # Check nodes
    local ready_nodes
    ready_nodes=$(kubectl get nodes --no-headers | grep -c "Ready" || echo "0")

    if [ "$ready_nodes" -eq 0 ]; then
        log_error "No ready nodes found in cluster"
        return 1
    fi

    log_info "Found $ready_nodes ready nodes"

    # Check API server
    if kubectl get --raw /healthz &> /dev/null; then
        log_success "API server is healthy"
    else
        log_error "API server health check failed"
        return 1
    fi

    return 0
}

# Setup test environment
setup_test_environment() {
    log_info "Setting up test environment..."

    # Check prerequisites
    check_prerequisites

    # Detect cluster
    if ! detect_cluster; then
        log_error "No accessible cluster found. Please ensure you have a Kubernetes cluster running."
        exit 1
    fi

    # Verify cluster health
    if ! verify_cluster_health; then
        log_error "Cluster health check failed"
        exit 1
    fi

    # Install CRDs
    if ! install_crds; then
        log_error "Failed to install CRDs"
        exit 1
    fi

    # Create test namespace
    create_test_namespace

    log_success "Test environment setup completed successfully"
}

# Cleanup function
cleanup() {
    log_info "Cleaning up test environment..."

    # Remove test namespace if it exists
    if kubectl get namespace neo4j-operator-system &> /dev/null; then
        kubectl delete namespace neo4j-operator-system --timeout=60s || true
    fi

    # Uninstall CRDs
    make uninstall ignore-not-found=true || true

    log_success "Cleanup completed"
}

# Main execution
main() {
    case "${1:-setup}" in
        "setup")
            setup_test_environment
            ;;
        "cleanup")
            cleanup
            ;;
        "check")
            check_prerequisites
            detect_cluster
            verify_cluster_health
            ;;
        *)
            echo "Usage: $0 {setup|cleanup|check}"
            echo "  setup   - Set up test environment (default)"
            echo "  cleanup - Clean up test environment"
            echo "  check   - Check prerequisites and cluster health"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
