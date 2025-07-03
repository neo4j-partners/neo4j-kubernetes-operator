#!/bin/bash

# Neo4j Operator Test Runner Script
# This script provides a unified interface for running all types of tests

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Default values
TEST_TYPE="all"
VERBOSE=false
PARALLEL=false
COVERAGE=false
CLEANUP=true
TIMEOUT="10m"
SKIP_SETUP=false
SKIP_CLEANUP=false
FAIL_FAST=false
RETAIN_LOGS=false
TEST_NAMESPACE="neo4j-operator-system"

# Test results
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Function to print colored output
print_status() {
    local color=$1
    local message=$2
    echo -e "${color}${message}${NC}"
}

# Function to print verbose output
verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[VERBOSE] $1${NC}"
    fi
}

# Function to print test results
print_test_results() {
    echo ""
    print_status $PURPLE "📊 Test Results Summary"
    print_status $PURPLE "======================"
    print_status $GREEN "✅ Passed: $PASSED_TESTS"
    print_status $RED "❌ Failed: $FAILED_TESTS"
    print_status $YELLOW "⏭️  Skipped: $SKIPPED_TESTS"
    print_status $CYAN "📈 Total: $TOTAL_TESTS"

    if [[ $FAILED_TESTS -gt 0 ]]; then
        print_status $RED "❌ Some tests failed!"
        return 1
    else
        print_status $GREEN "🎉 All tests passed!"
        return 0
    fi
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [OPTIONS] [TEST_TYPE]"
    echo ""
    echo "Test Types:"
    echo "  unit         - Run unit tests only"
    echo "  integration  - Run integration tests only"
    echo "  e2e          - Run end-to-end tests only"
    echo "  webhooks     - Run webhook tests only"
    echo "  all          - Run all tests (default)"
    echo "  simple       - Run simple integration tests"
    echo "  smoke        - Run smoke tests"
    echo ""
    echo "Options:"
    echo "  -v, --verbose        - Enable verbose output"
    echo "  -p, --parallel       - Run tests in parallel"
    echo "  -c, --coverage       - Generate coverage reports"
    echo "  --no-cleanup         - Skip cleanup after tests"
    echo "  --no-setup           - Skip test environment setup"
    echo "  --fail-fast          - Stop on first failure"
    echo "  --retain-logs        - Keep test logs after completion"
    echo "  --timeout DURATION   - Set test timeout (default: 10m)"
    echo "  --namespace NAME     - Set test namespace (default: neo4j-operator-system)"
    echo "  -h, --help           - Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                           # Run all tests"
    echo "  $0 integration               # Run integration tests only"
    echo "  $0 --coverage --verbose      # Run all tests with coverage and verbose output"
    echo "  $0 simple --no-cleanup       # Run simple tests without cleanup"
    echo "  $0 e2e --timeout 30m         # Run e2e tests with 30 minute timeout"
}

# Function to setup test environment using the environment manager
setup_test_environment() {
    if [[ "$SKIP_SETUP" == "true" ]]; then
        print_status $YELLOW "⏭️  Skipping test environment setup"
        return 0
    fi

    print_status $BLUE "🚀 Setting up test environment using environment manager..."

    # Use the new environment manager
    local env_manager_args=("setup" "--timeout=$TIMEOUT")
    if [[ "$VERBOSE" == "true" ]]; then
        env_manager_args+=("--verbose")
    fi

    if ! "$SCRIPT_DIR/test-environment-manager.sh" "${env_manager_args[@]}"; then
        print_status $RED "❌ Failed to setup test environment"
        return 1
    fi

    print_status $GREEN "✅ Test environment setup completed"
}

