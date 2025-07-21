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

## Backup Sidecar Architecture

Both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone deployments include an automatic backup sidecar container.

### Resource Requirements (CRITICAL)
- **Memory Limits**: 1Gi (increased from 256Mi to prevent OOMKilled containers)
- **Memory Requests**: 512Mi
- **CPU Limits**: 500m
- **CPU Requests**: 200m

### Features
- **Automatic deployment**: Added to all enterprise pods (cluster and standalone)
- **Neo4j 5.26+ support**: Uses correct `--to-path` command syntax
- **Version-aware configuration**: Handles both SemVer (5.x) and CalVer (2025.x) formats
- **Backup port**: Neo4j port 6362 automatically configured
- **Retention management**: Automatic cleanup based on retention policies
- **Path creation**: Automatically creates backup path before execution (required by Neo4j 5.26+)
- **RBAC management**: Automatically creates service accounts, roles, and role bindings in target namespaces

### Testing
```bash
# Test backup sidecar functionality
kubectl exec <pod> -c backup-sidecar -- \
  sh -c 'echo "{\"path\":\"/data/backups/test-$(date +%Y%m%d-%H%M%S)\",\"type\":\"FULL\"}" > /backup-requests/backup.request'

# Check backup status
kubectl exec <pod> -c backup-sidecar -- cat /backup-requests/backup.status
# Should return: 0 (success)
```

**Note**: Neo4j 5.26+ and 2025.x+ require the backup destination path to exist. The operator automatically handles this by creating the path before running the backup command.

### Deployment Configuration Approach

**Important**: The operator now uses two distinct deployment modes:

**Neo4jEnterpriseCluster** (Clustered Deployments):
- **Minimum Topology**: Requires either 1 primary + 1 secondary OR 2+ primaries
- **Unified Clustering**: Uses standard clustering infrastructure (no special single-raft mode)
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


### Unified Configuration Approach

**Important**: The operator uses a unified clustering approach for all deployment types:

- **No Special Single-Node Mode**: All deployments use clustering infrastructure, even standalone single-node deployments
- **Standard Clustering**: Uses Neo4j's standard clustering without special single-raft configurations
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

## Neo4j Kubernetes Discovery Architecture

### Service Discovery Fix (2025-07-17)

**Problem**: Neo4j Kubernetes discovery was finding multiple services (headless + internals) with the same pod endpoints, causing confusion in cluster formation.

**Root Cause**: Both headless and internals services were being discovered because they both had endpoints matching the discovery label selector `neo4j.com/service-type=internals`.

**Solution**: Modified service architecture to ensure only headless service is discovered:

1. **Discovery Label Selector**: Changed from `neo4j.com/service-type=internals` to `neo4j.com/clustering=true`
2. **Service Labels**:
   - Headless service: Has `neo4j.com/clustering=true` (discoverable)
   - Internals service: No clustering label (not discoverable)
3. **DNS Resolution**: Headless service resolves to individual pod IPs (required for cluster formation)

**Key Configuration**:
```yaml
# Discovery configuration
dbms.cluster.discovery.resolver_type=K8S
dbms.kubernetes.label_selector=neo4j.com/cluster=<name>,neo4j.com/clustering=true
dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
dbms.cluster.discovery.version=V2_ONLY
```

**Service Architecture**:
- **Headless Service**: ClusterIP=None, has `neo4j.com/clustering=true`, used for discovery
- **Internals Service**: ClusterIP, no clustering label, used for client connections
- **Client Service**: ClusterIP, for external client connections

**Verification**:
```bash
# Check discovered services
kubectl logs <pod> | grep "Resolved endpoints"
# Should show: [<cluster>-headless.default.svc.cluster.local:5000]

# Check DNS resolution
kubectl exec <pod> -- getent hosts <cluster>-headless.default.svc.cluster.local
# Should return individual pod IPs
```

**Important**: Neo4j requires headless services for discovery because they resolve to individual pod IPs, not service VIPs. ClusterIP services return a single VIP which prevents proper cluster formation.

## CRITICAL FIX: Neo4j V2_ONLY Discovery Configuration (2025-07-17)

### **Issue Summary**
Neo4j Enterprise Cluster formation was failing due to incorrect discovery port configuration in V2_ONLY mode. Pods would start successfully but remain as independent single-node clusters instead of forming a unified cluster.

### **Root Cause**
- **V2_ONLY Mode**: Neo4j V2_ONLY discovery mode disables the discovery port (6000) entirely
- **Port Mismatch**: Configuration was using `tcp-tx` port (6000) instead of `tcp-discovery` port (5000)
- **Discovery Failure**: Since port 6000 is disabled in V2_ONLY mode, cluster discovery was failing

