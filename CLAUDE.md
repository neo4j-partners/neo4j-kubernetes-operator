# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the Neo4j Enterprise Operator for Kubernetes, which manages Neo4j Enterprise deployments (v5.26+) in Kubernetes environments. Built with Kubebuilder framework. The operator supports only Neo4j database 5.26+ - Semver releases from 5.26.0 and up, Calver releases 2025.01.0 and up, Only Discovery v2 should be supported in 5.26.0 and other supported semver releases. All tests, samples, documents should enforce this.

**Deployment Types:**
- **Neo4jEnterpriseCluster**: For clustered deployments requiring high availability (minimum 1 primary + 1 secondary OR 2+ primaries)
- **Neo4jEnterpriseStandalone**: For single-node deployments in single mode (development/testing)

## Architecture

**Key Components:**
- **CRDs**: Neo4jEnterpriseCluster, Neo4jEnterpriseStandalone, Neo4jBackup/Restore, Neo4jDatabase, Neo4jPlugin
- **Controllers**: Enterprise cluster controller with autoscaling, standalone controller for single-node deployments
- **Validation**: Client-side validation with strict topology requirements

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
# Deploy a standalone instance for development
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Check standalone status
kubectl get neo4jenterprisestandalone
kubectl get pods

# Access Neo4j Browser (standalone)
kubectl port-forward svc/standalone-neo4j-service 7474:7474 &
open http://localhost:7474

# Or deploy a minimal cluster for testing
kubectl apply -f examples/clusters/minimal-cluster.yaml
kubectl get neo4jenterprisecluster
kubectl port-forward svc/minimal-cluster-client 7474:7474 &
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

### Deployment Configuration Approach

**Important**: The operator now uses two distinct deployment modes:

**Neo4jEnterpriseCluster** (Clustered Deployments):
- **Minimum Topology**: Requires either 1 primary + 1 secondary OR 2+ primaries
- **RAFT Enabled**: Uses `internal.dbms.single_raft_enabled=true` for seamless scaling
- **V2_ONLY Discovery**: Uses `dbms.cluster.discovery.version=V2_ONLY` for Neo4j 5.26+
- **Scalable by Design**: Can be scaled up/down while respecting minimum topology requirements

**Neo4jEnterpriseStandalone** (Single-Node Deployments):
- **Single Node Only**: Fixed at 1 replica, does not support scaling to multiple nodes
- **Unified Clustering**: Uses clustering infrastructure with single member (Neo4j 5.26+ approach)
- **No dbms.mode**: The `dbms.mode=SINGLE` setting is deprecated in Neo4j 5.x+ and should never be used
- **No Scaling**: For multi-node deployments, use Neo4jEnterpriseCluster instead
- **Development/Testing**: Ideal for development and testing environments

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

### Unified Configuration Approach

**Important**: The operator uses a unified clustering approach for all deployment types:

- **No Special Single-Node Mode**: All deployments use clustering infrastructure, even standalone single-node deployments
- **RAFT Enabled**: All deployments use `internal.dbms.single_raft_enabled=true` for consistency
- **Different Scaling Capabilities**:
  - Neo4jEnterpriseCluster: Supports scaling up/down while respecting topology constraints
  - Neo4jEnterpriseStandalone: Fixed at 1 replica, does not support scaling
- **No `dbms.mode=SINGLE`**: The deprecated single-node mode is not used (Neo4j 4.x only)

### Neo4j 5.26+ Configuration Notes

**Critical**: Neo4j 5.26+ configuration differs from older versions:

- **Deprecated Settings**: Never use `dbms.mode=SINGLE` (Neo4j 4.x only, no longer supported)
- **Clustering Infrastructure**: All deployments use clustering protocols, even single-node
- **Environment Variables**: Prefer environment variables over configuration file properties
- **Configuration Validation**: Neo4j 5.26+ has strict validation that rejects deprecated settings

### Neo4j 4.x to 5.x Configuration Migration

**Important**: The operator only supports Neo4j 5.26+. All Neo4j 4.x settings must be avoided:

**Removed Settings (NEVER USE)**:
- `dbms.mode=SINGLE` - Completely removed in 5.x, use clustering infrastructure for all deployments
- `causal_clustering.*` - Replaced with `dbms.cluster.*` and `server.cluster.*` prefixes
- `dbms.logs.debug.format`, `dbms.logs.debug.level` - Logging settings restructured
- `metrics.bolt.messages.enabled` and other `metrics.bolt.*` - Metrics configuration changed
- Fabric-related configurations - Fabric functionality restructured

**Deprecated Settings to Avoid**:
- `dbms.cluster.discovery.endpoints` - Deprecated in 5.23, use Kubernetes discovery
- `server.groups` - Deprecated in 5.4
- `db.cluster.raft.leader_transfer.priority_group` - Deprecated in 5.4

