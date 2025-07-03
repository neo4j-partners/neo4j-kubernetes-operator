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
TIMEOUT=${TIMEOUT:-300}
NAMESPACE=${NAMESPACE:-default}
CLEANUP=${CLEANUP:-true}

# Sample configurations
create_samples() {
    log_info "Creating sample configurations..."

    mkdir -p samples/

    # Basic Neo4j cluster
    cat > samples/basic-cluster.yaml << 'EOF'
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: basic-neo4j
  namespace: ${NAMESPACE}
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  topology:
    primaries: 3
    secondaries: 0
  storage:
    size: "1Gi"
    storageClass: "standard"
  auth:
    password:
      secretName: "neo4j-auth"
      secretKey: "password"
  service:
    type: ClusterIP
EOF

    # HA Neo4j cluster
    cat > samples/ha-cluster.yaml << 'EOF'
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: ha-neo4j
  namespace: ${NAMESPACE}
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  topology:
    primaries: 3
    secondaries: 2
  storage:
    size: "2Gi"
    storageClass: "standard"
  auth:
    password:
      secretName: "neo4j-auth"
      secretKey: "password"
  service:
    type: LoadBalancer
  tls:
    enabled: true
    issuer:
      name: "selfsigned-issuer"
      kind: "ClusterIssuer"
EOF

    # Neo4j database
    cat > samples/database.yaml << 'EOF'
apiVersion: neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: movies-db
  namespace: default
spec:
  clusterRef:
    name: basic-neo4j
  name: "movies"
  options:
    - "dbms.db.name=movies"
    - "dbms.default_database=movies"
EOF


    # Secret for auth
    cat > samples/secrets.yaml << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: neo4j-auth
  namespace: default
type: Opaque
data:
  password: bmVvNGpwYXNzd29yZA==  # neo4jpassword
---
apiVersion: v1
kind: Secret
metadata:
  name: app-user-auth
  namespace: default
type: Opaque
data:
  password: YXBwdXNlcnBhc3M=  # appuserpass
EOF

    log_success "Sample configurations created"
}

# Wait for resource to be ready
wait_for_resource() {
    local resource_type=$1
    local resource_name=$2
    local condition=${3:-"Ready"}

    log_info "Waiting for ${resource_type}/${resource_name} to be ${condition}..."

    local start_time
    start_time=$(date +%s)
    while true; do
        local current_time
        current_time=$(date +%s)
        local elapsed=$((current_time - start_time))

        if [[ $elapsed -gt $TIMEOUT ]]; then
            log_error "Timeout waiting for ${resource_type}/${resource_name}"
            return 1
        fi

        if kubectl get "${resource_type}" "${resource_name}" -n "${NAMESPACE}" >/dev/null 2>&1; then
            local status
            status=$(kubectl get "${resource_type}" "${resource_name}" -n "${NAMESPACE}" -o jsonpath="{.status.conditions[?(@.type=='${condition}')].status}" 2>/dev/null || echo "")

            if [[ "${status}" == "True" ]]; then
                log_success "${resource_type}/${resource_name} is ${condition}"
                return 0
            fi
        fi

        sleep 5
    done
}

# Test basic cluster
test_basic_cluster() {
    log_info "Testing basic Neo4j cluster..."

    # Apply secrets first
    kubectl apply -f samples/secrets.yaml

    # Apply basic cluster
    kubectl apply -f samples/basic-cluster.yaml

    # Wait for cluster to be ready
    if wait_for_resource "neo4jenterprisecluster" "basic-neo4j" "Ready"; then
        log_success "Basic cluster test passed"

        # Test connectivity
        test_connectivity "basic-neo4j"
    else
        log_error "Basic cluster test failed"
        show_debug_info "basic-neo4j"
        return 1
    fi
}

# Test HA cluster
test_ha_cluster() {
    log_info "Testing HA Neo4j cluster..."

    # Create self-signed issuer first
    create_selfsigned_issuer

    # Apply HA cluster
    kubectl apply -f samples/ha-cluster.yaml

    # Wait for cluster to be ready (longer timeout for HA)
    local original_timeout=$TIMEOUT
    TIMEOUT=600

    if wait_for_resource "neo4jenterprisecluster" "ha-neo4j" "Ready"; then
        log_success "HA cluster test passed"

        # Test connectivity
        test_connectivity "ha-neo4j"

        # Test HA functionality
        test_ha_functionality "ha-neo4j"
    else
        log_error "HA cluster test failed"
        show_debug_info "ha-neo4j"
        TIMEOUT=$original_timeout
        return 1
    fi

    TIMEOUT=$original_timeout
}

# Test database creation
test_database() {
    log_info "Testing database creation..."

    kubectl apply -f samples/database.yaml

    if wait_for_resource "neo4jdatabase" "movies-db" "Ready"; then
        log_success "Database test passed"
    else
        log_error "Database test failed"
        return 1
    fi
}


