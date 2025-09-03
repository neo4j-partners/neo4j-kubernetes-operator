#!/bin/bash
# Flexible operator setup script for Neo4j Operator
# Automatically detects available clusters and deploys accordingly

set -euo pipefail

# Configuration
DEV_CLUSTER="neo4j-operator-dev"
TEST_CLUSTER="neo4j-operator-test"
OPERATOR_IMAGE_BASE="neo4j-operator"

# Colors for output
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly RED='\033[0;31m'
readonly NC='\033[0m' # No Color

log() {
    echo -e "${BLUE}[$(date '+%H:%M:%S')]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[$(date '+%H:%M:%S')]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[$(date '+%H:%M:%S')]${NC} $1"
}

log_error() {
    echo -e "${RED}[$(date '+%H:%M:%S')]${NC} $1"
}

# Detect available Kind clusters
detect_clusters() {
    local available_clusters=()

    # Check for dev cluster
    if kind get clusters 2>/dev/null | grep -q "^${DEV_CLUSTER}$"; then
        available_clusters+=("${DEV_CLUSTER}")
    fi

    # Check for test cluster
    if kind get clusters 2>/dev/null | grep -q "^${TEST_CLUSTER}$"; then
        available_clusters+=("${TEST_CLUSTER}")
    fi

    if [[ ${#available_clusters[@]} -eq 0 ]]; then
        return 1
    fi

    # Return clusters as space-separated string for compatibility
    echo "${available_clusters[*]}"
}

# Log cluster detection results
log_cluster_detection() {
    local clusters_output="$1"

    log "Detecting available Kind clusters..."

    if [[ -z "${clusters_output}" ]]; then
        log_warning "No Neo4j operator clusters found"
        log "Available clusters:"
        kind get clusters 2>/dev/null | sed 's/^/  • /' || echo "  • None"
        return 1
    fi

    # Log found clusters
    read -ra clusters <<< "${clusters_output}"
    for cluster in "${clusters[@]}"; do
        if [[ "${cluster}" == "${DEV_CLUSTER}" ]]; then
            log_success "Found development cluster: ${cluster}"
        elif [[ "${cluster}" == "${TEST_CLUSTER}" ]]; then
            log_success "Found test cluster: ${cluster}"
        fi
    done

    return 0
}

# Deploy operator to a specific cluster
deploy_to_cluster() {
    local cluster_name=$1
    local image_tag
    local namespace

    # Determine image tag and namespace based on cluster type
    if [[ "${cluster_name}" == "${DEV_CLUSTER}" ]]; then
        image_tag="dev"
        namespace="neo4j-operator-dev"
    elif [[ "${cluster_name}" == "${TEST_CLUSTER}" ]]; then
        image_tag="test"
        namespace="neo4j-operator-system"
    else
        image_tag="latest"
        namespace="neo4j-operator-system"
    fi

    local operator_image="${OPERATOR_IMAGE_BASE}:${image_tag}"

    log "Deploying operator to cluster: ${cluster_name}"
    log "Using image: ${operator_image}"

    # Switch to cluster context
    log "Switching to cluster context..."
    kind export kubeconfig --name "${cluster_name}"

    # Verify cluster access
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot access cluster ${cluster_name}"
        return 1
    fi

    # Check if operator is already deployed
    if kubectl get deployment neo4j-operator-controller-manager -n "${namespace}" >/dev/null 2>&1; then
        log_warning "Operator already deployed to ${cluster_name}"
        if ! confirm "Redeploy operator to ${cluster_name}?"; then
            log "Skipping deployment to ${cluster_name}"
            return 0
        fi

        log "Removing existing operator deployment..."
        kubectl delete deployment neo4j-operator-controller-manager -n "${namespace}" --ignore-not-found=true
        kubectl wait --for=delete deployment/neo4j-operator-controller-manager -n "${namespace}" --timeout=60s || true
    fi

    # Build operator image
    log "Building operator image: ${operator_image}"
    make docker-build IMG="${operator_image}"

    # Load image to cluster
    log "Loading image to cluster: ${cluster_name}"
    kind load docker-image "${operator_image}" --name "${cluster_name}"

    # Deploy operator
    log "Deploying operator to ${cluster_name}..."
    if [[ "${image_tag}" == "dev" ]]; then
        make deploy-dev
    else
        make deploy-prod
    fi

    # Wait for operator to be ready
    log "Waiting for operator to be ready..."
    kubectl wait --for=condition=available deployment/neo4j-operator-controller-manager -n "${namespace}" --timeout=300s

    # Verify deployment
    log "Verifying deployment..."
    kubectl get pods -n "${namespace}"

    log_success "Operator successfully deployed to ${cluster_name}!"
}

# Confirmation helper
confirm() {
    # Skip confirmation if in automated mode
    if [[ "${SKIP_OPERATOR_CONFIRMATION:-false}" == "true" ]]; then
        log "Auto-confirming (SKIP_OPERATOR_CONFIRMATION=true)"
        return 0
    fi

    local response
    read -r -p "$1 [y/N] " response
    case "${response}" in
        [yY][eE][sS]|[yY])
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

# Setup operator on available clusters
setup() {
    log "🔧 Setting up Neo4j operator..."

    # Detect available clusters
    local clusters_output
    clusters_output=$(detect_clusters)

    # Log detection results
    if ! log_cluster_detection "${clusters_output}"; then
        log_error "No suitable clusters found for operator deployment"
        log "Available options:"
        log "  • Create dev cluster: make dev-cluster"
        log "  • Create test cluster: make test-cluster"
        exit 1
    fi

    # Convert space-separated string to array
    read -ra clusters <<< "${clusters_output}"

    local cluster_count=${#clusters[@]}
    log "Found ${cluster_count} suitable cluster(s): ${clusters[*]}"

    # Deploy based on available clusters
    if [[ ${cluster_count} -eq 1 ]]; then
        log "Deploying to the only available cluster: ${clusters[0]}"
        deploy_to_cluster "${clusters[0]}"
    else
        # Automated mode: prefer dev cluster, fallback to test, or deploy to both
        if [[ "${SKIP_OPERATOR_CONFIRMATION:-false}" == "true" ]]; then
            # Prioritize dev cluster in automated mode
            if [[ " ${clusters[*]} " =~ " ${DEV_CLUSTER} " ]]; then
                log "Auto-deploying to development cluster (preferred for demos)"
                deploy_to_cluster "${DEV_CLUSTER}"
            elif [[ " ${clusters[*]} " =~ " ${TEST_CLUSTER} " ]]; then
                log "Auto-deploying to test cluster (dev cluster not available)"
                deploy_to_cluster "${TEST_CLUSTER}"
            else
                log "Deploying to all available clusters..."
                for cluster in "${clusters[@]}"; do
                    echo
                    deploy_to_cluster "${cluster}"
                done
            fi
        else
            # Interactive mode
            log "Multiple clusters available. Choose deployment strategy:"
            echo "  1. Deploy to development cluster only (${DEV_CLUSTER})"
            echo "  2. Deploy to test cluster only (${TEST_CLUSTER})"
            echo "  3. Deploy to both clusters"
            echo "  4. Cancel"

            read -r -p "Choose option [1-4]: " choice
            case "${choice}" in
                1)
                    if [[ " ${clusters[*]} " =~ " ${DEV_CLUSTER} " ]]; then
                        deploy_to_cluster "${DEV_CLUSTER}"
                    else
                        log_error "Development cluster not available"
                        exit 1
                    fi
                    ;;
                2)
                    if [[ " ${clusters[*]} " =~ " ${TEST_CLUSTER} " ]]; then
                        deploy_to_cluster "${TEST_CLUSTER}"
                    else
                        log_error "Test cluster not available"
                        exit 1
                    fi
                    ;;
                3)
                    log "Deploying to all available clusters..."
                    for cluster in "${clusters[@]}"; do
                        echo
                        deploy_to_cluster "${cluster}"
                    done
                    ;;
                4|*)
                    log "Deployment cancelled"
                    exit 0
                    ;;
            esac
        fi
    fi

    log_success "Operator setup complete!"
}

