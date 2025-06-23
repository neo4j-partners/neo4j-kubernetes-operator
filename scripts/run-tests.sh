#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
OPERATOR_NAMESPACE="neo4j-operator-system"
TEST_TIMEOUT="30m"

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

show_help() {
    cat << EOF
Usage: $0 [OPTIONS] [TEST_TYPE]

Run tests for the Neo4j Kubernetes operator.

TEST_TYPE:
    unit            Run unit tests
    integration     Run integration tests
    e2e            Run end-to-end tests
    enterprise     Run enterprise feature tests
    all            Run all tests

OPTIONS:
    -v, --verbose      Verbose output
    -k, --keep-going   Continue testing after failures
    -t, --timeout SEC  Test timeout (default: $TEST_TIMEOUT)
    -n, --namespace NS Kubernetes namespace for tests
    --no-setup         Skip test environment setup
    --cleanup          Clean up test resources after completion
    -h, --help         Show this help message

Examples:
    $0 unit                    # Run unit tests
    $0 integration --verbose   # Run integration tests with verbose output
    $0 all --cleanup           # Run all tests and cleanup afterwards

EOF
}

setup_test_environment() {
    print_status "Setting up test environment..."

    # Ensure the cluster is available
    if ! kubectl cluster-info &> /dev/null; then
        print_error "Kubernetes cluster is not available. Please ensure kubectl is configured."
        return 1
    fi

    # Create test namespace if it doesn't exist
    if ! kubectl get namespace "$OPERATOR_NAMESPACE" &> /dev/null; then
        print_status "Creating operator namespace..."
        kubectl create namespace "$OPERATOR_NAMESPACE"
    fi

    # Check if CRDs exist and install if needed
    echo "🔍 Checking CRDs..."
    if ! kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com &> /dev/null; then
        echo "Installing CRDs..."
        make install
    fi

    print_success "Test environment ready."
}

run_unit_tests() {
    print_status "Running unit tests..."

    local args=()
    if [[ "$VERBOSE" == "true" ]]; then
        args+=("-v")
    fi

    if [ ${#args[@]} -eq 0 ]; then
        go test -timeout="$TEST_TIMEOUT" ./internal/... ./api/...
    else
        go test "${args[@]}" -timeout="$TEST_TIMEOUT" ./internal/... ./api/...
    fi

    if [ $? -eq 0 ]; then
        print_success "Unit tests passed."
        return 0
    else
        print_error "Unit tests failed."
        return 1
    fi
}

run_integration_tests() {
    print_status "Running integration tests..."

    local args=()
    if [[ "$VERBOSE" == "true" ]]; then
        args+=("-ginkgo.v")
    fi

    if [ ${#args[@]} -eq 0 ]; then
        go test -timeout="$TEST_TIMEOUT" ./test/integration/...
    else
        go test "${args[@]}" -timeout="$TEST_TIMEOUT" ./test/integration/...
    fi

    if [ $? -eq 0 ]; then
        print_success "Integration tests passed."
        return 0
    else
        print_error "Integration tests failed."
        return 1
    fi
}

run_e2e_tests() {
    print_status "Running end-to-end tests..."

    # Ensure operator is deployed
    if ! kubectl get deployment neo4j-operator-controller-manager -n "$OPERATOR_NAMESPACE" &> /dev/null; then
        print_status "Deploying operator for e2e tests..."
        make deploy IMG=neo4j-operator:dev

        # Wait for operator to be ready
        kubectl wait --for=condition=available deployment/neo4j-operator-controller-manager -n "$OPERATOR_NAMESPACE" --timeout=300s
    fi

    local args=()
    if [[ "$VERBOSE" == "true" ]]; then
        args+=("-ginkgo.v")
    fi

    if [ ${#args[@]} -eq 0 ]; then
        go test -timeout="$TEST_TIMEOUT" ./test/e2e/...
    else
        go test "${args[@]}" -timeout="$TEST_TIMEOUT" ./test/e2e/...
    fi

    if [ $? -eq 0 ]; then
        print_success "E2E tests passed."
        return 0
    else
        print_error "E2E tests failed."
        return 1
    fi
}

run_enterprise_tests() {
    print_status "Running enterprise feature tests..."

    # Check if we have enterprise features enabled
    if ! kubectl get crd neo4jdisasterrecoveries.neo4j.neo4j.com &> /dev/null; then
        print_warning "Enterprise CRDs not found. Installing..."
        make install
    fi

    local args=()
    if [[ "$VERBOSE" == "true" ]]; then
        args+=("-ginkgo.v")
    fi

    # Run enterprise-specific tests
    if [ ${#args[@]} -eq 0 ]; then
        go test -timeout="$TEST_TIMEOUT" ./test/integration/enterprise_features_test.go
    else
        go test "${args[@]}" -timeout="$TEST_TIMEOUT" ./test/integration/enterprise_features_test.go
    fi

    if [ $? -eq 0 ]; then
        print_success "Enterprise feature tests passed."
        return 0
    else
        print_error "Enterprise feature tests failed."
        return 1
    fi
}

cleanup_test_resources() {
    print_status "Cleaning up test resources..."

    # Delete test namespaces
    kubectl delete namespace --selector=test-type=neo4j-operator --wait=false || true

    # Clean up test resources
    echo "🧹 Cleaning up test resources..."
    kubectl delete neo4jenterprisecluster --all --all-namespaces --wait=false || true
    kubectl delete neo4jdatabase --all --all-namespaces --wait=false || true
    kubectl delete neo4jbackup --all --all-namespaces --wait=false || true
    kubectl delete neo4jrestore --all --all-namespaces --wait=false || true
    kubectl delete neo4juser --all --all-namespaces --wait=false || true
    kubectl delete neo4jrole --all --all-namespaces --wait=false || true
    kubectl delete neo4jgrant --all --all-namespaces --wait=false || true
    kubectl delete neo4jplugin --all --all-namespaces --wait=false || true

    print_success "Test cleanup completed."
}

# Default values
VERBOSE="false"
KEEP_GOING="false"
TEST_TIMEOUT="30m"
NAMESPACE=""
SETUP_ENV="true"
CLEANUP="false"
TEST_TYPE=""

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE="true"
            shift
            ;;
        -k|--keep-going)
            KEEP_GOING="true"
            shift
            ;;
        -t|--timeout)
            TEST_TIMEOUT="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --no-setup)
            SETUP_ENV="false"
            shift
            ;;
        --cleanup)
            CLEANUP="true"
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        unit|integration|e2e|enterprise|all)
            TEST_TYPE="$1"
            shift
            ;;
        *)
            print_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Use provided namespace or default
if [[ -n "$NAMESPACE" ]]; then
    OPERATOR_NAMESPACE="$NAMESPACE"
fi

# Validate test type
case "$TEST_TYPE" in
    "")
        print_error "Test type is required."
        show_help
        exit 1
        ;;
    unit|integration|e2e|enterprise|all)
        ;;
    *)
        print_error "Invalid test type: $TEST_TYPE"
        show_help
        exit 1
        ;;
