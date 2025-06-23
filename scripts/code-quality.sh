#!/bin/bash
set -euo pipefail

# Neo4j Operator Code Quality and Analysis Tool
# This script provides comprehensive code quality checks, security analysis, and performance profiling

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly PURPLE='\033[0;35m'
readonly CYAN='\033[0;36m'
readonly NC='\033[0m' # No Color

# Configuration
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly REPORTS_DIR="${PROJECT_ROOT}/reports"
readonly COVERAGE_DIR="${PROJECT_ROOT}/coverage"

# Create directories
mkdir -p "${REPORTS_DIR}" "${COVERAGE_DIR}"

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

# Validate environment
validate_environment() {
    # Check if we're in the right directory
    if [[ ! -f "${PROJECT_ROOT}/go.mod" ]] || ! grep -q "neo4j-kubernetes-operator" "${PROJECT_ROOT}/go.mod" 2>/dev/null; then
    log_error "This script must be run from the neo4j-kubernetes-operator project root directory"
        exit 1
    fi

    # Check Go installation
    if ! command_exists go; then
        log_error "Go is not installed. Please install Go first."
        exit 1
    fi

    log_info "Environment validation passed"
}

# Install required tools
install_quality_tools() {
    log_header "Installing Code Quality Tools"

    local go_tools=(
        "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
        "honnef.co/go/tools/cmd/staticcheck@latest"
        "github.com/kisielk/errcheck@latest"
        "github.com/fzipp/gocyclo/cmd/gocyclo@latest"
        "github.com/client9/misspell/cmd/misspell@latest"
        "github.com/gordonklaus/ineffassign@latest"
        "github.com/mdempsky/unconvert@latest"
        "github.com/sonatard/noctx/cmd/noctx@latest"
        "mvdan.cc/gofumpt@latest"
        "github.com/daixiang0/gci@latest"
        "github.com/segmentio/golines@latest"
        "github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"
        "golang.org/x/vuln/cmd/govulncheck@latest"
    )

    for tool in "${go_tools[@]}"; do
        local tool_name
        tool_name=$(basename "${tool%@*}")
        if ! command_exists "${tool_name}"; then
            log_info "Installing ${tool}..."
            if go install "${tool}"; then
                log_success "Installed ${tool_name}"
            else
                log_warning "Failed to install ${tool}"
            fi
        else
            log_info "${tool_name} already installed"
        fi
    done

    log_success "Code quality tools installation completed"
}

# Format code
format_code() {
    log_header "Formatting Code"

    cd "${PROJECT_ROOT}"

    # Standard go fmt
    log_info "Running go fmt..."
    if go fmt ./...; then
        log_success "go fmt completed"
    else
        log_error "go fmt failed"
        return 1
    fi

    # Enhanced formatting with gofumpt
    if command_exists gofumpt; then
        log_info "Running gofumpt..."
        if gofumpt -w -extra .; then
            log_success "gofumpt completed"
        else
            log_warning "gofumpt had issues"
        fi
    fi

    # Import organization with gci
    if command_exists gci; then
        log_info "Organizing imports with gci..."
        if gci write --skip-generated -s standard -s default -s "prefix(github.com/neo4j-labs/neo4j-kubernetes-operator)" .; then
            log_success "gci completed"
        else
            log_warning "gci had issues"
        fi
    fi

    # Line length formatting
    if command_exists golines; then
        log_info "Formatting line lengths..."
        if golines -w -m 120 .; then
            log_success "golines completed"
        else
            log_warning "golines had issues"
        fi
    fi

    log_success "Code formatting completed"
}

