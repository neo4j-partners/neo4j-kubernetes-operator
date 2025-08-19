# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Neo4j Enterprise Operator for Kubernetes - manages Neo4j Enterprise deployments (v5.26+) using Kubebuilder framework.

**Supported Neo4j Versions**: Only 5.26+ (Semver: 5.26.0+, Calver: 2025.01.0+)
**Discovery**: V2_ONLY mode exclusively

**Deployment Types:**
- **Neo4jEnterpriseCluster**: High availability clusters (minimum 2 servers that self-organize into primary/secondary roles)
- **Neo4jEnterpriseStandalone**: Single-node deployments (development/testing)

## Architecture

**Key Components:**
- CRDs: Neo4jEnterpriseCluster, Neo4jEnterpriseStandalone, Neo4jBackup/Restore
- Controllers: Cluster & standalone controllers with client-side validation
- Neo4j Client: Bolt protocol communication

**Directory Structure:**
- `api/v1alpha1/` - CRD definitions
- `internal/controller/` - Controller logic
- `internal/resources/` - K8s resource builders
- `test/` - Unit, integration, e2e tests

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

# Troubleshoot OOM issues
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"
kubectl top pod <pod-name> --containers  # Check memory usage
kubectl logs <pod-name> --previous | tail  # Check logs before restart

# Test Neo4j database operations
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "CREATE DATABASE testdb TOPOLOGY 1 PRIMARY"
```

## CRITICAL Current Architecture (August 2025)

**Server-Based Architecture**: Unified server deployment where Neo4j servers self-organize into primary/secondary roles.

**Current Topology**:
```yaml
topology:
  servers: 3  # Single StatefulSet: cluster-name-server (replicas: 3)
```

**Centralized Backup**: Single backup StatefulSet per cluster (`cluster-name-backup`) replaces expensive per-pod sidecars.

**Key Implementation**:
- Single `{cluster-name}-server` StatefulSet with `replicas: N`
- Pods: `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc.
- Backup: `{cluster-name}-backup-0` (if backups enabled)

**Resource Efficiency**: Centralized backup uses ~70% fewer resources than distributed sidecars.

### Server Role Specification

**Server Role Hints**: Control which databases a server can host using Neo4j's `initial.server.mode_constraint` configuration.

**Basic Usage**:
```yaml
# Self-organizing cluster (default)
topology:
  servers: 3

# Global constraint for all servers
topology:
  servers: 3
  serverModeConstraint: "PRIMARY"  # All servers only host primaries

# Per-server role hints (takes precedence over global)
topology:
  servers: 3
  serverRoles:
    - serverIndex: 0
      modeConstraint: "PRIMARY"    # Server-0: only primary databases
    - serverIndex: 1
      modeConstraint: "SECONDARY"  # Server-1: only secondary databases
    - serverIndex: 2
      modeConstraint: "NONE"       # Server-2: any database mode (default)
```

**Mode Constraints**:
- `NONE` (default): Server can host databases in any mode
- `PRIMARY`: Server only hosts databases in primary mode
- `SECONDARY`: Server only hosts databases in secondary mode

**Use Cases**:
- **Dedicated Primary Servers**: Assign high-performance nodes to host only primary databases
- **Secondary-Only Servers**: Use lower-cost nodes for read replicas and analytics workloads
- **Mixed Workloads**: Balance primary and secondary databases across servers
- **Resource Optimization**: Match server capabilities to database hosting requirements

**Validation**:
- Server indices must be within range (0 to servers-1)
- No duplicate server indices allowed
- Cannot configure all servers as SECONDARY (cluster needs primaries)
- Per-server role hints override global `serverModeConstraint`

See detailed implementation: `/reports/2025-08-19-server-based-architecture-implementation.md`

## Testing & Development

**Test Suite** (Ginkgo/Gomega):
- Unit Tests: `make test-unit` (run before commits)
- Integration Tests: `make test-integration` (requires cluster)
- E2E Tests: `make test-e2e`

**Key Notes**:
- Kind clusters only (no minikube/k3s)
- Webhooks require cert-manager
- Use envtest for controller unit tests
- Neo4j client uses Bolt protocol
- Integration tests use 300-second timeouts for CI compatibility

**Test Troubleshooting**:
- If tests timeout: Check image pull delays in CI - tests use 5-minute timeout
- If pod scheduling fails: Check resource constraints - tests use minimal CPU/memory
- If cluster formation fails: Check discovery service and endpoints RBAC permissions
- If pods get OOMKilled: Check memory limits - Neo4j Enterprise needs ≥ 1.5Gi for database operations
- If database creation hangs: Verify Neo4j 5.x syntax uses `TOPOLOGY` clause, not `OPTIONS`

### Development Environment

**Kind Clusters** (Kind only - no minikube/k3s):
- **Development**: `neo4j-operator-dev` - manual testing
- **Test**: `neo4j-operator-test` - automated tests
- Both include cert-manager v1.18.2 with `ca-cluster-issuer`

