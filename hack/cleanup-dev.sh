#!/bin/bash
set -euo pipefail

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly NC='\033[0m' # No Color

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
CLEANUP_CLUSTER=${CLEANUP_CLUSTER:-false}
CLEANUP_RESOURCES=${CLEANUP_RESOURCES:-true}
CLEANUP_TEMP=${CLEANUP_TEMP:-true}
CLEANUP_LOGS=${CLEANUP_LOGS:-false}
FORCE=${FORCE:-false}

# Cleanup Kind cluster
cleanup_cluster() {
    if [[ "${CLEANUP_CLUSTER}" == "true" ]]; then
        log_info "Cleaning up Kind cluster..."

        # Use safer pattern matching
        local clusters
        clusters=$(kind get clusters 2>/dev/null || echo "")
        if echo "${clusters}" | grep -q "^neo4j-operator-dev$"; then
            if [[ "${FORCE}" == "true" ]] || confirm "Delete Kind cluster 'neo4j-operator-dev'?"; then
                if kind delete cluster --name neo4j-operator-dev; then
                    log_success "Kind cluster deleted"
                else
                    log_error "Failed to delete Kind cluster"
                    return 1
                fi
            else
                log_info "Skipping cluster deletion"
            fi
        else
            log_info "No Kind cluster found"
        fi
    fi
}

# Cleanup Kubernetes resources
cleanup_resources() {
    if [[ "${CLEANUP_RESOURCES}" == "true" ]]; then
        log_info "Cleaning up Kubernetes resources..."

        # Check if kubectl is available
        if ! command -v kubectl >/dev/null 2>&1; then
            log_warning "kubectl not found, skipping resource cleanup"
            return 0
        fi

        # Check if cluster is accessible
        if ! kubectl cluster-info >/dev/null 2>&1; then
            log_warning "Kubernetes cluster not accessible, skipping resource cleanup"
            return 0
        fi

        # Cleanup Neo4j resources with better error handling
        log_info "Cleaning up Neo4j resources..."
        local resources=(
            "neo4jenterpriseclusters"
            "neo4jdatabases"
            "neo4jbackups"
            "neo4jrestores"
        )

        for resource in "${resources[@]}"; do
            if kubectl get "${resource}" --all-namespaces >/dev/null 2>&1; then
                log_info "Deleting ${resource}..."
                kubectl delete "${resource}" --all --all-namespaces --ignore-not-found=true --timeout=30s || {
                    log_warning "Failed to delete some ${resource} resources"
                }
            fi
        done

        # Cleanup operator deployment
        if kubectl get deployment neo4j-operator-controller-manager -n neo4j-operator-system >/dev/null 2>&1; then
            if [[ "${FORCE}" == "true" ]] || confirm "Delete operator deployment?"; then
                kubectl delete deployment neo4j-operator-controller-manager -n neo4j-operator-system --timeout=60s || {
                    log_warning "Failed to delete operator deployment"
                }
            fi
        fi

        # Cleanup CRDs
        if [[ "${FORCE}" == "true" ]] || confirm "Delete CRDs? This will remove all Neo4j resources!"; then
            local crds
            crds=$(kubectl get crd -l app.kubernetes.io/name=neo4j-operator -o name 2>/dev/null || echo "")
            if [[ -n "${crds}" ]]; then
                echo "${crds}" | xargs -r kubectl delete --ignore-not-found=true --timeout=60s || {
                    log_warning "Failed to delete some CRDs"
                }
            fi
        fi

        # Cleanup namespaces
        if kubectl get namespace neo4j-operator-system >/dev/null 2>&1; then
            if [[ "${FORCE}" == "true" ]] || confirm "Delete neo4j-operator-system namespace?"; then
                kubectl delete namespace neo4j-operator-system --timeout=120s || {
                    log_warning "Failed to delete namespace, it may be stuck"
                }
            fi
        fi

        log_success "Kubernetes resources cleaned up"
    fi
}

# Cleanup temporary files
cleanup_temp() {
    if [[ "${CLEANUP_TEMP}" == "true" ]]; then
        log_info "Cleaning up temporary files..."

        # Remove build artifacts safely
        local temp_dirs=("bin" "tmp" "dist")
        for dir in "${temp_dirs[@]}"; do
            if [[ -d "${dir}" ]]; then
                rm -rf "${dir}"
                log_info "Removed ${dir}/ directory"
            fi
        done

        # Remove individual files safely
        local temp_files=("cover.out" "results.sarif" "build-errors.log" ".air.toml")
        for file in "${temp_files[@]}"; do
            if [[ -f "${file}" ]]; then
                rm -f "${file}"
                log_info "Removed ${file}"
            fi
        done

        # Remove vendor if exists
        if [[ -d "vendor" ]]; then
            if [[ "${FORCE}" == "true" ]] || confirm "Delete vendor/ directory?"; then
                rm -rf vendor/
                log_info "Removed vendor/ directory"
            fi
        fi

        log_success "Temporary files cleaned up"
    fi
}

