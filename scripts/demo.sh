#!/bin/bash
set -euo pipefail

# Neo4j Kubernetes Operator Demo Script
# This script demonstrates the core capabilities of the Neo4j Kubernetes Operator
# including single-node and multi-node TLS-enabled cluster deployments

# Colors for beautiful output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly PURPLE='\033[0;35m'
readonly CYAN='\033[0;36m'
readonly WHITE='\033[1;37m'
readonly NC='\033[0m' # No Color

# Demo configuration
DEMO_NAMESPACE=${DEMO_NAMESPACE:-default}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"demo123456"}
CLUSTER_NAME_SINGLE=${CLUSTER_NAME_SINGLE:-"neo4j-single"}
CLUSTER_NAME_MULTI=${CLUSTER_NAME_MULTI:-"neo4j-cluster"}
SKIP_CONFIRMATIONS=${SKIP_CONFIRMATIONS:-false}
DEMO_SPEED=${DEMO_SPEED:-normal} # fast, normal, slow

# Timing configuration based on demo speed
case "${DEMO_SPEED}" in
    fast)
        PAUSE_SHORT=1
        PAUSE_MEDIUM=2
        PAUSE_LONG=3
        ;;
    slow)
        PAUSE_SHORT=3
        PAUSE_MEDIUM=5
        PAUSE_LONG=8
        ;;
    *)
        PAUSE_SHORT=2
        PAUSE_MEDIUM=3
        PAUSE_LONG=5
        ;;
esac

# Enhanced logging functions
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

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_demo() {
    echo -e "${PURPLE}[DEMO]${NC} $1"
}

log_command() {
    echo -e "${CYAN}[COMMAND]${NC} $1"
}

log_manifest() {
    echo -e "${YELLOW}[MANIFEST]${NC} $1"
}

# Progress indicator
show_progress() {
    local duration=$1
    local message=$2

    echo -n -e "${CYAN}${message}${NC}"
    for i in $(seq 1 $duration); do
        echo -n "."
        sleep 1
    done
    echo -e " ${GREEN}Done!${NC}"
}

# Confirmation with skip option
confirm() {
    if [[ "${SKIP_CONFIRMATIONS}" == "true" ]]; then
        log_info "Auto-continuing (SKIP_CONFIRMATIONS=true)"
        return 0
    fi

    local response
    read -r -p "$(echo -e "${CYAN}$1 [Enter to continue, 'q' to quit]${NC} ")" response
    case "${response}" in
        [qQ]|[qQ][uU][iI][tT])
            log_info "Demo terminated by user"
            exit 0
            ;;
        *)
            return 0
            ;;
    esac
}

# Wait for pods to be ready with visual feedback
wait_for_pods() {
    local label_selector=$1
    local namespace=$2
    local timeout=${3:-300}
    local expected_count=${4:-1}

    log_info "Waiting for ${expected_count} pod(s) with selector '${label_selector}' to be ready..."
    log_command "kubectl get pods -l '${label_selector}' -n ${namespace} --watch"

    local start_time=$(date +%s)
    local dots=0
    local last_status=""

    while true; do
        local ready_count=$(kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null | grep "1/1.*Running" | wc -l | tr -d ' ')
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))

        # Show current pod status every 10 seconds
        if [[ $((dots % 10)) -eq 0 ]]; then
            local current_status=$(kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null | head -3 || echo "No pods found")
            if [[ "${current_status}" != "${last_status}" ]]; then
                echo
                log_info "Current pod status:"
                kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null | head -3 | while read line; do
                    echo "  ${line}"
                done
                last_status="${current_status}"
            fi
        fi

        if [[ "${ready_count}" -eq "${expected_count}" ]]; then
            echo
            log_success "All ${expected_count} pod(s) are ready!"
            echo
            log_info "Final pod status:"
            kubectl get pods -l "${label_selector}" -n "${namespace}" -o wide
            return 0
        fi

        if [[ "${elapsed}" -gt "${timeout}" ]]; then
            echo
            log_error "Timeout waiting for pods to be ready"
            return 1
        fi

        # Visual progress indicator
        echo -n "."
        if [[ $((dots % 60)) -eq 59 ]]; then
            echo " (${elapsed}s elapsed, ${ready_count}/${expected_count} ready)"
        fi
        ((dots++))

        sleep 1
    done
}