### **Solution Implementation**
**Key Configuration Changes:**
```yaml
# CORRECT - Neo4j 5.26.x
dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery  # Port 5000
dbms.cluster.discovery.version=V2_ONLY

# CORRECT - Neo4j 2025.x
dbms.kubernetes.discovery.service_port_name=tcp-discovery     # Port 5000
# V2_ONLY is default, no explicit setting needed
```

**Service Port Mapping:**
- `tcp-discovery`: Port 5000 (cluster communication) - USED by V2_ONLY
- `tcp-tx`: Port 6000 (discovery) - DISABLED in V2_ONLY mode

### **Version-Specific Behavior**
1. **Neo4j 5.26.x**: Requires explicit `V2_ONLY` setting, uses `v2.service_port_name`
2. **Neo4j 2025.x**: V2_ONLY is default, uses `service_port_name` (no v2 prefix)
3. **Both versions**: Must use `tcp-discovery` port for cluster formation

### **Verification Steps**
```bash
# Check discovery logs
kubectl logs <pod> | grep "Resolved endpoints"
# Should show: tcp-discovery and port 5000

# Verify cluster formation
kubectl exec <pod> -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
# Should show both primary and secondary pods

# Test connectivity
kubectl exec <pod> -- timeout 2 bash -c "</dev/tcp/localhost/5000"
# Should return success (port open)
```

### **Implementation Files**
- `internal/resources/cluster.go`: `getKubernetesDiscoveryParameter()` function
- `internal/resources/cluster.go`: Service port definitions
- `examples/clusters/minimal-cluster.yaml`: Example configurations

### **Test Results**
- ✅ **5.26.x**: Cluster formation working with 1 primary + 1 secondary
- ✅ **2025.x**: Configuration correctly implemented (uses same tcp-discovery port)
- ✅ **Service Discovery**: Headless service properly resolves to pod IPs
- ✅ **Database Operations**: Can create/query data through cluster

**CRITICAL**: This fix is essential for Neo4j 5.26+ cluster formation. Without it, all cluster deployments will fail to form properly.

## CRITICAL MILESTONE: Neo4j Kubernetes Discovery Architecture (2025-07-18)

### **Issue Summary**
Neo4j cluster formation was investigated due to discovery returning service hostname instead of individual pod endpoints. This behavior was found to be **by design** and correct.

### **Key Discoveries**
1. **Neo4j K8s Discovery Behavior**:
   - Returns service DNS names (e.g., `test-cluster-discovery.default.svc.cluster.local:5000`)
   - This is EXPECTED behavior, not a bug
   - Neo4j internally uses this to query endpoints and discover pods

2. **RBAC Requirements**:
   - Discovery ServiceAccount MUST have `endpoints` permission in addition to `services`
   - Operator must have `endpoints` permission to grant it to discovery roles
   - Without endpoints access, Neo4j cannot resolve individual pods

3. **Service Architecture**:
   - Single shared discovery service with `neo4j.com/clustering=true` label
   - No per-pod services needed (matches Neo4j Helm chart pattern)
   - Discovery service is ClusterIP (not headless) - deliberate choice for stability

### **Implementation Details**
```go
// internal/resources/cluster.go - Discovery Role
Rules: []rbacv1.PolicyRule{
    {
        APIGroups: []string{""},
        Resources: []string{"services", "endpoints"},  // CRITICAL: endpoints permission required
        Verbs:     []string{"get", "list", "watch"},
    },
},
```

```yaml
# config/rbac/role.yaml - Operator permissions
- apiGroups:
  - ""
  resources:
  - endpoints  # CRITICAL: Operator needs this to grant it
  - services
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
```

### **Verification**
```bash
# Check discovery returns service hostname (EXPECTED)
kubectl logs <pod> | grep "Resolved endpoints"
# Output: '[test-cluster-discovery.default.svc.cluster.local:5000]'

# Verify cluster formation works
kubectl exec <pod> -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
# Shows all cluster members with their FQDNs
```

