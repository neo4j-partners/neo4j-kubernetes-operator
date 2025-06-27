#!/bin/bash

# Neo4j Operator Setup Script
# This script sets up the Neo4j operator with webhooks and cert-manager for local development and testing

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
OPERATOR_IMAGE="neo4j-operator:dev"
CLUSTER_NAME="neo4j-operator-test"
NAMESPACE="neo4j-operator-system"
TIMEOUT=300

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

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check if kind is available
    if ! command -v kind &> /dev/null; then
        log_error "kind is not installed. Please install kind first."
        exit 1
    fi

    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed. Please install kubectl first."
        exit 1
    fi

    # Check if docker is available
    if ! command -v docker &> /dev/null; then
        log_error "docker is not installed. Please install docker first."
        exit 1
    fi

    # Check if make is available
    if ! command -v make &> /dev/null; then
        log_error "make is not installed. Please install make first."
        exit 1
    fi

    log_success "All prerequisites are available"
}

# Check if cluster exists
check_cluster() {
    log_info "Checking if kind cluster exists..."

    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log_info "Cluster ${CLUSTER_NAME} already exists"
        return 0
    else
        log_warning "Cluster ${CLUSTER_NAME} does not exist"
        return 1
    fi
}

# Setup test environment
setup_environment() {
    log_info "Setting up test environment..."

    # Generate manifests
    make manifests

    # Setup test environment using environment manager
    if [ -f "scripts/test-environment-manager.sh" ]; then
        chmod +x scripts/test-environment-manager.sh
        ./scripts/test-environment-manager.sh setup --verbose --timeout 15m
    else
        log_warning "test-environment-manager.sh not found, skipping environment setup"
    fi

    log_success "Test environment setup completed"
}

# Build and load operator image
build_and_load_image() {
    log_info "Building operator image..."

    # Build the operator image
    make docker-build IMG=${OPERATOR_IMAGE}

    # Load the image into kind cluster
    kind load docker-image ${OPERATOR_IMAGE} --name ${CLUSTER_NAME}

    log_success "Operator image built and loaded"
}

# Apply webhook and cert-manager resources
apply_webhook_resources() {
    log_info "Applying webhook and cert-manager resources..."

    # Apply webhook configurations
    kubectl apply -k config/webhook/

    # Apply cert-manager resources directly to avoid kustomize version issues
    kubectl apply -f config/certmanager/certificate.yaml
    kubectl apply -f config/certmanager/issuer.yaml

    log_success "Webhook and cert-manager resources applied"
}

# Wait for webhook certificate
wait_for_certificate() {
    log_info "Waiting for webhook certificate to be ready..."

    # Wait for certificate to be ready
    kubectl wait --for=condition=Ready certificate/serving-cert -n ${NAMESPACE} --timeout=60s

    # Verify secret exists
    if ! kubectl get secret webhook-server-cert -n ${NAMESPACE} &> /dev/null; then
        log_error "Webhook certificate secret not found"
        kubectl get secrets -n ${NAMESPACE}
        exit 1
    fi

    log_success "Webhook certificate is ready"
}

# Deploy operator
deploy_operator() {
    log_info "Deploying operator..."

    # Deploy the operator
    make deploy IMG=${OPERATOR_IMAGE}

    log_success "Operator deployment initiated"
}

# Patch deployment with webhook certificate mount
patch_deployment() {
    log_info "Patching deployment with webhook certificate mount..."

    # Create the webhook patch with correct namespace
    cat > webhook_patch_fixed.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: neo4j-operator-controller-manager
  namespace: neo4j-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        ports:
        - containerPort: 9443
          name: webhook-server
          protocol: TCP
        volumeMounts:
        - mountPath: /tmp/k8s-webhook-server/serving-certs
          name: cert
          readOnly: true
      volumes:
      - name: cert
        secret:
          defaultMode: 420
          secretName: webhook-server-cert
EOF

    # Apply the patch
    kubectl patch deployment neo4j-operator-controller-manager -n ${NAMESPACE} --patch-file webhook_patch_fixed.yaml

    log_success "Webhook certificate mount patch applied"
}

