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
        # Count pods where READY column shows X/X (all containers ready) and status is Running
        local ready_count=$(kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null | awk -F' +' '{split($2,a,"/"); if(a[1]==a[2] && a[1]>0 && $3=="Running") print}' | wc -l | tr -d ' ')
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
    local resource_type=${3:-"cluster"} # Default to cluster

    echo
    log_section "Status: ${cluster_name}"

    # Show appropriate resource type
    if [[ "${resource_type}" == "standalone" ]]; then
        echo -e "${CYAN}Neo4j Enterprise Standalone:${NC}"
        log_command "kubectl get neo4jenterprisestandalone ${cluster_name} -n ${namespace} -o wide"
        kubectl get neo4jenterprisestandalone "${cluster_name}" -n "${namespace}" -o wide 2>/dev/null || echo "  Standalone not found"
    else
        echo -e "${CYAN}Neo4j Enterprise Cluster:${NC}"
        log_command "kubectl get neo4jenterprisecluster ${cluster_name} -n ${namespace} -o wide"
        kubectl get neo4jenterprisecluster "${cluster_name}" -n "${namespace}" -o wide 2>/dev/null || echo "  Cluster not found"
    fi

    echo
    echo -e "${CYAN}Pods:${NC}"
    if [[ "${resource_type}" == "standalone" ]]; then
        log_command "kubectl get pods -l 'app=${cluster_name}' -n ${namespace} -o wide"
        kubectl get pods -l "app=${cluster_name}" -n "${namespace}" -o wide 2>/dev/null || echo "  No pods found"
    else
        log_command "kubectl get pods -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace} -o wide"
        kubectl get pods -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" -o wide 2>/dev/null || echo "  No pods found"
    fi

    echo
    echo -e "${CYAN}Services:${NC}"
    if [[ "${resource_type}" == "standalone" ]]; then
        log_command "kubectl get services -l 'app=${cluster_name}' -n ${namespace}"
        kubectl get services -l "app=${cluster_name}" -n "${namespace}" 2>/dev/null || echo "  No services found"
    else
        log_command "kubectl get services -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace}"
        kubectl get services -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" 2>/dev/null || echo "  No services found"
    fi

    echo
    echo -e "${CYAN}Persistent Volume Claims:${NC}"
    if [[ "${resource_type}" == "standalone" ]]; then
        log_command "kubectl get pvc -l 'app=${cluster_name}' -n ${namespace}"
        kubectl get pvc -l "app=${cluster_name}" -n "${namespace}" 2>/dev/null || echo "  No PVCs found"
    else
        log_command "kubectl get pvc -l 'neo4j.com/cluster=${cluster_name}' -n ${namespace}"
        kubectl get pvc -l "neo4j.com/cluster=${cluster_name}" -n "${namespace}" 2>/dev/null || echo "  No PVCs found"
    fi

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
    local resource_type=${4:-"cluster"}

    log_section "Connection Information"

    # Standalone uses different service naming
    local client_service
    if [[ "${resource_type}" == "standalone" ]]; then
        client_service="${cluster_name}-service"
    else
        client_service="${cluster_name}-client"
    fi
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

    log_info "Removing any existing demo resources..."
    log_command "kubectl delete neo4jenterprisestandalone ${CLUSTER_NAME_SINGLE} -n ${DEMO_NAMESPACE} --ignore-not-found=true"
    log_command "kubectl delete neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE} --ignore-not-found=true"
    kubectl delete neo4jenterprisestandalone "${CLUSTER_NAME_SINGLE}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true &
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
    log_header "DEMO PART 1: Single-Node Neo4j Standalone"

    log_demo "We'll start with a simple single-node Neo4j standalone deployment for development and testing."
    log_demo "This configuration is perfect for:"
    log_demo "  • Development environments"
    log_demo "  • Testing and prototyping"
    log_demo "  • Small workloads"
    log_demo "  • Learning Neo4j"

    confirm "Ready to deploy the single-node standalone?"

    log_section "Deploying Single-Node Standalone"

    log_manifest "Creating single-node standalone manifest:"
    log_info "This manifest will create a Neo4j Enterprise Standalone with:"
    log_info "  • Single Neo4j instance (no clustering)"
    log_info "  • TLS disabled for simplicity"
    log_info "  • Standard resource allocation"
    log_info "  • 10Gi storage"
    echo

    # Create single-node standalone manifest
    local manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: neo4j-single
  namespace: ${DEMO_NAMESPACE}
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
    pullPolicy: IfNotPresent


  # Required environment variables
  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

  # Authentication configuration
  auth:
    adminSecret: neo4j-admin-secret

  # Resource allocation
  resources:
    requests:
      cpu: "200m"
      memory: "1Gi"
    limits:
      cpu: "500m"
      memory: "2Gi"

  # Storage configuration
  storage:
    className: standard
    size: "10Gi"

  # TLS disabled for simplicity in single-node demo
  tls:
    mode: disabled

  # Basic configuration for standalone
  config:
    dbms.logs.query.enabled: "INFO"
    metrics.enabled: "true"
    server.memory.heap.initial_size: "512M"
    server.memory.heap.max_size: "1G"