# Display cluster status in a nice format
show_cluster_status() {
    local cluster_name=$1
    local namespace=$2

    echo
    log_section "Cluster Status: ${cluster_name}"

    # Cluster resource
    echo -e "${CYAN}Neo4j Enterprise Cluster:${NC}"
    log_command "kubectl get neo4jenterprisecluster ${cluster_name} -n ${namespace} -o wide"
    kubectl get neo4jenterprisecluster "${cluster_name}" -n "${namespace}" -o wide 2>/dev/null || echo "  Cluster not found"

    echo
    echo -e "${CYAN}Pods:${NC}"
    log_command "kubectl get pods -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace} -o wide"
    kubectl get pods -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" -o wide 2>/dev/null || echo "  No pods found"

    echo
    echo -e "${CYAN}Services:${NC}"
    log_command "kubectl get services -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace}"
    kubectl get services -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" 2>/dev/null || echo "  No services found"

    echo
    echo -e "${CYAN}Persistent Volume Claims:${NC}"
    log_command "kubectl get pvc -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace}"
    kubectl get pvc -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" 2>/dev/null || echo "  No PVCs found"

    if kubectl get certificates -n "${namespace}" --no-headers 2>/dev/null | grep -q "${cluster_name}"; then
        echo
        echo -e "${CYAN}TLS Certificates:${NC}"
        log_command "kubectl get certificates -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace}"
        kubectl get certificates -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" 2>/dev/null || true
    fi
}

# Display connection information
show_connection_info() {
    local cluster_name=$1
    local namespace=$2
    local has_tls=${3:-false}

    log_section "Connection Information"

    local client_service="${cluster_name}-client"
    local bolt_port="7687"
    local http_port="7474"
    local https_port="7473"

    echo -e "${CYAN}Service Endpoints:${NC}"
    echo "  • Client Service: ${client_service}.${namespace}.svc.cluster.local"

    if [[ "${has_tls}" == "true" ]]; then
        echo "  • Bolt (TLS):     bolt+s://${client_service}:${bolt_port}"
        echo "  • HTTPS:          https://${client_service}:${https_port}"
        echo "  • HTTP:           http://${client_service}:${http_port} (fallback)"
    else
        echo "  • Bolt:           bolt://${client_service}:${bolt_port}"
        echo "  • HTTP:           http://${client_service}:${http_port}"
    fi

    echo
    echo -e "${CYAN}Local Access (kubectl port-forward):${NC}"
    if [[ "${has_tls}" == "true" ]]; then
        echo "  kubectl port-forward svc/${client_service} -n ${namespace} ${https_port}:${https_port} ${bolt_port}:${bolt_port}"
        echo "  Then open: https://localhost:${https_port}"
    else
        echo "  kubectl port-forward svc/${client_service} -n ${namespace} ${http_port}:${http_port} ${bolt_port}:${bolt_port}"
        echo "  Then open: http://localhost:${http_port}"
    fi

    echo
    echo -e "${CYAN}Credentials:${NC}"
    echo "  • Username: neo4j"
    echo "  • Password: ${ADMIN_PASSWORD}"
}

# Cleanup existing clusters
cleanup_existing() {
    log_section "Cleaning Up Existing Resources"

    log_info "Removing any existing demo clusters..."
    log_command "kubectl delete neo4jenterprisecluster ${CLUSTER_NAME_SINGLE} ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE} --ignore-not-found=true"
    kubectl delete neo4jenterprisecluster "${CLUSTER_NAME_SINGLE}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true &
    kubectl delete neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true &
    wait

    log_info "Waiting for cleanup to complete..."
    sleep 5

    # Wait for pods to be deleted
    while kubectl get pods -l "neo4j.com/cluster in (${CLUSTER_NAME_SINGLE},${CLUSTER_NAME_MULTI})" -n "${DEMO_NAMESPACE}" --no-headers 2>/dev/null | grep -q .; do
        echo -n "."
        sleep 2
    done

    log_success "Cleanup complete!"
}

# Create admin secret
create_admin_secret() {
    log_section "Creating Admin Credentials"

    log_info "Creating admin secret with secure password..."
    log_command "kubectl create secret generic neo4j-admin-secret --from-literal=username=neo4j --from-literal=password=*** -n ${DEMO_NAMESPACE}"
    kubectl create secret generic neo4j-admin-secret \
        --from-literal=username=neo4j \
        --from-literal=password="${ADMIN_PASSWORD}" \
        -n "${DEMO_NAMESPACE}" \
        --dry-run=client -o yaml | kubectl apply -f -

    log_success "Admin secret created successfully!"
}