# Show status across all clusters
status() {
    log "📊 Checking operator status across all clusters..."

    local clusters_output
    clusters_output=$(detect_clusters)

    # Log detection results
    if ! log_cluster_detection "${clusters_output}"; then
        return 1
    fi

    # Convert space-separated string to array
    read -ra clusters <<< "${clusters_output}"

    for cluster in "${clusters[@]}"; do
        echo
        log "=== Cluster: ${cluster} ==="

        # Determine namespace based on cluster type
        local namespace
        if [[ "${cluster}" == "${DEV_CLUSTER}" ]]; then
            namespace="neo4j-operator-dev"
        else
            namespace="neo4j-operator-system"
        fi

        # Switch context
        kind export kubeconfig --name "${cluster}"

        if ! kubectl cluster-info >/dev/null 2>&1; then
            log_error "Cannot access cluster ${cluster}"
            continue
        fi

        echo "Pods:"
        kubectl get pods -n "${namespace}" 2>/dev/null || echo "  No pods found"

        echo "Deployments:"
        kubectl get deployments -n "${namespace}" 2>/dev/null || echo "  No deployments found"

        echo "Services:"
        kubectl get services -n "${namespace}" 2>/dev/null || echo "  No services found"
    done
}

# Follow logs from any available cluster
logs() {
    log "📋 Following operator logs..."

    local clusters_output
    clusters_output=$(detect_clusters)
    if [[ $? -ne 0 ]]; then
        log_error "No clusters found"
        exit 1
    fi

    # Convert space-separated string to array
    read -ra clusters <<< "${clusters_output}"

    # If multiple clusters, let user choose
    if [[ ${#clusters[@]} -gt 1 ]]; then
        echo "Multiple clusters available:"
        for i in "${!clusters[@]}"; do
            echo "  $((i+1)). ${clusters[i]}"
        done

        read -r -p "Choose cluster [1-${#clusters[@]}]: " choice
        if [[ "${choice}" -ge 1 && "${choice}" -le ${#clusters[@]} ]]; then
            local selected_cluster="${clusters[$((choice-1))]}"
        else
            log_error "Invalid choice"
            exit 1
        fi
    else
        local selected_cluster="${clusters[0]}"
    fi

    log "Following logs from cluster: ${selected_cluster}"

    # Determine namespace based on cluster type
    local namespace
    if [[ "${selected_cluster}" == "${DEV_CLUSTER}" ]]; then
        namespace="neo4j-operator-dev"
    else
        namespace="neo4j-operator-system"
    fi

    kind export kubeconfig --name "${selected_cluster}"
    kubectl logs -n "${namespace}" deployment/neo4j-operator-controller-manager -f
}

# Cleanup operator from clusters
cleanup() {
    log "🧹 Cleaning up operator from all clusters..."

    local clusters_output
    clusters_output=$(detect_clusters)

    # Log detection results
    if ! log_cluster_detection "${clusters_output}"; then
        return 0
    fi

    # Convert space-separated string to array
    read -ra clusters <<< "${clusters_output}"

    for cluster in "${clusters[@]}"; do
        echo
        log "Cleaning up cluster: ${cluster}"

        kind export kubeconfig --name "${cluster}"

        if ! kubectl cluster-info >/dev/null 2>&1; then
            log_warning "Cannot access cluster ${cluster}, skipping"
            continue
        fi

        # Determine namespace based on cluster type
        local namespace
        if [[ "${cluster}" == "${DEV_CLUSTER}" ]]; then
            namespace="neo4j-operator-dev"
            make undeploy-dev 2>/dev/null || true
        else
            namespace="neo4j-operator-system"
            make undeploy-prod 2>/dev/null || true
        fi

        # Clean up any remaining resources
        kubectl delete namespace "${namespace}" --ignore-not-found=true --timeout=60s || true

        log_success "Cleaned up ${cluster}"
    done

    log_success "Cleanup complete!"
}

# Main script logic
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
    cleanup)
        cleanup
        ;;
    *)
        echo "Usage: $0 {setup|status|logs|cleanup}"
        echo
        echo "Commands:"
        echo "  setup    - Deploy operator to available cluster(s)"
        echo "  status   - Show operator status across all clusters"
        echo "  logs     - Follow operator logs from a cluster"
        echo "  cleanup  - Remove operator from all clusters"
        echo
        echo "The script automatically detects available Kind clusters:"
        echo "  • ${DEV_CLUSTER} (development)"
        echo "  • ${TEST_CLUSTER} (testing)"
        exit 1
        ;;
esac