# Run linting
run_linting() {
    log_header "Running Linting Analysis"

    cd "${PROJECT_ROOT}"

    local lint_success=true

    # golangci-lint (comprehensive)
    if command_exists golangci-lint; then
        log_info "Running golangci-lint..."
        if golangci-lint run --out-format=colored-line-number,checkstyle:"${REPORTS_DIR}/golangci-lint.xml" --timeout=10m ./...; then
            log_success "golangci-lint passed"
        else
            log_warning "golangci-lint found issues"
            lint_success=false
        fi
    fi

    # staticcheck
    if command_exists staticcheck; then
        log_info "Running staticcheck..."
        if staticcheck -f stylish ./... > "${REPORTS_DIR}/staticcheck.txt" 2>&1; then
            log_success "staticcheck passed"
        else
            log_warning "staticcheck found issues"
            lint_success=false
        fi
    fi

    # errcheck
    if command_exists errcheck; then
        log_info "Running errcheck..."
        if errcheck -verbose ./... > "${REPORTS_DIR}/errcheck.txt" 2>&1; then
            log_success "errcheck passed"
        else
            log_warning "errcheck found issues"
            lint_success=false
        fi
    fi

    # gocyclo (cyclomatic complexity)
    if command_exists gocyclo; then
        log_info "Running gocyclo..."
        if gocyclo -over 10 . > "${REPORTS_DIR}/gocyclo.txt" 2>&1; then
            log_success "gocyclo passed"
        else
            log_warning "gocyclo found complex functions"
            lint_success=false
        fi
    fi

    # misspell
    if command_exists misspell; then
        log_info "Running misspell..."
        if misspell -error . > "${REPORTS_DIR}/misspell.txt" 2>&1; then
            log_success "misspell passed"
        else
            log_warning "misspell found issues"
            lint_success=false
        fi
    fi

    # ineffassign
    if command_exists ineffassign; then
        log_info "Running ineffassign..."
        if ineffassign . > "${REPORTS_DIR}/ineffassign.txt" 2>&1; then
            log_success "ineffassign passed"
        else
            log_warning "ineffassign found issues"
            lint_success=false
        fi
    fi

    # unconvert
    if command_exists unconvert; then
        log_info "Running unconvert..."
        if unconvert . > "${REPORTS_DIR}/unconvert.txt" 2>&1; then
            log_success "unconvert passed"
        else
            log_warning "unconvert found issues"
            lint_success=false
        fi
    fi

    # noctx (context.Context checks)
    if command_exists noctx; then
        log_info "Running noctx..."
        if noctx ./... > "${REPORTS_DIR}/noctx.txt" 2>&1; then
            log_success "noctx passed"
        else
            log_warning "noctx found issues"
            lint_success=false
        fi
    fi

    if [[ "${lint_success}" == "true" ]]; then
        log_success "All linting checks passed"
    else
        log_warning "Some linting checks found issues. Check reports in ${REPORTS_DIR}/"
    fi
}

# Security analysis
run_security_analysis() {
    log_header "Running Security Analysis"

    cd "${PROJECT_ROOT}"

    local security_success=true

    # Go security checker
    if command_exists gosec; then
        log_info "Running gosec..."
        if gosec -fmt=json -out="${REPORTS_DIR}/gosec.json" ./... && \
           gosec -fmt=text -out="${REPORTS_DIR}/gosec.txt" ./...; then
            log_success "gosec passed"
        else
            log_warning "gosec found security issues"
            security_success=false
        fi
    else
        log_warning "gosec not available"
    fi

    # Vulnerability scanning
    if command_exists govulncheck; then
        log_info "Running govulncheck..."
        if govulncheck -json ./... > "${REPORTS_DIR}/vulncheck.json" 2>&1; then
            log_success "govulncheck passed"
        else
            log_warning "govulncheck found vulnerabilities"
            security_success=false
        fi
    else
        log_warning "govulncheck not available"
    fi

    # Check for hardcoded secrets
    if command_exists gitleaks; then
        log_info "Running gitleaks..."
        if gitleaks detect --source="${PROJECT_ROOT}" --report-format=json --report-path="${REPORTS_DIR}/gitleaks.json" --no-git; then
            log_success "gitleaks passed"
        else
            log_warning "gitleaks found potential secrets"
            security_success=false
        fi
    else
        log_info "gitleaks not available, skipping secret detection"
    fi

    if [[ "${security_success}" == "true" ]]; then
        log_success "All security checks passed"
    else
        log_warning "Some security checks found issues. Check reports in ${REPORTS_DIR}/"
    fi
}