esac

# Main execution
main() {
    print_status "Starting Neo4j Operator test suite..."
    print_status "Test type: $TEST_TYPE"
    print_status "Timeout: $TEST_TIMEOUT"
    print_status "Namespace: $OPERATOR_NAMESPACE"

    if [[ "$SETUP_ENV" == "true" ]]; then
        setup_test_environment
    fi

    local failed_tests=()
    local exit_code=0

    case "$TEST_TYPE" in
        unit)
            run_unit_tests || { failed_tests+=("unit"); exit_code=1; }
            ;;
        integration)
            run_integration_tests || { failed_tests+=("integration"); exit_code=1; }
            ;;
        e2e)
            run_e2e_tests || { failed_tests+=("e2e"); exit_code=1; }
            ;;
        enterprise)
            run_enterprise_tests || { failed_tests+=("enterprise"); exit_code=1; }
            ;;
        all)
            run_unit_tests || { failed_tests+=("unit"); exit_code=1; }
            if [[ "$KEEP_GOING" == "true" ]] || [[ $exit_code -eq 0 ]]; then
                run_integration_tests || { failed_tests+=("integration"); exit_code=1; }
            fi
            if [[ "$KEEP_GOING" == "true" ]] || [[ $exit_code -eq 0 ]]; then
                run_enterprise_tests || { failed_tests+=("enterprise"); exit_code=1; }
            fi
            if [[ "$KEEP_GOING" == "true" ]] || [[ $exit_code -eq 0 ]]; then
                run_e2e_tests || { failed_tests+=("e2e"); exit_code=1; }
            fi
            ;;
    esac

    if [[ "$CLEANUP" == "true" ]]; then
        cleanup_test_resources
    fi

    # Report results
    echo ""
    if [[ $exit_code -eq 0 ]]; then
        print_success "All tests passed! ✅"
    else
        print_error "Some tests failed! ❌"
        if [[ ${#failed_tests[@]} -gt 0 ]]; then
            print_error "Failed test suites: ${failed_tests[*]}"
        fi
    fi

    exit $exit_code
}

main "$@"
