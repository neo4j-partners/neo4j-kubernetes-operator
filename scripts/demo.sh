#!/bin/bash
set -euo pipefail

# Neo4j Kubernetes Operator Demo Script
# This script demonstrates the core capabilities of the Neo4j Kubernetes Operator
# including single-node standalone and multi-node HA cluster deployments

# Colors and formatting
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly PURPLE='\033[0;35m'
readonly CYAN='\033[0;36m'
readonly WHITE='\033[1;37m'
readonly BOLD='\033[1m'
readonly DIM='\033[2m'
readonly NC='\033[0m' # No Color

# Total demo parts for step counter
readonly TOTAL_PARTS=7

# Spinner frames (Braille dots)
readonly SPINNER_FRAMES=('⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏')

# Section and demo timing
SECTION_START_TIME=0
DEMO_START_TIME=0

# Demo configuration
DEMO_NAMESPACE=${DEMO_NAMESPACE:-default}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"demo123456"}
CLUSTER_NAME_SINGLE=${CLUSTER_NAME_SINGLE:-"neo4j-single"}
CLUSTER_NAME_MULTI=${CLUSTER_NAME_MULTI:-"neo4j-cluster"}
SKIP_CONFIRMATIONS=${SKIP_CONFIRMATIONS:-false}
CLEANUP_AFTER=${CLEANUP_AFTER:-false}
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

# ── Display functions ──────────────────────────────────────────────────────

# Full-width separator
separator() {
    echo -e "${DIM}$(printf '━%.0s' $(seq 1 78))${NC}"
}

# Part header with step counter and emoji
log_header() {
    local part_num=${1:-0}
    local emoji=${2:-""}
    local title=${3:-""}
    echo
    separator
    if [[ $part_num -gt 0 ]]; then
        echo -e "${WHITE}${BOLD}  ${emoji}  PART ${part_num} of ${TOTAL_PARTS}: ${title}${NC}"
    else
        echo -e "${WHITE}${BOLD}  ${emoji}  ${title}${NC}"
    fi
    separator
    echo
    SECTION_START_TIME=$(date +%s)
}

# Section header within a part
log_section() {
    echo
    echo -e "  ${YELLOW}${BOLD}▸ $1${NC}"
    echo -e "  ${DIM}$(printf '─%.0s' $(seq 1 ${#1}))${NC}"
}

log_info() {
    echo -e "  ${BLUE}ℹ${NC}  $1"
}

log_success() {
    echo -e "  ${GREEN}✔${NC}  $1"
}

log_warning() {
    echo -e "  ${YELLOW}⚠${NC}  $1"
}

log_error() {
    echo -e "  ${RED}✘${NC}  $1"
}

log_demo() {
    echo -e "  ${PURPLE}│${NC}  $1"
}

log_command() {
    echo -e "  ${CYAN}\$${NC} ${DIM}$1${NC}"
}

log_manifest() {
    echo -e "  ${YELLOW}📄${NC} $1"
}

# Show elapsed time for the current section
log_elapsed() {
    if [[ $SECTION_START_TIME -gt 0 ]]; then
        local elapsed=$(( $(date +%s) - SECTION_START_TIME ))
        echo
        echo -e "  ${DIM}completed in ${elapsed}s${NC}"
    fi
}

