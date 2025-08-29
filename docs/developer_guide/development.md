# Development Guide

This guide explains how to set up your development environment and get started with contributing to the Neo4j Enterprise Operator.

## Prerequisites

> **⚠️ CRITICAL: Kind (Kubernetes in Docker) is Mandatory**
> This project **exclusively uses Kind** for all development and testing workflows. You cannot develop, test, or contribute to this project without Kind installed. We do **not** support minikube, k3s, or other local Kubernetes solutions.

### Required Tools

- **Go**: Version 1.22+ (for development and testing)
- **Docker**: Container runtime for building images
- **kubectl**: Kubernetes CLI tool
- **Kind**: **MANDATORY** - Kubernetes in Docker for local clusters v0.20+ (see installation below)
- **make**: Build automation (GNU Make)

### Kind Installation (Required)

Kind is essential for all development workflows. Install it for your platform:

#### macOS (Recommended: Homebrew)
```bash
# Install Kind via Homebrew
brew install kind

# Verify installation
kind version
# Expected output: kind v0.x.x go1.x.x ...
```

#### Linux (Binary Installation)
```bash
# Download and install Kind binary
# For AMD64 / x86_64
[ $(uname -m) = x86_64 ] && curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64

# For ARM64
[ $(uname -m) = aarch64 ] && curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-arm64

# Make executable and move to PATH
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind

# Verify installation
kind version
```

#### Alternative: Install via Go
```bash
# If you have Go installed
go install sigs.k8s.io/kind@latest

# Verify installation
kind version
```

### Verify Kind Installation

Test that Kind works correctly:

```bash
# Create a test cluster
kind create cluster --name test-installation

# Check cluster status
kubectl cluster-info --context kind-test-installation

# Clean up test cluster
kind delete cluster --name test-installation
```

### Why Kind is Required

- **Development Clusters**: All `make dev-*` targets use Kind
- **Testing Infrastructure**: Integration tests (`make test-integration`) require Kind clusters
- **CI Emulation**: The `make test-ci-local` target creates Kind clusters with CI-appropriate constraints
- **Consistent Environment**: Ensures all developers use identical Kubernetes environments
- **Resource Efficiency**: Kind clusters are lightweight and fast to create/destroy

### Recommended Tools
- **VS Code** with Go extension
- **golangci-lint**: For code linting (installed via make targets)
- **git**: Version control
- **curl**: For API testing and examples

## Quick Start

### 1. Repository Setup

```bash
# Fork and clone the repository
git clone https://github.com/<your-username>/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Add upstream remote for pulling updates
git remote add upstream https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
```

### 2. Generate Code and Manifests

```bash
# Generate CRDs, RBAC manifests, and Go DeepCopy methods
make manifests generate
```

This command generates:
- **CRDs**: Custom Resource Definitions in `config/crd/bases/`
- **RBAC**: Role-based access control manifests in `config/rbac/`
- **DeepCopy Methods**: Go code generation for CRD types

### 3. Development Cluster Setup

The operator uses **Kind clusters** exclusively for development and testing:

```bash
# Create development cluster (neo4j-operator-dev)
make dev-cluster
```

This creates a Kind cluster with:
- **Cert-Manager**: v1.18.2 with `ca-cluster-issuer`
- **Development-Optimized**: Fast cluster creation for development
- **Neo4j CRDs**: Automatically installed

### 4. Local Development with Built Images

**RECOMMENDED**: Use local image deployment for development to avoid registry dependencies:

```bash
# Build and deploy operator with local image (RECOMMENDED)
make deploy-dev-local   # Uses neo4j-operator:dev image
# or
make deploy-prod-local  # Uses neo4j-operator:latest image with prod settings
```

**Alternative**: Use pre-built images (requires image availability):
```bash
# Deploy using overlay configurations
make deploy-dev   # Uses neo4j-operator:dev (must be built locally first)
make deploy-prod  # Uses ghcr.io registry image (requires authentication)
```

**⚠️ CRITICAL:** The operator must run in-cluster to avoid DNS resolution issues and ensure proper Neo4j cluster formation.

**Benefits of local image development**:
- **Complete Control**: Use exact code you're working on
- **No Registry Dependencies**: No need for ghcr.io access or authentication
- **Rapid Testing**: Build and deploy changes quickly
- **Consistent Environment**: Same image used for development and testing

**Development logging**:
```bash
# Enable debug logging on running operator
kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--leader-elect","--health-probe-bind-address=:8081","--zap-log-level=debug"]}]}}}}'
```

## Development Workflow

### Standard Development Cycle

1. **Start with clean environment**:
   ```bash
   make dev-cluster          # Create fresh cluster
   make deploy-dev-local     # Build and deploy operator with local image
   ```

2. **Test your changes**:
   ```bash
   # Deploy test resources
   kubectl apply -f examples/clusters/minimal-cluster.yaml

   # Monitor operator logs
   kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager
   # Check cluster status
   kubectl get neo4jenterprisecluster
   kubectl describe neo4jenterprisecluster minimal-cluster
   ```

3. **Iterate quickly**:
   - Make code changes
   - Rebuild and redeploy: `make docker-build deploy-dev`
   - Test changes immediately

