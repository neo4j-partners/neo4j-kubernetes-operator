# Conditional Test Execution

This document explains how the Neo4j Kubernetes Operator test suite has been enhanced to run conditionally based on cluster availability.

## Overview

The test suite now intelligently detects whether a Kubernetes cluster is available and runs tests accordingly:

- **Tests that don't require a cluster** run regardless of cluster availability
- **Tests that require a cluster** only run when a cluster is available and ready
- **Clear feedback** is provided about which tests are running and why

## Test Categories

### 🟢 Tests That Don't Require a Cluster

These tests can run in any environment and don't need a Kubernetes cluster:

- **Unit Tests**: Controller logic, webhook validation, utility functions
- **Webhook Tests**: API validation and admission control
- **Security Tests**: Security coordinator and security-related functionality
- **Neo4j Client Tests**: Client library functionality (uses mocks)
- **Controller Tests**: Controller logic with fake clients

**Makefile Targets:**
```bash
make test-no-cluster          # Run all tests that don't require a cluster
make test-unit                # Run unit tests only
make test-webhooks            # Run webhook tests only
make test-security            # Run security tests only
make test-neo4j-client        # Run Neo4j client tests only
make test-controllers         # Run controller tests only
```

### 🟡 Tests That Require a Cluster

These tests need a real Kubernetes cluster to run:

- **Integration Tests**: End-to-end cluster lifecycle, multi-cluster scenarios
- **E2E Tests**: Full end-to-end workflows
- **Backup/Restore Tests**: Actual backup and restore operations
- **Cloud Provider Tests**: EKS, GKE, AKS specific functionality

**Makefile Targets:**
```bash
make test-with-cluster        # Run all tests that require a cluster
make test-integration         # Run integration tests
make test-e2e                 # Run e2e tests
make test-backup-restore      # Run backup/restore tests
make test-cloud               # Run cloud provider tests
```

## Cluster Detection

The system uses `scripts/check-cluster.sh` to detect cluster availability:

### Supported Cluster Types

1. **Kind Clusters**: Local development clusters
2. **OpenShift Clusters**: Enterprise Kubernetes platforms
3. **Remote Clusters**: Any Kubernetes cluster accessible via kubectl

### Detection Logic

```bash
# Check any available cluster
./scripts/check-cluster.sh --verbose

# Check specific cluster type
./scripts/check-cluster.sh --type kind --verbose
./scripts/check-cluster.sh --type openshift --verbose
./scripts/check-cluster.sh --type remote --verbose
```

### Health Checks

The script performs comprehensive health checks:

- **API Server Connectivity**: `kubectl cluster-info`
- **Node Readiness**: `kubectl wait --for=condition=ready nodes --all`
- **API Server Health**: `kubectl get --raw /healthz`
- **Component Status**: Core system components verification

## Usage Examples

### Local Development

```bash
# Run only tests that don't need a cluster (fast feedback)
make test-no-cluster

# Create a cluster and run all tests
make dev-cluster
make test-all

# Run specific test categories
make test-unit
make test-integration  # Will skip if no cluster
```

### CI/CD Pipelines

The GitHub workflows automatically use conditional execution:

```yaml
# Always runs (no cluster required)
- name: Run tests that don't require a cluster
  run: make test-no-cluster

# Conditionally runs (cluster required)
- name: Check cluster availability
  run: |
    if ./scripts/check-cluster.sh --verbose; then
      make test-all
    else
      echo "No cluster available, skipping cluster-dependent tests"
    fi
```

### Cloud Provider Testing

```bash
# Set cluster type and run cloud-specific tests
export CLUSTER_TYPE=eks
make test-eks  # Will check for EKS cluster availability

export CLUSTER_TYPE=gke
make test-gke  # Will check for GKE cluster availability

export CLUSTER_TYPE=aks
make test-aks  # Will check for AKS cluster availability
```

## Makefile Targets Reference

### Core Test Targets