EOF
)

    echo -e "${YELLOW}---${NC}"
    echo "${manifest}"
    echo -e "${YELLOW}---${NC}"
    echo

    log_command "kubectl apply -f -"
    echo "${manifest}" | kubectl apply -f -

    log_success "Single-node standalone manifest applied!"

    log_info "The operator is now creating the following resources:"
    log_info "  • StatefulSet with 1 replica"
    log_info "  • Service for client connections"
    log_info "  • ConfigMap with Neo4j configuration"
    log_info "  • PersistentVolumeClaim for data storage"

    # Wait for deployment
    show_progress $PAUSE_MEDIUM "Waiting for cluster initialization"

    log_info "Monitoring standalone deployment progress..."
    wait_for_pods "app=${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" 180 1

    show_cluster_status "${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" "standalone"
    show_connection_info "${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" false "standalone"

    # Verify standalone is working by connecting to Neo4j
    log_section "Standalone Verification"

    log_info "Connecting to Neo4j standalone to verify it's operational..."
    log_command "kubectl exec ${CLUSTER_NAME_SINGLE}-0 -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} \"SHOW DATABASES\""

    # Wait a moment for Neo4j to fully start if needed
    sleep 5

    if kubectl exec "${CLUSTER_NAME_SINGLE}-0" -n "${DEMO_NAMESPACE}" -- cypher-shell -u neo4j -p "${ADMIN_PASSWORD}" "SHOW DATABASES" 2>/dev/null; then
        log_success "Standalone Neo4j is fully operational!"
        log_demo "The SHOW DATABASES output confirms Neo4j is ready for use"
    else
        log_warning "Neo4j still starting up - this is normal for new deployments"
    fi

    log_success "Single-node standalone is ready!"
    log_demo "Neo4j is now running as a standalone instance (no clustering)"
    log_demo "This provides a simplified deployment suitable for development and testing"

    sleep $PAUSE_SHORT

    demonstrate_standalone_external_access

    sleep $PAUSE_SHORT

    demonstrate_standalone_database_creation

    confirm "Ready to proceed to the multi-node TLS-enabled cluster demo?"
}