### Development Environment Management

#### Cluster Management
```bash
# Clean operator resources (keep cluster running)
make dev-cluster-clean

# Reset cluster (delete and recreate)
make dev-cluster-reset

# Delete cluster entirely
make dev-cluster-delete

# Complete cleanup (cluster + environment)
make dev-destroy
```

#### Quick Testing Commands
```bash
# Create minimal cluster for testing
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123
kubectl apply -f examples/clusters/minimal-cluster.yaml

# Monitor cluster formation
kubectl get pods -l app.kubernetes.io/name=neo4j

# Check cluster status
kubectl get neo4jenterprisecluster
```

## Project Structure

### Key Directories

```
neo4j-kubernetes-operator/
├── api/v1alpha1/                    # CRD type definitions
│   ├── neo4jenterprisecluster_types.go
│   ├── neo4jenterprisestandalone_types.go
│   ├── neo4jdatabase_types.go
│   ├── neo4jplugin_types.go
│   ├── neo4jbackup_types.go
│   └── neo4jrestore_types.go
├── internal/controller/             # Controller implementations
│   ├── neo4jenterprisecluster_controller.go
│   ├── neo4jenterprisestandalone_controller.go
│   ├── neo4jdatabase_controller.go
│   ├── plugin_controller.go
│   ├── neo4jbackup_controller.go
│   └── neo4jrestore_controller.go
├── internal/resources/              # Resource builders
├── internal/validation/             # Validation framework
├── internal/monitoring/             # Resource monitoring
├── config/                         # Kubernetes manifests
│   ├── crd/bases/                  # Generated CRDs
│   ├── rbac/                       # RBAC manifests
│   └── manager/                    # Operator deployment
├── examples/                       # Example configurations
├── test/integration/               # Integration tests
└── docs/                          # Documentation
```

### Current Architecture (August 2025)

The operator implements a **server-based architecture**:

- **Neo4jEnterpriseCluster**: Creates `{cluster-name}-server` StatefulSet
- **Neo4jEnterpriseStandalone**: Creates `{standalone-name}` StatefulSet
- **Centralized Backup**: Single `{cluster-name}-backup-0` per cluster
- **Self-Organizing Servers**: Neo4j servers automatically assign database roles

## Testing During Development

### Quick Testing
```bash
# Unit tests (no cluster required)
make test-unit

# Run specific controller tests
go test ./internal/controller -run TestClusterReconciler -v

# Run validation tests
go test ./internal/validation -v
```

### Integration Testing
```bash
# Full integration test suite (automatically creates cluster and deploys operator)
make test-integration

# Alternative: step-by-step approach for debugging
make test-cluster         # Create test cluster only
make test-integration     # Run tests with existing cluster
make test-cluster-delete  # Clean up test cluster

# Run specific integration tests
ginkgo run -focus "should create backup" ./test/integration
```

### CI Workflow Emulation (Added 2025-08-22)

When debugging CI failures or testing resource-constrained environments, use the CI workflow emulation target:

```bash
# Emulate complete CI workflow locally with debug logging
make test-ci-local
```

**What it does:**
1. **Environment Setup**: Sets `CI=true GITHUB_ACTIONS=true` environment variables
2. **Unit Tests**: Runs unit tests with CI constraints and logging
3. **Integration Tests**: Creates test cluster with 512Mi memory limits (same as CI)
4. **Debug Logging**: Saves comprehensive logs for troubleshooting
5. **Cleanup**: Complete environment cleanup

**Generated Debug Files:**
- `logs/ci-local-unit.log` - Unit test output with environment info
- `logs/ci-local-integration.log` - Integration test output with cluster setup
- `logs/ci-local-cleanup.log` - Environment cleanup output

**Key Benefits:**
- **Identical CI Environment**: Same memory constraints (512Mi vs 1.5Gi local)
- **Resource Constraint Testing**: Tests memory limits that cause CI failures
- **Debug Information**: Comprehensive logging for troubleshooting
- **Complete Workflow**: Unit → Integration → Cleanup (like CI)

**Usage Scenarios:**
- Debugging CI failures locally
- Testing memory-constrained environments
- Validating resource requirements
- Troubleshooting integration test failures

**Troubleshooting Commands (automatically provided on failure):**
```bash
# Check operator logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager

# Check pod status
kubectl get pods --all-namespaces

# Check events
kubectl get events --all-namespaces --sort-by='.lastTimestamp'

# Review specific debug logs
cat logs/ci-local-integration.log
tail -f logs/ci-local-integration.log  # Follow real-time
```

### Manual Testing Examples

#### Test Cluster Deployment
```bash
# Create admin secret
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

# Deploy cluster
kubectl apply -f examples/clusters/minimal-cluster.yaml

# Verify cluster formation
kubectl get neo4jenterprisecluster
kubectl get pods -l app.kubernetes.io/name=neo4j

# Check Neo4j cluster status
kubectl exec minimal-cluster-server-0 -- \
  cypher-shell -u neo4j -p admin123 "SHOW SERVERS"
```