# Dependency analysis
analyze_dependencies() {
    log_header "Analyzing Dependencies"

    cd "${PROJECT_ROOT}"

    # Go mod analysis
    log_info "Analyzing Go modules..."

    # List all dependencies
    go list -m -u all > "${REPORTS_DIR}/dependencies.txt"

    # Dependency graph
    go mod graph > "${REPORTS_DIR}/dependency-graph.txt"

    # Unused dependencies
    if command_exists gomod; then
        gomod check > "${REPORTS_DIR}/unused-deps.txt" 2>&1 || true
    fi

    # License analysis
    if command_exists go-licenses; then
        log_info "Analyzing licenses..."
        go-licenses csv ./... > "${REPORTS_DIR}/licenses.csv" 2>&1 || true
    else
        log_info "Installing go-licenses..."
        go install github.com/google/go-licenses@latest
        go-licenses csv ./... > "${REPORTS_DIR}/licenses.csv" 2>&1 || true
    fi

    log_success "Dependency analysis completed"
}

# Test coverage analysis
analyze_coverage() {
    log_header "Analyzing Test Coverage"

    cd "${PROJECT_ROOT}"

    # Run tests with coverage
    log_info "Running tests with coverage..."
    go test -race -coverprofile="${COVERAGE_DIR}/coverage.out" -covermode=atomic ./...

    # Generate coverage report
    go tool cover -html="${COVERAGE_DIR}/coverage.out" -o "${COVERAGE_DIR}/coverage.html"
    go tool cover -func="${COVERAGE_DIR}/coverage.out" > "${COVERAGE_DIR}/coverage-summary.txt"

    # Coverage by package
    go test -race -coverprofile="${COVERAGE_DIR}/coverage-detailed.out" -covermode=atomic -coverpkg=./... ./...

    # Generate detailed coverage reports
    for pkg in $(go list ./...); do
        pkg_name=$(echo "$pkg" | sed 's|.*/||')
        go test -race -coverprofile="${COVERAGE_DIR}/coverage-${pkg_name}.out" -covermode=atomic "$pkg" || true
        if [[ -f "${COVERAGE_DIR}/coverage-${pkg_name}.out" ]]; then
            go tool cover -html="${COVERAGE_DIR}/coverage-${pkg_name}.out" -o "${COVERAGE_DIR}/coverage-${pkg_name}.html"
        fi
    done

    # Coverage statistics
    local coverage_pct=$(go tool cover -func="${COVERAGE_DIR}/coverage.out" | grep total | awk '{print $3}')
    echo "Total Coverage: ${coverage_pct}" > "${COVERAGE_DIR}/coverage-stats.txt"

    log_info "Coverage: ${coverage_pct}"
    log_success "Coverage analysis completed"
}

# Performance analysis
analyze_performance() {
    log_header "Analyzing Performance"

    cd "${PROJECT_ROOT}"

    # Run benchmarks
    log_info "Running benchmarks..."
    go test -bench=. -benchmem -cpuprofile="${REPORTS_DIR}/cpu.prof" -memprofile="${REPORTS_DIR}/mem.prof" ./... > "${REPORTS_DIR}/benchmark.txt" 2>&1 || true

    # Generate profiling reports
    if [[ -f "${REPORTS_DIR}/cpu.prof" ]]; then
        go tool pprof -text "${REPORTS_DIR}/cpu.prof" > "${REPORTS_DIR}/cpu-profile.txt" 2>&1 || true
        go tool pprof -svg "${REPORTS_DIR}/cpu.prof" > "${REPORTS_DIR}/cpu-profile.svg" 2>&1 || true
    fi

    if [[ -f "${REPORTS_DIR}/mem.prof" ]]; then
        go tool pprof -text "${REPORTS_DIR}/mem.prof" > "${REPORTS_DIR}/mem-profile.txt" 2>&1 || true
        go tool pprof -svg "${REPORTS_DIR}/mem.prof" > "${REPORTS_DIR}/mem-profile.svg" 2>&1 || true
    fi

    # Memory usage analysis
    if command_exists goleak; then
        log_info "Running memory leak detection..."
        go test -race -tags=goleak ./... > "${REPORTS_DIR}/goleak.txt" 2>&1 || true
    fi

    log_success "Performance analysis completed"
}