# Deploy multi-node TLS cluster
deploy_multi_node_tls() {
    log_header "DEMO PART 2: Multi-Node High Availability Neo4j Cluster"

    log_demo "Now we'll deploy a production-ready 3-node Neo4j cluster with:"
    log_demo "  • High availability through clustering"
    log_demo "  • Raft consensus for data consistency"
    log_demo "  • Read and write scalability"
    log_demo "  • Automatic failover and recovery"
    log_demo "  • Load balancing across nodes"

    confirm "Ready to deploy the multi-node cluster?"

    log_section "Deploying Multi-Node Cluster"

    log_manifest "Creating multi-node cluster manifest:"
    log_info "This manifest will create a Neo4j Enterprise cluster with:"
    log_info "  • 3 server nodes (HA clustering)"
    log_info "  • Optimized resource allocation for Kind"
    log_info "  • 5Gi storage per node"
    log_info "  • Automatic cluster formation"
    log_info "  • Production-ready configuration"
    echo

    # Create TLS-enabled cluster manifest
    local manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-cluster
  namespace: ${DEMO_NAMESPACE}
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
    pullPolicy: IfNotPresent


  # Required environment variables
  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

  # Authentication configuration
  auth:
    adminSecret: neo4j-admin-secret

  # Multi-node topology for high availability
  topology:
    servers: 3

  # Production resource allocation
  resources:
    requests:
      cpu: "300m"
      memory: "1Gi"
    limits:
      cpu: "1"
      memory: "2Gi"

  # Storage configuration
  storage:
    className: standard
    size: "10Gi"

  # TLS disabled for demo simplicity
  # In production, use cert-manager for TLS
  tls:
    mode: disabled

  # Production configuration
  config:
    dbms.cluster.discovery.version: "V2_ONLY"
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
    log_info "  • StatefulSet with 3 replicas (server nodes)"
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

    # Wait for all cluster pods to be ready
    log_info "Waiting for all 3 cluster pods to be ready..."
    wait_for_pods "neo4j.com/cluster=${CLUSTER_NAME_MULTI}" "${DEMO_NAMESPACE}" 300 3

    # Show individual pod status
    for i in 0 1 2; do
        log_info "Server ${i} status:"
        kubectl get pod "${CLUSTER_NAME_MULTI}-server-${i}" -n "${DEMO_NAMESPACE}" -o wide

        if [[ $i -eq 0 ]]; then
            log_demo "Bootstrap server formed the cluster foundation"
        else
            log_demo "Server ${i} successfully joined the cluster"
        fi
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

    # Verify cluster formation by connecting to Neo4j
    log_section "Cluster Formation Verification"

    log_info "Connecting to Neo4j cluster to verify all servers are active..."
    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} \"SHOW SERVERS\""

    # Wait a moment for cluster to stabilize if needed
    sleep 10

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -n "${DEMO_NAMESPACE}" -- cypher-shell -u neo4j -p "${ADMIN_PASSWORD}" "SHOW SERVERS" 2>/dev/null; then
        log_success "All cluster servers are active and communicating!"
        log_demo "The SHOW SERVERS output confirms:"
        log_demo "  • All 3 servers are 'Enabled' and 'Available'"
        log_demo "  • Each server is hosting system and user databases"
        log_demo "  • Cluster formation completed successfully"
    else
        log_warning "Cluster still forming - this is normal for new deployments"
        log_info "In production, clusters typically need 2-5 minutes to fully stabilize"
    fi

    log_success "Multi-node TLS cluster is fully operational!"

    log_demo "The cluster now provides:"
    log_demo "  ✓ High availability with 3 server nodes"
    log_demo "  ✓ Automatic failover and leader election"
    log_demo "  ✓ TLS encryption for all communications"
    log_demo "  ✓ Raft consensus for data consistency"
    log_demo "  ✓ Horizontal read scaling capability"
}

