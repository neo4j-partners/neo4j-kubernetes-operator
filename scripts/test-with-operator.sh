#!/bin/bash

# Neo4j Operator Test Script with Webhooks and Cert-Manager
# This script sets up the operator and runs tests with proper webhook configuration

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
OPERATOR_IMAGE="neo4j-operator:test"
CLUSTER_NAME="neo4j-operator-test"
NAMESPACE="neo4j-operator-system"
TEST_TIMEOUT="30m"
TEST_PARALLEL_JOBS=2

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

# Parse command line arguments
TEST_TYPE=""
VERBOSE=false
COVERAGE=false
RETAIN_LOGS=false
SKIP_SETUP=false

while [[ $# -gt 0 ]]; do
    case $1 in
        unit|integration|e2e|smoke|all)
            TEST_TYPE="$1"
            shift
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --coverage|-c)
            COVERAGE=true
            shift
            ;;
        --retain-logs|-r)
            RETAIN_LOGS=true
            shift
            ;;
        --no-setup|-n)
            SKIP_SETUP=true
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Show help
show_help() {
    echo "Usage: $0 [TEST_TYPE] [OPTIONS]"
    echo ""
    echo "TEST_TYPE:"
    echo "  unit         - Run unit tests (no cluster required)"
    echo "  integration  - Run integration tests (requires operator setup)"
    echo "  e2e          - Run end-to-end tests (requires operator setup)"
    echo "  smoke        - Run smoke tests (basic functionality)"
    echo "  all          - Run all tests"
    echo ""
    echo "OPTIONS:"
    echo "  --verbose, -v     - Enable verbose output"
    echo "  --coverage, -c    - Generate coverage reports"
    echo "  --retain-logs, -r - Retain test logs"
    echo "  --no-setup, -n    - Skip operator setup (assumes already running)"
    echo "  --help, -h        - Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 unit --coverage"
    echo "  $0 integration --verbose"
    echo "  $0 all --coverage --retain-logs"
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

# Setup operator if needed
setup_operator() {
    if [ "$SKIP_SETUP" = true ]; then
        log_info "Skipping operator setup (--no-setup flag provided)"
        return 0
    fi

    log_info "Setting up operator with webhooks and cert-manager..."

    # Check if operator setup script exists
    if [ ! -f "scripts/setup-operator.sh" ]; then
        log_error "Operator setup script not found"
        exit 1
    fi

    # Run operator setup
    chmod +x scripts/setup-operator.sh
    ./scripts/setup-operator.sh setup

    log_success "Operator setup completed"
}

# Run unit tests
run_unit_tests() {
    log_info "Running unit tests..."

    # Set test environment variables
    export TEST_MODE=true
    export TEST_TIMEOUT=${TEST_TIMEOUT}
    export TEST_PARALLEL_JOBS=${TEST_PARALLEL_JOBS}
    export TEST_VERBOSE=${VERBOSE}

    # Run unit tests
    if [ "$COVERAGE" = true ]; then
        make test-unit
        # Generate coverage report
        if [ -f "cover.out" ]; then
            go tool cover -html=cover.out -o coverage-unit.html
            log_success "Unit test coverage report generated: coverage-unit.html"
        fi
    else
        make test-unit
    fi

    log_success "Unit tests completed"
}

# Run integration tests
run_integration_tests() {
    log_info "Running integration tests..."

    # Set test environment variables
    export TEST_MODE=true
    export TEST_TIMEOUT=${TEST_TIMEOUT}
    export TEST_PARALLEL_JOBS=${TEST_PARALLEL_JOBS}
    export TEST_VERBOSE=${VERBOSE}
    export KUBECONFIG=$(kind export kubeconfig --name ${CLUSTER_NAME})

    # Run integration tests
    if [ "$COVERAGE" = true ]; then
        make test-integration
        # Generate coverage report
        if [ -f "cover.out" ]; then
            go tool cover -html=cover.out -o coverage-integration.html
            log_success "Integration test coverage report generated: coverage-integration.html"
        fi
    else
        make test-integration
    fi

    log_success "Integration tests completed"
}

# Run E2E tests
run_e2e_tests() {
    log_info "Running E2E tests..."

    # Set test environment variables
    export TEST_MODE=true
    export TEST_TIMEOUT=${TEST_TIMEOUT}
    export TEST_PARALLEL_JOBS=${TEST_PARALLEL_JOBS}
    export TEST_VERBOSE=${VERBOSE}
    export KUBECONFIG=$(kind export kubeconfig --name ${CLUSTER_NAME})
    export KIND_CLUSTER=${CLUSTER_NAME}
    export E2E_TEST=true

    # Run E2E tests
    make test-e2e

    log_success "E2E tests completed"
}

# Run smoke tests
run_smoke_tests() {
    log_info "Running smoke tests..."

    # Set test environment variables
    export TEST_MODE=true
    export TEST_TIMEOUT="10m"
    export TEST_PARALLEL_JOBS=1
    export TEST_VERBOSE=${VERBOSE}

    # Run smoke tests
    make test-smoke

    log_success "Smoke tests completed"
}

# Run all tests
run_all_tests() {
    log_info "Running all tests..."

    # Run unit tests first (no cluster required)
    run_unit_tests

    # Setup operator for cluster-based tests
    setup_operator

    # Run integration tests
    run_integration_tests

    # Run E2E tests
    run_e2e_tests

    # Run smoke tests
    run_smoke_tests

    log_success "All tests completed"
}

# Generate comprehensive coverage report
generate_coverage_report() {
    if [ "$COVERAGE" = true ]; then
        log_info "Generating comprehensive coverage report..."

        # Combine coverage files if they exist
        if [ -f "coverage-unit.html" ] || [ -f "coverage-integration.html" ]; then
            echo "Coverage reports generated:"
            [ -f "coverage-unit.html" ] && echo "  - coverage-unit.html"
            [ -f "coverage-integration.html" ] && echo "  - coverage-integration.html"
        fi

        # Show overall coverage if available
        if [ -f "cover.out" ]; then
            echo "Overall coverage:"
            go tool cover -func=cover.out | tail -1
        fi
    fi
}

# Cleanup function
cleanup() {
    if [ "$RETAIN_LOGS" = false ]; then
        log_info "Cleaning up test artifacts..."

        # Remove temporary files
        rm -f webhook_patch_fixed.yaml

        # Clean up test environment if script exists
        if [ -f "scripts/test-environment-manager.sh" ]; then
            ./scripts/test-environment-manager.sh cleanup --force || true
        fi
    else
        log_info "Retaining test artifacts (--retain-logs flag provided)"
    fi
}

# Main function
main() {
    log_info "Starting Neo4j Operator tests..."

    # Set up cleanup trap
    trap cleanup EXIT

    # Check prerequisites
    check_prerequisites

    # Determine test type
    if [ -z "$TEST_TYPE" ]; then
        log_error "No test type specified"
        show_help
        exit 1
    fi

    # Run tests based on type
    case "$TEST_TYPE" in
        "unit")
            run_unit_tests
            ;;
        "integration")
            setup_operator
            run_integration_tests
            ;;
        "e2e")
            setup_operator
            run_e2e_tests
            ;;
        "smoke")
            run_smoke_tests
            ;;
        "all")
            run_all_tests
            ;;
        *)
            log_error "Unknown test type: $TEST_TYPE"
            show_help
            exit 1
            ;;
    esac

    # Generate coverage report
    generate_coverage_report

    log_success "Test execution completed successfully!"
}

# Run main function
main "$@"
