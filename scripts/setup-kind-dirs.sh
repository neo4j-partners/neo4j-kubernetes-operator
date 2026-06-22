#!/bin/bash

# Setup script for Kind cluster host directories
# This script creates the necessary host directories for Kind cluster mounts

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Directories needed for Kind cluster mounts
KIND_DIRS=(
    "/tmp/kind-control-plane"
    "/tmp/kind-worker-1"
    "/tmp/kind-worker-2"
)

setup_kind_directories() {
    log_info "Setting up Kind cluster host directories..."

    for dir in "${KIND_DIRS[@]}"; do
        if [[ ! -d "$dir" ]]; then
            log_info "Creating directory: $dir"
            mkdir -p "$dir"
        else
            log_info "Directory already exists: $dir"
        fi

        # Ensure proper permissions
        chmod 755 "$dir"
        log_info "Set permissions for: $dir"
    done

    log_info "Kind directories setup completed"
}

check_docker_permissions() {
    log_info "Checking Docker permissions..."

    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is not accessible. Please ensure Docker is running and you have proper permissions."
        log_info "You may need to add your user to the docker group or run with sudo."
        return 1
    fi

    log_info "Docker permissions OK"
    return 0
}

check_kind_installation() {
    log_info "Checking Kind installation..."

    if ! command -v kind >/dev/null 2>&1; then
        log_error "Kind is not installed. Please install Kind first:"
        log_info "  go install sigs.k8s.io/kind@latest"
        return 1
    fi

    local kind_version
    kind_version=$(kind version 2>/dev/null | head -n1 || echo "unknown")
    log_info "Kind version: $kind_version"
    return 0
}

check_port_availability() {
    log_info "Checking port availability..."

    local ports=(7474 7687 8080 8443 6443)
    local conflicts=()

    for port in "${ports[@]}"; do
        if lsof -i ":$port" >/dev/null 2>&1; then
            conflicts+=("$port")
        fi
    done

    if [[ ${#conflicts[@]} -gt 0 ]]; then
        log_warning "Port conflicts detected: ${conflicts[*]}"
        log_info "These ports are used by the Kind cluster configuration."
        log_info "Please ensure these ports are available before creating the cluster."
        return 1
    fi

    log_info "All required ports are available"
    return 0
}

main() {
    log_info "Starting Kind cluster setup..."

    # Check prerequisites
    if ! check_docker_permissions; then
        exit 1
    fi

    if ! check_kind_installation; then
        exit 1
    fi

    # Setup directories
    setup_kind_directories

    # Check ports (warning only)
    check_port_availability || log_warning "Port check failed, but continuing..."

    log_info "Kind cluster setup completed successfully!"
    log_info "You can now create a cluster with: make dev-cluster"
}

main "$@"
