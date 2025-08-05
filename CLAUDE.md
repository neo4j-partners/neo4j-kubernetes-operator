# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Neo4j Enterprise Operator for Kubernetes - manages Neo4j Enterprise deployments (v5.26+) using Kubebuilder framework.

**Supported Neo4j Versions**: Only 5.26+ (Semver: 5.26.0+, Calver: 2025.01.0+)
**Discovery**: V2_ONLY mode exclusively

**Deployment Types:**
- **Neo4jEnterpriseCluster**: High availability clusters (minimum 1 primary + 1 secondary OR 2+ primaries)
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
```

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

### Backup Sidecar
- **Automatic**: Added to all pods with RBAC auto-creation
- **Resources**: Memory: 512Mi/1Gi, CPU: 200m/500m (prevents OOM)
- **Neo4j 5.26+ Support**: Correct `--to-path` syntax, auto path creation
- **Test**: `kubectl exec <pod> -c backup-sidecar -- sh -c 'echo "{\"path\":\"/data/backups/test\",\"type\":\"FULL\"}" > /backup-requests/backup.request'`

### Deployment Configuration

**Neo4jEnterpriseCluster**:
- Min topology: 1 primary + 1 secondary OR 2+ primaries
- Scalable, uses V2_ONLY discovery

**Neo4jEnterpriseStandalone**:
- Fixed single node (no scaling)
- Uses clustering infrastructure (no `dbms.mode=SINGLE`)

**Version-Specific Discovery**:
- **5.x**: `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery`
- **2025.x**: `dbms.kubernetes.discovery.service_port_name=tcp-discovery`
- Auto-detected via `getKubernetesDiscoveryParameter()`

### Configuration Guidelines

**Never Use** (Neo4j 4.x settings):
- `dbms.mode=SINGLE`
- `causal_clustering.*`
- `metrics.bolt.*`
- `server.groups`

**Always Use**:
- `dbms.cluster.discovery.version=V2_ONLY`
- `server.*` instead of `dbms.connector.*`
- `dbms.ssl.policy.{scope}.*` for TLS
- Environment variables over config files

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



## Critical Architecture Decisions

### V2_ONLY Discovery (Fixed 2025-07-17)
- **Issue**: Wrong port used (6000 instead of 5000)
- **Solution**: Use `tcp-discovery` port (5000) for V2_ONLY mode
- **Verification**: `kubectl logs <pod> | grep "Resolved endpoints"` should show port 5000

### K8s Discovery Architecture (Fixed 2025-07-18)
- **Discovery returns service hostname**: This is correct behavior
- **RBAC**: Must have `endpoints` permission for discovery
- **Service**: Single ClusterIP discovery service (not headless)
- **DO NOT CHANGE**: This matches Neo4j Helm charts

### Parallel Cluster Formation (Fixed 2025-07-18)
- **Configuration**: `MIN_PRIMARIES=1`, `ParallelPodManagement`
- **Result**: 100% cluster formation success
- **Key**: All pods start simultaneously, first forms cluster



### TLS Cluster Formation (Fixed 2025-07-18)
- **Solution**: `ParallelPodManagement` + `trust_all=true` for cluster SSL
- **Result**: 100% success rate (was failing with split-brain)
- **Key**: Don't reduce timeouts, ensure endpoints RBAC

### Neo4j Configuration & Cluster Formation (Updated 2025-08-05)
- **Discovery Service Architecture**: V2_ONLY mode correctly uses discovery service hostname, not individual endpoints
- **Port Configuration**: Always use `tcp-discovery` (port 5000) for K8s discovery, not `tcp-tx` (port 6000)
- **Minimum Primaries**: Set to 1 (`dbms.cluster.minimum_initial_system_primaries_count=1`) for flexible cluster formation
- **FQDN Addressing**: All advertised addresses use FQDN format via headless service
- **Service Setup**:
  - **Discovery Service**: ClusterIP with `tcp-discovery:5000`, selector includes `neo4j.com/clustering=true`
  - **Headless Service**: For pod-to-pod communication with all cluster ports
  - **Client Service**: ClusterIP for external access (bolt/http)
  - **Internals Service**: ClusterIP for operator management access
- **Key Success Factor**: Service-based discovery more reliable than endpoint-based for Neo4j in K8s

### Critical Neo4j Settings for Clusters (Added 2025-08-05)
These settings are automatically configured by the operator:
- `dbms.cluster.discovery.resolver_type=K8S`
- `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery` (5.x)
- `dbms.kubernetes.discovery.service_port_name=tcp-discovery` (2025.x)
- `dbms.cluster.discovery.version=V2_ONLY` (5.x only, default in 2025.x)
- `initial.dbms.automatically_enable_free_servers=true`
- `dbms.cluster.minimum_initial_system_primaries_count=1`

**Variable Substitution**: `${MIN_PRIMARIES}` and `${HOSTNAME_FQDN}` are substituted in startup script

## CRITICAL: Resource Version Conflict Handling (Added 2025-08-05)

**MANDATORY FOR CLUSTER FORMATION**: The operator MUST include resource version conflict retry logic to prevent timing-sensitive cluster formation failures.

### Resource Version Conflict Fix Implementation
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

### Why This Fix Is Critical
1. **Neo4j 2025.01.0 Dependency**: Without retry logic, Neo4j 2025.01.0 fails to form clusters due to timing-sensitive resource conflicts during bootstrap
2. **StatefulSet Conflicts**: Kubernetes StatefulSet controller and operator reconciliation create concurrent updates
3. **Cluster Bootstrap Window**: Resource conflicts during critical cluster formation window cause permanent failure
4. **Production Reliability**: Essential for consistent cluster formation across all Neo4j versions

### Expected Behavior WITH Fix
- **Conflict Detection**: `Retrying resource update due to conflict ... retryCount: 1`
- **Fast Resolution**: `Successfully updated resource after conflict resolution ... duration: "18-25ms"`
- **Pod-2 Rolling Updates**: Expected side effect - highest-indexed pods restart during StatefulSet updates
- **100% Success Rate**: All conflicts resolved automatically

### Expected Behavior WITHOUT Fix
- **Neo4j 5.26.x**: Usually works but may have occasional timing issues
- **Neo4j 2025.01.0**: Consistently fails to form clusters - gets stuck at discovery resolution
- **Resource Conflicts**: Unresolved conflicts cause reconciliation failures
- **Manual Intervention**: Requires cluster deletion and recreation

### Verification Commands
```bash
# Check for conflict resolution in operator logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -E "(conflict|retry)"

