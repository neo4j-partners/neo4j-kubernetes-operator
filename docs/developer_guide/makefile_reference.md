# Makefile Reference Guide

This comprehensive reference covers all Make targets available in the Neo4j Kubernetes Operator project, organized by purpose and workflow.

## Table of Contents

- [Quick Start](#quick-start)
- [Target Categories](#target-categories)
- [General Targets](#general-targets)
- [Code Generation](#code-generation)
- [Testing](#testing)
- [Build & Images](#build--images)
- [Deployment](#deployment)
- [Development Environment](#development-environment)
- [Dependencies](#dependencies)
- [Code Quality](#code-quality)
- [Environment Variables](#environment-variables)
- [Workflows](#workflows)

## Quick Start

**Essential commands for getting started:**

```bash
# Get help with all available targets
make help

# Create development environment
make dev-cluster          # Create Kind cluster
make operator-setup       # Deploy operator

# Run tests
make test-unit            # Fast unit tests (no cluster)
make test-integration     # Full integration tests (auto-creates cluster)

# Build and deploy
make build                # Build operator binary
make docker-build         # Build container image
make deploy-dev           # Deploy to development namespace
```

## Target Categories

The Makefile is organized into logical categories:

| Category | Purpose | Key Targets |
|----------|---------|-------------|
| **General** | Help and basic tasks | `help`, `all` |
| **Code Generation** | Generate Kubernetes manifests | `manifests`, `generate` |
| **Testing** | Unit and integration testing | `test-unit`, `test-integration` |
| **Build & Images** | Build binaries and containers | `build`, `docker-build` |
| **Deployment** | Install and deploy operator | `install`, `deploy-dev`, `deploy-prod` |
| **Development Environment** | Local development setup | `dev-cluster`, `operator-setup` |
| **Dependencies** | Download and manage tools | `kustomize`, `controller-gen` |
| **Code Quality** | Linting, formatting, security | `fmt`, `lint`, `security` |

## General Targets

### `make help`
**Description**: Display comprehensive help with all available targets
**Usage**: `make help`
**Dependencies**: None
**Example**:
```bash
make help
```

### `make all`
**Description**: Default target - builds the operator binary
**Usage**: `make all` or just `make`
**Dependencies**: `build`
**Example**:
```bash
make         # Same as 'make all'
make all     # Explicit call
```

## Code Generation

### `make manifests`
**Description**: Generate ClusterRole and CustomResourceDefinition objects from code annotations
**Usage**: `make manifests`
**Dependencies**: `controller-gen`
**Output**: Updates files in `config/crd/bases/` and RBAC manifests
**Example**:
```bash
make manifests
# Generates CRDs from api/v1alpha1/*_types.go files
# Updates RBAC from controller annotations
```

### `make generate`
**Description**: Generate DeepCopy, DeepCopyInto, and DeepCopyObject method implementations
**Usage**: `make generate`
**Dependencies**: `controller-gen`
**Output**: Updates `*_deepcopy.go` files
**Example**:
```bash
make generate
# Generates DeepCopy methods for all API types
```

### `make fmt`
**Description**: Format Go code using `go fmt`
**Usage**: `make fmt`
**Dependencies**: None
**Example**:
```bash
make fmt
# Formats all Go files in the project
```

### `make vet`
**Description**: Run `go vet` static analysis
**Usage**: `make vet`
**Dependencies**: None
**Example**:
```bash
make vet
# Reports suspicious constructs
```

### `make lint`
**Description**: Run golangci-lint with strict settings
**Usage**: `make lint`
**Dependencies**: `golangci-lint`
**Example**:
```bash
make lint
# Runs comprehensive linting checks
```

### `make lint-lenient`
**Description**: Run golangci-lint with relaxed settings (CI-friendly)
**Usage**: `make lint-lenient`
**Dependencies**: `golangci-lint`
**Example**:
```bash
make lint-lenient
# More permissive linting for CI environments
```

## Testing

> **Important**: All testing requires Kind (Kubernetes in Docker). Install Kind before running tests.

### Unit Testing

#### `make test-unit`
**Description**: Run fast unit tests without requiring a Kubernetes cluster
**Usage**: `make test-unit`
**Dependencies**: `manifests`, `generate`, `fmt`, `vet`, `envtest`
**Duration**: ~30 seconds
**Example**:
```bash
make test-unit
# ✅ Fast tests for controller logic
# ✅ No cluster setup required
# ✅ Includes coverage reporting
```

#### `make test-coverage`
**Description**: Generate detailed coverage report
**Usage**: `make test-coverage`
**Dependencies**: Test environment
**Output**: `coverage/coverage.html`
**Example**:
```bash
make test-coverage
# Generates HTML coverage report
# Opens coverage/coverage.html in browser
```

### Integration Testing

#### `make test-integration`
**Description**: Run comprehensive integration tests with real Kubernetes API
**Usage**: `make test-integration`
**Dependencies**: `test-cluster`, Kind cluster
**Duration**: ~10-15 minutes
**Features**:
- Auto-creates test cluster if needed
- Deploys operator automatically
- Tests real Neo4j deployments
- Includes plugin testing
- Cleanup handled automatically

**Example**:
```bash
make test-integration
# 🔄 Creates neo4j-operator-test cluster
# 📦 Builds and deploys operator
# 🧪 Runs full test suite
# 🧹 Automatic cleanup
```

#### `make test-integration-ci`
**Description**: Run essential integration tests optimized for CI environments
**Usage**: `make test-integration-ci`
**Dependencies**: Existing test cluster and deployed operator
**Duration**: ~5-8 minutes
**Features**:
- Assumes cluster and operator already deployed
- Skips resource-intensive tests
- Focuses on core functionality
- Optimized for CI resource constraints

**Example**:
```bash
make test-integration-ci
# 🚀 CI-optimized test suite
# ⚡ Essential tests only
# 💾 Reduces resource usage
```

#### `make test-integration-ci-full`
**Description**: Run complete integration test suite in CI environment
**Usage**: `make test-integration-ci-full`
**Dependencies**: Existing test cluster and deployed operator
**Duration**: ~15-20 minutes
**⚠️ **Warning**: May cause resource exhaustion in CI

**Example**:
```bash
make test-integration-ci-full
# ⚠️  Full test suite - use with caution in CI
# 🔋 High resource consumption
```

### Test Environment Management

#### `make test-cluster`
**Description**: Create dedicated Kind cluster for testing
**Usage**: `make test-cluster`
**Dependencies**: Kind installed
**Cluster Name**: `neo4j-operator-test`
**Features**:
- Includes cert-manager v1.18.2
- Pre-configured with self-signed issuer
- Optimized for testing workloads

**Example**:
```bash
make test-cluster
# Creates neo4j-operator-test cluster
# Installs cert-manager
# Sets up TLS certificates
```

#### `make test-cluster-clean`
**Description**: Remove operator resources from test cluster (keep cluster running)
**Usage**: `make test-cluster-clean`
**Example**:
```bash
make test-cluster-clean
# Removes operator deployment
# Removes test namespaces
# Keeps cluster running
```

#### `make test-cluster-reset`
**Description**: Delete and recreate test cluster
**Usage**: `make test-cluster-reset`
**Dependencies**: `test-cluster-delete`, `test-cluster`
**Example**:
```bash
make test-cluster-reset
# Complete cluster refresh
# Preserves no state
```

#### `make test-cluster-delete`
**Description**: Delete test cluster completely
**Usage**: `make test-cluster-delete`
**Example**:
```bash
make test-cluster-delete
# Removes neo4j-operator-test cluster
```

#### `make test-destroy`
**Description**: Complete test environment cleanup
**Usage**: `make test-destroy`
**Example**:
```bash
make test-destroy
# Removes all test artifacts
# Deletes test cluster
# Cleans temporary files
```

### Comprehensive Testing

#### `make test`
**Description**: Run complete test suite (unit + integration)
**Usage**: `make test`
**Dependencies**: `test-unit`, `test-integration`
**Duration**: ~15-20 minutes
**Example**:
```bash
make test
# ✅ Unit tests
# ✅ Integration tests
# ✅ Full validation
```

#### `make test-ci-local` 🆕
**Description**: Emulate GitHub Actions CI workflow locally with comprehensive debug logging
**Usage**: `make test-ci-local`
**Duration**: ~20-25 minutes
**Features**:
- **Complete CI emulation**: Uses `CI=true GITHUB_ACTIONS=true` environment
- **Resource constraints**: Tests with 512Mi memory limits (same as CI)
- **Debug logging**: Comprehensive logs saved to `logs/` directory
- **Automatic troubleshooting**: Provides debugging commands on failure
- **Self-contained**: Creates, tests, and destroys environment

**Output Files**:
- `logs/ci-local-unit.log` - Unit test output with environment info
- `logs/ci-local-integration.log` - Integration test execution
- `logs/ci-local-cleanup.log` - Environment cleanup

**Example**:
```bash
make test-ci-local
# 🔄 Phase 1: Unit tests with CI environment
# 🔗 Phase 2: Integration tests with CI constraints
# 🧹 Phase 3: Complete cleanup
# 📁 Debug logs in logs/ directory
```

**When to use**:
- Debugging CI failures locally
- Testing resource-constrained scenarios
- Validating changes before CI push
- Reproducing CI-specific issues

## Build & Images

### `make build`
**Description**: Build the operator binary
**Usage**: `make build`
**Dependencies**: `manifests`, `generate`, `fmt`, `vet`
**Output**: `bin/manager`
**Example**:
```bash
make build
# Builds bin/manager executable
# Includes all code generation
```

### `make docker-build`
**Description**: Build Docker image with operator
**Usage**: `make docker-build [IMG=<image-name>]`
**Dependencies**: None
**Default Image**: `controller:latest`
**Example**:
```bash
make docker-build
# Builds controller:latest image

make docker-build IMG=my-operator:v1.0
# Builds custom image name
```

### `make docker-push`
**Description**: Push Docker image to registry
**Usage**: `make docker-push [IMG=<image-name>]`
**Dependencies**: `docker-build`
**Example**:
```bash
make docker-push IMG=ghcr.io/my-org/neo4j-operator:latest
# Pushes image to GitHub Container Registry
```

## Deployment

> **Critical**: The operator **must** run inside the Kubernetes cluster. Running outside the cluster causes DNS resolution failures.

### CRD Management

#### `make install`
**Description**: Install CustomResourceDefinitions into cluster
**Usage**: `make install`
**Dependencies**: `manifests`, `kustomize`
**Example**:
```bash
make install
# Installs all CRDs to current cluster context
```

#### `make uninstall`
**Description**: Remove CRDs from cluster
**Usage**: `make uninstall`
**Dependencies**: `manifests`, `kustomize`
**⚠️ **Warning**: This will delete all Neo4j instances
**Example**:
```bash
make uninstall
# Removes all CRDs and instances
```

### Operator Deployment

#### `make deploy-dev`
**Description**: Deploy operator with development configuration using local images
**Usage**: `make deploy-dev`
**Dependencies**: `deploy-dev-local`
**Namespace**: `neo4j-operator-dev`
**Image**: `neo4j-operator:dev` (built locally)
**Features**:
- Auto-loads image to Kind cluster
- Development-friendly settings
- Enhanced logging

**Example**:
```bash
make deploy-dev
# 🔨 Builds neo4j-operator:dev image
# 📦 Loads to Kind cluster
# 🚀 Deploys to neo4j-operator-dev namespace
```

#### `make deploy-prod`
**Description**: Deploy operator with production configuration using local images
**Usage**: `make deploy-prod`
**Dependencies**: `deploy-prod-local`
**Namespace**: `neo4j-operator-system`
**Image**: `neo4j-operator:latest` (built locally)
**Features**:
- Production-grade settings
- Resource limits applied
- Standard logging levels

**Example**:
```bash
make deploy-prod
# 🔨 Builds neo4j-operator:latest image
# 📦 Loads to Kind cluster
# 🚀 Deploys to neo4j-operator-system namespace
```

#### `make deploy-dev-registry`
**Description**: Deploy development configuration using registry image
**Usage**: `make deploy-dev-registry`
**Dependencies**: `manifests`, `kustomize`
**Image Source**: Container registry (requires authentication)
**Example**:
```bash
make deploy-dev-registry
# 📥 Pulls from ghcr.io registry
# 🚀 Deploys dev configuration
```

#### `make deploy-prod-registry`
**Description**: Deploy production configuration using registry image
**Usage**: `make deploy-prod-registry`
**Dependencies**: `manifests`, `kustomize`
**Image Source**: Container registry (requires authentication)
**Example**:
```bash
make deploy-prod-registry
# 📥 Pulls from ghcr.io registry
# 🚀 Deploys production configuration
```

### Deployment Removal

#### `make undeploy-dev`
**Description**: Remove development operator deployment
**Usage**: `make undeploy-dev`
**Example**:
```bash
make undeploy-dev
# Removes development operator
# Keeps CRDs and instances
```

#### `make undeploy-prod`
**Description**: Remove production operator deployment
**Usage**: `make undeploy-prod`
**Example**:
```bash
make undeploy-prod
# Removes production operator
# Keeps CRDs and instances
```

## Development Environment

> **Mandatory**: This project exclusively uses Kind for development. Install Kind before using development targets.

### Cluster Management

#### `make dev-cluster`
**Description**: Create Kind cluster for development
**Usage**: `make dev-cluster`
**Cluster Name**: `neo4j-operator-dev`
**Dependencies**: Kind installed
**Features**:
- Includes cert-manager v1.18.2
- Self-signed ClusterIssuer for TLS
- Development-optimized configuration

**Example**:
```bash
make dev-cluster
# Creates neo4j-operator-dev cluster
# Installs cert-manager
# Sets up development certificates
```

#### `make dev-cluster-clean`
**Description**: Remove operator resources from development cluster
**Usage**: `make dev-cluster-clean`
**Example**:
```bash
make dev-cluster-clean
# Removes operator deployment
# Removes CRDs and instances
# Keeps cluster running
```

#### `make dev-cluster-reset`
**Description**: Reset development cluster (delete and recreate)
**Usage**: `make dev-cluster-reset`
**Dependencies**: `dev-cluster-delete`, `dev-cluster`
**Example**:
```bash
make dev-cluster-reset
# Complete cluster refresh
# Loses all state and data
```

#### `make dev-cluster-delete`
**Description**: Delete development cluster
**Usage**: `make dev-cluster-delete`
**Example**:
```bash
make dev-cluster-delete
# Removes neo4j-operator-dev cluster
```

#### `make dev-destroy`
**Description**: Complete development environment cleanup
**Usage**: `make dev-destroy`
**Example**:
```bash
make dev-destroy
# Removes all development artifacts
# Deletes development cluster
```

### Operator Management

#### `make operator-setup`
**Description**: Automated operator deployment to available Kind cluster
**Usage**: `make operator-setup`
**Features**:
- **Auto-detection**: Finds available Kind cluster (dev or test)
- **Smart deployment**: Chooses appropriate configuration
- **Status verification**: Confirms successful deployment
- **Error handling**: Provides troubleshooting guidance

**Example**:
```bash
make operator-setup
# 🔍 Detects available Kind cluster
# 📦 Builds and loads appropriate image
# 🚀 Deploys operator
# ✅ Verifies deployment status
```

#### `make operator-setup-interactive`
**Description**: Interactive operator deployment with user prompts
**Usage**: `make operator-setup-interactive`
**Example**:
```bash
make operator-setup-interactive
# 💬 Interactive cluster selection
# 🎛️  Configuration options
# 📋 Detailed status reporting
```

#### `make operator-status`
**Description**: Display comprehensive operator status
**Usage**: `make operator-status`
**Example**:
```bash
make operator-status
# 📊 Operator deployment status
# 🔍 Pod health and logs
# 📈 Resource usage
```

#### `make operator-logs`
**Description**: Follow operator logs in real-time
**Usage**: `make operator-logs`
**Example**:
```bash
make operator-logs
# 📋 Real-time log streaming
# 🔍 Filtered for relevant events
```

### Demo Environment

#### `make demo-setup`
**Description**: Set up complete demo environment
**Usage**: `make demo-setup`
**Features**:
- Creates cluster if needed
- Deploys operator
- Prepares demo resources
**Example**:
```bash
make demo-setup
# 🎪 Complete demo environment ready
```

#### `make demo`
**Description**: Run interactive operator demo
**Usage**: `make demo`
**Dependencies**: `demo-setup`
**Example**:
```bash
make demo
# 🎪 Interactive demonstration
# 💬 Guided walkthrough
# 📚 Educational content
```

#### `make demo-fast`
**Description**: Run automated demo without confirmations
**Usage**: `make demo-fast`
**Dependencies**: `demo-setup`
**Example**:
```bash
make demo-fast
# 🚀 Fast automated demo
# ⚡ No user interaction required
```

## Dependencies

The Makefile automatically manages tool dependencies:

### Core Tools

#### `make kustomize`
**Description**: Download kustomize for Kubernetes manifest management
**Version**: v5.4.3
**Location**: `bin/kustomize`

#### `make controller-gen`
**Description**: Download controller-gen for code generation
**Version**: v0.16.1
**Location**: `bin/controller-gen`

#### `make envtest`
**Description**: Download setup-envtest for testing
**Version**: release-0.19
**Location**: `bin/setup-envtest`

#### `make golangci-lint`
**Description**: Download golangci-lint for code quality
**Version**: v1.64.8
**Location**: `bin/golangci-lint`

#### `make ginkgo`
**Description**: Download Ginkgo BDD testing framework
**Version**: v2.23.4
**Location**: `bin/ginkgo`

#### `make operator-sdk`
**Description**: Download Operator SDK for bundle management
**Version**: v1.39.1
**Location**: `bin/operator-sdk`

### Bundle Management

#### `make bundle`
**Description**: Generate operator bundle manifests
**Usage**: `make bundle [VERSION=<version>]`
**Dependencies**: `manifests`, `kustomize`, `operator-sdk`

#### `make bundle-build`
**Description**: Build bundle image
**Usage**: `make bundle-build`

#### `make bundle-push`
**Description**: Push bundle image to registry
**Usage**: `make bundle-push`

## Code Quality

### `make security`
**Description**: Run security analysis with gosec
**Usage**: `make security`
**Features**:
- Auto-installs gosec if needed
- Scans for security vulnerabilities
- Reports potential issues

**Example**:
```bash
make security
# 🛡️  Security vulnerability scan
# 📋 Detailed security report
```

### `make tidy`
**Description**: Clean up Go module dependencies
**Usage**: `make tidy`
**Example**:
```bash
make tidy
# 🧹 Removes unused dependencies
# ✅ Verifies module integrity
```

### `make clean`
**Description**: Clean all build artifacts and temporary files
**Usage**: `make clean`
**Removes**:
- `bin/` directory
- `tmp/` directory
- Coverage files
- Build logs
- Test artifacts

**Example**:
```bash
make clean
# 🧹 Complete cleanup
# 🗑️  Removes all build artifacts
```

## Environment Variables

### Image Configuration
- `IMG`: Container image name (default: `controller:latest`)
- `VERSION`: Project version (default: `0.0.1`)
- `CONTAINER_TOOL`: Container tool (default: `docker`)

### Tool Configuration
- `KUBECONFIG`: Kubernetes config file location
- `LOCALBIN`: Local tool installation directory (default: `bin/`)
- `GOBIN`: Go binary installation directory

### Testing Configuration
- `CI`: Enable CI mode (affects resource limits)
- `GITHUB_ACTIONS`: Enable GitHub Actions mode
- `ENVTEST_K8S_VERSION`: Kubernetes version for testing (1.31.0)

### Bundle Configuration
- `CHANNELS`: Bundle channels for OLM
- `DEFAULT_CHANNEL`: Default bundle channel
- `BUNDLE_IMG`: Bundle image name

**Examples**:
```bash
# Custom image build
make docker-build IMG=my-registry/neo4j-operator:v2.0

# CI-mode testing
CI=true make test-unit

# Custom tool location
LOCALBIN=/usr/local/bin make kustomize
```

## Workflows

### Complete Development Setup
```bash
# 1. Create development environment
make dev-cluster           # Create Kind cluster
make operator-setup        # Deploy operator

# 2. Develop and test
make test-unit            # Fast feedback loop
make build                # Build changes
make deploy-dev           # Update deployment

# 3. Comprehensive testing
make test-integration     # Full validation
make test-ci-local        # CI simulation
```

### Production Deployment Workflow
```bash
# 1. Validate code quality
make fmt vet lint         # Code formatting and analysis
make security             # Security scanning

# 2. Test comprehensively
make test                 # Unit + integration tests
make test-coverage        # Verify coverage

# 3. Build and deploy
make docker-build         # Build production image
make deploy-prod          # Production deployment
```

### CI/CD Emulation
```bash
# Reproduce CI failures locally
make test-ci-local        # Complete CI workflow
# Check logs/ci-local-*.log for detailed analysis

# Debug specific CI issues
CI=true GITHUB_ACTIONS=true make test-unit
```

### Testing Workflow
```bash
# Quick development testing
make test-unit            # ~30 seconds

# Comprehensive validation
make test-cluster         # Create test environment
make test-integration     # ~15 minutes
make test-cluster-clean   # Clean resources

# CI preparation
make test-ci-local        # Full CI emulation
```

### Cleanup Workflows
```bash
# Development cleanup
make dev-cluster-clean    # Remove operator only
make dev-destroy          # Complete destruction

# Test cleanup
make test-cluster-clean   # Remove test resources
make test-destroy         # Complete test cleanup

# Complete cleanup
make clean                # Remove build artifacts
```

## Common Patterns

### Error Recovery
```bash
# If cluster is in bad state
make dev-cluster-reset    # or test-cluster-reset
make operator-setup

# If operator is misbehaving
make undeploy-dev         # or undeploy-prod
make deploy-dev           # or deploy-prod
```

### Development Iteration
```bash
# Fast iteration cycle
make test-unit            # Quick validation
make build                # Build changes
make docker-build         # Update image
make deploy-dev           # Deploy changes

# Full validation cycle
make fmt vet              # Code quality
make test                 # Comprehensive testing
make deploy-prod          # Production deployment
```

### Troubleshooting
```bash
# Check operator status
make operator-status      # Deployment overview
make operator-logs        # Real-time logs

# Debug testing issues
make test-ci-local        # Reproduce CI environment
# Check logs/ci-local-*.log

# Clean slate approach
make dev-destroy          # Complete restart
make dev-cluster
make operator-setup
```

---

## Best Practices

1. **Always use Kind**: This project exclusively supports Kind for development
2. **Run tests before commits**: Use `make test-unit` for fast feedback
3. **Test integration changes**: Use `make test-integration` for comprehensive validation
4. **Emulate CI locally**: Use `make test-ci-local` before pushing to CI
5. **Keep dependencies current**: Tools are auto-managed and version-pinned
6. **Use appropriate deployment**: `deploy-dev` for development, `deploy-prod` for production testing
7. **Clean up regularly**: Use cleanup targets to prevent resource exhaustion
8. **Monitor operator logs**: Use `make operator-logs` for real-time debugging

For additional help, run `make help` or consult the [Contributing Guide](contributing.md).