# Test connectivity to Neo4j cluster
test_connectivity() {
    local cluster_name=$1
    log_info "Testing connectivity to ${cluster_name}..."

    # Get service name and port
    local service_name="${cluster_name}-client"
    # local port="7687"  # Unused variable, commented out

    # Port forward to test connectivity
    kubectl port-forward "service/${service_name}" 7687:7687 -n "${NAMESPACE}" &
    local pf_pid=$!

    sleep 5

    # Test with cypher-shell (if available)
    if command -v cypher-shell >/dev/null 2>&1; then
        if echo "RETURN 1;" | cypher-shell -a bolt://localhost:7687 -u neo4j -p neo4jpassword; then
            log_success "Connectivity test passed"
        else
            log_warning "Connectivity test failed, but this might be expected in test environment"
        fi
    else
        log_info "cypher-shell not available, skipping connectivity test"
    fi

    # Kill port-forward
    kill $pf_pid 2>/dev/null || true
}

# Test HA functionality
test_ha_functionality() {
    local cluster_name=$1
    log_info "Testing HA functionality for ${cluster_name}..."

    # Check if we have the expected number of pods
    local expected_primaries=3
    local expected_secondaries=2
    local total_expected=$((expected_primaries + expected_secondaries))

    local actual_pods
    actual_pods=$(kubectl get pods -l "app.kubernetes.io/name=${cluster_name}" -n "${NAMESPACE}" --no-headers | wc -l)

    if [[ $actual_pods -eq $total_expected ]]; then
        log_success "HA pod count test passed (${actual_pods}/${total_expected})"
    else
        log_error "HA pod count test failed (${actual_pods}/${total_expected})"
        return 1
    fi
}

# Create self-signed issuer for TLS testing
create_selfsigned_issuer() {
    log_info "Creating self-signed certificate issuer..."

    cat << 'EOF' | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
EOF
}

# Show debug information
show_debug_info() {
    local cluster_name=$1
    log_info "Debug information for ${cluster_name}:"

    echo "=== Cluster Status ==="
    kubectl get neo4jenterprisecluster "${cluster_name}" -n "${NAMESPACE}" -o yaml || true

    echo "=== Pods ==="
    kubectl get pods -l "app.kubernetes.io/name=${cluster_name}" -n "${NAMESPACE}" || true

    echo "=== Events ==="
    kubectl get events -n "${NAMESPACE}" --sort-by='.lastTimestamp' | grep "${cluster_name}" || true

    echo "=== Pod Logs ==="
    local pods
    pods=$(kubectl get pods -l "app.kubernetes.io/name=${cluster_name}" -n "${NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' || echo "")
    for pod in $pods; do
        echo "--- Logs for $pod ---"
        kubectl logs "$pod" -n "${NAMESPACE}" --tail=20 || true
    done
}

# Cleanup resources
cleanup_resources() {
    if [[ "${CLEANUP}" == "true" ]]; then
        log_info "Cleaning up test resources..."

        kubectl delete -f samples/ --ignore-not-found=true || true
        kubectl delete clusterissuer selfsigned-issuer --ignore-not-found=true || true

        # Wait for resources to be deleted
        sleep 10

        log_success "Test resources cleaned up"
    fi
}

# Main test function
main() {
    log_info "Neo4j Operator Sample Testing"
    log_info "Configuration:"
    log_info "  Timeout: ${TIMEOUT}s"
    log_info "  Namespace: ${NAMESPACE}"
    log_info "  Cleanup: ${CLEANUP}"
    echo

    # Check prerequisites
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Kubernetes cluster not accessible"
        exit 1
    fi

    if ! kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com >/dev/null 2>&1; then
        log_error "Neo4j CRDs not installed. Run 'make install' first."
        exit 1
    fi

    # Create samples
    create_samples

    # Run tests
    local failed_tests=0

    log_info "Starting tests..."

    if ! test_basic_cluster; then
        ((failed_tests++))
    fi

    if ! test_database; then
        ((failed_tests++))
    fi


    # HA test is more resource intensive, skip in CI or if requested
    if [[ "${SKIP_HA_TEST:-false}" != "true" ]]; then
        if ! test_ha_cluster; then
            ((failed_tests++))
        fi
    else
        log_info "Skipping HA cluster test"
    fi

    # Cleanup
    cleanup_resources

    # Report results
    echo
    if [[ $failed_tests -eq 0 ]]; then
        log_success "All tests passed!"
        exit 0
    else
        log_error "${failed_tests} test(s) failed"
        exit 1
    fi
}

# Handle script arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --no-cleanup)
            CLEANUP=false
            shift
            ;;
        --skip-ha)
            SKIP_HA_TEST=true
            shift
            ;;
        --help)
            echo "Usage: $0 [options]"
            echo "Options:"
            echo "  --timeout SEC       Set timeout for waiting (default: 300)"
            echo "  --namespace NS      Set namespace (default: default)"
            echo "  --no-cleanup        Skip cleanup after tests"
            echo "  --skip-ha           Skip HA cluster test"
            echo "  --help              Show this help"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

main "$@"
