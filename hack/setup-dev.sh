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

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Install pre-commit if not present
install_precommit() {
    if ! command_exists pre-commit; then
        log_info "Installing pre-commit..."
        if command_exists pip3; then
            pip3 install pre-commit
        elif command_exists pip; then
            pip install pre-commit
        elif command_exists brew; then
            brew install pre-commit
        else
            log_error "Cannot install pre-commit. Please install Python/pip or Homebrew first."
            return 1
        fi
        log_success "pre-commit installed successfully"
    else
        log_info "pre-commit already installed"
    fi
}

# Install kind if not present
install_kind() {
    if ! command_exists kind; then
        log_info "Installing kind..."
        if [[ "$OSTYPE" == "darwin"* ]]; then
            if command_exists brew; then
                brew install kind
            else
                curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-darwin-amd64
                chmod +x ./kind
                sudo mv ./kind /usr/local/bin/kind
            fi
        elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
            curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
            chmod +x ./kind
            sudo mv ./kind /usr/local/bin/kind
        else
            log_error "Unsupported OS for automatic kind installation"
            return 1
        fi
        log_success "kind installed successfully"
    else
        log_info "kind already installed"
    fi
}

# Install kubectl if not present
install_kubectl() {
    if ! command_exists kubectl; then
        log_info "Installing kubectl..."
        if [[ "$OSTYPE" == "darwin"* ]]; then
            if command_exists brew; then
                brew install kubectl
            else
                curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/amd64/kubectl"
                chmod +x kubectl
                sudo mv kubectl /usr/local/bin/
            fi
        elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
            curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
            chmod +x kubectl
            sudo mv kubectl /usr/local/bin/
        else
            log_error "Unsupported OS for automatic kubectl installation"
            return 1
        fi
        log_success "kubectl installed successfully"
    else
        log_info "kubectl already installed"
    fi
}

# Install helm if not present
install_helm() {
    if ! command_exists helm; then
        log_info "Installing helm..."
        if [[ "$OSTYPE" == "darwin"* ]]; then
            if command_exists brew; then
                brew install helm
            else
                curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
            fi
        elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
            curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
        else
            log_error "Unsupported OS for automatic helm installation"
            return 1
        fi
        log_success "helm installed successfully"
    else
        log_info "helm already installed"
    fi
}

# Check Go installation
check_go() {
    if ! command_exists go; then
        log_error "Go is not installed. Please install Go 1.21+ first."
        log_info "Visit: https://golang.org/doc/install"
        return 1
    fi

    local go_version
    go_version=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | grep -oE '[0-9]+\.[0-9]+')
    local required_version="1.21"

    if ! printf '%s\n%s\n' "${required_version}" "${go_version}" | sort -V -C; then
        log_error "Go version ${go_version} is installed, but version ${required_version} or higher is required"
        return 1
    fi

    log_success "Go version ${go_version} is installed"
}

# Check Docker installation
check_docker() {
    if ! command_exists docker; then
        log_error "Docker is not installed. Please install Docker first."
        log_info "Visit: https://docs.docker.com/get-docker/"
        return 1
    fi

    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is installed but not running. Please start Docker."
        return 1
    fi

    log_success "Docker is installed and running"
}

# Install Go tools
install_go_tools() {
    log_info "Installing Go development tools..."

    # Install goimports
    if ! command_exists goimports; then
        go install golang.org/x/tools/cmd/goimports@latest
        log_success "goimports installed"
    fi

    # Install delve debugger
    if ! command_exists dlv; then
        go install github.com/go-delve/delve/cmd/dlv@latest
        log_success "delve debugger installed"
    fi

    # Install air for hot reloading
    if ! command_exists air; then
        go install github.com/air-verse/air@latest
        log_success "air hot reload tool installed"
    fi

    log_success "Go development tools installed"
}

# Setup development environment
setup_dev_env() {
    log_info "Setting up development environment..."

    # Create necessary directories
    mkdir -p bin/
    mkdir -p logs/
    mkdir -p tmp/

    # Download Go dependencies
    log_info "Downloading Go dependencies..."
    go mod download
    go mod tidy

    log_success "Development environment setup complete"
}

# Main setup function
main() {
    log_info "Starting Neo4j Operator development environment setup..."

    # Check prerequisites
    check_go
    check_docker

    # Install tools
    install_kind
    install_kubectl
    install_helm
    install_precommit
    install_go_tools

    # Setup environment
    setup_dev_env

    log_success "Development environment setup complete!"
    log_info "Next steps:"
    log_info "  1. Run 'make install-hooks' to install pre-commit hooks"
    log_info "  2. Run 'make dev-cluster' to create a development cluster"
    log_info "  3. Run 'make dev-run' to start the operator locally"
    log_info "  4. Open the project in VS Code for optimal development experience"
}

main "$@"