**Configuration Best Practices**:
1. **Discovery**: Always use `dbms.cluster.discovery.version=V2_ONLY` for 5.26+
2. **Kubernetes**: Use version-appropriate parameters:
   - 5.x semver: `dbms.kubernetes.service_port_name` and `dbms.kubernetes.discovery.v2.service_port_name`
   - 2025.x+ calver: `dbms.kubernetes.discovery.service_port_name`
3. **SSL/TLS**: Use `dbms.ssl.policy.{scope}.*` format, not legacy SSL settings
4. **Clustering**: Use `dbms.cluster.*` and `server.cluster.*`, not `causal_clustering.*`
5. **Validation**: Test configurations - Neo4j 5.x will reject invalid/deprecated settings at startup

### TLS/SSL Configuration

The operator supports TLS/SSL encryption for both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone deployments using cert-manager for automatic certificate management.

**Prerequisites**:
- cert-manager must be installed in the cluster
- A ClusterIssuer or Issuer must be available (development clusters have `ca-cluster-issuer` pre-configured)

**Configuration**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```

**SSL Policy Implementation**:
- **Neo4j 5.26+**: Uses `dbms.ssl.policy.{scope}.enabled=true` format
- **Neo4j 2025.x+**: Uses same SSL policy framework
- **Supported Scopes**: `https` (web interface), `bolt` (database connections)

**Automatic Certificate Management**:
- Certificates are automatically created via cert-manager
- Certificates include all necessary DNS names for service discovery
- Certificate renewal is handled automatically by cert-manager
- Certificates are mounted at `/ssl/` in Neo4j containers

**Example TLS Configuration**:
```yaml
# Generated Neo4j configuration for TLS
server.https.enabled=true
server.https.listen_address=0.0.0.0:7473
server.bolt.enabled=true
server.bolt.listen_address=0.0.0.0:7687
server.bolt.tls_level=REQUIRED

# SSL Policy for HTTPS
dbms.ssl.policy.https.enabled=true
dbms.ssl.policy.https.base_directory=/ssl
dbms.ssl.policy.https.private_key=tls.key
dbms.ssl.policy.https.public_certificate=tls.crt
dbms.ssl.policy.https.client_auth=NONE
dbms.ssl.policy.https.tls_versions=TLSv1.3,TLSv1.2

# SSL Policy for Bolt
dbms.ssl.policy.bolt.enabled=true
dbms.ssl.policy.bolt.base_directory=/ssl
dbms.ssl.policy.bolt.private_key=tls.key
dbms.ssl.policy.bolt.public_certificate=tls.crt
dbms.ssl.policy.bolt.client_auth=NONE
dbms.ssl.policy.bolt.tls_versions=TLSv1.3,TLSv1.2
```

**Testing TLS Connections**:
```bash
# Test HTTPS endpoint
curl -k https://localhost:7473

# Test Bolt TLS (using Neo4j driver)
kubectl port-forward svc/deployment-service 7687:7687
# Connect using bolt+ssc://localhost:7687
```

## Configuration Settings Validation

When working with Neo4j configurations, ensure you use the correct settings for Neo4j 5.26+:

### Quick Reference - Deprecated vs Correct Settings

| Deprecated (Don't Use) | Correct (Use This) | Notes |
|------------------------|-------------------|-------|
| `dbms.memory.heap.initial_size` | `server.memory.heap.initial_size` | Changed in 5.x |
| `dbms.memory.heap.max_size` | `server.memory.heap.max_size` | Changed in 5.x |
| `dbms.memory.pagecache.size` | `server.memory.pagecache.size` | Changed in 5.x |
| `dbms.connector.bolt.tls_level` | `server.bolt.tls_level` | Changed in 5.x |
| `dbms.connector.https.enabled` | `server.https.enabled` | Changed in 5.x |
| `dbms.mode=SINGLE` | (Don't set) | Removed in 5.x, use unified clustering |
| `causal_clustering.*` | `dbms.cluster.*` | Renamed in 5.x |
| `dbms.cluster.discovery.type` | `dbms.cluster.discovery.resolver_type` | Renamed |
| `db.format: "standard"` | `db.format: "block"` | Deprecated in 5.23 |
| `db.format: "high_limit"` | `db.format: "block"` | Deprecated in 5.23 |
| `server.groups` | `initial.server.tags` | Deprecated in 5.4 |

### Testing Configuration Changes

When testing configuration changes:
1. Always check Neo4j logs for deprecation warnings
2. Verify the operator doesn't add deprecated settings
3. Test with both Neo4j 5.26.x and 2025.x images
4. Ensure examples use correct settings

### Configuration Documentation

All documentation should reference the [Configuration Best Practices Guide](docs/user_guide/guides/configuration_best_practices.md) which contains:
- Complete list of deprecated settings
- Migration guidance from 4.x to 5.x
- Examples with correct settings
- Version-specific considerations

## Reports

All reports that Claude generates should go into the reports directory. The reports can be reviewed by Claude to determine changes that were made.
