#!/bin/bash

# Aggressive Test Environment Cleanup Script
# This script performs comprehensive cleanup and sanity checks for test environments

set -euo pipefail

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
CLEANUP_TIMEOUT=${CLEANUP_TIMEOUT:-300}  # 5 minutes
FORCE_CLEANUP=${FORCE_CLEANUP:-true}
DELETE_NAMESPACES=${DELETE_NAMESPACES:-true}
DELETE_CRDS=${DELETE_CRDS:-false}
VERBOSE=${VERBOSE:-false}
AGGRESSIVE_CLEANUP=${AGGRESSIVE_CLEANUP:-true}

# Check if kubectl is available
check_kubectl() {
    log_info "Checking kubectl availability..."
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi

    # Test cluster connectivity
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi

    log_success "kubectl is available and cluster is accessible"
}

# Check cluster health
check_cluster_health() {
    log_info "Checking cluster health..."

    # Check if kubectl can connect
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot connect to Kubernetes cluster"
        return 1
    fi

    # Check node readiness
    local ready_nodes=$(kubectl get nodes --no-headers 2>/dev/null | grep -c "Ready" || echo "0")
    if [ "${ready_nodes:-0}" -eq 0 ]; then
        log_warning "No ready nodes found in cluster"
        return 1
    fi

    # Check for storage classes
    local storage_classes=$(kubectl get storageclass --no-headers 2>/dev/null | wc -l || echo "0")
    if [ "${storage_classes:-0}" -eq 0 ]; then
        log_warning "No storage classes found in cluster"
    fi

    log_success "Cluster health check passed"
    return 0
}

