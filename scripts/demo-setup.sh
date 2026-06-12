#!/bin/bash
set -euo pipefail

# Neo4j Kubernetes Operator Demo Setup Script
# This script sets up the complete demo environment

# Colors for output
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly CYAN='\033[0;36m'
readonly WHITE='\033[1;37m'
readonly NC='\033[0m' # No Color

log_header() {
    echo
    echo -e "${WHITE}╔══════════════════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${WHITE}║${NC} ${CYAN}$1${NC} ${WHITE}║${NC}"
    echo -e "${WHITE}╚══════════════════════════════════════════════════════════════════════════════╝${NC}"
    echo
}

log_section() {
    echo
    echo -e "${YELLOW}▶ $1${NC}"
    echo -e "${YELLOW}$(printf '─%.0s' $(seq 1 ${#1}))${NC}"
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

main() {
    log_header "Neo4j Kubernetes Operator Demo Setup"

    log_info "This script will set up a complete demo environment including:"
    log_info "  • Fresh Kind development cluster (neo4j-operator-dev)"
    log_info "  • cert-manager with self-signed ClusterIssuer"
    log_info "  • Neo4j Kubernetes Operator"
    log_info "  • All prerequisites for the demo"
    echo

    # Detect existing clusters that would be destroyed
    local existing_clusters=""
    if kind get clusters 2>/dev/null | grep -q "neo4j-operator-dev"; then
        existing_clusters="${existing_clusters}  • neo4j-operator-dev (development cluster)\n"
    fi
    if kind get clusters 2>/dev/null | grep -q "neo4j-operator-test"; then
        existing_clusters="${existing_clusters}  • neo4j-operator-test (test cluster)\n"
    fi

    if [[ -n "${existing_clusters}" ]]; then
        echo -e "${YELLOW}[WARNING]${NC} The following Kind clusters will be DESTROYED:"
        echo -e "${existing_clusters}"
    fi

    # Check if running in automated mode
    if [[ "${SKIP_SETUP_CONFIRMATION:-false}" == "true" ]]; then
        log_info "Auto-proceeding with setup (SKIP_SETUP_CONFIRMATION=true)"
    else
        read -r -p "$(echo -e "${CYAN}Proceed with setup? [y/N]${NC} ")" response
        case "${response}" in
            [yY][eE][sS]|[yY])
                ;;
            *)
                log_info "Setup cancelled"
                exit 0
                ;;
        esac
    fi

    # Step 1: Destroy existing environment
    log_section "Cleaning Up Existing Environment"
    log_info "Destroying any existing dev and test clusters..."
    FORCE=true make dev-destroy 2>/dev/null || true
    make test-destroy 2>/dev/null || true
    sleep 2

    # Step 2: Create development cluster
    log_section "Creating Development Cluster"
    make dev-cluster

    # Step 3: Deploy operator using flexible setup
    log_section "Deploying Neo4j Operator"
    log_info "Using flexible operator setup (auto-detects available clusters)..."
    make operator-setup

    # Step 4: Verify setup
    log_section "Verifying Setup"

    log_info "Checking dev cluster access..."
    kubectl cluster-info --context kind-neo4j-operator-dev

    log_info "Checking cert-manager..."
    kubectl get clusterissuer ca-cluster-issuer

    # Detect which namespace the operator was deployed to
    local operator_namespace=""
    if kubectl get deployment neo4j-operator-controller-manager -n neo4j-operator-dev >/dev/null 2>&1; then
        operator_namespace="neo4j-operator-dev"
    elif kubectl get deployment neo4j-operator-controller-manager -n neo4j-operator-system >/dev/null 2>&1; then
        operator_namespace="neo4j-operator-system"
    fi

    if [[ -n "${operator_namespace}" ]]; then
        log_info "Checking operator deployment in ${operator_namespace}..."
        kubectl get deployment -n "${operator_namespace}"

        log_info "Verifying operator pods are running..."
        kubectl get pods -n "${operator_namespace}"
    else
        log_info "Operator namespace not detected — verify with: make operator-status"
    fi

    log_success "Demo environment setup complete!"
    echo
    log_info "Environment details:"
    log_info "  • Cluster: neo4j-operator-dev (Kind)"
    log_info "  • Context: kind-neo4j-operator-dev"
    log_info "  • Operator: Deployed in ${operator_namespace:-unknown} namespace"
    log_info "  • cert-manager: ClusterIssuer 'ca-cluster-issuer' ready"
    echo
    log_info "Ready to run the demo:"
    log_info "  • Interactive demo: make demo"
    log_info "  • Fast automated demo: make demo-fast"
    log_info "  • Direct script: ./scripts/demo.sh --help"
}

main "$@"