### **DO NOT CHANGE**
1. Keep discovery service as ClusterIP (not headless)
2. Keep single shared discovery service (not per-pod)
3. Keep endpoints permission in discovery role
4. Keep service hostname return behavior (it's correct)

**IMPORTANT**: This architecture matches Neo4j Helm charts and is the correct implementation. The discovery returning service hostname is by design - Neo4j uses this to query endpoints internally.

## CRITICAL MILESTONE: Optimized Parallel Cluster Formation (2025-07-18)

### **Achievement Summary**
Successfully implemented and tested an optimized cluster formation strategy that achieves **100% success rate** with the fastest possible startup time.

### **Final Configuration**
1. **Minimum Primaries**: Always set to 1 (`MIN_PRIMARIES=1`)
2. **Pod Management**: Parallel startup (`PodManagementPolicy: ParallelPodManagement`)
3. **Secondary Timing**: No delay - all pods start simultaneously
4. **Discovery Service**: `PublishNotReadyAddresses: true` for early pod discovery

### **Test Results**
- **Previous approaches**: 60-80% cluster formation success
- **Final approach**: 100% success - all nodes join single cluster
- **Example**: 5-node cluster (3 primaries + 2 secondaries) formed perfectly

### **Key Implementation Details**
```go
// In buildStatefulSetForEnterprise()
PodManagementPolicy: appsv1.ParallelPodManagement, // All pods start simultaneously

// In buildStartupScriptForEnterprise()
MIN_PRIMARIES=1  // First pod forms cluster, others join

// In BuildDiscoveryServiceForEnterprise()
PublishNotReadyAddresses: true, // Pods discoverable before ready
```

### **Why This Works**
- **No timing dependencies**: Eliminates race conditions
- **Natural cluster formation**: First pod creates cluster, others join
- **Kubernetes-native**: Leverages K8s endpoints for discovery
- **Simplified logic**: Removed complex sequencing code

### **Important**: This configuration is now the standard for all Neo4j Enterprise clusters deployed by the operator.

## Reports

All reports that Claude generates should go into the reports directory. The reports can be reviewed by Claude to determine changes that were made.

## Split-Brain Recovery Documentation

**Status**: Completed (2025-07-18)

The operator currently does NOT automatically detect or recover from split-brain scenarios. Manual intervention is required.

**Documentation Created**:
1. **Comprehensive Guide**: `docs/user_guide/troubleshooting/split-brain-recovery.md`
   - Complete identification and recovery procedures
   - Multiple recovery methods with risk assessment
   - TLS-specific considerations and workarounds
   - Prevention strategies and monitoring scripts

2. **Quick Reference**: `docs/user_guide/quick-reference/split-brain-recovery-quick-ref.md`
   - Emergency procedures for quick resolution
   - Command snippets for common scenarios
   - TLS cluster special handling

3. **Troubleshooting Integration**: Updated main troubleshooting guide to reference split-brain as #1 issue

**Key Points**:
- Split-brain is more common with TLS-enabled clusters
- Manual recovery methods range from targeted pod restarts to full scale down/up
- Prevention involves timeout adjustments and resource allocation
- Future enhancement: Implement automatic detection and recovery (see `docs/developer_guide/split-brain-recovery-proposal.md`)

## TLS Cluster Formation Solution

**Status**: Completed (2025-07-18)

Successfully resolved TLS cluster formation issues through multiple optimizations.

### Problem
TLS-enabled clusters experienced split-brain scenarios where nodes formed multiple independent clusters instead of joining together.

### Solution Implemented

1. **Parallel Pod Management** (CRITICAL)
   - Changed from `OrderedReadyPodManagement` to `ParallelPodManagement`
   - Location: `internal/resources/cluster.go` line 135
   - All pods start simultaneously for faster and more reliable formation

2. **Trust All for Cluster SSL**
   - Added `dbms.ssl.policy.cluster.trust_all=true` to cluster SSL policy
   - Allows nodes to trust each other's certificates during formation
   - Essential for TLS cluster handshake success

3. **Endpoints RBAC Permission**
   - Added endpoints permission via kubebuilder marker
   - Required for Neo4j Kubernetes discovery to resolve pod IPs
   - Location: `internal/controller/neo4jenterprisecluster_controller.go`

4. **Configuration Maintained**
   - `MIN_PRIMARIES=1` - Already optimal for flexible formation
   - `dbms.cluster.raft.membership.join_timeout=10m` - Do NOT reduce
   - `dbms.cluster.raft.binding_timeout=1d` - Very generous timeout

### Results
- **Before fixes**: Complete split-brain (5 separate single-node clusters)
- **Partial fixes**: Improved to 2 clusters (3+2 nodes)
- **All fixes applied**: 100% success - single unified cluster with all nodes

### Documentation Updates
1. **User Guide**: Created comprehensive TLS configuration guide at `docs/user_guide/configuration/tls.md`
2. **Architecture**: Updated with TLS-specific cluster formation details
3. **Examples**: Added production TLS cluster examples with best practices
4. **Tests**: Added TLS-specific tests validating trust_all configuration

### Key Learnings
- Parallel pod startup is essential for TLS clusters
- Certificate trust must be established early (trust_all=true)
- Standard timeouts (10m join, 1d binding) work well - don't reduce them
- Endpoints RBAC is critical for discovery to work properly
