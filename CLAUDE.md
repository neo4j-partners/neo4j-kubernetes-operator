# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the Neo4j Enterprise Operator for Kubernetes, which manages Neo4j Enterprise clusters (v5.26+) in Kubernetes environments. Built with Kubebuilder framework. The operator supports only Neo4j database 5.26+ - Semver releases from 5.26.0 and up, Calver releases 2025.01.0 and up, Only Discovery v2 should be supported in 5.26.0 and other supported semver releases. All tests, samples, documents should enforce this.

## Architecture

**Key Components:**
- **CRDs**: Neo4jEnterpriseCluster, Neo4jBackup/Restore, Neo4jDatabase, Neo4jPlugin
- **Controllers**: Enterprise cluster controller with autoscaling
- **Webhooks**: Validation webhooks integrated with cert-manager

**Directory Structure:**
- `api/v1alpha1/` - CRD type definitions
- `internal/controller/` - Reconciliation logic
- `internal/resources/` - K8s resource builders
- `internal/neo4j/` - Neo4j client implementation
- `test/` - Unit, integration, and e2e tests

## Essential Commands

### Build & Development
```bash
make build                 # Build operator binary
make docker-build         # Build container image
make manifests            # Generate CRDs and RBAC
make generate             # Generate DeepCopy methods
make dev-run              # Run operator locally (outside cluster)

# Development cluster management
make dev-cluster          # Create Kind development cluster (neo4j-operator-dev)
make dev-cluster-clean    # Clean operator resources from dev cluster
make dev-cluster-reset    # Delete and recreate dev cluster
make dev-cluster-delete   # Delete dev cluster
make dev-cleanup          # Clean dev environment (keep cluster)
make dev-destroy          # Completely destroy dev environment

make operator-setup       # Deploy operator to test cluster
```

### Quick Testing with Examples
```bash
# Deploy a single-node cluster for testing
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123
kubectl apply -f examples/clusters/single-node.yaml

# Check cluster status
kubectl get neo4jenterprisecluster
kubectl get pods

# Access Neo4j Browser
kubectl port-forward svc/single-node-cluster-client 7474:7474 &
open http://localhost:7474
```

### Testing
```bash
# Quick tests (no cluster required)
make test-unit            # Unit tests only
make test-webhooks        # Webhook validation tests with envtest

# Test cluster management
make test-cluster         # Create test cluster (neo4j-operator-test)
make test-cluster-clean   # Clean operator resources from test cluster
make test-cluster-reset   # Delete and recreate test cluster
make test-cluster-delete  # Delete test cluster

# Cluster-based tests
make test-integration     # Integration tests (requires test cluster)
make test-e2e            # End-to-end tests (requires test cluster)

# Full test suite
make test                 # Run unit + integration tests
make test-coverage       # Generate coverage report

# Environment cleanup
make test-cleanup        # Clean test artifacts (keep cluster)
make test-destroy        # Completely destroy test environment

# Run specific test
go test ./internal/controller -run TestClusterReconciler
ginkgo run -focus "should create backup" ./test/integration
```

### Code Quality
```bash
make fmt                  # Format code with gofmt
make lint                 # Run golangci-lint (strict mode)
make lint-lenient        # Run with relaxed rules for CI
make vet                  # Run go vet
make security            # Run gosec security scan
```

### Debugging & Troubleshooting
```bash
# View operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Check webhook certificates
kubectl get certificate -n neo4j-operator

# Validate CRDs
kubectl explain neo4jenterprisecluster.spec

# Test webhook locally
make webhook-test
```

## Testing Strategy

The project uses Ginkgo/Gomega for BDD-style testing:

1. **Unit Tests** (`test/unit/`) - Test individual components without K8s
2. **Webhook Tests** (`test/webhooks/`) - Validate admission webhooks
3. **Integration Tests** (`test/integration/`) - Test with real K8s cluster
4. **E2E Tests** (`test/e2e/`) - Full deployment scenarios

Always run `make test-unit` before committing. Integration tests require a cluster with cert-manager.

## Important Development Notes

1. **Kind Only**: This project uses Kind clusters exclusively. No other cluster types (minikube, k3s, etc.) are supported.
2. **Webhook Development**: Webhooks require cert-manager. Use `make operator-setup` for local testing.
3. **Controller Testing**: Use envtest for controller tests without real cluster.
4. **Neo4j Client**: The operator communicates with Neo4j via Bolt protocol (internal/neo4j/client.go).

## Common Development Tasks