**Cleanup Commands**:
- `make dev-cluster-clean` / `make test-cluster-clean` - Remove operator only
- `make dev-cluster-reset` / `make test-cluster-reset` - Recreate cluster
- `make dev-destroy` / `make test-destroy` - Complete destruction

## CI/CD & Debugging

**GitHub Actions**: Unit/lint → Integration → E2E → Multi-arch builds
**PR Requirements**: All checks must pass, use conventional commits

**Debug Failed Reconciliation**:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f
kubectl describe neo4jenterprisecluster <name>
make dev-run ARGS="--zap-log-level=debug"
```

## Key Features

### Centralized Backup System
- **Architecture**: Single backup StatefulSet per cluster (replaces expensive per-pod sidecars)
- **Resource Efficiency**: 100m CPU/256Mi memory for entire cluster vs N×200m CPU/512Mi per sidecar
- **Connectivity**: Connects to cluster via client service using Bolt protocol
- **Neo4j 5.26+ Support**: Correct `--to-path` syntax, automated path creation
- **Test**: `kubectl exec <cluster>-backup-0 -- sh -c 'echo "{\"path\":\"/backups/test\",\"type\":\"FULL\"}" > /backup-requests/backup.request'`
- **Benefits**: No coordination issues, centralized storage, single point of monitoring

### Deployment Configuration

**Neo4jEnterpriseCluster**:
- Min topology: 2+ servers (self-organize into primary/secondary roles)
- Scalable, uses V2_ONLY discovery

**Neo4jEnterpriseStandalone**:
- Fixed single node (no scaling)
- Uses clustering infrastructure (no `dbms.mode=SINGLE`)

**Version-Specific Discovery**:
- **5.x**: `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery`
- **2025.x**: `dbms.kubernetes.discovery.service_port_name=tcp-discovery`
- Auto-detected via `getKubernetesDiscoveryParameter()`

### Configuration Guidelines

**Never Use** (Neo4j 4.x settings - DEPRECATED):
- `dbms.mode=SINGLE` - Use server-based architecture instead
- `causal_clustering.*` - Replaced by modern clustering in 5.26+
- `metrics.bolt.*` - Use `server.metrics.*` instead
- `server.groups` - Not applicable to 5.26+ clustering
- `dbms.cluster.role` - Use `SHOW DATABASES` for cluster status
- `causal_clustering.leader_election_timeout` - Use `causal_clustering.leader_failure_detection_window`

**Always Use** (Neo4j 5.26+ and 2025.x):
- `dbms.cluster.discovery.version=V2_ONLY` (5.x) / default in 2025.x
- `server.*` instead of `dbms.connector.*`
- `dbms.ssl.policy.{scope}.*` for TLS
- Environment variables over config files
- Modern database topology syntax (see below)

### TLS Configuration

**Setup**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```

**Auto-generated**:
- SSL policies for `https` and `bolt` scopes
- Certificates mounted at `/ssl/`
- `dbms.ssl.policy.cluster.trust_all=true` for cluster formation

**Test**: `curl -k https://localhost:7473`

## Neo4j Database Syntax Reference (5.26+ and 2025.x)

### CREATE DATABASE Syntax

**Neo4j 5.26+ (Cypher 5)**:
```cypher
CREATE DATABASE name [IF NOT EXISTS]
[TOPOLOGY n PRIMAR{Y|IES} [m SECONDAR{Y|IES}]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]

CREATE OR REPLACE DATABASE name
[TOPOLOGY n PRIMAR{Y|IES} [m SECONDAR{Y|IES}]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]
```

**Neo4j 2025.x (Cypher 25)**:
```cypher
CREATE DATABASE name [IF NOT EXISTS]
[[SET] DEFAULT LANGUAGE CYPHER {5|25}]
[[SET] TOPOLOGY n PRIMARIES [m SECONDARIES]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]
```

### Example Usage

**Basic Database Creation**:
```cypher
-- Single primary with secondaries
CREATE DATABASE mydb TOPOLOGY 1 PRIMARY 2 SECONDARIES

-- Multiple primaries for fault tolerance
CREATE DATABASE proddb TOPOLOGY 3 PRIMARIES 2 SECONDARIES

-- Neo4j 2025.x with Cypher 25
CREATE DATABASE moderndb
DEFAULT LANGUAGE CYPHER 25
TOPOLOGY 3 PRIMARIES 2 SECONDARIES
```

**Parameterized Creation** (Operator Usage):
```cypher
-- Using parameters from operator
CREATE DATABASE $dbname
TOPOLOGY $primary PRIMARIES $secondary SECONDARIES WAIT
```

### CRITICAL: Deprecated 4.x Syntax to AVOID

**❌ NEVER USE** (Neo4j 4.x - Will Fail in 5.26+):
```cypher
-- DEPRECATED: OPTIONS with primaries/secondaries
CREATE DATABASE baddb OPTIONS {primaries: 1, secondaries: 1}

-- DEPRECATED: dbms.cluster.role usage
CALL dbms.cluster.role()

-- DEPRECATED: Causal clustering syntax
-- Any causal_clustering.* configuration
```