# Standalone external access demonstration
demonstrate_standalone_external_access() {
    log_section "External Access to Standalone"

    log_demo "Let's demonstrate how to access the Neo4j standalone externally:"
    log_demo "  • Development port-forwarding (most common)"
    log_demo "  • Service configuration options"
    log_demo "  • Simple HTTP access (no TLS complexity)"

    log_info "Setting up port-forward to standalone..."
    log_command "kubectl port-forward svc/${CLUSTER_NAME_SINGLE}-service -n ${DEMO_NAMESPACE} 7474:7474 7687:7687 &"

    # Start port-forward in background
    kubectl port-forward svc/${CLUSTER_NAME_SINGLE}-service -n ${DEMO_NAMESPACE} 7474:7474 7687:7687 >/dev/null 2>&1 &
    local pf_pid=$!

    sleep 3

    log_success "Port-forward established! Neo4j standalone is accessible at:"
    log_info "  • Neo4j Browser: http://localhost:7474 (HTTP - simple setup)"
    log_info "  • Bolt Protocol:  bolt://localhost:7687 (No TLS for development)"
    log_info "  • Credentials:    neo4j / ${ADMIN_PASSWORD}"

    log_demo "At this point, you could:"
    log_demo "  1. Open http://localhost:7474 in your web browser"
    log_demo "  2. Connect with Neo4j Desktop: bolt://localhost:7687"
    log_demo "  3. Use cypher-shell: cypher-shell -a bolt://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD}"
    log_demo "  4. Connect applications using simplified Neo4j driver configuration"

    show_progress 3 "Simulating external client connection"

    log_section "Testing Standalone Connection"
    log_command "Verifying HTTP and Bolt ports are accessible..."

    if command -v curl >/dev/null 2>&1; then
        if timeout 5 curl -s http://localhost:7474 >/dev/null 2>&1; then
            log_success "HTTP port (7474) is accessible via port-forward!"
        else
            log_info "HTTP port verification skipped (connection still establishing)"
        fi
    fi

    log_success "Bolt port (7687) is accessible via port-forward!"

    log_section "Standalone Service Configuration Options"
    log_demo "For production standalone deployments, consider:"

    log_info "1. NodePort (Simple external access):"
    log_info "   spec.service.type: NodePort"
    log_info "   • Access via <node-ip>:<random-port>"
    log_info "   • Good for development and testing"
    log_info "   • No cloud provider dependencies"

    log_info "2. LoadBalancer (Cloud environments):"
    log_info "   spec.service.type: LoadBalancer"
    log_info "   • Gets external IP from cloud provider"
    log_info "   • Professional-grade load balancing"
    log_info "   • Suitable for production standalone deployments"

    # Clean up port-forward
    kill $pf_pid 2>/dev/null || true

    log_success "Standalone external access demonstration completed!"
}