### Adding a New Controller
1. Create types in `api/v1alpha1/`
2. Run `make generate manifests`
3. Implement controller in `internal/controller/`
4. Add tests in `test/unit/controllers/`
5. Update RBAC in `config/rbac/role.yaml`

### Environment Separation

The project uses two separate Kind clusters:

- **Development Cluster** (`neo4j-operator-dev`): For local development and manual testing
  - Created with `make dev-cluster`
  - Uses `hack/kind-config.yaml` with development optimizations
  - Includes cert-manager v1.18.2 with self-signed ClusterIssuer (`ca-cluster-issuer`)
  - Ready for TLS-enabled Neo4j deployments

- **Test Cluster** (`neo4j-operator-test`): For automated testing only
  - Created with `make test-cluster`
  - Includes cert-manager v1.18.2 with self-signed ClusterIssuer (`ca-cluster-issuer`)
  - Minimal configuration for fast test execution
  - Automatically managed by test scripts

### Cleanup Strategy

The project provides granular cleanup options for both environments:

**Operator Resource Cleanup** (keeps cluster running):
- `make dev-cluster-clean` - Remove operator resources from dev cluster
- `make test-cluster-clean` - Remove operator resources from test cluster

**Environment Reset** (recreate cluster):
- `make dev-cluster-reset` - Delete and recreate dev cluster
- `make test-cluster-reset` - Delete and recreate test cluster

**Complete Destruction**:
- `make dev-destroy` - Destroy entire dev environment
- `make test-destroy` - Destroy entire test environment

**Artifact Cleanup** (files only):
- `make dev-cleanup` - Clean dev files (keep cluster)
- `make test-cleanup` - Clean test files (keep cluster)

### Debugging Failed Reconciliation
```bash
# Check controller logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f

# Check events
kubectl describe neo4jenterprisecluster <name>

# Enable debug logging
make dev-run ARGS="--zap-log-level=debug"
```

## CI/CD Workflow

GitHub Actions runs:
1. Fast feedback tests (unit, lint, security)
2. Integration tests with webhooks
3. E2E tests with full cluster deployment
4. Multi-arch container builds

PRs must pass all checks. Use conventional commits (feat:, fix:, docs:).

## Version Support

- **Supported Neo4j Versions**:
  - The operator supports only Neo4j database 5.26+
    - Semver releases from 5.26.0 and up
    - Calver releases 2025.01.0 and up
  - Only Discovery v2 should be supported in 5.26.0 and other supported semver releases.
  - All tests, samples, documents should enforce this

### Unified Configuration Approach

**Important**: The operator uses a unified clustering approach for all deployment sizes:

- **No Special Single-Node Mode**: All deployments use clustering infrastructure, even single-primary clusters
- **RAFT Enabled**: All deployments use `internal.dbms.single_raft_enabled=true` for seamless scaling
- **Scalable by Design**: Single-primary clusters can be scaled to multi-node without data migration
- **No `dbms.mode=SINGLE`**: The deprecated single-node mode is not used

### Version-Specific Configuration

**Critical**: Neo4j Kubernetes discovery parameters differ between versions:

- **Neo4j 5.x (semver releases)**:
  - Use `dbms.kubernetes.service_port_name=discovery`
  - Use `dbms.kubernetes.discovery.v2.service_port_name=discovery`
  - **MANDATORY**: Set `dbms.cluster.discovery.version=V2_ONLY` for 5.26+
- **Neo4j 2025.x+ (calver releases)**: Use `dbms.kubernetes.discovery.service_port_name`

The operator automatically detects the Neo4j version from the image tag and applies the correct parameters. This is implemented in `internal/resources/cluster.go` via the `getKubernetesDiscoveryParameter()` function.

### Automatic Scaling Transitions

The operator supports automatic scaling from single-primary to multi-node clusters:

- **Detection**: Controller detects topology changes from 1 primary to multiple primaries
- **Restart Logic**: Automatically restarts existing pods with multi-node configuration
- **Configuration Update**: Applies proper Kubernetes discovery settings for cluster formation
- **Implementation**: See `neo4jenterprisecluster_controller.go` functions:
  - `detectSingleNodeToMultiNodeScaling()`
  - `handleSingleNodeToMultiNodeScaling()`

## Reports

All reports that Claude generates should go into the reports directory. The reports can be reviewed by Claude to determine changes that were made.

## Important Considerations
  - Compliance requirements @docs/reports/neo4j-operator-comprehensive-audit-report.md