# Check for required CRDs
check_crds() {
    log_info "Checking required CRDs..."

    local required_crds=(
        "neo4jenterpriseclusters.neo4j.neo4j.com"
        "neo4jbackups.neo4j.neo4j.com"
        "neo4jrestores.neo4j.neo4j.com"
        "neo4jusers.neo4j.neo4j.com"
        "neo4jroles.neo4j.neo4j.com"
        "neo4jgrants.neo4j.neo4j.com"
        "neo4jdatabases.neo4j.neo4j.com"
        "neo4jplugins.neo4j.neo4j.com"
    )

    local missing_crds=()
    for crd in "${required_crds[@]}"; do
        if ! kubectl get crd "$crd" &> /dev/null; then
            missing_crds+=("$crd")
        fi
    done

    if [ ${#missing_crds[@]} -gt 0 ]; then
        # If this is just a check (not cleanup), be more lenient
        if [[ "${2:-}" == "check_only" ]]; then
            log_warning "Missing required CRDs (this is expected before operator deployment):"
            for crd in "${missing_crds[@]}"; do
                log_warning "  - $crd"
            done
            log_info "CRDs will be installed when the operator is deployed"
            return 0
        fi

        log_error "Missing required CRDs:"
        for crd in "${missing_crds[@]}"; do
            log_error "  - $crd"
        done
        exit 1
    fi

    log_success "All required CRDs are installed"
}

# Force remove finalizers from resources
force_remove_finalizers() {
    log_info "Force removing finalizers from stuck resources..."

    # Remove finalizers from Neo4jEnterpriseClusters
    kubectl get neo4jenterpriseclusters --all-namespaces --no-headers -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Removing finalizers from Neo4jEnterpriseCluster $namespace/$name"
            kubectl patch neo4jenterprisecluster "$name" -n "$namespace" -p '{"metadata":{"finalizers":[]}}' --type=merge || log_warning "Failed to remove finalizers from $namespace/$name"
        fi
    done

    # Remove finalizers from Neo4jDatabases
    kubectl get neo4jdatabases --all-namespaces --no-headers -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Removing finalizers from Neo4jDatabase $namespace/$name"
            kubectl patch neo4jdatabase "$name" -n "$namespace" -p '{"metadata":{"finalizers":[]}}' --type=merge || log_warning "Failed to remove finalizers from $namespace/$name"
        fi
    done

    # Remove finalizers from other Neo4j resources
    local neo4j_resources=("neo4jbackups" "neo4jrestores" "neo4jusers" "neo4jroles" "neo4jgrants" "neo4jplugins")
    for resource in "${neo4j_resources[@]}"; do
        kubectl get "$resource" --all-namespaces --no-headers -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
            if [ -n "$namespace" ] && [ -n "$name" ]; then
                log_info "  Removing finalizers from $resource $namespace/$name"
                kubectl patch "$resource" "$name" -n "$namespace" -p '{"metadata":{"finalizers":[]}}' --type=merge || log_warning "Failed to remove finalizers from $resource $namespace/$name"
            fi
        done
    done

    log_success "Finalizer removal completed"
}

# Clean up Neo4j custom resources
cleanup_neo4j_resources() {
    log_info "Cleaning up Neo4j custom resources..."

    # First, force remove finalizers if aggressive cleanup is enabled
    if [ "$AGGRESSIVE_CLEANUP" = "true" ]; then
        force_remove_finalizers
    fi

    local neo4j_resources=(
        "neo4jenterpriseclusters"
        "neo4jbackups"
        "neo4jrestores"
        "neo4jusers"
        "neo4jroles"
        "neo4jgrants"
        "neo4jdatabases"
        "neo4jplugins"
    )

    for resource in "${neo4j_resources[@]}"; do
        log_info "Cleaning up $resource..."

        # Get all instances of this resource
        local instances=$(kubectl get "$resource" --all-namespaces --no-headers -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null || true)

        if [ -n "$instances" ]; then
            echo "$instances" | while read -r namespace name; do
                if [ -n "$namespace" ] && [ -n "$name" ]; then
                    log_info "  Deleting $resource $namespace/$name"
                    if [ "$FORCE_CLEANUP" = "true" ]; then
                        kubectl delete "$resource" "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete $resource $namespace/$name"
                    else
                        kubectl delete "$resource" "$name" -n "$namespace" --timeout=60s || log_warning "Failed to delete $resource $namespace/$name"
                    fi
                fi
            done
        fi
    done

    log_success "Neo4j resources cleanup completed"
}

# Clean up orphaned Kubernetes resources
cleanup_orphaned_resources() {
    log_info "Cleaning up orphaned Kubernetes resources..."

    # Clean up StatefulSets
    log_info "Cleaning up orphaned StatefulSets..."
    kubectl get statefulsets --all-namespaces --no-headers -l "app.kubernetes.io/part-of=neo4j-operator" -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Deleting StatefulSet $namespace/$name"
            kubectl delete statefulset "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete StatefulSet $namespace/$name"
        fi
    done

    # Clean up Jobs
    log_info "Cleaning up orphaned Jobs..."
    kubectl get jobs --all-namespaces --no-headers -l "app.kubernetes.io/part-of=neo4j-operator" -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Deleting Job $namespace/$name"
            kubectl delete job "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete Job $namespace/$name"
        fi
    done

    # Clean up Pods
    log_info "Cleaning up orphaned Pods..."
    kubectl get pods --all-namespaces --no-headers -l "app.kubernetes.io/part-of=neo4j-operator" -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Deleting Pod $namespace/$name"
            kubectl delete pod "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete Pod $namespace/$name"
        fi
    done

    # Clean up PVCs
    log_info "Cleaning up orphaned PVCs..."
    kubectl get pvc --all-namespaces --no-headers -l "app.kubernetes.io/part-of=neo4j-operator" -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Deleting PVC $namespace/$name"
            kubectl delete pvc "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete PVC $namespace/$name"
        fi
    done

    # Clean up Services
    log_info "Cleaning up orphaned Services..."
    kubectl get services --all-namespaces --no-headers -l "app.kubernetes.io/part-of=neo4j-operator" -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Deleting Service $namespace/$name"
            kubectl delete service "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete Service $namespace/$name"
        fi
    done

    # Clean up ServiceAccounts
    log_info "Cleaning up orphaned ServiceAccounts..."
    kubectl get serviceaccounts --all-namespaces --no-headers -l "app.kubernetes.io/part-of=neo4j-operator" -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name" 2>/dev/null | while read -r namespace name; do
        if [ -n "$namespace" ] && [ -n "$name" ]; then
            log_info "  Deleting ServiceAccount $namespace/$name"
            kubectl delete serviceaccount "$name" -n "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete ServiceAccount $namespace/$name"
        fi
    done

    log_success "Orphaned resources cleanup completed"
}

# Clean up test namespaces
cleanup_test_namespaces() {
    if [ "$DELETE_NAMESPACES" != "true" ]; then
        log_info "Skipping namespace cleanup (DELETE_NAMESPACES=false)"
        return
    fi

    log_info "Cleaning up test namespaces..."

    # Get all namespaces
    kubectl get namespaces --no-headers -o custom-columns="NAME:.metadata.name" | while read -r namespace; do
        if [ -n "$namespace" ] && is_test_namespace "$namespace"; then
            log_info "  Deleting test namespace: $namespace"
            kubectl delete namespace "$namespace" --force --grace-period=0 --timeout=60s || log_warning "Failed to delete namespace $namespace"
        fi
    done

    log_success "Test namespaces cleanup completed"
}

# Check for conflicting resources
check_conflicting_resources() {
    log_info "Checking for conflicting resources..."

    # Check for existing Neo4j resources
    local existing_clusters=$(kubectl get neo4jenterpriseclusters --all-namespaces --no-headers 2>/dev/null | wc -l || echo "0")
    if [ "$existing_clusters" -gt 0 ]; then
        log_warning "Found $existing_clusters existing Neo4jEnterpriseClusters that might conflict with tests"
        if [ "$VERBOSE" = "true" ]; then
            kubectl get neo4jenterpriseclusters --all-namespaces
        fi
    fi

    # Check for test namespaces
    local test_namespaces=$(kubectl get namespaces --no-headers -o custom-columns="NAME:.metadata.name" | grep -E "^(test-|gke-|aks-|eks-)" | wc -l || echo "0")
    if [ "$test_namespaces" -gt 0 ]; then
        log_warning "Found $test_namespaces test namespaces that might conflict"
        if [ "$VERBOSE" = "true" ]; then
            kubectl get namespaces | grep -E "^(test-|gke-|aks-|eks-)"
        fi
    fi
}

# Verify cleanup completion
verify_cleanup() {
    log_info "Verifying cleanup completion..."

    local timeout_counter=0
    local max_timeout=$((CLEANUP_TIMEOUT / 10))

    while [ $timeout_counter -lt $max_timeout ]; do
        local remaining_resources=0

        # Check for remaining Neo4j resources
        local remaining_clusters=$(kubectl get neo4jenterpriseclusters --all-namespaces --no-headers 2>/dev/null | wc -l || echo "0")
        local remaining_backups=$(kubectl get neo4jbackups --all-namespaces --no-headers 2>/dev/null | wc -l || echo "0")
        local remaining_restores=$(kubectl get neo4jrestores --all-namespaces --no-headers 2>/dev/null | wc -l || echo "0")
        local remaining_databases=$(kubectl get neo4jdatabases --all-namespaces --no-headers 2>/dev/null | wc -l || echo "0")

        remaining_resources=$((remaining_clusters + remaining_backups + remaining_restores + remaining_databases))

        # Check for remaining test namespaces
        if [ "$DELETE_NAMESPACES" = "true" ]; then
            local remaining_namespaces=$(kubectl get namespaces --no-headers -o custom-columns="NAME:.metadata.name" | grep -E "^(test-|gke-|aks-|eks-)" | wc -l || echo "0")
            remaining_resources=$((remaining_resources + remaining_namespaces))
        fi

        if [ "${remaining_resources:-0}" -eq 0 ]; then
            log_success "Cleanup verification passed - no conflicting resources found"
            return 0
        fi

        log_info "Waiting for cleanup to complete... ($timeout_counter/$max_timeout)"
        sleep 10
        timeout_counter=$((timeout_counter + 1))
    done

    log_warning "Cleanup verification timeout - some resources may still exist"
    return 1
}

# Helper function to check if namespace is a test namespace
is_test_namespace() {
    local namespace="$1"
    [[ "$namespace" =~ ^(test-|gke-|aks-|eks-) ]]
}

# Main cleanup function
main_cleanup() {
    log_info "Starting aggressive test environment cleanup..."
    log_info "Configuration:"
    log_info "  FORCE_CLEANUP: $FORCE_CLEANUP"
    log_info "  DELETE_NAMESPACES: $DELETE_NAMESPACES"
    log_info "  DELETE_CRDS: $DELETE_CRDS"
    log_info "  CLEANUP_TIMEOUT: ${CLEANUP_TIMEOUT}s"
    log_info "  VERBOSE: $VERBOSE"
    log_info "  AGGRESSIVE_CLEANUP: $AGGRESSIVE_CLEANUP"

    # Perform checks
    check_kubectl
    check_cluster_health
    check_crds "cleanup"
    check_conflicting_resources

    # Perform cleanup
    cleanup_neo4j_resources
    cleanup_orphaned_resources
    cleanup_test_namespaces

    # Verify cleanup
    verify_cleanup

    log_success "Aggressive test environment cleanup completed successfully"
}

# Handle script arguments
case "${1:-cleanup}" in
    "cleanup")
        main_cleanup
        ;;
    "check")
        check_kubectl
        check_cluster_health
        check_crds "check_only"
        check_conflicting_resources
        log_success "Environment checks completed"
        ;;
    "help"|"-h"|"--help")
        echo "Usage: $0 [cleanup|check|help]"
        echo ""
        echo "Commands:"
        echo "  cleanup  - Perform aggressive cleanup (default)"
        echo "  check    - Perform environment checks only"
        echo "  help     - Show this help message"
        echo ""
        echo "Environment variables:"
        echo "  FORCE_CLEANUP     - Force deletion (default: true)"
        echo "  DELETE_NAMESPACES - Delete test namespaces (default: true)"
        echo "  DELETE_CRDS       - Delete CRDs (default: false)"
        echo "  CLEANUP_TIMEOUT   - Cleanup timeout in seconds (default: 300)"
        echo "  VERBOSE           - Verbose output (default: false)"
        echo "  AGGRESSIVE_CLEANUP - Force remove finalizers (default: true)"
        ;;
    *)
        log_error "Unknown command: $1"
        echo "Use '$0 help' for usage information"
        exit 1
        ;;
esac