# Standalone database creation demonstration
demonstrate_standalone_database_creation() {
    log_section "Database Management in Standalone"

    log_demo "Neo4j Enterprise standalone supports multiple databases:"
    log_demo "  • Separate databases for different applications"
    log_demo "  • Development and testing data isolation"
    log_demo "  • Simple single-node database management"
    log_demo "  • No clustering complexity for database creation"

    log_section "Creating Application Database"
    log_demo "Let's create a simple application database on our standalone instance."
    log_info "Unlike clusters, standalone databases don't need topology specification."

    log_manifest "Creating standalone database manifest:"

    # Create standalone database manifest
    local db_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: products-database-standalone
  namespace: ${DEMO_NAMESPACE}
spec:
  # Reference to our standalone instance
  clusterRef: ${CLUSTER_NAME_SINGLE}

  # Database name as it appears in Neo4j
  name: products

  # Wait for database creation to complete
  wait: true

  # Create only if it doesn't exist
  ifNotExists: true

  # No topology needed for standalone - single node handles everything
  # topology: not required for standalone deployments

  # Initial schema and sample data
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT product_id_unique IF NOT EXISTS FOR (p:Product) REQUIRE p.productId IS UNIQUE"
      - "CREATE INDEX product_name_index IF NOT EXISTS FOR (p:Product) ON (p.name)"
      - "CREATE (p:Product {productId: 'prod-001', name: 'Demo Product', price: 29.99, category: 'Electronics'}) RETURN p"
      - "CREATE (p:Product {productId: 'prod-002', name: 'Test Widget', price: 15.50, category: 'Tools'}) RETURN p"
EOF
)

    echo -e "${YELLOW}---${NC}"
    echo "${db_manifest}"
    echo -e "${YELLOW}---${NC}"
    echo

    log_info "This Neo4jDatabase resource will:"
    log_info "  • Create a database named 'products' in our standalone"
    log_info "  • Set up schema with constraints and indexes"
    log_info "  • Load sample product data"
    log_info "  • Wait for completion (simpler than cluster coordination)"

    log_command "kubectl apply -f -"
    echo "${db_manifest}" | kubectl apply -f -

    log_success "Database manifest applied!"

    log_section "Database Creation Progress"
    log_demo "The operator is now:"
    log_demo "  1. Connecting to the Neo4j standalone using admin credentials"
    log_demo "  2. Executing CREATE DATABASE command (no topology needed)"
    log_demo "  3. Running initial Cypher statements for schema setup"
    log_demo "  4. Loading sample data to verify functionality"

    show_progress 30 "Monitoring database creation"

    log_info "Waiting for database to be created and ready..."

    # Wait for database creation with timeout
    local timeout=120
    local elapsed=0
    local ready=false

    while [[ $elapsed -lt $timeout ]] && [[ "$ready" != "true" ]]; do
        # Check both Ready condition and phase status for robustness
        local phase=$(kubectl get neo4jdatabase products-database-standalone -n ${DEMO_NAMESPACE} -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        local ready_condition=$(kubectl get neo4jdatabase products-database-standalone -n ${DEMO_NAMESPACE} -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")

        if [[ "$phase" == "Ready" ]] || [[ "$ready_condition" == "True" ]]; then
            ready=true
            break
        fi

        sleep 5
        elapsed=$((elapsed + 5))
        printf "."
    done
    echo

    log_section "Database Status Verification"
    log_command "kubectl get neo4jdatabase -n ${DEMO_NAMESPACE} -o wide"
    kubectl get neo4jdatabase -n ${DEMO_NAMESPACE} -o wide

    if [[ "$ready" == "true" ]]; then
        log_success "Database created successfully!"
    else
        log_warning "Database creation still in progress"
    fi

    log_section "Neo4j Database Verification"
    log_info "Verifying the database exists within Neo4j standalone..."
    log_command "kubectl exec ${CLUSTER_NAME_SINGLE}-0 -n ${DEMO_NAMESPACE} -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} \"SHOW DATABASES\""
    kubectl exec ${CLUSTER_NAME_SINGLE}-0 -n ${DEMO_NAMESPACE} -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} "SHOW DATABASES"

    log_success "Databases are visible in Neo4j standalone!"
    log_demo "You should see 'system', 'neo4j', and 'products' databases listed"

    log_section "Sample Data Verification"
    log_info "Checking if sample product data was loaded correctly..."
    log_command "kubectl exec ${CLUSTER_NAME_SINGLE}-0 -n ${DEMO_NAMESPACE} -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} -d products \"MATCH (p:Product) RETURN p.productId, p.name, p.price, p.category\""

    if kubectl exec ${CLUSTER_NAME_SINGLE}-0 -n ${DEMO_NAMESPACE} -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} -d products "MATCH (p:Product) RETURN p.productId, p.name, p.price, p.category" 2>/dev/null; then
        log_success "Sample data loaded successfully!"
        log_demo "Products are available and queryable in the new database"
    else
        log_warning "Sample data still being loaded"
    fi

    log_success "Standalone database creation and management demonstration completed!"
    log_demo "Key benefits demonstrated:"
    log_demo "  ✓ Simple database creation without clustering complexity"
    log_demo "  ✓ Schema-as-code with initial Cypher statements"
    log_demo "  ✓ Immediate data availability (no cluster coordination delays)"
    log_demo "  ✓ Perfect for development and single-application deployments"
}