# Cleanup logs
cleanup_logs() {
    if [[ "${CLEANUP_LOGS}" == "true" ]]; then
        log_info "Cleaning up log files..."

        if [[ -d "logs" ]]; then
            if [[ "${FORCE}" == "true" ]] || confirm "Delete all log files?"; then
                # Safer log cleanup
                find logs/ -type f -name "*.log" -delete 2>/dev/null || true
                find logs/ -type f -name "*.out" -delete 2>/dev/null || true
                # Recreate logs directory structure
                mkdir -p logs/
                log_success "Log files cleaned up"
            fi
        else
            log_info "No logs directory found"
        fi
    fi
}

# Confirmation helper with timeout
confirm() {
    if [[ "${FORCE}" == "true" ]]; then
        return 0
    fi

    local response
    read -r -t 30 -p "$1 [y/N] " response || {
        echo
        log_info "No response received, defaulting to 'no'"
        return 1
    }

    case "${response}" in
        [yY][eE][sS]|[yY])
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

# Show cleanup plan
show_plan() {
    log_info "Cleanup Plan:"
    log_info "  Cluster: ${CLEANUP_CLUSTER}"
    log_info "  Resources: ${CLEANUP_RESOURCES}"
    log_info "  Temp Files: ${CLEANUP_TEMP}"
    log_info "  Logs: ${CLEANUP_LOGS}"
    log_info "  Force: ${FORCE}"
    echo
}

# Validate environment
validate_environment() {
    # Check if we're in the right directory
    if [[ ! -f "go.mod" ]] || ! grep -q "neo4j-kubernetes-operator" go.mod 2>/dev/null; then
        log_error "This script must be run from the neo4j-kubernetes-operator project root directory"
        exit 1
    fi

    # Check if running as root (which could be dangerous)
    if [[ "${EUID}" -eq 0 ]]; then
        log_warning "Running as root. This may be dangerous for file cleanup operations."
        if [[ "${FORCE}" != "true" ]] && ! confirm "Continue as root?"; then
            log_info "Exiting for safety"
            exit 1
        fi
    fi
}

# Main function
main() {
    log_info "Neo4j Operator Development Cleanup"

    validate_environment
    show_plan

    if [[ "${FORCE}" != "true" ]] && ! confirm "Proceed with cleanup?"; then
        log_info "Cleanup cancelled"
        exit 0
    fi

    # Run cleanup operations in order
    cleanup_resources
    cleanup_cluster
    cleanup_temp
    cleanup_logs

    log_success "Cleanup complete!"
}

# Handle script arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --cluster)
            CLEANUP_CLUSTER=true
            shift
            ;;
        --no-resources)
            CLEANUP_RESOURCES=false
            shift
            ;;
        --no-temp)
            CLEANUP_TEMP=false
            shift
            ;;
        --logs)
            CLEANUP_LOGS=true
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --help|-h)
            cat << EOF
Usage: $0 [options]

Options:
  --cluster       Also cleanup Kind cluster
  --no-resources  Skip Kubernetes resources cleanup
  --no-temp       Skip temporary files cleanup
  --logs          Also cleanup log files
  --force         Skip confirmation prompts
  --help, -h      Show this help

Environment Variables:
  CLEANUP_CLUSTER    Set to 'true' to cleanup cluster (default: false)
  CLEANUP_RESOURCES  Set to 'false' to skip resources (default: true)
  CLEANUP_TEMP       Set to 'false' to skip temp files (default: true)
  CLEANUP_LOGS       Set to 'true' to cleanup logs (default: false)
  FORCE              Set to 'true' to skip prompts (default: false)

Examples:
  $0                    # Basic cleanup (resources + temp files)
  $0 --cluster --logs   # Full cleanup including cluster and logs
  $0 --force --cluster  # Silent full cleanup
EOF
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            log_info "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Ensure we're not in a pipe that could mask errors
if [[ -t 0 ]]; then
    main "$@"
else
    log_error "This script should not be run in a pipeline for safety reasons"
    exit 1
fi
