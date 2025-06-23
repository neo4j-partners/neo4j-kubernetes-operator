#!/bin/bash
set -euo pipefail

# Neo4j Operator Testing Framework
# This script provides comprehensive testing capabilities including unit, integration, e2e, and performance tests

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TEST_RESULTS_DIR="${PROJECT_ROOT}/test-results"
COVERAGE_DIR="${PROJECT_ROOT}/coverage"

# Create directories
mkdir -p "${TEST_RESULTS_DIR}" "${COVERAGE_DIR}"

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

log_header() {
    echo -e "${CYAN}==== $1 ====${NC}"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Setup test environment
setup_test_env() {
    log_header "Setting up Test Environment"

    cd "${PROJECT_ROOT}"

    # Install test dependencies
    go mod download
    go mod tidy

    # Install required testing tools
    local test_tools=(
        "github.com/onsi/ginkgo/v2/ginkgo@latest"
        "github.com/onsi/gomega@latest"
        "sigs.k8s.io/controller-runtime/tools/setup-envtest@latest"
        "github.com/golang/mock/mockgen@latest"
        "github.com/vektra/mockery/v2@latest"
        "go.uber.org/goleak@latest"
    )

    for tool in "${test_tools[@]}"; do
        log_info "Installing $tool..."
        go install "$tool" || log_warning "Failed to install $tool"
    done

    # Setup test cluster if needed
    setup_test_cluster

    log_success "Test environment setup completed"
}

# Setup test cluster
setup_test_cluster() {
    log_info "Setting up test cluster..."

    # Check if kind cluster exists
    if ! kind get clusters | grep -q "test-cluster"; then
        log_info "Creating test cluster..."

        # Create test cluster configuration
        cat > "${PROJECT_ROOT}/test-cluster-config.yaml" << 'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: test-cluster
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 30000
  - containerPort: 30001
    hostPort: 30001
- role: worker
- role: worker
EOF

        kind create cluster --config "${PROJECT_ROOT}/test-cluster-config.yaml"

        # Wait for cluster to be ready
        kubectl wait --for=condition=ready node --all --timeout=300s

        # Install cert-manager for webhook tests
        kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
        kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s

        log_success "Test cluster created"
    else
        log_info "Test cluster already exists"
    fi
}

# Run unit tests
run_unit_tests() {
    log_header "Running Unit Tests"

    cd "${PROJECT_ROOT}"

    local test_flags=(
        "-race"
        "-v"
        "-coverprofile=${COVERAGE_DIR}/unit-coverage.out"
        "-covermode=atomic"
        "-timeout=10m"
    )

    if [[ "${PARALLEL:-true}" == "true" ]]; then
        test_flags+=("-p" "4")
    fi

    log_info "Running unit tests with flags: ${test_flags[*]}"

    # Run tests and capture output
    if go test "${test_flags[@]}" ./... 2>&1 | tee "${TEST_RESULTS_DIR}/unit-tests.log"; then
        log_success "Unit tests passed"

        # Generate coverage report
        go tool cover -html="${COVERAGE_DIR}/unit-coverage.out" -o "${COVERAGE_DIR}/unit-coverage.html"
        local coverage=$(go tool cover -func="${COVERAGE_DIR}/unit-coverage.out" | grep total | awk '{print $3}')
        log_info "Unit test coverage: ${coverage}"

        return 0
    else
        log_error "Unit tests failed"
        return 1
    fi
}

# Run integration tests
run_integration_tests() {
    log_header "Running Integration Tests"

    cd "${PROJECT_ROOT}"

    # Setup envtest
    export KUBEBUILDER_ASSETS=$(setup-envtest use --use-env -p path)

    local test_flags=(
        "-race"
        "-v"
        "-coverprofile=${COVERAGE_DIR}/integration-coverage.out"
        "-tags=integration"
        "-timeout=20m"
    )

    log_info "Running integration tests..."

    if go test "${test_flags[@]}" ./test/integration/... 2>&1 | tee "${TEST_RESULTS_DIR}/integration-tests.log"; then
        log_success "Integration tests passed"

        # Generate coverage report
        go tool cover -html="${COVERAGE_DIR}/integration-coverage.out" -o "${COVERAGE_DIR}/integration-coverage.html"
        local coverage=$(go tool cover -func="${COVERAGE_DIR}/integration-coverage.out" | grep total | awk '{print $3}')
        log_info "Integration test coverage: ${coverage}"

        return 0
    else
        log_error "Integration tests failed"
        return 1
    fi
}

# Run e2e tests
run_e2e_tests() {
    log_header "Running End-to-End Tests"

    cd "${PROJECT_ROOT}"

    # Ensure test cluster is available
    if ! kubectl cluster-info --context="kind-test-cluster" >/dev/null 2>&1; then
        log_error "Test cluster not available. Run setup first."
        return 1
    fi

    # Install CRDs
    make install

    # Deploy operator
    make deploy IMG=controller:test

    local test_flags=(
        "-v"
        "-timeout=30m"
        "-tags=e2e"
    )

    log_info "Running e2e tests..."

    if go test "${test_flags[@]}" ./test/e2e/... 2>&1 | tee "${TEST_RESULTS_DIR}/e2e-tests.log"; then
        log_success "E2E tests passed"
        return 0
    else
        log_error "E2E tests failed"
        return 1
    fi
}

# Run Ginkgo tests
run_ginkgo_tests() {
    log_header "Running Ginkgo Tests"

    cd "${PROJECT_ROOT}"

    if ! command_exists ginkgo; then
        log_error "Ginkgo not installed. Run 'go install github.com/onsi/ginkgo/v2/ginkgo@latest'"
        return 1
    fi

    local ginkgo_flags=(
        "-r"
        "--race"
        "--randomize-all"
        "--randomize-suites"
        "--fail-on-pending"
        "--cover"
        "--coverprofile=${COVERAGE_DIR}/ginkgo-coverage.out"
        "--trace"
        "--progress"
        "--json-report=${TEST_RESULTS_DIR}/ginkgo-report.json"
        "--junit-report=${TEST_RESULTS_DIR}/ginkgo-junit.xml"
    )

    if [[ "${PARALLEL:-true}" == "true" ]]; then
        ginkgo_flags+=("-p")
    fi

    log_info "Running Ginkgo tests..."

    if ginkgo "${ginkgo_flags[@]}" ./... 2>&1 | tee "${TEST_RESULTS_DIR}/ginkgo-tests.log"; then
        log_success "Ginkgo tests passed"

        # Generate coverage report
        if [[ -f "${COVERAGE_DIR}/ginkgo-coverage.out" ]]; then
            go tool cover -html="${COVERAGE_DIR}/ginkgo-coverage.out" -o "${COVERAGE_DIR}/ginkgo-coverage.html"
        fi

        return 0
    else
        log_error "Ginkgo tests failed"
        return 1
    fi
}

# Run benchmark tests
run_benchmark_tests() {
    log_header "Running Benchmark Tests"

    cd "${PROJECT_ROOT}"

    local bench_flags=(
        "-bench=."
        "-benchmem"
        "-benchtime=10s"
        "-cpuprofile=${TEST_RESULTS_DIR}/benchmark-cpu.prof"
        "-memprofile=${TEST_RESULTS_DIR}/benchmark-mem.prof"
        "-trace=${TEST_RESULTS_DIR}/benchmark-trace.out"
    )

    log_info "Running benchmark tests..."

    if go test "${bench_flags[@]}" ./... > "${TEST_RESULTS_DIR}/benchmark-results.txt" 2>&1; then
        log_success "Benchmark tests completed"

        # Generate benchmark reports
        if [[ -f "${TEST_RESULTS_DIR}/benchmark-cpu.prof" ]]; then
            go tool pprof -text "${TEST_RESULTS_DIR}/benchmark-cpu.prof" > "${TEST_RESULTS_DIR}/benchmark-cpu.txt"
            go tool pprof -svg "${TEST_RESULTS_DIR}/benchmark-cpu.prof" > "${TEST_RESULTS_DIR}/benchmark-cpu.svg"
        fi

        if [[ -f "${TEST_RESULTS_DIR}/benchmark-mem.prof" ]]; then
            go tool pprof -text "${TEST_RESULTS_DIR}/benchmark-mem.prof" > "${TEST_RESULTS_DIR}/benchmark-mem.txt"
            go tool pprof -svg "${TEST_RESULTS_DIR}/benchmark-mem.prof" > "${TEST_RESULTS_DIR}/benchmark-mem.svg"
        fi

        return 0
    else
        log_error "Benchmark tests failed"
        return 1
    fi
}

# Run security tests
run_security_tests() {
    log_header "Running Security Tests"

    cd "${PROJECT_ROOT}"

    # Install security testing tools
    if ! command_exists gosec; then
        go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
    fi

    if ! command_exists govulncheck; then
        go install golang.org/x/vuln/cmd/govulncheck@latest
    fi

    # Run gosec
    log_info "Running gosec security scan..."
    gosec -fmt=json -out="${TEST_RESULTS_DIR}/gosec-report.json" ./...
    gosec -fmt=text -out="${TEST_RESULTS_DIR}/gosec-report.txt" ./...

    # Run vulnerability check
    log_info "Running vulnerability check..."
    govulncheck -json ./... > "${TEST_RESULTS_DIR}/vuln-report.json" 2>&1 || true

    # Check for hardcoded secrets
    if command_exists gitleaks; then
        log_info "Running secret detection..."
        gitleaks detect --source="${PROJECT_ROOT}" --report-format=json --report-path="${TEST_RESULTS_DIR}/secrets-report.json" --no-git || true
    fi

    log_success "Security tests completed"
}

# Run performance tests
run_performance_tests() {
    log_header "Running Performance Tests"

    cd "${PROJECT_ROOT}"

    # Performance test configuration
    local duration="${PERF_DURATION:-5m}"
    local concurrent="${PERF_CONCURRENT:-10}"

    log_info "Running performance tests (duration: $duration, concurrent: $concurrent)..."

    # Run load tests if available
    if [[ -d "test/performance" ]]; then
        go test -tags=performance -timeout=30m ./test/performance/... \
            -duration="$duration" \
            -concurrent="$concurrent" \
            > "${TEST_RESULTS_DIR}/performance-results.txt" 2>&1 || true
    fi

    # Run memory leak detection
    if command_exists goleak; then
        log_info "Running memory leak detection..."
        go test -tags=goleak -run=TestMain ./... > "${TEST_RESULTS_DIR}/leak-detection.txt" 2>&1 || true
    fi

    log_success "Performance tests completed"
}

# Run chaos tests
run_chaos_tests() {
    log_header "Running Chaos Tests"

    cd "${PROJECT_ROOT}"

    # Chaos testing requires a cluster
    if ! kubectl cluster-info --context="kind-test-cluster" >/dev/null 2>&1; then
        log_warning "Test cluster not available. Skipping chaos tests."
        return 0
    fi

    log_info "Running chaos tests..."

    # Install chaos testing tools
    if ! kubectl get namespace chaos-engineering >/dev/null 2>&1; then
        kubectl create namespace chaos-engineering

        # Install Chaos Mesh (lightweight chaos testing)
        kubectl apply -f https://mirrors.chaos-mesh.org/v2.4.3/install.sh || true
    fi

    # Run chaos tests if available
    if [[ -d "test/chaos" ]]; then
        go test -tags=chaos -timeout=45m ./test/chaos/... \
            > "${TEST_RESULTS_DIR}/chaos-results.txt" 2>&1 || true
    fi

    log_success "Chaos tests completed"
}

# Generate test report
generate_test_report() {
    log_header "Generating Test Report"

    local report_file="${TEST_RESULTS_DIR}/test-report.html"

    cat > "$report_file" << 'EOF'
<!DOCTYPE html>
<html>
<head>
    <title>Neo4j Operator - Test Report</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background: #2196F3; color: white; padding: 20px; border-radius: 5px; }
        .section { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; }
        .success { background: #d4edda; border-color: #c3e6cb; }
        .warning { background: #fff3cd; border-color: #ffeaa7; }
        .error { background: #f8d7da; border-color: #f5c6cb; }
        .metric { display: inline-block; margin: 10px; padding: 10px; background: #f8f9fa; border-radius: 3px; }
        pre { background: #f8f9fa; padding: 10px; border-radius: 3px; overflow-x: auto; font-size: 12px; }
        .log-snippet { max-height: 200px; overflow-y: auto; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Neo4j Operator - Test Report</h1>
        <p>Generated on: $(date)</p>
    </div>
EOF

    # Add test results sections
    for test_type in "unit" "integration" "e2e" "ginkgo" "benchmark" "security"; do
        if [[ -f "${TEST_RESULTS_DIR}/${test_type}-tests.log" ]]; then
            local status="success"
            if grep -q "FAIL" "${TEST_RESULTS_DIR}/${test_type}-tests.log"; then
                status="error"
            elif grep -q "SKIP\|WARNING" "${TEST_RESULTS_DIR}/${test_type}-tests.log"; then
                status="warning"
            fi

            cat >> "$report_file" << EOF
    <div class="section $status">
        <h2>${test_type^} Tests</h2>
        <div class="log-snippet">
            <pre>$(tail -50 "${TEST_RESULTS_DIR}/${test_type}-tests.log" | head -30)</pre>
        </div>
        <p><a href="${test_type}-tests.log">View full log</a></p>
    </div>
EOF
        fi
    done

    # Add coverage information
    if [[ -f "${COVERAGE_DIR}/unit-coverage.out" ]]; then
        local coverage=$(go tool cover -func="${COVERAGE_DIR}/unit-coverage.out" | grep total | awk '{print $3}')
        cat >> "$report_file" << EOF
    <div class="section success">
        <h2>Test Coverage</h2>
        <div class="metric">
            <strong>Unit Test Coverage: $coverage</strong>
        </div>
        <p><a href="../coverage/unit-coverage.html">View detailed coverage report</a></p>
    </div>
EOF
    fi

    cat >> "$report_file" << 'EOF'
</body>
</html>
EOF

    log_success "Test report generated: $report_file"
}

# Clean test artifacts
clean_test_artifacts() {
    log_header "Cleaning Test Artifacts"

    rm -rf "${TEST_RESULTS_DIR}"/* "${COVERAGE_DIR}"/*

    # Clean test cluster
    if kind get clusters | grep -q "test-cluster"; then
        log_info "Deleting test cluster..."
        kind delete cluster --name test-cluster
    fi

    # Clean test files
    find "${PROJECT_ROOT}" -name "*.test" -delete
    find "${PROJECT_ROOT}" -name "*.prof" -delete
    find "${PROJECT_ROOT}" -name "*.out" -delete

    log_success "Test artifacts cleaned"
}

# Show test results summary
show_test_summary() {
    log_header "Test Results Summary"

    echo -e "${BLUE}Test Results Directory:${NC} ${TEST_RESULTS_DIR}"
    echo -e "${BLUE}Coverage Directory:${NC} ${COVERAGE_DIR}"
    echo

    # List generated files
    if [[ -d "${TEST_RESULTS_DIR}" ]]; then
        echo -e "${BLUE}Generated Test Files:${NC}"
        ls -la "${TEST_RESULTS_DIR}/" | grep -v "^d" | awk '{print "  " $9 " (" $5 " bytes)"}'
    fi

    if [[ -d "${COVERAGE_DIR}" ]]; then
        echo -e "\n${BLUE}Generated Coverage Files:${NC}"
        ls -la "${COVERAGE_DIR}/" | grep -v "^d" | awk '{print "  " $9 " (" $5 " bytes)"}'
    fi

    # Show coverage summary
    if [[ -f "${COVERAGE_DIR}/unit-coverage.out" ]]; then
        echo -e "\n${BLUE}Coverage Summary:${NC}"
        go tool cover -func="${COVERAGE_DIR}/unit-coverage.out" | tail -5
    fi

    echo -e "\n${GREEN}Test execution completed!${NC}"
}

# Main function
main() {
    case "${1:-help}" in
        "setup")
            setup_test_env
            ;;
        "unit")
            run_unit_tests
            ;;
        "integration")
            run_integration_tests
            ;;
        "e2e")
            run_e2e_tests
            ;;
        "ginkgo")
            run_ginkgo_tests
            ;;
        "benchmark")
            run_benchmark_tests
            ;;
        "security")
            run_security_tests
            ;;
        "performance")
            run_performance_tests
            ;;
        "chaos")
            run_chaos_tests
            ;;
        "all")
            setup_test_env
            run_unit_tests
            run_integration_tests
            run_e2e_tests
            run_security_tests
            generate_test_report
            show_test_summary
            ;;
        "quick")
            run_unit_tests
            run_integration_tests
            show_test_summary
            ;;
        "report")
            generate_test_report
            ;;
        "summary")
            show_test_summary
            ;;
        "clean")
            clean_test_artifacts
            ;;
        "help"|"-h"|"--help")
            show_help
            ;;
        *)
            log_error "Unknown command: $1"
            show_help
            exit 1
            ;;
    esac
}

# Show help
show_help() {
    echo -e "${CYAN}Neo4j Operator Testing Framework${NC}"
    echo
    echo "Usage: $0 [command] [options]"
    echo
    echo "Commands:"
    echo "  setup        Setup test environment"
    echo "  unit         Run unit tests"
    echo "  integration  Run integration tests"
    echo "  e2e          Run end-to-end tests"
    echo "  ginkgo       Run Ginkgo BDD tests"
    echo "  benchmark    Run benchmark tests"
    echo "  security     Run security tests"
    echo "  performance  Run performance tests"
    echo "  chaos        Run chaos engineering tests"
    echo "  all          Run all tests"
    echo "  quick        Run quick test suite"
    echo "  report       Generate test report"
    echo "  summary      Show test results summary"
    echo "  clean        Clean test artifacts"
    echo "  help         Show this help message"
    echo
    echo "Environment Variables:"
    echo "  PARALLEL=true/false     Enable parallel test execution (default: true)"
    echo "  PERF_DURATION=5m        Performance test duration (default: 5m)"
    echo "  PERF_CONCURRENT=10      Concurrent performance test connections (default: 10)"
    echo
    echo "Examples:"
    echo "  $0 setup                # Setup test environment"
    echo "  $0 unit                 # Run unit tests"
    echo "  $0 all                  # Run complete test suite"
    echo "  PARALLEL=false $0 unit  # Run unit tests sequentially"
}

# Run main function
main "$@"