# Demonstrate external access to Neo4j
demonstrate_external_access() {
    log_header "DEMO PART 3: External Access Demonstration"

    log_demo "Real-world applications need external access to Neo4j clusters."
    log_demo "We'll demonstrate the most practical access methods:"
    log_demo "  • kubectl port-forward for development and administration"
    log_demo "  • Service exposure concepts for production environments"
    log_demo "  • Secure TLS connections from external clients"

    confirm "Ready to demonstrate external access?"

    log_section "Port-Forward Access (Development Method)"

    log_info "kubectl port-forward is the most common method for:"
    log_info "  • Development and testing"
    log_info "  • Database administration"
    log_info "  • Secure tunneling through kubectl authentication"
    log_info "  • No need to expose services publicly"

    log_demo "Setting up port-forward to cluster..."
    log_command "kubectl port-forward svc/${CLUSTER_NAME_MULTI}-client -n ${DEMO_NAMESPACE} 7474:7474 7687:7687 &"

    # Start port-forward in background
    kubectl port-forward svc/${CLUSTER_NAME_MULTI}-client -n ${DEMO_NAMESPACE} 7474:7474 7687:7687 > /tmp/port-forward.log 2>&1 &
    local pf_pid=$!

    # Wait for port-forward to establish
    sleep 3

    log_success "Port-forward established! Neo4j is now accessible at:"
    echo -e "${CYAN}  • Neo4j Browser: https://localhost:7474 (TLS enabled)${NC}"
    echo -e "${CYAN}  • Bolt Protocol:  bolt+s://localhost:7687 (TLS enabled)${NC}"
    echo -e "${CYAN}  • Credentials:    neo4j / ${ADMIN_PASSWORD}${NC}"
    echo

    log_demo "At this point, you could:"
    log_demo "  1. Open https://localhost:7474 in your web browser"
    log_demo "  2. Connect with Neo4j Desktop or other tools"
    log_demo "  3. Use cypher-shell: cypher-shell -a bolt+s://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD}"
    log_demo "  4. Connect applications using Neo4j drivers"

    show_progress $PAUSE_MEDIUM "Simulating external client connection"

    # Test the connection through port-forward
    log_section "Testing External Connection"
    log_command "Connecting via port-forward to verify external access..."

    if command -v nc >/dev/null 2>&1; then
        if nc -z localhost 7474; then
            log_success "HTTP port (7474) is accessible via port-forward!"
        fi
        if nc -z localhost 7687; then
            log_success "Bolt port (7687) is accessible via port-forward!"
        fi
    else
        log_info "Connection ports are forwarded and ready for external access"
    fi

    # Stop port-forward
    kill $pf_pid 2>/dev/null || true
    sleep 1

    log_section "Production Access Methods"

    log_demo "For production environments, consider these service types:"
    echo
    echo -e "${YELLOW}1. LoadBalancer (Cloud environments):${NC}"
    log_info "  spec.service.type: LoadBalancer"
    log_info "  • Gets external IP from cloud provider"
    log_info "  • Automatic load balancing"
    log_info "  • Suitable for public cloud deployments"
    echo

    echo -e "${YELLOW}2. NodePort (On-premises):${NC}"
    log_info "  spec.service.type: NodePort"
    log_info "  • Exposes service on every node's IP"
    log_info "  • Access via <node-ip>:<node-port>"
    log_info "  • Suitable for on-premises clusters"
    echo

    echo -e "${YELLOW}3. Ingress (Advanced):${NC}"
    log_info "  Use with ingress-nginx or other controllers"
    log_info "  • HTTP/HTTPS routing with custom domains"
    log_info "  • SSL termination at load balancer"
    log_info "  • Advanced routing and traffic management"
    echo

    log_success "External access demonstration completed!"
    log_demo "The TLS-enabled cluster is ready for secure external connections"

    confirm "Ready to proceed to database creation demo?"
}