# Function to cleanup test environment using the environment manager
cleanup_test_environment() {
    if [[ "$SKIP_CLEANUP" == "true" ]]; then
        print_status $YELLOW "⏭️  Skipping test environment cleanup"
        return 0
    fi

    print_status $BLUE "🧹 Cleaning up test environment using environment manager..."

    # Use the new environment manager
    local env_manager_args=("cleanup")
    if [[ "$VERBOSE" == "true" ]]; then
        env_manager_args+=("--verbose")
    fi

    if ! "$SCRIPT_DIR/test-environment-manager.sh" "${env_manager_args[@]}"; then
        print_status $YELLOW "⚠️  Cleanup had some issues, but continuing..."
    fi

    print_status $GREEN "✅ Test environment cleanup completed"
}

# Function to run unit tests
run_unit_tests() {
    print_status $BLUE "🧪 Running unit tests..."

    local start_time=$(date +%s)
    local test_args=()

    if [[ "$VERBOSE" == "true" ]]; then
        test_args+=("-v")
    fi

    if [[ "$COVERAGE" == "true" ]]; then
        test_args+=("-coverprofile=coverage/coverage-unit.out")
        test_args+=("-covermode=atomic")
    fi

    if [[ "$FAIL_FAST" == "true" ]]; then
        test_args+=("-failfast")
    fi

    # Run unit tests - exclude integration, e2e, and webhook test packages
    # Use go list to get packages excluding test directories
    local unit_packages=$(go list ./... | grep -v "/test/integration" | grep -v "/test/e2e" | grep -v "/test/cloud" | grep -v "/test/webhooks")

    if [[ "$VERBOSE" == "true" ]]; then
        print_status $BLUE "📋 Running unit tests in packages:"
        echo "$unit_packages"
    fi

    # Run unit tests on the filtered packages
    # Use PIPESTATUS to get the exit code of go test, not tee
    echo "$unit_packages" | xargs go test -timeout="$TIMEOUT" "${test_args[@]}" 2>&1 | tee -a logs/unit-tests.log
    local test_exit_code=${PIPESTATUS[1]}  # Get exit code of xargs go test

    if [[ $test_exit_code -eq 0 ]]; then
        print_status $GREEN "✅ Unit tests passed"
        ((PASSED_TESTS++))
    else
        print_status $RED "❌ Unit tests failed"
        ((FAILED_TESTS++))
        return 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    verbose "Unit tests completed in ${duration}s"

    ((TOTAL_TESTS++))
}

# Function to run integration tests
run_integration_tests() {
    print_status $BLUE "🔗 Running integration tests with webhooks and cert-manager..."

    local start_time=$(date +%s)

    # Run the integration tests with webhooks
    go test -v -timeout="$TIMEOUT" ./test/integration/... 2>&1 | tee -a logs/integration-tests.log
    local test_exit_code=${PIPESTATUS[0]}  # Get exit code of go test

    if [[ $test_exit_code -eq 0 ]]; then
        print_status $GREEN "✅ Integration tests passed"
        ((PASSED_TESTS++))
    else
        print_status $RED "❌ Integration tests failed"
        ((FAILED_TESTS++))
        return 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    verbose "Integration tests completed in ${duration}s"

    ((TOTAL_TESTS++))
}

# Function to run simple integration tests
run_simple_integration_tests() {
    print_status $BLUE "🔗 Running simple integration tests..."

    local start_time=$(date +%s)

    # Run simple integration tests (subset of integration tests)
    go test -v -timeout="$TIMEOUT" ./test/integration/... -ginkgo.focus="Simple" 2>&1 | tee -a logs/simple-integration-tests.log
    local test_exit_code=${PIPESTATUS[0]}  # Get exit code of go test

    if [[ $test_exit_code -eq 0 ]]; then
        print_status $GREEN "✅ Simple integration tests passed"
        ((PASSED_TESTS++))
    else
        print_status $RED "❌ Simple integration tests failed"
        ((FAILED_TESTS++))
        return 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    verbose "Simple integration tests completed in ${duration}s"

    ((TOTAL_TESTS++))
}