#### Test Standalone Deployment
```bash
# Deploy standalone instance
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Verify deployment
kubectl get neo4jenterprisestandalone
kubectl get pods -l app=standalone

# Test database creation
kubectl apply -f examples/database/database-standalone.yaml
```

#### Test Plugin Installation
```bash
# Install APOC plugin on cluster
kubectl apply -f examples/plugins/cluster-plugin-example.yaml

# Monitor plugin installation
kubectl get neo4jplugin
kubectl describe neo4jplugin cluster-apoc-plugin
```

## Code Generation

### When to Regenerate Code

Run code generation after:
- Adding/modifying CRD fields
- Changing CRD validation tags
- Adding new CRDs
- Updating RBAC permissions

```bash
# Full regeneration
make manifests generate

# Format and vet code
make fmt vet
```

### Generated Files
- `api/v1alpha1/zz_generated.deepcopy.go`: DeepCopy methods for CRDs
- `config/crd/bases/*.yaml`: Kubernetes CRD manifests
- `config/rbac/*.yaml`: RBAC resources

## Debugging

### Local Debugging with VS Code

1. **Set up launch configuration** (`.vscode/launch.json`):
   ```json
   {
     "version": "0.2.0",
     "configurations": [
       {
         "name": "Launch Operator",
         "type": "go",
         "request": "launch",
         "mode": "debug",
         "program": "${workspaceFolder}/cmd/main.go",
         "args": ["--zap-log-level=debug"],
         "env": {
           "KUBECONFIG": "${env:HOME}/.kube/config"
         }
       }
     ]
   }
   ```

2. **Set breakpoints** in controller code
3. **Start debugging** (F5 in VS Code)

### Troubleshooting Common Issues

#### Cluster Formation Problems
```bash
# Check operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Examine cluster events
kubectl describe neo4jenterprisecluster <cluster-name>

# Check pod status and logs
kubectl get pods -l app.kubernetes.io/name=neo4j
kubectl logs <pod-name> -c neo4j
```

#### Resource Version Conflicts
If you see resource version conflicts during development:
```bash
# Reset development cluster
make dev-cluster-reset

# Clean up stuck resources
kubectl patch neo4jenterprisecluster <name> -p '{"metadata":{"finalizers":[]}}' --type=merge
```

#### Test Failures
```bash
# Clean test environment
make test-cleanup

# Reset test cluster
make test-cluster-reset

# Run specific failing test
go test ./internal/controller -run TestSpecificFunction -v
```

## Performance Development

### Resource Monitoring

The operator includes built-in resource monitoring:

```bash
# Check resource recommendations
kubectl logs -l app.kubernetes.io/name=neo4j-operator | grep -i "resource"

# Monitor reconciliation efficiency
kubectl logs -l app.kubernetes.io/name=neo4j-operator | grep -i "reconcile"
```

### Development Performance Tips

1. **Use in-cluster development**: Required for proper DNS resolution
2. **Limit cluster size**: Use minimal examples during development
3. **Clean up regularly**: Use `make dev-cleanup` to avoid resource buildup
4. **Monitor memory usage**: Watch for memory leaks during long development sessions

## Advanced Development

### Custom Resource Development

When adding new CRDs:

1. **Define types** in `api/v1alpha1/`
2. **Create controller** in `internal/controller/`
3. **Add validation** in `internal/validation/`
4. **Update RBAC** in `config/rbac/`
5. **Generate code** with `make manifests generate`
6. **Add tests** in appropriate test directories

### Controller Development Best Practices

1. **Use builder pattern** for resource creation (see `internal/resources/`)
2. **Implement proper status updates** with conditions
3. **Add comprehensive validation** for user inputs
4. **Include resource version conflict retry** logic
5. **Implement proper finalizer handling** for cleanup
6. **Add observability** through events and logs

### Testing New Features

1. **Start with unit tests**: Test individual functions
2. **Add integration tests**: Test full workflows
3. **Test error scenarios**: Handle edge cases and failures
4. **Performance test**: Ensure reconciliation efficiency
5. **Manual testing**: Verify end-to-end functionality

## Environment Variables

### Development Environment Variables

```bash
# Development cluster configuration
export KUBECONFIG=~/.kube/config

# Operator debugging
export ZAP_LOG_LEVEL=debug

# Development mode settings
export DEVELOPMENT_MODE=true
export OPERATOR_NAMESPACE=neo4j-operator
```

### Testing Environment Variables

```bash
# CI/CD testing
export CI=true
export KUBEBUILDER_ASSETS="$(pwd)/bin/k8s/1.31.0-linux-amd64"
```

## Next Steps

- Review the [Testing Guide](testing.md) for comprehensive testing strategies
- Check the [Contributing Guide](contributing.md) for code contribution guidelines
- Explore the [Architecture Guide](architecture.md) for system design details

## Getting Help

- **Issues**: Create GitHub issues for bugs or feature requests
- **Discussions**: Use GitHub Discussions for questions
- **Code Review**: Follow pull request process for code changes

The development environment is designed for rapid iteration and comprehensive testing. Use the provided tools and workflows to contribute effectively to the Neo4j Enterprise Operator.