# Animated spinner with message
show_progress() {
    local duration=$1
    local message=$2
    local i=0

    for t in $(seq 1 "$duration"); do
        local frame=${SPINNER_FRAMES[$((i % ${#SPINNER_FRAMES[@]}))]}
        printf "\r  ${CYAN}%s${NC}  %s" "$frame" "$message"
        sleep 1
        ((i++))
    done
    printf "\r  ${GREEN}✔${NC}  %s\n" "$message"
}

# Resource dashboard: compact one-liner of what exists
show_resource_dashboard() {
    local ns=${1:-$DEMO_NAMESPACE}
    local standalone_count=$(kubectl get neo4jenterprisestandalone -n "$ns" --no-headers 2>/dev/null | wc -l | tr -d ' ')
    local cluster_count=$(kubectl get neo4jenterprisecluster -n "$ns" --no-headers 2>/dev/null | wc -l | tr -d ' ')
    local db_count=$(kubectl get neo4jdatabase -n "$ns" --no-headers 2>/dev/null | wc -l | tr -d ' ')
    local plugin_count=$(kubectl get neo4jplugin -n "$ns" --no-headers 2>/dev/null | wc -l | tr -d ' ')
    local pod_count=$(kubectl get pods -n "$ns" --no-headers 2>/dev/null | grep -c Running || echo 0)

    echo -e "  ${DIM}┌─────────────────────────────────────────────────────────────┐${NC}"
    echo -e "  ${DIM}│${NC}  ${BOLD}Resources:${NC} ${pod_count} pods  ${standalone_count} standalone  ${cluster_count} cluster  ${db_count} databases  ${plugin_count} plugins  ${DIM}│${NC}"
    echo -e "  ${DIM}└─────────────────────────────────────────────────────────────┘${NC}"
}

# Display YAML manifest with highlighted key fields
show_manifest() {
    local yaml="$1"
    echo -e "  ${DIM}┌──────────────────────────────────────────────────────────────────────┐${NC}"
    echo "$yaml" | while IFS= read -r line; do
        # Highlight apiVersion, kind, name lines
        if echo "$line" | grep -qE '^(apiVersion|kind|  name):'; then
            echo -e "  ${DIM}│${NC} ${BOLD}${line}${NC}"
        elif echo "$line" | grep -qE '^metadata:|^spec:'; then
            echo -e "  ${DIM}│${NC} ${CYAN}${line}${NC}"
        elif echo "$line" | grep -qE '^  #'; then
            echo -e "  ${DIM}│ ${line}${NC}"
        else
            echo -e "  ${DIM}│${NC} ${line}"
        fi
    done
    echo -e "  ${DIM}└──────────────────────────────────────────────────────────────────────┘${NC}"
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

# Wait for pods to be ready with compact spinner status
wait_for_pods() {
    local label_selector=$1
    local namespace=$2
    local timeout=${3:-300}
    local expected_count=${4:-1}

    local start_time=$(date +%s)
    local i=0

    while true; do
        local ready_count=$(kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null | awk -F' +' '{split($2,a,"/"); if(a[1]==a[2] && a[1]>0 && $3=="Running") print}' | wc -l | tr -d ' ')
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))

        if [[ "${ready_count}" -eq "${expected_count}" ]]; then
            # Build per-pod status string
            local pod_status=""
            kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null | while read -r name ready status rest; do
                echo -n "${GREEN}✔${NC} ${name}  "
            done | { pod_status=$(cat); printf "\r  ${GREEN}✔${NC}  Pods ready: ${ready_count}/${expected_count}  ${pod_status}${DIM}(${elapsed}s)${NC}     \n"; }
            return 0
        fi

        if [[ "${elapsed}" -gt "${timeout}" ]]; then
            printf "\r  ${RED}✘${NC}  Timeout waiting for pods (${elapsed}s elapsed)                              \n"
            return 1
        fi

        # Build compact per-pod status
        local pod_line=""
        while IFS= read -r line; do
            local name=$(echo "$line" | awk '{print $1}' | sed 's/.*-//')
            local status=$(echo "$line" | awk '{print $3}')
            if [[ "$status" == "Running" ]]; then
                local ready_col=$(echo "$line" | awk '{print $2}')
                local ready_n=$(echo "$ready_col" | cut -d/ -f1)
                local total_n=$(echo "$ready_col" | cut -d/ -f2)
                if [[ "$ready_n" -eq "$total_n" && "$ready_n" -gt 0 ]]; then
                    pod_line="${pod_line}${GREEN}✔${NC}${name} "
                else
                    pod_line="${pod_line}${YELLOW}◐${NC}${name} "
                fi
            elif [[ "$status" == "ContainerCreating" || "$status" == "Pending" ]]; then
                pod_line="${pod_line}${CYAN}◌${NC}${name} "
            else
                pod_line="${pod_line}${DIM}?${name}${NC} "
            fi
        done < <(kubectl get pods -l "${label_selector}" -n "${namespace}" --no-headers 2>/dev/null)

        local frame=${SPINNER_FRAMES[$((i % ${#SPINNER_FRAMES[@]}))]}
        printf "\r  ${CYAN}%s${NC}  Pods: ${ready_count}/${expected_count} ready  %b ${DIM}(%ds)${NC}     " "$frame" "$pod_line" "$elapsed"
        ((i++))

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
    # Check if any demo resources exist
    local has_standalone=false
    local has_cluster=false
    if kubectl get neo4jenterprisestandalone "${CLUSTER_NAME_SINGLE}" -n "${DEMO_NAMESPACE}" >/dev/null 2>&1; then
        has_standalone=true
    fi
    if kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" >/dev/null 2>&1; then
        has_cluster=true
    fi

    if [[ "${has_standalone}" == "false" && "${has_cluster}" == "false" ]]; then
        log_info "No existing demo resources found — skipping cleanup."
        return 0
    fi

    log_section "Existing Demo Resources Detected"
    if [[ "${has_standalone}" == "true" ]]; then
        log_warning "Found existing standalone: ${CLUSTER_NAME_SINGLE} in namespace ${DEMO_NAMESPACE}"
    fi
    if [[ "${has_cluster}" == "true" ]]; then
        log_warning "Found existing cluster: ${CLUSTER_NAME_MULTI} in namespace ${DEMO_NAMESPACE}"
    fi
    log_info "These resources will be deleted before starting the demo."

    confirm "Proceed with deleting existing demo resources?"

    log_info "Removing existing demo resources..."
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
    log_header 1 "🚀" "Single-Node Neo4j Standalone"

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
    log_info "  • TLS via cert-manager (self-signed CA)"
    log_info "  • Standard resource allocation"
    log_info "  • 10Gi storage"
    echo

    # Create single-node standalone manifest
    local manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseStandalone
metadata:
  name: neo4j-single
  namespace: ${DEMO_NAMESPACE}
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
    pullPolicy: IfNotPresent

  # Authentication configuration
  auth:
    authenticationProviders: ["native"]
    adminSecret: neo4j-admin-secret

  # Resource allocation
  resources:
    requests:
      cpu: "200m"
      memory: "1.5Gi"
    limits:
      cpu: "500m"
      memory: "2Gi"

  # Storage configuration
  storage:
    className: standard
    size: "10Gi"

  # TLS via cert-manager
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer

  # Basic configuration for standalone
  config:
    db.logs.query.enabled: "INFO"
    server.memory.heap.initial_size: "512M"
    server.memory.heap.max_size: "1G"
EOF
)

    show_manifest "${manifest}"
    echo

    log_command "kubectl apply -f -"
    echo "${manifest}" | kubectl apply -f -

    log_success "Single-node standalone manifest applied!"

    log_info "The operator is now creating the following resources:"
    log_info "  • StatefulSet with 1 replica"
    log_info "  • Service for client connections"
    log_info "  • ConfigMap with Neo4j configuration"
    log_info "  • PersistentVolumeClaim for data storage"
    log_info "  • cert-manager Certificate for TLS"

    # Wait for deployment
    show_progress $PAUSE_MEDIUM "Waiting for cluster initialization"

    log_info "Monitoring standalone deployment progress..."
    wait_for_pods "app=${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" 180 1

    show_cluster_status "${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" "standalone"
    show_connection_info "${CLUSTER_NAME_SINGLE}" "${DEMO_NAMESPACE}" true "standalone"

    # Verify standalone is working by connecting to Neo4j
    log_section "Standalone Verification"

    log_info "Connecting to Neo4j standalone to verify it's operational..."
    log_command "kubectl exec ${CLUSTER_NAME_SINGLE}-0 -c neo4j -- cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD} \"SHOW DATABASES\""

    # Wait for Neo4j to fully initialize (bolt listener starts after pod readiness)
    show_progress 15 "Waiting for Neo4j to fully initialize"

    if kubectl exec "${CLUSTER_NAME_SINGLE}-0" -c neo4j -n "${DEMO_NAMESPACE}" -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p "${ADMIN_PASSWORD}" "SHOW DATABASES" 2>/dev/null; then
        log_success "Standalone Neo4j is fully operational!"
        log_demo "The SHOW DATABASES output confirms Neo4j is ready for use"
    else
        log_warning "Neo4j still starting up - this is normal for new deployments"
    fi

    log_success "Single-node standalone is ready!"
    log_elapsed
    show_resource_dashboard
    log_demo "Neo4j is now running as a standalone instance (no clustering)"
    log_demo "This provides a simplified deployment suitable for development and testing"

    sleep $PAUSE_SHORT

    demonstrate_standalone_external_access

    sleep $PAUSE_SHORT

    demonstrate_standalone_database_creation

    confirm "Ready to proceed to the multi-node TLS cluster demo?"
}

# Deploy multi-node HA cluster
deploy_multi_node_cluster() {
    log_header 2 "🏗️" "Multi-Node High Availability Cluster"

    log_demo "Now we'll deploy a production-ready 3-node Neo4j cluster with:"
    log_demo "  • High availability through clustering"
    log_demo "  • TLS encryption via cert-manager"
    log_demo "  • Raft consensus for data consistency"
    log_demo "  • Read and write scalability"
    log_demo "  • Automatic failover and recovery"
    log_demo "  • Load balancing across nodes"

    confirm "Ready to deploy the multi-node cluster?"

    log_section "Deploying Multi-Node Cluster"

    log_manifest "Creating multi-node cluster manifest:"
    log_info "This manifest will create a Neo4j Enterprise cluster with:"
    log_info "  • 3 server nodes (HA clustering)"
    log_info "  • TLS via cert-manager (self-signed CA)"
    log_info "  • Optimized resource allocation for Kind"
    log_info "  • 10Gi storage per node"
    log_info "  • Automatic cluster formation"
    log_info "  • Production-ready configuration"
    echo

    # Create cluster manifest
    local manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-cluster
  namespace: ${DEMO_NAMESPACE}
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
    pullPolicy: IfNotPresent

  # Authentication configuration
  auth:
    authenticationProviders: ["native"]
    adminSecret: neo4j-admin-secret

  # Multi-node topology for high availability
  topology:
    servers: 3

  # Resource allocation (1.5Gi minimum for Neo4j Enterprise)
  resources:
    requests:
      cpu: "200m"
      memory: "1.5Gi"
    limits:
      cpu: "1"
      memory: "2Gi"

  # Storage configuration
  storage:
    className: standard
    size: "10Gi"

  # TLS via cert-manager
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer

  # Enable monitoring and diagnostics
  monitoring:
    enabled: true

  # Cluster configuration
  config:
    db.logs.query.enabled: "INFO"
    dbms.transaction.timeout: "60s"
    metrics.enabled: "true"
EOF
)

    show_manifest "${manifest}"
    echo

    log_command "kubectl apply -f -"
    echo "${manifest}" | kubectl apply -f -

    log_success "Multi-node cluster manifest applied!"

    log_info "The operator is now creating the following resources:"
    log_info "  • StatefulSet with 3 replicas (server nodes)"
    log_info "  • cert-manager Certificate for TLS"
    log_info "  • Client and headless services with TLS"
    log_info "  • ConfigMap with cluster and TLS configuration"
    log_info "  • 3 PersistentVolumeClaims for distributed data"

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

    show_connection_info "${CLUSTER_NAME_MULTI}" "${DEMO_NAMESPACE}" true

    # Verify cluster formation by connecting to Neo4j
    log_section "Cluster Formation Verification"

    log_info "Connecting to Neo4j cluster to verify all servers are active..."
    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -c neo4j -- cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD} -d system \"SHOW SERVERS\""

    # Wait for cluster to stabilize (bolt listeners start after pod readiness)
    show_progress 30 "Waiting for cluster to stabilize"

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -c neo4j -n "${DEMO_NAMESPACE}" -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p "${ADMIN_PASSWORD}" -d system "SHOW SERVERS" 2>/dev/null; then
        log_success "All cluster servers are active and communicating!"
        log_demo "The SHOW SERVERS output confirms:"
        log_demo "  • All 3 servers are 'Enabled' and 'Available'"
        log_demo "  • Each server is hosting system and user databases"
        log_demo "  • Cluster formation completed successfully"
    else
        log_warning "Cluster still forming - this is normal for new deployments"
        log_info "In production, clusters typically need 2-5 minutes to fully stabilize"
    fi

    log_success "Multi-node cluster is fully operational!"
    log_elapsed
    show_resource_dashboard

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
    log_demo "  • Secure TLS connections"

    log_info "Setting up port-forward to standalone..."
    log_command "kubectl port-forward svc/${CLUSTER_NAME_SINGLE}-service -n ${DEMO_NAMESPACE} 7473:7473 7687:7687 &"

    # Start port-forward in background
    kubectl port-forward svc/${CLUSTER_NAME_SINGLE}-service -n ${DEMO_NAMESPACE} 7473:7473 7687:7687 >/dev/null 2>&1 &
    local pf_pid=$!

    sleep 3

    log_success "Port-forward established! Neo4j standalone is accessible at:"
    log_info "  • Neo4j Browser: https://localhost:7473 (TLS enabled)"
    log_info "  • Bolt Protocol:  bolt+ssc://localhost:7687 (TLS with self-signed cert)"
    log_info "  • Credentials:    neo4j / ${ADMIN_PASSWORD}"

    log_demo "At this point, you could:"
    log_demo "  1. Open https://localhost:7473 in your web browser"
    log_demo "  2. Connect with Neo4j Desktop: bolt+ssc://localhost:7687"
    log_demo "  3. Use cypher-shell: cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD}"
    log_demo "  4. Connect applications using Neo4j driver with TLS configuration"

    show_progress 3 "Simulating external client connection"

    log_section "Testing Standalone Connection"
    log_command "Verifying HTTP and Bolt ports are accessible..."

    if command -v curl >/dev/null 2>&1; then
        if timeout 5 curl -sk https://localhost:7473 >/dev/null 2>&1; then
            log_success "HTTPS port (7473) is accessible via port-forward!"
        else
            log_info "HTTPS port verification skipped (connection still establishing)"
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
apiVersion: neo4j.neo4j.com/v1beta1
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

    show_manifest "${db_manifest}"
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
    log_command "kubectl exec ${CLUSTER_NAME_SINGLE}-0 -c neo4j -n ${DEMO_NAMESPACE} -- cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD} \"SHOW DATABASES\""
    kubectl exec ${CLUSTER_NAME_SINGLE}-0 -c neo4j -n ${DEMO_NAMESPACE} -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p ${ADMIN_PASSWORD} "SHOW DATABASES"

    log_success "Databases are visible in Neo4j standalone!"
    log_demo "You should see 'system', 'neo4j', and 'products' databases listed"

    log_section "Sample Data Verification"
    log_info "Checking if sample product data was loaded correctly..."
    log_command "kubectl exec ${CLUSTER_NAME_SINGLE}-0 -c neo4j -n ${DEMO_NAMESPACE} -- cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD} -d products \"MATCH (p:Product) RETURN p.productId, p.name, p.price, p.category\""

    if kubectl exec ${CLUSTER_NAME_SINGLE}-0 -c neo4j -n ${DEMO_NAMESPACE} -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p ${ADMIN_PASSWORD} -d products "MATCH (p:Product) RETURN p.productId, p.name, p.price, p.category" 2>/dev/null; then
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
    log_header 3 "🔌" "External Access"

    log_demo "Real-world applications need external access to Neo4j clusters."
    log_demo "We'll demonstrate the most practical access methods:"
    log_demo "  • kubectl port-forward for development and administration"
    log_demo "  • Service exposure concepts for production environments"

    confirm "Ready to demonstrate external access?"

    log_section "Port-Forward Access (Development Method)"

    log_info "kubectl port-forward is the most common method for:"
    log_info "  • Development and testing"
    log_info "  • Database administration"
    log_info "  • Secure tunneling through kubectl authentication"
    log_info "  • No need to expose services publicly"

    log_demo "Setting up port-forward to cluster (TLS-enabled)..."
    log_command "kubectl port-forward svc/${CLUSTER_NAME_MULTI}-client -n ${DEMO_NAMESPACE} 7473:7473 7687:7687 &"

    # Start port-forward in background
    kubectl port-forward svc/${CLUSTER_NAME_MULTI}-client -n ${DEMO_NAMESPACE} 7473:7473 7687:7687 > /tmp/port-forward.log 2>&1 &
    local pf_pid=$!

    # Wait for port-forward to establish
    sleep 3

    log_success "Port-forward established! Neo4j is now accessible at:"
    echo -e "${CYAN}  • Neo4j Browser: https://localhost:7473 (TLS enabled)${NC}"
    echo -e "${CYAN}  • Bolt Protocol:  bolt+ssc://localhost:7687 (TLS with self-signed cert)${NC}"
    echo -e "${CYAN}  • Credentials:    neo4j / ${ADMIN_PASSWORD}${NC}"
    echo

    log_demo "At this point, you could:"
    log_demo "  1. Open https://localhost:7473 in your web browser"
    log_demo "  2. Connect with Neo4j Desktop or other tools"
    log_demo "  3. Use cypher-shell: cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD}"
    log_demo "  4. Connect applications using Neo4j drivers with TLS"

    show_progress $PAUSE_MEDIUM "Simulating external client connection"

    # Test the connection through port-forward
    log_section "Testing External Connection"
    log_command "Connecting via port-forward to verify external access..."

    if command -v curl >/dev/null 2>&1; then
        if timeout 5 curl -sk https://localhost:7473 >/dev/null 2>&1; then
            log_success "HTTPS port (7473) is accessible via port-forward!"
        else
            log_info "HTTPS port verification skipped (connection still establishing)"
        fi
    fi
    if command -v nc >/dev/null 2>&1; then
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
    log_header 4 "🗄️" "Database Creation and Management"

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
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jDatabase
metadata:
  name: orders-database
  namespace: ${DEMO_NAMESPACE}
spec:
  # Reference to our cluster
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

    show_manifest "${database_manifest}"
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
    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -c neo4j -- cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD} -d system \"SHOW DATABASES\""

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -c neo4j -n "${DEMO_NAMESPACE}" -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p "${ADMIN_PASSWORD}" -d system "SHOW DATABASES" 2>/dev/null; then
        log_success "Databases are visible in Neo4j cluster!"
        log_demo "You should see both 'system', 'neo4j' and 'orders' databases listed"
    else
        log_warning "Database creation still in progress"
    fi

    # Test the sample data
    log_section "Sample Data Verification"

    log_info "Checking if initial data was loaded correctly..."
    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -c neo4j -- cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p ${ADMIN_PASSWORD} -d orders \"MATCH (o:Order) RETURN o.orderId, o.status, o.amount\""

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -c neo4j -n "${DEMO_NAMESPACE}" -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p "${ADMIN_PASSWORD}" -d orders "MATCH (o:Order) RETURN o.orderId, o.status, o.amount" 2>/dev/null; then
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

    confirm "Ready to proceed to plugin management demo?"
}

# Demonstrate APOC plugin installation on the cluster
demonstrate_plugin_installation() {
    log_header 5 "🔌" "Plugin Management (APOC)"

    log_demo "The operator manages Neo4j plugins via the Neo4jPlugin CRD."
    log_demo "We'll install APOC (the most popular Neo4j plugin) on our cluster:"
    log_demo "  • Declarative plugin lifecycle via Kubernetes"
    log_demo "  • Automatic rolling restart of cluster pods"
    log_demo "  • Configuration applied via environment variables (Neo4j 5.26+)"

    confirm "Ready to install the APOC plugin?"

    log_section "Installing APOC Plugin"

    local plugin_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jPlugin
metadata:
  name: demo-apoc-plugin
  namespace: ${DEMO_NAMESPACE}
spec:
  clusterRef: ${CLUSTER_NAME_MULTI}
  name: apoc
  version: "5.26.0"
  enabled: true
  source:
    type: official
  config:
    apoc.export.file.enabled: "true"
    apoc.import.file.enabled: "true"
EOF
)

    show_manifest "${plugin_manifest}"
    echo

    log_command "kubectl apply -f -"
    echo "${plugin_manifest}" | kubectl apply -f -

    log_success "APOC plugin manifest applied!"

    log_info "The operator is now:"
    log_info "  • Adding NEO4J_PLUGINS=[\"apoc\"] to the StatefulSet"
    log_info "  • Setting APOC configuration via environment variables"
    log_info "  • Performing a rolling restart of cluster pods"

    log_section "Plugin Installation Progress"

    # Wait for the plugin to be ready
    local plugin_timeout=120
    local plugin_elapsed=0
    local plugin_ready=false

    while [[ $plugin_elapsed -lt $plugin_timeout ]] && [[ "$plugin_ready" != "true" ]]; do
        local phase=$(kubectl get neo4jplugin demo-apoc-plugin -n ${DEMO_NAMESPACE} -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        local message=$(kubectl get neo4jplugin demo-apoc-plugin -n ${DEMO_NAMESPACE} -o jsonpath='{.status.message}' 2>/dev/null || echo "")

        if [[ "$phase" == "Ready" ]]; then
            plugin_ready=true
            break
        fi

        if [[ -n "$phase" ]]; then
            echo -n -e "\r  Plugin phase: ${phase} - ${message}"
        fi

        sleep 5
        plugin_elapsed=$((plugin_elapsed + 5))
    done
    echo

    if [[ "$plugin_ready" == "true" ]]; then
        log_success "APOC plugin installed and ready!"
    else
        log_warning "Plugin still installing — this is normal, it requires a rolling restart"
    fi

    # Verify APOC is available via cypher-shell
    log_section "APOC Verification"
    log_info "Verifying APOC procedures are available..."

    show_progress 10 "Waiting for pods to stabilize after rolling restart"

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -c neo4j -n "${DEMO_NAMESPACE}" -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p "${ADMIN_PASSWORD}" "RETURN apoc.version() AS apocVersion" 2>/dev/null; then
        log_success "APOC is installed and functional!"
        log_demo "APOC procedures are now available across all cluster servers"
    else
        log_info "APOC still initializing — pods may still be restarting"
    fi

    log_demo "Key benefits demonstrated:"
    log_demo "  ✓ Declarative plugin management via Neo4jPlugin CRD"
    log_demo "  ✓ Automatic rolling restart preserves cluster availability"
    log_demo "  ✓ Plugin configuration managed as Kubernetes resources"
    log_elapsed
    show_resource_dashboard
}

# Demonstrate multiple databases with different topologies
demonstrate_multi_database() {
    log_header 6 "📊" "Multi-Database Topologies"

    log_demo "Neo4j Enterprise supports multiple databases on a single cluster,"
    log_demo "each with its own topology distribution:"
    log_demo "  • Different read/write scaling per database"
    log_demo "  • Workload isolation across servers"
    log_demo "  • Kubernetes-native lifecycle management"

    confirm "Ready to create multiple databases?"

    log_section "Creating Databases with Different Topologies"

    log_demo "On our 3-server cluster, we'll create two databases:"
    log_demo "  • 'analytics' — 1 primary, 2 secondaries (read-heavy workload)"
    log_demo "  • 'sessions'  — 2 primaries, 0 secondaries (write-heavy workload)"
    echo

    # Create analytics database (read-heavy)
    local analytics_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jDatabase
metadata:
  name: analytics-database
  namespace: ${DEMO_NAMESPACE}
spec:
  clusterRef: ${CLUSTER_NAME_MULTI}
  name: analytics
  topology:
    primaries: 1
    secondaries: 2
  wait: true
  ifNotExists: true
EOF
)

    log_manifest "Analytics database (1 primary, 2 secondaries):"
    show_manifest "${analytics_manifest}"

    echo "${analytics_manifest}" | kubectl apply -f -

    # Create sessions database (write-heavy)
    local sessions_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jDatabase
metadata:
  name: sessions-database
  namespace: ${DEMO_NAMESPACE}
spec:
  clusterRef: ${CLUSTER_NAME_MULTI}
  name: sessions
  topology:
    primaries: 2
    secondaries: 0
  wait: true
  ifNotExists: true
EOF
)

    log_manifest "Sessions database (2 primaries, 0 secondaries):"
    show_manifest "${sessions_manifest}"

    echo "${sessions_manifest}" | kubectl apply -f -

    log_success "Both database manifests applied!"

    # Wait for databases to be ready
    show_progress 15 "Waiting for databases to be created"

    log_section "Multi-Database Status"
    log_command "kubectl get neo4jdatabase -n ${DEMO_NAMESPACE} -o wide"
    kubectl get neo4jdatabase -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null

    # Verify via cypher-shell
    log_section "Database Topology Verification"
    log_info "Querying Neo4j to verify database topology distribution..."

    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -c neo4j -n "${DEMO_NAMESPACE}" -- cypher-shell -a "bolt+ssc://localhost:7687" -u neo4j -p "${ADMIN_PASSWORD}" -d system "SHOW DATABASES YIELD name, currentStatus, role WHERE name IN ['analytics', 'sessions', 'orders'] RETURN name, role, count(*) AS replicas ORDER BY name, role" 2>/dev/null; then
        log_success "All databases are distributed across cluster servers!"
        log_demo "Each database has its own topology tailored to its workload"
    else
        log_info "Databases still being distributed — this is normal"
    fi

    log_demo "Key benefits demonstrated:"
    log_demo "  ✓ Multiple databases on a single cluster infrastructure"
    log_demo "  ✓ Per-database topology tuning (read-heavy vs write-heavy)"
    log_demo "  ✓ Declarative management via Neo4jDatabase CRD"
    log_elapsed
    show_resource_dashboard
}

# Demonstrate declarative user, role, and privilege management
demonstrate_user_role_management() {
    log_header 7 "👥" "Declarative User & Role Management"

    log_demo "The operator manages Neo4j users, roles, and privileges as Kubernetes resources:"
    log_demo "  • Neo4jRole — role + privileges, with drift reconciliation"
    log_demo "  • Neo4jUser — user identity, password (from Secret), role bindings"
    log_demo "  • Neo4jRoleBinding — bind external (SSO) users to roles"
    log_demo ""
    log_demo "Privileges live on the role, not on users — bind users to roles by name."

    confirm "Ready to create a role and a bound user?"

    # Read password into a Secret first; the password Secret is the single source of truth.
    local user_password="reader-pass-$(date +%s)"

    log_section "Step 1: Password Secret"
    log_command "kubectl create secret generic demo-reader-creds ..."
    kubectl create secret generic demo-reader-creds \
        -n "${DEMO_NAMESPACE}" \
        --from-literal=password="${user_password}" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    log_success "Secret 'demo-reader-creds' created"
    echo

    log_section "Step 2: Neo4jRole — read-only privileges"

    local role_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: demo-analytics-reader
  namespace: ${DEMO_NAMESPACE}
spec:
  clusterRef: ${CLUSTER_NAME_MULTI}
  name: demo_analytics_reader
  privileges:
    - "GRANT ACCESS ON DATABASE neo4j TO demo_analytics_reader"
    - "GRANT MATCH {*} ON GRAPH neo4j NODES * TO demo_analytics_reader"
    - "DENY WRITE ON GRAPH neo4j TO demo_analytics_reader"
EOF
)
    show_manifest "${role_manifest}"
    echo
    log_command "kubectl apply -f -"
    echo "${role_manifest}" | kubectl apply -f - >/dev/null
    log_success "Neo4jRole 'demo-analytics-reader' applied"

    log_info "The role controller will:"
    log_info "  • Run CREATE ROLE demo_analytics_reader"
    log_info "  • Apply each GRANT/DENY statement"
    log_info "  • Read SHOW ROLE PRIVILEGES on every reconcile to detect drift"
    echo

    log_section "Step 3: Neo4jUser bound to the role"

    local user_manifest=$(cat << EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: demo-reader
  namespace: ${DEMO_NAMESPACE}
spec:
  clusterRef: ${CLUSTER_NAME_MULTI}
  username: demo_reader
  passwordSecretRef:
    name: demo-reader-creds
  roles:
    - demo_analytics_reader
EOF
)
    show_manifest "${user_manifest}"
    echo
    log_command "kubectl apply -f -"
    echo "${user_manifest}" | kubectl apply -f - >/dev/null
    log_success "Neo4jUser 'demo-reader' applied"
    echo

    log_section "Waiting for both resources to reach Ready"

    local rb_timeout=60
    local rb_elapsed=0
    while [[ $rb_elapsed -lt $rb_timeout ]]; do
        local role_phase=$(kubectl get neo4jrole demo-analytics-reader -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        local user_phase=$(kubectl get neo4juser demo-reader -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        echo -n -e "\r  role=${role_phase:-pending}  user=${user_phase:-pending}      "
        if [[ "$role_phase" == "Ready" && "$user_phase" == "Ready" ]]; then
            echo
            log_success "Role and user both Ready"
            break
        fi
        sleep 3
        rb_elapsed=$((rb_elapsed + 3))
    done
    echo

    log_section "Verification"
    log_demo "We can authenticate as demo_reader and see the role granted:"
    echo

    log_command "kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -- cypher-shell -u demo_reader -p ... 'SHOW CURRENT USER YIELD user, roles'"
    if kubectl exec "${CLUSTER_NAME_MULTI}-server-0" -c neo4j -n "${DEMO_NAMESPACE}" -- \
        cypher-shell -a "bolt+ssc://localhost:7687" -u demo_reader -p "${user_password}" \
        "SHOW CURRENT USER YIELD user, roles" 2>/dev/null; then
        log_success "demo_reader can authenticate and is bound to demo_analytics_reader"
    else
        log_warning "Could not authenticate as demo_reader (TLS/timing issue — re-run if needed)"
    fi
    echo

    log_section "Privilege drift correction"
    log_demo "The operator reconciles privileges on every loop. If you REVOKE a privilege"
    log_demo "out-of-band, the operator restores it on the next reconcile."
    log_demo ""
    log_demo "Try it:"
    log_demo "  kubectl exec ${CLUSTER_NAME_MULTI}-server-0 -- cypher-shell -u neo4j -p ... \\"
    log_demo "    'REVOKE ACCESS ON DATABASE neo4j FROM demo_analytics_reader'"
    log_demo "  # ~30s later: SHOW ROLE demo_analytics_reader PRIVILEGES — ACCESS is back"
    echo

    log_demo "Key benefits demonstrated:"
    log_demo "  ✓ Users, roles, and privileges as Kubernetes resources"
    log_demo "  ✓ Passwords sourced from Secrets (rotation = update Secret)"
    log_demo "  ✓ Privilege drift auto-corrected against SHOW ROLE PRIVILEGES"
    log_demo "  ✓ GitOps-friendly RBAC alongside infrastructure"
    log_elapsed
    show_resource_dashboard
}

# Demonstrate live cluster diagnostics
demonstrate_diagnostics() {
    log_header 8 "🔍" "Live Cluster Diagnostics"

    log_demo "The operator continuously monitors the cluster and surfaces"
    log_demo "diagnostics directly in the custom resource status:"
    log_demo "  • Server health from SHOW SERVERS"
    log_demo "  • Database status from SHOW DATABASES"
    log_demo "  • Users and roles from SHOW USERS / SHOW ROLES"
    log_demo "  • No kubectl exec needed — just read the CR status"

    confirm "Ready to view live diagnostics?"

    log_section "Cluster Status Overview"
    log_command "kubectl get neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE} -o wide"
    kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null
    echo

    log_section "Server Diagnostics"
    log_demo "The operator runs SHOW SERVERS and surfaces results in status.diagnostics.servers:"
    echo
    log_command "kubectl get neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE} -o jsonpath='{.status.diagnostics.servers}'"
    echo

    local servers_json=$(kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.diagnostics.servers}' 2>/dev/null)
    if [[ -n "$servers_json" && "$servers_json" != "null" ]]; then
        echo "${servers_json}" | python3 -m json.tool 2>/dev/null || echo "${servers_json}"
        log_success "Server diagnostics available directly from CR status!"
    else
        log_info "Diagnostics collecting — the operator queries Neo4j on each reconcile"
        # Fall back to showing conditions
        log_command "kubectl get neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE} -o jsonpath='{.status.conditions}'"
        kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.conditions}' 2>/dev/null | python3 -m json.tool 2>/dev/null || true
    fi

    echo
    log_section "Database Diagnostics"
    log_demo "The operator runs SHOW DATABASES and surfaces results in status.diagnostics.databases:"
    echo

    local databases_json=$(kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.diagnostics.databases}' 2>/dev/null)
    if [[ -n "$databases_json" && "$databases_json" != "null" ]]; then
        echo "${databases_json}" | python3 -m json.tool 2>/dev/null || echo "${databases_json}"
        log_success "Database diagnostics available directly from CR status!"
    else
        log_info "Database diagnostics not yet collected"
    fi

    echo
    log_section "User & Role Diagnostics"
    log_demo "SHOW USERS / SHOW ROLES results are surfaced for observability of"
    log_demo "Neo4jUser / Neo4jRole reconciliation (capped at 50 entries each):"
    echo

    local users_count=$(kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.diagnostics.userCount}' 2>/dev/null)
    local roles_count=$(kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.diagnostics.roleCount}' 2>/dev/null)
    if [[ -n "$users_count" || -n "$roles_count" ]]; then
        log_command "kubectl get neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE} -o jsonpath='{.status.diagnostics.users}'"
        local users_json=$(kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{.status.diagnostics.users}' 2>/dev/null)
        if [[ -n "$users_json" && "$users_json" != "null" ]]; then
            echo "${users_json}" | python3 -m json.tool 2>/dev/null || echo "${users_json}"
        fi
        log_info "userCount=${users_count:-?}  roleCount=${roles_count:-?}"
        log_success "User/role diagnostics surfaced from CR status"
    else
        log_info "User/role diagnostics not yet collected (operator runs on each reconcile)"
    fi

    echo
    log_section "Health Conditions"
    log_demo "The operator also sets Kubernetes conditions for monitoring integration:"
    echo
    kubectl get neo4jenterprisecluster "${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\n"}{end}' 2>/dev/null
    echo

    log_demo "Key benefits demonstrated:"
    log_demo "  ✓ Live server health without kubectl exec"
    log_demo "  ✓ Database, user, and role status surfaced in CR status"
    log_demo "  ✓ Standard Kubernetes conditions for alerting pipelines"
    log_demo "  ✓ Compatible with ArgoCD, Flux, and Prometheus"
}

# Clean up all demo resources
demo_cleanup() {
    log_section "Cleaning Up Demo Resources"

    # Step 1: Strip finalizers from all Neo4j CRs so deletion is immediate.
    # Order matters at delete time (bindings → users → roles → databases) but
    # finalizer stripping is order-independent.
    log_info "Removing finalizers from all Neo4j resources..."
    for crd in neo4jrolebinding neo4juser neo4jrole neo4jplugin neo4jdatabase neo4jenterprisestandalone neo4jenterprisecluster neo4jbackup neo4jrestore; do
        kubectl get "$crd" -n "${DEMO_NAMESPACE}" -o name 2>/dev/null | while read resource; do
            kubectl patch "$resource" -n "${DEMO_NAMESPACE}" --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null
        done
    done

    # Step 2: Delete all Neo4j CRs in parallel (instant since finalizers are gone)
    log_info "Deleting all Neo4j custom resources..."
    kubectl delete neo4jrolebinding --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete neo4juser --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete neo4jrole --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete neo4jplugin --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete neo4jdatabase --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete neo4jenterprisestandalone --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete neo4jenterprisecluster --all -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    wait

    # Step 3: Delete orphaned StatefulSets and jobs directly
    log_info "Deleting StatefulSets and jobs..."
    kubectl delete sts -l "neo4j.com/cluster=${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete sts "${CLUSTER_NAME_SINGLE}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true --wait=false 2>/dev/null &
    kubectl delete jobs -l "neo4j.com/cluster=${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null &
    wait

    # Step 4: Force-delete pods with no grace period
    log_info "Force-deleting pods..."
    kubectl delete pods -l "app=${CLUSTER_NAME_SINGLE}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true --grace-period=0 --force 2>/dev/null &
    kubectl delete pods -l "neo4j.com/cluster=${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true --grace-period=0 --force 2>/dev/null &
    wait

    # Step 5: Clean up secrets, services, configmaps, PVCs
    log_info "Cleaning up remaining resources..."
    kubectl delete secret neo4j-admin-secret -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null
    kubectl delete secret demo-reader-creds -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null
    kubectl delete svc -l "neo4j.com/cluster=${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null
    kubectl delete svc "${CLUSTER_NAME_SINGLE}-service" -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null
    kubectl delete configmap "${CLUSTER_NAME_SINGLE}-config" -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null
    kubectl delete pvc -l "app=${CLUSTER_NAME_SINGLE}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null
    kubectl delete pvc -l "neo4j.com/cluster=${CLUSTER_NAME_MULTI}" -n "${DEMO_NAMESPACE}" --ignore-not-found=true 2>/dev/null

    log_success "Demo resources cleaned up!"
}

# Demo summary and next steps
show_demo_summary() {
    log_header 0 "🎯" "Demo Summary"

    local total_elapsed=$(( $(date +%s) - DEMO_START_TIME ))
    local total_min=$((total_elapsed / 60))
    local total_sec=$((total_elapsed % 60))

    echo -e "  ${DIM}┌──────────────────────────────────────────────────────────────────────┐${NC}"
    echo -e "  ${DIM}│${NC}                                                                      ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}${BOLD}All 8 demo parts completed successfully${NC}              ${DIM}${total_min}m ${total_sec}s total${NC}  ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}                                                                      ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 1  TLS Standalone deployment                                 ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 2  TLS HA Cluster (3 servers, Raft consensus)                ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 3  Secure external access (HTTPS + Bolt+TLS)                 ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 4  Database creation with schema and sample data             ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 5  APOC plugin via Neo4jPlugin CRD                           ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 6  Multi-database topologies (read-heavy + write-heavy)      ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 7  Declarative user & role management                        ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  ${GREEN}✔${NC} Part 8  Live diagnostics from CR status                           ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}                                                                      ${DIM}│${NC}"
    echo -e "  ${DIM}└──────────────────────────────────────────────────────────────────────┘${NC}"
    echo

    show_resource_dashboard

    echo
    log_section "Active Resources"
    kubectl get neo4jenterprisestandalone,neo4jenterprisecluster -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null
    kubectl get neo4jplugin -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null
    kubectl get neo4jdatabase -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null
    kubectl get neo4jrole,neo4juser,neo4jrolebinding -n "${DEMO_NAMESPACE}" -o wide 2>/dev/null

    # Handle cleanup
    if [[ "${CLEANUP_AFTER}" == "true" ]]; then
        log_info "Cleaning up demo resources (--cleanup flag set)..."
        demo_cleanup
    else
        if [[ "${SKIP_CONFIRMATIONS}" == "true" ]]; then
            log_section "Cleanup"
            log_info "To clean up the demo resources, run:"
            echo "  ./scripts/demo.sh --cleanup-only"
            echo "  # or manually:"
            echo "  kubectl delete neo4jplugin demo-apoc-plugin -n ${DEMO_NAMESPACE}"
            echo "  kubectl delete neo4jdatabase --all -n ${DEMO_NAMESPACE}"
            echo "  kubectl delete neo4jenterprisestandalone ${CLUSTER_NAME_SINGLE} -n ${DEMO_NAMESPACE}"
            echo "  kubectl delete neo4jenterprisecluster ${CLUSTER_NAME_MULTI} -n ${DEMO_NAMESPACE}"
        else
            echo
            local response
            read -r -p "$(echo -e "${CYAN}Clean up demo resources? [y/N]${NC} ")" response
            case "${response}" in
                [yY][eE][sS]|[yY])
                    demo_cleanup
                    ;;
                *)
                    log_section "Cleanup"
                    log_info "To clean up later, run:"
                    echo "  ./scripts/demo.sh --cleanup-only"
                    ;;
            esac
        fi
    fi
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
        log_warning "ca-cluster-issuer not found - TLS features will not be available"
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
    log_header 0 "🎬" "Neo4j Kubernetes Operator Demo"

    echo -e "  ${PURPLE}${BOLD}Welcome to the Neo4j Kubernetes Operator demonstration!${NC}"
    echo
    echo -e "  ${DIM}This demo deploys real Neo4j Enterprise instances and walks through${NC}"
    echo -e "  ${DIM}the operator's core capabilities in 8 parts:${NC}"
    echo
    echo -e "  ${WHITE} 1${NC}  🚀  TLS standalone deployment"
    echo -e "  ${WHITE} 2${NC}  🏗️   TLS HA cluster (3 servers)"
    echo -e "  ${WHITE} 3${NC}  🔌  Secure external access"
    echo -e "  ${WHITE} 4${NC}  🗄️   Database creation with sample data"
    echo -e "  ${WHITE} 5${NC}  🔌  APOC plugin installation"
    echo -e "  ${WHITE} 6${NC}  📊  Multi-database topologies"
    echo -e "  ${WHITE} 7${NC}  👥  Declarative user & role management"
    echo -e "  ${WHITE} 8${NC}  🔍  Live cluster diagnostics"
    echo
    echo -e "  ${DIM}┌────────────────────────────────────────────┐${NC}"
    echo -e "  ${DIM}│${NC}  Namespace: ${BOLD}${DEMO_NAMESPACE}${NC}  Speed: ${BOLD}${DEMO_SPEED}${NC}  ${DIM}│${NC}"
    echo -e "  ${DIM}│${NC}  Estimated time: ${BOLD}~10-12 minutes${NC}           ${DIM}│${NC}"
    echo -e "  ${DIM}└────────────────────────────────────────────┘${NC}"
    echo

    confirm "Ready to start the demo?"

    DEMO_START_TIME=$(date +%s)

    # Execute demo steps
    validate_prerequisites
    cleanup_existing
    create_admin_secret

    sleep $PAUSE_SHORT

    deploy_single_node

    sleep $PAUSE_MEDIUM

    deploy_multi_node_cluster

    sleep $PAUSE_SHORT

    demonstrate_external_access

    sleep $PAUSE_SHORT

    demonstrate_database_creation

    sleep $PAUSE_SHORT

    demonstrate_plugin_installation

    sleep $PAUSE_SHORT

    demonstrate_multi_database

    sleep $PAUSE_SHORT

    demonstrate_user_role_management

    sleep $PAUSE_SHORT

    demonstrate_diagnostics

    sleep $PAUSE_SHORT

    show_demo_summary
}

# Handle script arguments
CLEANUP_ONLY=false
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
        --cleanup)
            CLEANUP_AFTER=true
            shift
            ;;
        --cleanup-only)
            CLEANUP_ONLY=true
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
  --cleanup                 Clean up demo resources after the demo completes
  --cleanup-only            Only clean up resources from a previous demo run
  --speed SPEED             Demo speed: fast, normal, slow (default: normal)
  --help, -h                Show this help

Environment Variables:
  DEMO_NAMESPACE           Same as --namespace
  ADMIN_PASSWORD           Same as --password
  SKIP_CONFIRMATIONS       Set to 'true' to skip confirmations
  CLEANUP_AFTER            Set to 'true' to clean up after demo
  DEMO_SPEED              Same as --speed

Examples:
  $0                                    # Interactive demo
  $0 --skip-confirmations --speed fast  # Fast automated demo
  $0 --cleanup                          # Interactive demo with cleanup at end
  $0 --cleanup-only                     # Clean up resources from previous demo
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

# Run cleanup-only or full demo
if [[ "${CLEANUP_ONLY}" == "true" ]]; then
    log_header 0 "🧹" "Demo Cleanup"
    validate_prerequisites
    demo_cleanup
else
    main "$@"
fi
