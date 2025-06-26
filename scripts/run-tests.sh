#!/bin/bash

# Enhanced test runner with optimized settings and better error handling
set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TIMEOUT_MINUTES=${TEST_TIMEOUT_MINUTES:-15}
PARALLEL_JOBS=${TEST_PARALLEL_JOBS:-4}
VERBOSE=${TEST_VERBOSE:-false}
CLEANUP_ON_FAILURE=${TEST_CLEANUP_ON_FAILURE:-true}

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

# Cleanup function
cleanup() {
    if [ "$CLEANUP_ON_FAILURE" = "true" ]; then
        log_info "Performing cleanup..."
        cd "$PROJECT_ROOT"
        make test-cleanup || true
    fi
}

# Set up trap for cleanup on exit
trap cleanup EXIT

# Function to check cluster health
check_cluster_health() {
    log_info "Checking cluster health..."

    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cluster is not accessible"
        return 1
    fi

    # Check if operator is running
    if ! kubectl get pods -n neo4j-operator-system --field-selector=status.phase=Running | grep -q neo4j-operator-controller-manager; then
        log_warning "Neo4j operator is not running. Starting it..."
        make deploy-test-with-webhooks
        sleep 30
    fi

    log_success "Cluster health check passed"
    return 0
}

# Function to run integration tests with optimized settings
run_integration_tests() {
    log_info "Running integration tests with optimized settings..."

    # Check for ginkgo CLI
    if ! command -v ginkgo &> /dev/null; then
        log_warning "Ginkgo CLI not found. Installing..."
        go install github.com/onsi/ginkgo/v2/ginkgo@latest
        export PATH="$PATH:$(go env GOPATH)/bin"
    fi

    # Set optimized environment variables
    export TEST_TIMEOUT="${TIMEOUT_MINUTES}m"
    export TEST_PARALLEL="$PARALLEL_JOBS"
    export GOMAXPROCS="$PARALLEL_JOBS"

    # Run tests with Ginkgo CLI
    cd "$PROJECT_ROOT/test/integration"

    if [ "$VERBOSE" = "true" ]; then
        ginkgo -v -p --fail-fast --timeout="${TIMEOUT_MINUTES}m" --output-dir="../../" --coverprofile=coverage-integration.out
    else
        ginkgo -v -p --fail-fast --timeout="${TIMEOUT_MINUTES}m" --output-dir="../../" --coverprofile=coverage-integration.out 2>&1 | tee ../../test-output.log
    fi

    cd "$PROJECT_ROOT"

    # Generate coverage report
    if [ -f "coverage-integration.out" ]; then
        go tool cover -html=coverage-integration.out -o coverage-integration.html
    fi

    log_success "Integration tests completed successfully"
}

# Function to run unit tests
run_unit_tests() {
    log_info "Running unit tests..."

    cd "$PROJECT_ROOT"
    go test -v -race -coverprofile=coverage-unit.out \
        -timeout=5m \
        -parallel="$PARALLEL_JOBS" \
        ./internal/controller/... \
        ./internal/webhooks/... \
        ./internal/neo4j/...

    go tool cover -html=coverage-unit.out -o coverage-unit.html

    log_success "Unit tests completed successfully"
}

# Function to run webhook tests
run_webhook_tests() {
    log_info "Running webhook tests..."

    cd "$PROJECT_ROOT"
    go test -v -race -coverprofile=coverage-webhook.out \
        -timeout=5m \
        ./internal/webhooks/...

    go tool cover -html=coverage-webhook.out -o coverage-webhook.html

    log_success "Webhook tests completed successfully"
}

# Main execution
main() {
    log_info "Starting enhanced test runner..."
    log_info "Configuration:"
    log_info "  Timeout: ${TIMEOUT_MINUTES} minutes"
    log_info "  Parallel jobs: $PARALLEL_JOBS"
    log_info "  Verbose: $VERBOSE"
    log_info "  Cleanup on failure: $CLEANUP_ON_FAILURE"

    # Parse command line arguments
    case "${1:-all}" in
        "unit")
            run_unit_tests
            ;;
        "integration")
            check_cluster_health || exit 1
            run_integration_tests
            ;;
        "webhook")
            run_webhook_tests
            ;;
        "all")
            run_unit_tests
            run_webhook_tests
            check_cluster_health && run_integration_tests || log_warning "Skipping integration tests due to cluster issues"
            ;;
        *)
            log_error "Unknown test type: $1"
            log_info "Usage: $0 [unit|integration|webhook|all]"
            exit 1
            ;;
    esac

    log_success "All tests completed successfully!"
}

# Run main function with all arguments
main "$@"