# Deploy single node cluster
deploy_single_node() {
    log_header "DEMO PART 1: Single-Node Neo4j Cluster"

    log_demo "We'll start with a simple single-node Neo4j cluster for development and testing."
    log_demo "This configuration is perfect for:"
    log_demo "  • Development environments"
    log_demo "  • Testing and prototyping"
    log_demo "  • Small workloads"
    log_demo "  • Learning Neo4j"

    confirm "Ready to deploy the single-node cluster?"

    log_section "Deploying Single-Node Cluster"

    log_manifest "Creating single-node cluster manifest:"
    log_info "This manifest will create a Neo4j Enterprise cluster with:"
    log_info "  • 1 primary node (no clustering)"
    log_info "  • TLS disabled for simplicity"
    log_info "  • Standard resource allocation"
    log_info "  • 10Gi storage per node"
    echo

    # Create single-node cluster manifest
    local manifest=$(cat << 'EOF'
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-single
  namespace: default
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
    pullPolicy: IfNotPresent

  edition: enterprise

  # Required environment variables
  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

  # Authentication configuration
  auth:
    adminSecret: neo4j-admin-secret

  # Single-node topology
  topology:
    primaries: 1
    secondaries: 0

  # Resource allocation
  resources:
    requests:
      cpu: "500m"
      memory: "2Gi"
    limits:
      cpu: "1"
      memory: "4Gi"

  # Storage configuration
  storage:
    className: standard
    size: "10Gi"

  # TLS disabled for simplicity in single-node demo
  tls:
    mode: disabled

  # Basic configuration for single-node
  config:
    dbms.mode: "SINGLE"
    dbms.logs.query.enabled: "INFO"
    metrics.enabled: "true"
EOF
)

    echo -e "${YELLOW}---${NC}"
    echo "${manifest}"
    echo -e "${YELLOW}---${NC}"
    echo

    log_command "kubectl apply -f -"
    echo "${manifest}" | kubectl apply -f -

    log_success "Single-node cluster manifest applied!"

    log_info "The operator is now creating the following resources:"
    log_info "  • StatefulSet with 1 replica"
    log_info "  • Client and headless services"
    log_info "  • ConfigMap with Neo4j configuration"
    log_info "  • PersistentVolumeClaim for data storage"

    # Wait for deployment
    show_progress $PAUSE_MEDIUM "Waiting for cluster initialization"

    log_info "Monitoring cluster deployment progress..."
    wait_for_pods "neo4j.com/cluster=${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" 180 1

    show_cluster_status "${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}"
    show_connection_info "${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" false

    log_success "Single-node cluster is ready!"
    log_demo "The cluster is now running with a single member using unified clustering infrastructure"
    log_demo "This provides a simplified deployment suitable for development and testing"

    confirm "Ready to proceed to the multi-node TLS-enabled cluster demo?"
}