## CRITICAL: Split-Brain Detection and Repair (Added 2025-08-09)

**MANDATORY FOR PRODUCTION**: The operator includes comprehensive split-brain detection and automatic repair to prevent Neo4j cluster inconsistencies.

**Location**: `internal/controller/splitbrain_detector.go`

**Key Features**:
- **Multi-Pod Analysis**: Connects to each Neo4j server pod individually to compare cluster views
- **Smart Detection**: Distinguishes between split-brain scenarios and normal startup/formation
- **Automatic Repair**: Restarts orphaned pods to rejoin the main cluster
- **Production Ready**: Includes comprehensive logging, events, and fallback mechanisms

**Why This Fix Is Critical**:
1. **Split-Brain Prevention**: Detects scenarios where servers form separate clusters instead of one unified cluster
2. **Data Consistency**: Prevents data divergence between isolated cluster partitions
3. **Automatic Recovery**: No manual intervention required for common split-brain scenarios
4. **Production Reliability**: Essential for maintaining cluster integrity in production environments

**Monitoring Commands**:
```bash
# Check for split-brain events
kubectl get events --field-selector reason=SplitBrainDetected -A

# Monitor cluster formation
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -E "(split|brain|SplitBrain)"

# Verify cluster health after repair
kubectl exec <cluster>-server-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
```

## CRITICAL: Resource Version Conflict Handling (Added 2025-08-05)

**MANDATORY FOR CLUSTER FORMATION**: The operator MUST include resource version conflict retry logic to prevent timing-sensitive cluster formation failures.

**Location**: `internal/controller/neo4jenterprisecluster_controller.go`

**Essential Pattern**:
```go
import "k8s.io/client-go/util/retry"

func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateResource(ctx context.Context, obj client.Object, owner client.Object) error {
    return retry.RetryOnConflict(retry.DefaultRetry, func() error {
        return r.createOrUpdateResourceInternal(ctx, obj, owner)
    })
}
```

**Why This Fix Is Critical**:
1. **Neo4j 2025.01.0 Dependency**: Without retry logic, Neo4j 2025.01.0 fails to form clusters due to timing-sensitive resource conflicts during bootstrap
2. **StatefulSet Conflicts**: Kubernetes StatefulSet controller and operator reconciliation create concurrent updates
3. **Cluster Bootstrap Window**: Resource conflicts during critical cluster formation window cause permanent failure
4. **Production Reliability**: Essential for consistent cluster formation across all Neo4j versions

## Configuration Validation

### Integration Test Configuration
- **Timeouts**: All integration tests use 300-second timeout for CI compatibility
- **Resources**: Minimal CPU (50m-200m), memory limits at 1.5Gi (Neo4j Enterprise requirement for database operations)
- **Storage**: Reduced storage sizes (500Mi-1Gi) to avoid PVC scheduling issues
- **Image Pull**: Tests account for image pull delays in CI environments
- **Memory Validation**: Neo4j Enterprise requires minimum 1.5Gi for database creation and topology operations
- **OOM Prevention**: Tests configured to prevent Out of Memory kills (exit code 137) during database operations

### Template Comparison Fix (Critical)
**Issue**: Original logic used `sts.ResourceVersion != ""` to check if StatefulSet exists
**Problem**: ResourceVersion is populated even for new resources, preventing initial creation
**Solution**: Use `sts.UID != ""` which correctly identifies existing vs new resources
**Impact**: Enables immediate StatefulSet creation instead of being blocked by template comparison

### Regression Prevention Checklist
1. **Resource Conflicts**: Always use `retry.RetryOnConflict` with `controllerutil.CreateOrUpdate`
2. **Template Comparison**: Use `UID != ""` to check resource existence, not `ResourceVersion != ""`
3. **Test Timeouts**: Use 300-second timeout for all integration tests
4. **Resource Requirements**: Keep CPU ≤ 200m, memory limits must be ≥ 1.5Gi for Neo4j Enterprise (database operations)
5. **Cluster Formation**: Verify using `SHOW SERVERS` command, not just status checks
6. **Server Architecture**: Always use `servers` field for clusters, preserve `primaries`/`secondaries` for databases
7. **Pod Naming**: Expect `<cluster>-server-*` naming, not `<cluster>-primary-*` or `<cluster>-secondary-*`
8. **Certificate DNS**: Include all server pod DNS names in certificates
9. **Discovery Port**: Always use `tcp-discovery` (5000), never `tcp-tx` (6000) for V2_ONLY mode

## Reports

All reports go in `/reports/` directory with mandatory `YYYY-MM-DD-descriptive-name.md` format.

**Key Reports:**
- `/reports/2025-08-19-server-based-architecture-implementation.md` - Detailed server-based architecture implementation
- `/reports/2025-08-05-neo4j-2025.01.0-enterprise-cluster-analysis.md` - Neo4j 2025.x compatibility analysis
- `/reports/2025-08-08-seed-uri-and-server-architecture-release-notes.md` - Seed URI feature implementation

# important-instruction-reminders
Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
ALWAYS prefer editing an existing file to creating a new one.
NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