# Verify cluster formation success
kubectl get neo4jenterprisecluster
kubectl get pods | grep -E "(primary|secondary)"

# Monitor StatefulSet revisions (should be minimal)
kubectl rollout history statefulset <cluster-name>-primary
```

### ConfigMap Debounce Configuration
**Location**: `internal/controller/configmap_manager.go:120`

**Recommended Setting**:
```go
minInterval := 1 * time.Second // Fast updates for cluster formation
```

**NOT**: `minInterval := 2 * time.Minute // Can cause timing issues`

### Troubleshooting Resource Conflicts
1. **Symptoms**: Pods stuck in ContainerCreating, cluster never reaches Ready state
2. **Diagnosis**: Check operator logs for "resource version" or "conflict" errors
3. **Resolution**: Ensure retry logic is implemented and debounce period is minimal
4. **Prevention**: Always include conflict handling in any resource update operations

### REGRESSION PREVENTION CHECKLIST
- [ ] Retry logic present in `createOrUpdateResource` methods
- [ ] Import `k8s.io/client-go/util/retry` in controller
- [ ] ConfigMap debounce ≤ 1 second for cluster formation
- [ ] Test with Neo4j 2025.01.0 to verify cluster formation
- [ ] Monitor operator logs for conflict resolution messages
- [ ] Verify StatefulSet rolling updates complete successfully

**DO NOT**: Remove or modify retry logic without comprehensive testing across all Neo4j versions

## Configuration Validation

### Integration Test Configuration
- **Timeouts**: All integration tests use 300-second timeout for CI compatibility
- **Resources**: Minimal CPU (50m-200m), memory limits at 1Gi (Neo4j Enterprise minimum requirement)
- **Storage**: Reduced storage sizes (500Mi-1Gi) to avoid PVC scheduling issues
- **Image Pull**: Tests account for image pull delays in CI environments
- **Memory Validation**: Neo4j Enterprise validates minimum 1Gi memory at startup

### Template Comparison Fix (Critical)
**Issue**: Original logic used `sts.ResourceVersion != ""` to check if StatefulSet exists
**Problem**: ResourceVersion is populated even for new resources, preventing initial creation
**Solution**: Use `sts.UID != ""` which correctly identifies existing vs new resources
**Impact**: Enables immediate StatefulSet creation instead of being blocked by template comparison

### CI Environment Considerations
- **Resource Limits**: GitHub Actions runners have limited CPU/memory
- **Storage Classes**: Use 'standard' storage class for compatibility
- **Pod Scheduling**: Avoid resource requests that exceed node capacity
- **Network Policies**: Ensure cert-manager can issue certificates

### Regression Prevention Checklist
1. **Resource Conflicts**: Always use `retry.RetryOnConflict` with `controllerutil.CreateOrUpdate`
2. **Template Comparison**: Use `UID != ""` to check resource existence, not `ResourceVersion != ""`
3. **Test Timeouts**: Use 300-second timeout for all integration tests
4. **Resource Requirements**: Keep CPU ≤ 200m, memory limits must be ≥ 1Gi for Neo4j Enterprise
5. **Cluster Formation**: Verify using `SHOW SERVERS` command, not just status checks

## Reports

All reports go in `/reports/` directory with mandatory `YYYY-MM-DD-descriptive-name.md` format.

Example: `2025-07-23-integration-tests-fix-summary.md`