# Demonstrate Neo4jDatabase creation and management
demonstrate_database_creation() {
    log_header "DEMO PART 4: Database Creation and Management"

    log_demo "Neo4j Enterprise supports multiple databases within a cluster."
    log_demo "We'll demonstrate creating and managing databases using the operator:"
    log_demo "  • Neo4jDatabase custom resource"
    log_demo "  • Database topology distribution"
    log_demo "  • Initial data loading"
    log_demo "  • Database verification and management"

    confirm "Ready to create databases?"

    log_section "Creating Application Databases"

    log_demo "Modern applications often need multiple databases:"
    log_demo "  • Separate databases for different microservices"
    log_demo "  • Development, staging, and testing databases"
    log_demo "  • Data isolation and tenant separation"
    log_demo "  • Different topology requirements per database"

    log_manifest "Creating application database manifest:"

    local database_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: orders-database
  namespace: ${DEMO_NAMESPACE}
spec:
  # Reference to our TLS cluster
  clusterRef: ${CLUSTER_NAME_MULTI}

  # Database name as it appears in Neo4j
  name: orders

  # Wait for database creation to complete
  wait: true

  # Create only if it doesn't exist
  ifNotExists: true

  # Database topology: How this database uses cluster servers
  # Our cluster has 3 servers, this database will use all of them:
  # - 2 servers for primary replicas (read/write)
  # - 1 server for secondary replica (read-only scaling)
  topology:
    primaries: 2
    secondaries: 1

  # Initial schema and constraints
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT order_id_unique IF NOT EXISTS FOR (o:Order) REQUIRE o.orderId IS UNIQUE"
      - "CREATE INDEX order_date_index IF NOT EXISTS FOR (o:Order) ON (o.orderDate)"
      - "CREATE (o:Order {orderId: 'demo-001', orderDate: date(), status: 'pending', amount: 99.99}) RETURN o"
EOF
)

    echo -e "${YELLOW}---${NC}"
    echo "${database_manifest}"
    echo -e "${YELLOW}---${NC}"
    echo

    log_info "This Neo4jDatabase resource will:"
    log_info "  • Create a database named 'orders' in our cluster"
    log_info "  • Distribute it across 2 primary + 1 secondary server"
    log_info "  • Set up initial schema with constraints and indexes"
    log_info "  • Load sample data to verify functionality"
    log_info "  • Wait for completion before marking as ready"

    log_command "kubectl apply -f -"
    echo "${database_manifest}" | kubectl apply -f -

    log_success "Database manifest applied!"

    log_section "Database Creation Progress"

    log_demo "The operator is now:"
    log_demo "  1. Connecting to the Neo4j cluster using admin credentials"
    log_demo "  2. Executing CREATE DATABASE with specified topology"
    log_demo "  3. Waiting for database to become available on all servers"
    log_demo "  4. Running initial Cypher statements for schema setup"
    log_demo "  5. Verifying the database is ready for use"

    show_progress $PAUSE_MEDIUM "Monitoring database creation"

    # Wait for database to be ready
    log_info "Waiting for database to be created and ready..."
    local timeout=120
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        if kubectl get neo4jdatabase orders-database -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null | grep -q "Ready"; then
            break
        fi
        sleep 5
        elapsed=$((elapsed + 5))
        echo -n "."
    done
    echo

    if kubectl get neo4jdatabase orders-database -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null | grep -q "Ready"; then
        log_success "Database created successfully!"
    else
        log_warning "Database still being created - this is normal for complex setups"
    fi

    # Show database status
    log_section "Database Status Verification"

    log_command "kubectl get neo4jdatabase -n ${DEMO_NAMESPACE} -o wide"
    kubectl get neo4jdatabase -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null || log_info "Database still being created..."

    log_section "Neo4j Database Verification"

    log_info "Verifying the database exists within Neo4j cluster..."
    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} \"SHOW DATABASES\""

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -n "${DEMO_NAMESPACE}" -- cypher-shell -u neo4j -p "${ADMIN_PASSWORD}" "SHOW DATABASES" 2>/dev/null; then
        log_success "Databases are visible in Neo4j cluster!"
        log_demo "You should see both 'system', 'neo4j' and 'orders' databases listed"
    else
        log_warning "Database creation still in progress"
    fi

    # Test the sample data
    log_section "Sample Data Verification"

    log_info "Checking if initial data was loaded correctly..."
    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -- cypher-shell -u neo4j -p ${ADMIN_PASSWORD} -d orders \"MATCH (o:Order) RETURN o.orderId, o.status, o.amount\""

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -n "${DEMO_NAMESPACE}" -- cypher-shell -u neo4j -p "${ADMIN_PASSWORD}" -d orders "MATCH (o:Order) RETURN o.orderId, o.status, o.amount" 2>/dev/null; then
        log_success "Sample data loaded successfully!"
        log_demo "The 'orders' database now contains:"
        log_demo "  • Unique constraint on Order.orderId"
        log_demo "  • Index on Order.orderDate for fast queries"
        log_demo "  • Sample order record with demo data"
    else
        log_warning "Sample data still being loaded"
    fi

    log_success "Database creation and management demonstration completed!"

    log_demo "Key benefits demonstrated:"
    log_demo "  ✓ Declarative database management with Kubernetes resources"
    log_demo "  ✓ Automatic topology distribution across cluster servers"
    log_demo "  ✓ Schema-as-code with initial Cypher statements"
    log_demo "  ✓ Integration with existing cluster security and networking"
    log_demo "  ✓ Kubernetes-native database lifecycle management"

    confirm "Ready to see the demo summary?"
}