# Code metrics
generate_metrics() {
    log_header "Generating Code Metrics"

    cd "${PROJECT_ROOT}"

    # Lines of code
    log_info "Calculating lines of code..."
    find . -name "*.go" -not -path "./vendor/*" -not -path "./bin/*" | xargs wc -l | tail -1 > "${REPORTS_DIR}/loc.txt"

    # Function count
    grep -r "^func " --include="*.go" --exclude-dir=vendor --exclude-dir=bin . | wc -l >> "${REPORTS_DIR}/metrics.txt"

    # Struct count
    grep -r "^type .* struct" --include="*.go" --exclude-dir=vendor --exclude-dir=bin . | wc -l >> "${REPORTS_DIR}/metrics.txt"

    # Interface count
    grep -r "^type .* interface" --include="*.go" --exclude-dir=vendor --exclude-dir=bin . | wc -l >> "${REPORTS_DIR}/metrics.txt"

    # Test coverage by directory
    for dir in $(find . -type d -name "*" -not -path "./vendor/*" -not -path "./bin/*" -not -path "./.git/*"); do
        if ls "$dir"/*.go >/dev/null 2>&1; then
            pkg_coverage=$(go test -coverprofile=/tmp/coverage.out "$dir" 2>/dev/null | grep "coverage:" | awk '{print $2}' || echo "0%")
            echo "$dir: $pkg_coverage" >> "${REPORTS_DIR}/coverage-by-dir.txt"
        fi
    done

    log_success "Code metrics generated"
}

# Generate comprehensive report
generate_report() {
    log_header "Generating Comprehensive Report"

    local report_file="${REPORTS_DIR}/quality-summary.md"

    cat > "${report_file}" << EOF
# Code Quality Report

Generated on: $(date)
Project: Neo4j Operator

## Summary

This report provides a comprehensive overview of code quality, security analysis, and performance metrics.

## Reports Generated

EOF

    # List all generated reports
    for report in "${REPORTS_DIR}"/*; do
        if [[ -f "${report}" && "${report}" != "${report_file}" ]]; then
            local report_name
            report_name=$(basename "${report}")
            echo "- [${report_name}](${report_name})" >> "${report_file}"
        fi
    done

    cat >> "${report_file}" << EOF

## Coverage Reports

EOF

    # List coverage reports
    for coverage in "${COVERAGE_DIR}"/*; do
        if [[ -f "${coverage}" ]]; then
            local coverage_name
            coverage_name=$(basename "${coverage}")
            echo "- [${coverage_name}](../coverage/${coverage_name})" >> "${report_file}"
        fi
    done

    log_success "Comprehensive report generated: ${report_file}"
}

# Main execution function
main() {
    local run_format=false
    local run_lint=false
    local run_security=false
    local install_tools=false

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --format)
                run_format=true
                shift
                ;;
            --lint)
                run_lint=true
                shift
                ;;
            --security)
                run_security=true
                shift
                ;;
            --install)
                install_tools=true
                shift
                ;;
            --all)
                run_format=true
                run_lint=true
                run_security=true
                shift
                ;;
            --help|-h)
                cat << EOF
Usage: $0 [options]

Options:
  --format      Run code formatting
  --lint        Run linting analysis
  --security    Run security analysis
  --install     Install required tools
  --all         Run all checks (format, lint, security)
  --help, -h    Show this help

Examples:
  $0 --all                # Run all quality checks
  $0 --format --lint      # Run formatting and linting only
  $0 --install --all      # Install tools and run all checks
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

    # Default to running all if no specific options provided
    if [[ "${run_format}" == "false" && "${run_lint}" == "false" && "${run_security}" == "false" && "${install_tools}" == "false" ]]; then
        run_format=true
        run_lint=true
        run_security=true
    fi

    log_header "Neo4j Operator Code Quality Analysis"

    validate_environment

    if [[ "${install_tools}" == "true" ]]; then
        install_quality_tools
    fi

    if [[ "${run_format}" == "true" ]]; then
        format_code
    fi

    if [[ "${run_lint}" == "true" ]]; then
        run_linting
    fi

    if [[ "${run_security}" == "true" ]]; then
        run_security_analysis
    fi

    generate_report

    log_success "Code quality analysis completed!"
    log_info "Reports available in: ${REPORTS_DIR}/"
    log_info "Summary report: ${REPORTS_DIR}/quality-summary.md"
}

# Ensure script is not sourced
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