| Target | Description | Cluster Required |
|--------|-------------|------------------|
| `test-no-cluster` | All tests that don't require a cluster | ❌ No |
| `test-with-cluster` | All tests that require a cluster | ✅ Yes |
| `test-all` | All tests (conditional on cluster) | 🔄 Conditional |
| `test-comprehensive` | Comprehensive test suite | 🔄 Conditional |

### Unit Test Targets

| Target | Description | Cluster Required |
|--------|-------------|------------------|
| `test-unit` | Unit tests with envtest | ❌ No |
| `test-unit-only` | Pure unit tests only | ❌ No |
| `test-webhooks` | Webhook tests | ❌ No |
| `test-security` | Security-focused tests | ❌ No |
| `test-neo4j-client` | Neo4j client tests | ❌ No |
| `test-controllers` | Controller logic tests | ❌ No |

### Integration Test Targets

| Target | Description | Cluster Required |
|--------|-------------|------------------|
| `test-integration` | Integration tests | ✅ Yes |
| `test-e2e` | End-to-end tests | ✅ Yes |
| `test-backup-restore` | Backup/restore tests | ✅ Yes |
| `test-local` | Local cluster tests | ✅ Yes |

### Cloud Test Targets

| Target | Description | Cluster Required |
|--------|-------------|------------------|
| `test-cloud` | All cloud provider tests | ✅ Yes |
| `test-eks` | EKS-specific tests | ✅ Yes |
| `test-gke` | GKE-specific tests | ✅ Yes |
| `test-aks` | AKS-specific tests | ✅ Yes |

## Error Handling

### Graceful Degradation

When no cluster is available:

```bash
$ make test-integration
Checking cluster availability for integration tests...
❌ No cluster available for integration tests
💡 Run 'make dev-cluster' to create a local cluster
💡 Or set up a remote cluster and configure kubectl
💡 Skipping integration tests...
```

### Clear Feedback

The system provides clear feedback about:

- **What tests are running** and why
- **What tests are being skipped** and why
- **How to set up a cluster** if needed
- **Exit codes** that indicate success/failure

## Benefits

### 🚀 Faster Development

- **Quick feedback**: Unit tests run immediately without cluster setup
- **Parallel development**: Multiple developers can run tests without cluster conflicts
- **CI efficiency**: Faster CI runs when cluster setup fails

### 🔧 Better Developer Experience

- **Clear messaging**: Understand exactly what's happening
- **Helpful guidance**: Instructions for setting up clusters
- **Flexible execution**: Run tests based on available resources

### 🛡️ Improved Reliability

- **Fail-fast**: Detect cluster issues early
- **Graceful degradation**: Continue testing when possible
- **Comprehensive coverage**: Ensure all testable code is tested

## Troubleshooting

### Common Issues

1. **"No cluster detected"**
   - Run `make dev-cluster` to create a local cluster
   - Or configure kubectl for a remote cluster

2. **"Cluster is not ready"**
   - Wait for cluster initialization to complete
   - Check cluster health with `make cluster-health`

3. **"API server not accessible"**
   - Verify kubectl configuration
   - Check network connectivity to cluster

### Debug Commands

```bash
# Check cluster status
./scripts/check-cluster.sh --verbose

# Get detailed cluster information
make cluster-info

# Check cluster health
make cluster-health

# Validate cluster connectivity
make validate-cluster
```

### Environment Variables

```bash
# For cloud provider tests
export CLUSTER_TYPE=eks|gke|aks

# For OpenShift tests
export OPENSHIFT_SERVER=https://api.example.com:6443
export OPENSHIFT_TOKEN=sha256~...
```

## Future Enhancements

1. **Multi-cluster Support**: Run tests against multiple clusters
2. **Cluster Provisioning**: Automatic cluster creation for testing
3. **Test Parallelization**: Run tests in parallel across multiple clusters
4. **Performance Metrics**: Track test execution time and resource usage
5. **Test Categorization**: More granular test categorization and execution