# Wait for operator to be ready
wait_for_operator() {
    log_info "Waiting for operator to be ready..."

    # Wait for deployment rollout to complete
    kubectl rollout status deployment/neo4j-operator-controller-manager -n ${NAMESPACE} --timeout=${TIMEOUT}s

    # Check operator status
    kubectl get pods -n ${NAMESPACE}
    kubectl get deployment neo4j-operator-controller-manager -n ${NAMESPACE}

    log_success "Operator deployment completed"
}

# Verify operator logs
verify_operator_logs() {
    log_info "Verifying operator logs..."

    # Wait a moment for operator to fully initialize
    sleep 10

    # Check operator logs for successful startup
    kubectl logs -n ${NAMESPACE} deployment/neo4j-operator-controller-manager --tail=50

    # Verify no crash loop backoff
    if kubectl get pods -n ${NAMESPACE} | grep -q "CrashLoopBackOff"; then
        log_error "Operator pod is in CrashLoopBackOff state"
        kubectl describe pods -n ${NAMESPACE}
        exit 1
    fi

    log_success "Operator logs verified - no crash loop detected"
}

# Show operator status
show_status() {
    log_info "Operator status:"
    echo "=== Pods ==="
    kubectl get pods -n ${NAMESPACE}
    echo ""
    echo "=== Services ==="
    kubectl get services -n ${NAMESPACE}
    echo ""
    echo "=== Deployments ==="
    kubectl get deployments -n ${NAMESPACE}
    echo ""
    echo "=== Webhook Configurations ==="
    kubectl get mutatingwebhookconfiguration
    kubectl get validatingwebhookconfiguration
    echo ""
    echo "=== Certificates ==="
    kubectl get certificate -n ${NAMESPACE}
    echo ""
    echo "=== Secrets ==="
    kubectl get secrets -n ${NAMESPACE}
}

# Cleanup function
cleanup() {
    log_info "Cleaning up..."

    # Remove temporary files
    rm -f webhook_patch_fixed.yaml

    # Cleanup test environment if script exists
    if [ -f "scripts/test-environment-manager.sh" ]; then
        ./scripts/test-environment-manager.sh cleanup --force || true
    fi
}

# Main function
main() {
    log_info "Starting Neo4j Operator setup..."

    # Set up cleanup trap
    trap cleanup EXIT

    # Check prerequisites
    check_prerequisites

    # Check if cluster exists
    if ! check_cluster; then
        log_error "Kind cluster ${CLUSTER_NAME} does not exist. Please create it first with: make dev-cluster"
        exit 1
    fi

    # Setup environment
    setup_environment

    # Build and load image
    build_and_load_image

    # Apply webhook resources
    apply_webhook_resources

    # Wait for certificate
    wait_for_certificate

    # Deploy operator
    deploy_operator

    # Patch deployment
    patch_deployment

    # Wait for operator
    wait_for_operator

    # Verify logs
    verify_operator_logs

    # Show status
    show_status

    log_success "Neo4j Operator setup completed successfully!"
    log_info "You can now run tests or deploy Neo4j resources."
}

# Parse command line arguments
case "${1:-setup}" in
    "setup")
        main
        ;;
    "status")
        show_status
        ;;
    "logs")
        kubectl logs -n ${NAMESPACE} deployment/neo4j-operator-controller-manager -f
        ;;
    "cleanup")
        cleanup
        ;;
    "help"|"-h"|"--help")
        echo "Usage: $0 [setup|status|logs|cleanup|help]"
        echo ""
        echo "Commands:"
        echo "  setup    - Set up the Neo4j operator with webhooks (default)"
        echo "  status   - Show operator status"
        echo "  logs     - Follow operator logs"
        echo "  cleanup  - Clean up temporary files"
        echo "  help     - Show this help message"
        ;;
    *)
        log_error "Unknown command: $1"
        echo "Use '$0 help' for usage information"
        exit 1
        ;;
esac