# Function to run e2e tests
run_e2e_tests() {
    print_status $BLUE "🌐 Running e2e tests..."

    local start_time=$(date +%s)

    # Check if ginkgo is available
    if ! command -v ginkgo &> /dev/null; then
        print_status $YELLOW "📦 Installing ginkgo..."
        go install github.com/onsi/ginkgo/v2/ginkgo@latest
    fi

    # Set environment variables for e2e tests
    export E2E_TEST=true
    export KIND_CLUSTER=neo4j-operator-test

    # Run e2e tests
    ginkgo -v -timeout="$TIMEOUT" ./test/e2e/... 2>&1 | tee -a logs/e2e-tests.log
    local test_exit_code=${PIPESTATUS[0]}  # Get exit code of ginkgo

    if [[ $test_exit_code -eq 0 ]]; then
        print_status $GREEN "✅ E2E tests passed"
        ((PASSED_TESTS++))
    else
        print_status $RED "❌ E2E tests failed"
        ((FAILED_TESTS++))
        return 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    verbose "E2E tests completed in ${duration}s"

    ((TOTAL_TESTS++))
}

# Function to run smoke tests
run_smoke_tests() {
    print_status $BLUE "💨 Running smoke tests..."

    local start_time=$(date +%s)

    # Run smoke tests (basic functionality tests)
    go test -v -timeout="$TIMEOUT" ./test/integration/... -ginkgo.focus="Smoke" 2>&1 | tee -a logs/smoke-tests.log
    local test_exit_code=${PIPESTATUS[0]}  # Get exit code of go test

    if [[ $test_exit_code -eq 0 ]]; then
        print_status $GREEN "✅ Smoke tests passed"
        ((PASSED_TESTS++))
    else
        print_status $RED "❌ Smoke tests failed"
        ((FAILED_TESTS++))
        return 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    verbose "Smoke tests completed in ${duration}s"

    ((TOTAL_TESTS++))
}

# Function to run webhook tests
run_webhook_tests() {
    print_status $BLUE "🔗 Running webhook tests..."

    local start_time=$(date +%s)
    local test_args=()

    if [[ "$VERBOSE" == "true" ]]; then
        test_args+=("-v")
    fi

    if [[ "$COVERAGE" == "true" ]]; then
        test_args+=("-coverprofile=coverage/coverage-webhooks.out")
        test_args+=("-covermode=atomic")
    fi

    if [[ "$FAIL_FAST" == "true" ]]; then
        test_args+=("-failfast")
    fi

    # Run webhook tests - both internal/webhooks and test/webhooks directories
    go test -timeout="$TIMEOUT" "${test_args[@]}" ./internal/webhooks/... ./test/webhooks/... 2>&1 | tee -a logs/webhook-tests.log
    local test_exit_code=${PIPESTATUS[0]}  # Get exit code of go test

    if [[ $test_exit_code -eq 0 ]]; then
        print_status $GREEN "✅ Webhook tests passed"
        ((PASSED_TESTS++))
    else
        print_status $RED "❌ Webhook tests failed"
        ((FAILED_TESTS++))
        return 1
    fi

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    verbose "Webhook tests completed in ${duration}s"

    ((TOTAL_TESTS++))
}