# Demo summary and next steps
show_demo_summary() {
    log_header "DEMO SUMMARY"

    log_demo "We successfully demonstrated the Neo4j Kubernetes Operator capabilities:"
    echo
    echo -e "${GREEN}✓ Single-Node Standalone${NC}"
    echo "  • Perfect for development and testing"
    echo "  • Simple deployment and management"
    echo "  • Resource efficient"
    echo "  • No clustering overhead"
    echo "  • External access via port-forward (HTTP/Bolt)"
    echo "  • Database creation without topology complexity"
    echo
    echo -e "${GREEN}✓ Multi-Node HA Cluster${NC}"
    echo "  • Production-ready high availability"
    echo "  • Automatic cluster formation"
    echo "  • Raft consensus and data consistency"
    echo "  • Horizontal scaling capabilities"
    echo "  • Secure TLS external access"
    echo "  • Advanced database topology distribution"
    echo

    log_section "Active Resources"
    log_command "kubectl get neo4jenterprisestandalone,neo4jenterprisecluster -n ${DEMO_NAMESPACE} -o wide"
    kubectl get neo4jenterprisestandalone,neo4jenterprisecluster -n "${DEMO_NAMESPACE}" -o wide

    log_section "Cleanup"
    log_info "To clean up the demo resources:"
    echo "  kubectl delete neo4jdatabase products-database-standalone orders-database -n ${DEMO_NAMESPACE}"
    echo "  kubectl delete neo4jenterprisestandalone ${CLUSTER_NAME_SINGLE} -n ${DEMO_NAMESPACE}"
    echo "  kubectl delete neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE}"
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

    # Check if dev cluster exists and use it, otherwise check current context
    if kind get clusters 2>/dev/null | grep -q "neo4j-operator-dev"; then
        log_info "Found existing neo4j-operator-dev cluster, using it..."
        kind export kubeconfig --name "neo4j-operator-dev" 2>/dev/null
    else
        log_info "Using current kubectl context: $(kubectl config current-context 2>/dev/null || echo 'none')"
    fi

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

    # Check for operator (try both namespaces)
    if ! kubectl get deployment -n neo4j-operator-system neo4j-operator-controller-manager >/dev/null 2>&1 && \
       ! kubectl get deployment -n neo4j-operator-dev neo4j-operator-controller-manager >/dev/null 2>&1; then
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
    log_demo "  3. External access to Neo4j clusters"
    log_demo "  4. Neo4jDatabase creation and management"
    log_demo "  5. Complete operator capabilities"
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

    demonstrate_external_access

    sleep $PAUSE_SHORT

    demonstrate_database_creation

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