# Deploy multi-node TLS cluster
deploy_multi_node_tls() {
    log_header "DEMO PART 2: Multi-Node TLS-Enabled Neo4j Cluster"

    log_demo "Now we'll deploy a production-ready 3-node Neo4j cluster with:"
    log_demo "  • High availability through clustering"
    log_demo "  • TLS encryption using cert-manager"
    log_demo "  • Automatic certificate management"
    log_demo "  • Raft consensus for data consistency"
    log_demo "  • Read and write scalability"

    confirm "Ready to deploy the TLS-enabled cluster?"

    log_section "Deploying Multi-Node TLS Cluster"

    log_manifest "Creating multi-node TLS cluster manifest:"
    log_info "This manifest will create a Neo4j Enterprise cluster with:"
    log_info "  • 3 primary nodes (HA clustering)"
    log_info "  • TLS enabled using cert-manager"
    log_info "  • Production resource allocation"
    log_info "  • 20Gi storage per node"
    log_info "  • Automatic certificate management"
    echo

    # Create TLS-enabled cluster manifest
    local manifest=$(cat << 'EOF'
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-cluster
  namespace: default
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
    pullPolicy: IfNotPresent

  edition: enterprise

  # Required environment variables
  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

  # Authentication configuration
  auth:
    adminSecret: neo4j-admin-secret

  # Multi-node topology for high availability
  topology:
    primaries: 3
    secondaries: 0

  # Production resource allocation
  resources:
    requests:
      cpu: "1"
      memory: "4Gi"
    limits:
      cpu: "2"
      memory: "8Gi"

  # Storage configuration
  storage:
    className: standard
    size: "20Gi"

  # TLS configuration using cert-manager
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
    duration: "8760h"  # 1 year
    renewBefore: "720h" # 30 days
    usages:
      - server auth
      - client auth

  # Production configuration
  config:
    dbms.cluster.minimum_initial_system_primaries_count: "3"
    dbms.logs.query.enabled: "INFO"
    dbms.transaction.timeout: "60s"
    metrics.enabled: "true"
    server.metrics.prometheus.enabled: "true"
    server.metrics.prometheus.endpoint: "0.0.0.0:2004"
EOF
)

    echo -e "${YELLOW}---${NC}"
    echo "${manifest}"
    echo -e "${YELLOW}---${NC}"
    echo

    log_command "kubectl apply -f -"
    echo "${manifest}" | kubectl apply -f -

    log_success "Multi-node TLS cluster manifest applied!"

    log_info "The operator is now creating the following resources:"
    log_info "  • StatefulSet with 3 replicas (primary nodes)"
    log_info "  • cert-manager Certificate for TLS"
    log_info "  • Client and headless services with TLS support"
    log_info "  • ConfigMap with cluster and TLS configuration"
    log_info "  • 3 PersistentVolumeClaims for distributed data"

    # Show certificate creation
    show_progress $PAUSE_SHORT "Waiting for certificate creation"

    log_section "TLS Certificate Status"
    log_command "kubectl get certificates -n ${DEMO_NAMESPACE}"
    echo
    kubectl get certificates -n "${DEMO_NAMESPACE}" | grep "${CLUSTER_NAME_MULTI}" || log_info "Certificate still being created..."
    echo

    log_demo "cert-manager is automatically:"
    log_demo "  • Generating TLS certificates using the self-signed CA"
    log_demo "  • Creating Kubernetes secrets with private keys and certificates"
    log_demo "  • Managing certificate renewal before expiration"

    # Wait for cluster deployment with detailed progress
    log_section "Cluster Formation Progress"

    log_demo "Neo4j clusters start pods sequentially for data consistency:"
    log_demo "  1. Pod 0 (bootstrap): Forms the initial cluster"
    log_demo "  2. Pod 1: Joins the existing cluster"
    log_demo "  3. Pod 2: Joins and completes the cluster"
    log_demo "This typically takes 3-6 minutes for a 3-node cluster."

    show_progress $PAUSE_MEDIUM "Monitoring cluster formation"

    # Monitor each pod coming online
    for i in 0 1 2; do
        log_info "Waiting for ${CLUSTER_NAME_MULTI}-primary-${i} to be ready..."
        log_command "kubectl get pod ${CLUSTER_NAME_MULTI}-primary-${i} -n ${DEMO_NAMESPACE} --watch"

        local pod_ready=false
        local dots=0

        while [[ "${pod_ready}" != "true" ]]; do
            if kubectl get pod "${CLUSTER_NAME_MULTI}-primary-${i}" -n "${DEMO_NAMESPACE}" --no-headers 2>/dev/null | grep -q "1/1.*Running"; then
                pod_ready=true
            else
                # Show pod status every 5 seconds
                if [[ $((dots % 5)) -eq 0 ]]; then
                    local pod_status=$(kubectl get pod "${CLUSTER_NAME_MULTI}-primary-${i}" -n "${DEMO_NAMESPACE}" --no-headers 2>/dev/null || echo "Pod not found")
                    echo
                    log_info "Pod ${i} status: ${pod_status}"
                fi
                echo -n "."
                sleep 1
                ((dots++))
            fi
        done
        echo

        log_success "Pod ${i} is ready!"
        kubectl get pod "${CLUSTER_NAME_MULTI}-primary-${i}" -n "${DEMO_NAMESPACE}" -o wide

        if [[ $i -eq 0 ]]; then
            log_demo "Bootstrap pod formed the cluster foundation"
        else
            log_demo "Pod ${i} successfully joined the cluster"
        fi
        echo
    done

    log_success "All cluster nodes are ready!"

    # Final status display
    show_cluster_status "${CLUSTER_NAME_MULTI}" "${DEMO_NAMESPACE}"

    log_section "TLS Configuration Verification"

    # Show TLS certificate details
    if kubectl get certificate "${CLUSTER_NAME_MULTI}-tls" -n "${DEMO_NAMESPACE}" &>/dev/null; then
        kubectl get certificate "${CLUSTER_NAME_MULTI}-tls" -n "${DEMO_NAMESPACE}" -o wide
        log_success "TLS certificate is ready and issued!"
    fi

    show_connection_info "${CLUSTER_NAME_MULTI}" "${DEMO_NAMESPACE}" true

    log_success "Multi-node TLS cluster is fully operational!"

    log_demo "The cluster now provides:"
    log_demo "  ✓ High availability with 3 primary nodes"
    log_demo "  ✓ Automatic failover and leader election"
    log_demo "  ✓ TLS encryption for all communications"
    log_demo "  ✓ Raft consensus for data consistency"
    log_demo "  ✓ Horizontal read scaling capability"
}