# Function to run all tests
run_all_tests() {
    print_status $BLUE "🚀 Running all tests..."

    local tests_to_run=("unit" "webhooks" "integration" "e2e")
    local failed_tests=()

    for test_type in "${tests_to_run[@]}"; do
        print_status $BLUE "Running $test_type tests..."

        case "$test_type" in
            "unit")
                if ! run_unit_tests; then
                    failed_tests+=("unit")
                    if [[ "$FAIL_FAST" == "true" ]]; then
                        break
                    fi
                fi
                ;;
            "webhooks")
                if ! run_webhook_tests; then
                    failed_tests+=("webhooks")
                    if [[ "$FAIL_FAST" == "true" ]]; then
                        break
                    fi
                fi
                ;;
            "integration")
                if ! run_integration_tests; then
                    failed_tests+=("integration")
                    if [[ "$FAIL_FAST" == "true" ]]; then
                        break
                    fi
                fi
                ;;
            "e2e")
                if ! run_e2e_tests; then
                    failed_tests+=("e2e")
                    if [[ "$FAIL_FAST" == "true" ]]; then
                        break
                    fi
                fi
                ;;
        esac
    done

    if [[ ${#failed_tests[@]} -gt 0 ]]; then
        print_status $RED "❌ The following test types failed: ${failed_tests[*]}"
        return 1
    fi

    print_status $GREEN "✅ All tests completed successfully"
}

# Function to generate coverage report
generate_coverage_report() {
    if [[ "$COVERAGE" == "true" ]]; then
        print_status $BLUE "📊 Generating coverage report..."

        # Combine coverage files if they exist
        if [[ -f "coverage/coverage-unit.out" ]]; then
            go tool cover -html=coverage/coverage-unit.out -o coverage/coverage.html
            print_status $GREEN "✅ Coverage report generated: coverage/coverage.html"
        fi

        # Show coverage summary
        if [[ -f "coverage/coverage-unit.out" ]]; then
            echo "Coverage Summary:"
            go tool cover -func=coverage/coverage-unit.out | tail -1
        fi
    fi
}

# Function to cleanup logs
cleanup_logs() {
    if [[ "$RETAIN_LOGS" != "true" ]]; then
        verbose "Cleaning up test logs..."
        rm -f logs/*.log
    fi
}

# Main function
main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            unit|integration|e2e|webhooks|all|simple|smoke)
                TEST_TYPE="$1"
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -p|--parallel)
                PARALLEL=true
                shift
                ;;
            -c|--coverage)
                COVERAGE=true
                shift
                ;;
            --no-cleanup)
                SKIP_CLEANUP=true
                shift
                ;;
            --no-setup)
                SKIP_SETUP=true
                shift
                ;;
            --fail-fast)
                FAIL_FAST=true
                shift
                ;;
            --retain-logs)
                RETAIN_LOGS=true
                shift
                ;;
            --timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            --namespace)
                TEST_NAMESPACE="$2"
                shift 2
                ;;
            -h|--help)
                show_usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done

    # Change to project root
    cd "$PROJECT_ROOT"

    # Create logs directory
    mkdir -p logs

    print_status $BLUE "🚀 Neo4j Operator Test Runner"
    print_status $BLUE "Test Type: $TEST_TYPE"
    print_status $BLUE "Verbose: $VERBOSE"
    print_status $BLUE "Coverage: $COVERAGE"
    print_status $BLUE "Timeout: $TIMEOUT"

    # Setup test environment
    if ! setup_test_environment; then
        print_status $RED "❌ Failed to setup test environment"
        exit 1
    fi

    # Run tests based on type
    case "$TEST_TYPE" in
        "unit")
            run_unit_tests
            ;;
        "integration")
            run_integration_tests
            ;;
        "e2e")
            run_e2e_tests
            ;;
        "webhooks")
            run_webhook_tests
            ;;
        "simple")
            run_simple_integration_tests
            ;;
        "smoke")
            run_smoke_tests
            ;;
        "all")
            run_all_tests
            ;;
        *)
            echo "Unknown test type: $TEST_TYPE"
            show_usage
            exit 1
            ;;
    esac

    # Generate coverage report
    generate_coverage_report

    # Cleanup logs
    cleanup_logs

    # Print test results
    print_status $BLUE "🔍 Debug: About to print test results..."
    print_test_results
    local exit_code=$?
    print_status $BLUE "🔍 Debug: print_test_results returned exit code: $exit_code"

    # Cleanup test environment
    print_status $BLUE "🔍 Debug: About to cleanup test environment..."
    cleanup_test_environment
    print_status $BLUE "🔍 Debug: Cleanup completed, exiting with code: $exit_code"

    exit $exit_code
}

# Run main function with all arguments
main "$@"
