#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CLUSTER_NAME="neo4j-operator-dev"
NAMESPACE="neo4j-operator-system"
CERT_MANAGER_VERSION="v1.13.0"
PROMETHEUS_VERSION="v0.68.0"

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_dependencies() {
    print_status "Checking dependencies..."

    local missing_deps=()

    if ! command -v kind &> /dev/null; then
        missing_deps+=("kind")
    fi

    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi

    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    fi

    if ! command -v helm &> /dev/null; then
        missing_deps+=("helm")
    fi

    if [ ${#missing_deps[@]} -ne 0 ]; then
        print_error "Missing dependencies: ${missing_deps[*]}"
        print_error "Please install them and try again."
        exit 1
    fi

    print_success "All dependencies are available."
}

create_cluster() {
    print_status "Creating Kind cluster '$CLUSTER_NAME'..."

    if kind get clusters | grep -q "$CLUSTER_NAME"; then
        print_warning "Cluster '$CLUSTER_NAME' already exists. Skipping creation."
        return 0
    fi

    kind create cluster --name "$CLUSTER_NAME" --config hack/kind-config.yaml

    # Wait for cluster to be ready
    print_status "Waiting for cluster to be ready..."
    kubectl wait --for=condition=ready node --all --timeout=300s

    print_success "Cluster '$CLUSTER_NAME' created successfully."
}

install_cert_manager() {
    print_status "Installing cert-manager..."

    if kubectl get namespace cert-manager &> /dev/null; then
        print_warning "cert-manager is already installed. Skipping."
        return 0
    fi

    kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/$CERT_MANAGER_VERSION/cert-manager.yaml"

    # Wait for cert-manager to be ready
    print_status "Waiting for cert-manager to be ready..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s

    print_success "cert-manager installed successfully."
}

install_prometheus() {
    print_status "Installing Prometheus for monitoring..."

    if kubectl get namespace monitoring &> /dev/null; then
        print_warning "Prometheus is already installed. Skipping."
        return 0
    fi

    # Add Prometheus Helm repository
    helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
    helm repo update

    # Create monitoring namespace
    kubectl create namespace monitoring

    # Install Prometheus
    helm install prometheus prometheus-community/kube-prometheus-stack \
        --namespace monitoring \
        --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
        --set grafana.adminPassword=admin \
        --wait

    print_success "Prometheus installed successfully."
}

build_and_load_image() {
    print_status "Building and loading operator image..."

    # Build the operator image
    make docker-build IMG=neo4j-operator:dev

    # Load image into Kind cluster
    kind load docker-image neo4j-operator:dev --name "$CLUSTER_NAME"

    print_success "Operator image built and loaded."
}

deploy_operator() {
    print_status "Deploying Neo4j operator..."

    # Create namespace
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

    # Install CRDs
    print_status "Installing CRDs..."
    make install

    # Deploy the operator
    print_status "Deploying operator..."
    make deploy IMG=neo4j-operator:dev

    # Wait for operator to be ready
    print_status "Waiting for operator to be ready..."
    kubectl wait --for=condition=available deployment/neo4j-operator-controller-manager -n "$NAMESPACE" --timeout=300s

    print_success "Neo4j operator deployed successfully."
}

create_test_resources() {
    print_status "Creating test resources..."

    # Create admin secret
    kubectl create secret generic neo4j-admin-secret \
        --from-literal=NEO4J_AUTH=neo4j/testpassword123 \
        --namespace default \
        --dry-run=client -o yaml | kubectl apply -f -

    # Create TLS issuer
    cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
EOF

    # Create storage class for testing
    cat <<EOF | kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: neo4j-ssd
provisioner: rancher.io/local-path
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
EOF

    print_success "Test resources created."
}

deploy_sample_cluster() {
    print_status "Deploying sample Neo4j cluster with enterprise features..."

    # Apply the comprehensive sample
    kubectl apply -f config/samples/cluster-with-all-features.yaml

    print_status "Sample cluster deployment initiated. Check status with:"
    echo "  kubectl get neo4jenterprisecluster -o wide"
    echo "  kubectl describe neo4jenterprisecluster sample-cluster"
}

show_cluster_info() {
    print_success "Development environment setup complete!"
    echo ""
    echo "Cluster Information:"
    echo "  Name: $CLUSTER_NAME"
    echo "  Namespace: $NAMESPACE"
    echo ""
    echo "Useful Commands:"
    echo "  # Check operator status"
    echo "  kubectl get pods -n $NAMESPACE"
    echo ""
    echo "  # Check Neo4j clusters"
    echo "  kubectl get neo4jenterprisecluster -o wide"
    echo ""
    echo "  # View operator logs"
    echo "  kubectl logs -f deployment/neo4j-operator-controller-manager -n $NAMESPACE"
    echo ""
    echo "  # Access Grafana (admin/admin)"
    echo "  kubectl port-forward svc/prometheus-grafana 3000:80 -n monitoring"
    echo ""
    echo "  # Access Prometheus"
    echo "  kubectl port-forward svc/prometheus-kube-prometheus-prometheus 9090:9090 -n monitoring"
    echo ""
    echo "  # Port-forward to Neo4j (after cluster is ready)"
    echo "  kubectl port-forward svc/sample-cluster-client 7474:7474 7687:7687"
}

main() {
    print_status "Setting up Neo4j Operator development environment..."

    check_dependencies
    create_cluster
    install_cert_manager
    install_prometheus
    build_and_load_image
    deploy_operator
    create_test_resources
    deploy_sample_cluster
    show_cluster_info
}

# Handle script arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [--help|--clean]"
        echo "  --help    Show this help message"
        echo "  --clean   Clean up the development environment"
        exit 0
        ;;
    --clean)
        print_status "Cleaning up development environment..."
        kind delete cluster --name "$CLUSTER_NAME" || true
        print_success "Development environment cleaned up."
        exit 0
        ;;
    "")
        main
        ;;
    *)
        print_error "Unknown argument: $1"
        echo "Use --help for usage information."
        exit 1
        ;;
esac