# Demo summary and next steps
show_demo_summary() {
    log_header "DEMO SUMMARY"

    log_demo "We successfully demonstrated the Neo4j Kubernetes Operator capabilities:"
    echo
    echo -e "${GREEN}✓ Single-Node Cluster${NC}"
    echo "  • Perfect for development and testing"
    echo "  • Simple deployment and management"
    echo "  • Resource efficient"
    echo
    echo -e "${GREEN}✓ Multi-Node TLS Cluster${NC}"
    echo "  • Production-ready high availability"
    echo "  • Automatic TLS certificate management"
    echo "  • Raft consensus and data consistency"
    echo "  • Horizontal scaling capabilities"
    echo

    log_section "Active Clusters"
    log_command "kubectl get neo4jenterprisecluster -n ${DEMO_NAMESPACE} -o wide"
    kubectl get neo4jenterprisecluster -n "${DEMO_NAMESPACE}" -o wide

    log_section "Cleanup"
    log_info "To clean up the demo clusters:"
    echo "  kubectl delete neo4jenterprisecluster ${CLUSTER_NAME_SINGLE} ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE}"
    echo

    log_success "Demo completed successfully! 🎉"
}

# Validate prerequisites
validate_prerequisites() {
    log_section "Validating Prerequisites"

    # Check kubectl
    if ! command -v kubectl >/dev/null 2>&1; then
        log_error "kubectl is required but not installed"
        exit 1
    fi

    # Ensure we're using the dev cluster context
    kind export kubeconfig --name "neo4j-operator-dev" 2>/dev/null || true

    # Check cluster access
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot access Kubernetes cluster"
        log_info "Run 'make demo-setup' to set up the demo environment"
        exit 1
    fi

    # Check for cert-manager
    if ! kubectl get clusterissuer ca-cluster-issuer >/dev/null 2>&1; then
        log_warning "ca-cluster-issuer not found - TLS demo may fail"
        log_info "Run 'make demo-setup' to set up the demo environment"
    fi

    # Check for operator
    if ! kubectl get deployment -n neo4j-operator-system neo4j-operator-controller-manager >/dev/null 2>&1; then
        log_warning "Neo4j operator not found"
        log_info "Run 'make demo-setup' to set up the demo environment"
    fi

    # Check namespace
    kubectl create namespace "${DEMO_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1

    log_success "Prerequisites validated!"
}

# Main demo flow
main() {
    clear
    log_header "Neo4j Kubernetes Operator Demo"

    log_demo "Welcome to the Neo4j Kubernetes Operator demonstration!"
    log_demo "This demo will showcase:"
    log_demo "  1. Single-node cluster deployment"
    log_demo "  2. Multi-node TLS-enabled cluster deployment"
    log_demo "  3. Operator capabilities and features"
    echo
    log_info "Demo configuration:"
    log_info "  • Namespace: ${DEMO_NAMESPACE}"
    log_info "  • Admin password: ${ADMIN_PASSWORD}"
    log_info "  • Demo speed: ${DEMO_SPEED}"
    log_info "  • Skip confirmations: ${SKIP_CONFIRMATIONS}"
    echo

    confirm "Ready to start the demo?"

    # Execute demo steps
    validate_prerequisites
    cleanup_existing
    create_admin_secret

    sleep $PAUSE_SHORT

    deploy_single_node

    sleep $PAUSE_MEDIUM

    deploy_multi_node_tls

    sleep $PAUSE_SHORT

    show_demo_summary
}

# Handle script arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --namespace)
            DEMO_NAMESPACE="$2"
            shift 2
            ;;
        --password)
            ADMIN_PASSWORD="$2"
            shift 2
            ;;
        --skip-confirmations)
            SKIP_CONFIRMATIONS=true
            shift
            ;;
        --speed)
            DEMO_SPEED="$2"
            shift 2
            ;;
        --help|-h)
            cat << EOF
Neo4j Kubernetes Operator Demo Script

Usage: $0 [options]

Options:
  --namespace NAMESPACE     Kubernetes namespace for demo (default: default)
  --password PASSWORD       Admin password (default: demo123456)
  --skip-confirmations      Skip interactive confirmations
  --speed SPEED             Demo speed: fast, normal, slow (default: normal)
  --help, -h                Show this help

Environment Variables:
  DEMO_NAMESPACE           Same as --namespace
  ADMIN_PASSWORD           Same as --password
  SKIP_CONFIRMATIONS       Set to 'true' to skip confirmations
  DEMO_SPEED              Same as --speed

Examples:
  $0                                    # Interactive demo
  $0 --skip-confirmations --speed fast  # Fast automated demo
  $0 --namespace demo --password secret123  # Custom namespace and password
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

# Run the demo
main "$@"
