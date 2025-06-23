#!/bin/bash
set -euo pipefail

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

# Configuration
DEBUG_MODE=${DEBUG:-false}
HOT_RELOAD=${HOT_RELOAD:-false}
WEBHOOK_ENABLE=${WEBHOOK_ENABLE:-false}
LOG_LEVEL=${LOG_LEVEL:-info}
METRICS_PORT=${METRICS_PORT:-8080}
HEALTH_PORT=${HEALTH_PORT:-8081}
WEBHOOK_PORT=${WEBHOOK_PORT:-9443}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check if kubectl is available and cluster is accessible
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Kubernetes cluster not accessible. Please ensure kubectl is configured and cluster is running."
        log_info "Run 'make dev-cluster' to create a development cluster."
        exit 1
    fi

    # Check if CRDs are installed
    if ! kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com >/dev/null 2>&1; then
        log_warning "Neo4j CRDs not found. Installing CRDs..."
        make install
    fi

    log_success "Prerequisites check passed"
}

# Setup environment
setup_environment() {
    log_info "Setting up development environment..."

    # Create logs directory
    mkdir -p logs/

    # Set environment variables
    export KUBECONFIG=${KUBECONFIG:-~/.kube/config}
    export ENABLE_WEBHOOKS=${WEBHOOK_ENABLE}
    export METRICS_BIND_ADDRESS=":${METRICS_PORT}"
    export HEALTH_PROBE_BIND_ADDRESS=":${HEALTH_PORT}"
    export WEBHOOK_PORT=${WEBHOOK_PORT}

    # Development specific settings
    export WATCH_NAMESPACE=${WATCH_NAMESPACE:-""}
    export LEADER_ELECT=false
    export LOG_LEVEL=${LOG_LEVEL}
    export PPROF_BIND_ADDRESS=${PPROF_BIND_ADDRESS:-":6060"}

    log_success "Environment setup complete"
}

# Generate certificates for webhooks
setup_webhooks() {
    if [[ "${WEBHOOK_ENABLE}" == "true" ]]; then
        log_info "Setting up webhook certificates..."

        # Create certificates directory
        mkdir -p tmp/k8s-webhook-server/serving-certs/

        # Generate self-signed certificates for local development
        openssl req -new -newkey rsa:4096 -x509 -sha256 -days 365 -nodes \
            -out tmp/k8s-webhook-server/serving-certs/tls.crt \
            -keyout tmp/k8s-webhook-server/serving-certs/tls.key \
            -subj "/C=US/ST=CA/L=San Francisco/O=Neo4j/OU=Engineering/CN=webhook-service.default.svc" \
            -addext "subjectAltName=DNS:webhook-service.default.svc,DNS:webhook-service.default.svc.cluster.local,DNS:localhost,IP:127.0.0.1"

        log_success "Webhook certificates generated"
    fi
}

# Run with hot reload
run_with_hot_reload() {
    log_info "Starting operator with hot reload..."

    # Create air configuration if not exists
    if [[ ! -f .air.toml ]]; then
        cat > .air.toml << EOF
root = "."
testdata_dir = "testdata"
tmp_dir = "tmp"

[build]
  args_bin = ["--zap-devel=true", "--zap-log-level=${LOG_LEVEL}"]
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main cmd/main.go"
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "testdata", "bin", "config"]
  exclude_file = []
  exclude_regex = ["_test.go"]
  exclude_unchanged = false
  follow_symlink = false
  full_bin = ""
  include_dir = []
  include_ext = ["go", "tpl", "tmpl", "html"]
  kill_delay = "0s"
  log = "build-errors.log"
  send_interrupt = false
  stop_on_root = false

[color]
  app = ""
  build = "yellow"
  main = "magenta"
  runner = "green"
  watcher = "cyan"

[log]
  time = false

[misc]
  clean_on_exit = false
EOF
    fi

    air
}

# Run with debugger
run_with_debugger() {
    log_info "Starting operator with debugger..."

    # Build with debug symbols
    go build -gcflags="all=-N -l" -o tmp/main cmd/main.go

    # Start delve
    dlv exec tmp/main -- \
        --zap-devel=true \
        --zap-log-level=debug \
        --leader-elect=false
}

# Run normally
run_normal() {
    log_info "Starting operator..."

    go run cmd/main.go \
        --zap-devel=true \
        --zap-log-level=${LOG_LEVEL} \
        --leader-elect=false \
        --metrics-bind-address=:${METRICS_PORT} \
        --health-probe-bind-address=:${HEALTH_PORT}
}

# Main function
main() {
    log_info "Neo4j Operator Development Runner"
    log_info "Configuration:"
    log_info "  Debug Mode: ${DEBUG_MODE}"
    log_info "  Hot Reload: ${HOT_RELOAD}"
    log_info "  Webhooks: ${WEBHOOK_ENABLE}"
    log_info "  Log Level: ${LOG_LEVEL}"
    log_info "  Metrics Port: ${METRICS_PORT}"
    log_info "  Health Port: ${HEALTH_PORT}"

    check_prerequisites
    setup_environment
    setup_webhooks

    # Choose run mode
    if [[ "${DEBUG_MODE}" == "true" ]]; then
        run_with_debugger
    elif [[ "${HOT_RELOAD}" == "true" ]]; then
        run_with_hot_reload
    else
        run_normal
    fi
}

# Handle script arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --debug)
            DEBUG_MODE=true
            shift
            ;;
        --hot-reload)
            HOT_RELOAD=true
            shift
            ;;
        --webhooks)
            WEBHOOK_ENABLE=true
            shift
            ;;
        --log-level)
            LOG_LEVEL="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [options]"
            echo "Options:"
            echo "  --debug         Run with debugger"
            echo "  --hot-reload    Run with hot reload"
            echo "  --webhooks      Enable webhooks"
            echo "  --log-level     Set log level (debug, info, warn, error)"
            echo "  --help          Show this help"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

main "$@"
