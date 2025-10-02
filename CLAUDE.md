# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Neo4j Enterprise Operator for Kubernetes - manages Neo4j Enterprise deployments (v5.26+) using Kubebuilder framework.

**Supported Neo4j Versions**: Only 5.26+ (Semver: 5.26.0+, Calver: 2025.01.0+)
**CRITICAL: KIND IS MANDATORY**: This project exclusively uses Kind (Kubernetes in Docker) for ALL development, testing, and CI workflows. No alternatives (minikube, k3s) are supported.

**CRITICAL: ENTERPRISE IMAGES ONLY**: Never use Neo4j community images (neo4j:5.26), only enterprise ones (neo4j:5.26-enterprise, neo4j:2025.01.0-enterprise)
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

### Prerequisites Check & Kind Installation
```bash
# Verify Kind is installed (MANDATORY)
kind version

# Install Kind if missing:
# macOS: brew install kind
# Linux: curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64 && chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind

# Test Kind functionality
kind create cluster --name test && kind delete cluster --name test
```

### Build & Development
```bash
make build                 # Build operator binary
make docker-build         # Build container image
make docker-push          # Push docker image
make manifests            # Generate CRDs and RBAC
make generate             # Generate DeepCopy methods

# Development cluster management
make dev-cluster          # Create Kind development cluster (neo4j-operator-dev)
make dev-cluster-clean    # Clean operator resources from dev cluster
make dev-cluster-reset    # Delete and recreate dev cluster
make dev-cluster-delete   # Delete dev cluster
make dev-cleanup          # Clean dev environment (keep cluster)
make dev-destroy          # Completely destroy dev environment

# Standard deployment (uses local images by default)
make deploy-dev           # Deploy to dev namespace with local neo4j-operator:dev image
make deploy-prod          # Deploy to prod namespace with local neo4j-operator:latest image
make deploy-dev-local     # Build and deploy controller with local dev image to Kind cluster
make deploy-prod-local    # Build and deploy controller with local prod image to Kind cluster
make operator-setup       # Deploy operator to available Kind cluster

# Registry-based deployment (uses Docker Hub)
make deploy-dev-registry  # Deploy dev overlay with registry image
make deploy-prod-registry # Deploy prod overlay with Docker Hub image

# Undeployment
make undeploy-dev         # Undeploy development controller from cluster
make undeploy-prod        # Undeploy production controller from cluster

# Operator management utilities
make operator-setup-interactive  # Set up the Neo4j operator interactively
make operator-status      # Show operator status
make operator-logs        # Follow operator logs

# Demo capabilities
make demo                 # Run interactive demo of the operator capabilities
make demo-fast            # Run fast automated demo (no confirmations)
make demo-only            # Run fast demo without environment setup (assumes cluster exists)
make demo-interactive     # Run interactive demo without environment setup
make demo-setup           # Setup complete demo environment (cluster + operator)
```

**CRITICAL: NEVER run `make dev-run` (operator outside cluster)**
- DNS resolution fails when operator runs outside cluster
- Cluster formation verification requires in-cluster connectivity
- Always use `make operator-setup` to deploy operator inside cluster
- This applies to ALL development and testing workflows

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
# Environment management
make test-setup           # Setup test environment
make test-cleanup         # Clean test artifacts (keep cluster)
make test-destroy         # Completely destroy test environment

# Quick tests (no cluster required)
make test-unit            # Unit tests only

# Test cluster management
make test-cluster         # Create test cluster (neo4j-operator-test)
make test-cluster-clean   # Clean operator resources from test cluster
make test-cluster-reset   # Delete and recreate test cluster
make test-cluster-delete  # Delete test cluster

# Cluster-based tests
make test-integration     # Integration tests (auto-creates cluster and deploys operator)
make test-integration-ci  # Run integration tests in CI (assumes cluster already exists)
make test-integration-ci-full  # Run ALL integration tests in CI (use with caution - may exhaust resources)

# Full test suite
make test                 # Run unit + integration tests
make test-coverage        # Generate coverage report

# CI Workflow Emulation (Added 2025-08-22)
make test-ci-local        # Emulate CI workflow locally with debug logging
                          # - Runs unit tests with CI=true GITHUB_ACTIONS=true
                          # - Creates test cluster and deploys operator
                          # - Runs integration tests with 512Mi memory constraints
                          # - Provides detailed logging for troubleshooting
                          # - Logs saved to: logs/ci-local-*.log

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
make tidy                 # Tidy go modules and verify
make clean                # Clean build artifacts and temporary files
```

### Debugging & Troubleshooting
```bash
# View operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Validate CRDs
kubectl explain neo4jenterprisecluster.spec

# Install and uninstall CRDs
make install              # Install CRDs into the K8s cluster
make uninstall           # Uninstall CRDs from the K8s cluster

# Troubleshoot OOM issues
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"
kubectl top pod <pod-name> --containers  # Check memory usage
kubectl logs <pod-name> --previous | tail  # Check logs before restart

# Test Neo4j database operations
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "CREATE DATABASE testdb TOPOLOGY 1 PRIMARY"
```

### Bundle and Catalog Management (Operator SDK)
```bash
# Bundle generation and management
make bundle               # Generate bundle manifests and metadata, validate files
make bundle-build         # Build the bundle image
make bundle-push          # Push the bundle image

# Catalog image management
make catalog-build        # Build a catalog image
make catalog-push         # Push a catalog image

# Dependency tools
make operator-sdk         # Download operator-sdk locally if necessary
make opm                  # Download opm (Operator Package Manager) locally
make kustomize            # Download kustomize locally if necessary
make controller-gen       # Download controller-gen locally if necessary
make envtest             # Download setup-envtest locally if necessary
make golangci-lint       # Download golangci-lint locally if necessary
make ginkgo              # Download ginkgo locally if necessary
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

**Key Notes**:
- Kind clusters only (no minikube/k3s)
- TLS features require cert-manager
- Use envtest for controller unit tests
- Neo4j client uses Bolt protocol
- Integration tests use 300-second timeouts for CI compatibility
- **Property Sharding tests**: Skipped in CI due to resource requirements (run locally only)

**Property Sharding Tests** (Local Only):
```bash
# Run property sharding tests locally (requires Neo4j 2025.07.1+ images)
# IMPORTANT: Property sharding tests require significant resources:
# - 16GB+ memory per server (12GB+ heap + system overhead)
# - 2+ CPU cores per server
# - 5+ servers minimum
# Only run on high-spec development machines
make test-integration FOCUS="Property Sharding"

# Or with ginkgo directly
ginkgo run -focus "Property Sharding" ./test/integration
```

**✅ Property Sharding Test Results** (Updated based on Implementation Report 2025-09-05):
- **Minimum servers**: 5 servers (validated working configuration)
- **Memory requirements**: 4-8Gi per server (20-40Gi total cluster memory)
- **CPU requirements**: 2+ cores per server for cross-shard query processing
- **Resource overhead**: 20-30% additional CPU and 2-4GB extra memory for shard coordination
- **Network requirements**: Low-latency networking essential for transaction log synchronization
- **Test duration**: ~130 seconds for full cluster creation and validation
- **Authentication**: Required (Auth.AdminSecret must be configured)
- **Storage**: Storage class must be specified (e.g., `className: standard`)

**Test Troubleshooting**:
- If tests timeout: Check image pull delays in CI - tests use 5-minute timeout
- If pod scheduling fails: Check resource constraints - tests use minimal CPU/memory
- If cluster formation fails: Check discovery service and endpoints RBAC permissions
- If pods get OOMKilled: Check memory limits - Neo4j Enterprise needs ≥ 1.5Gi for database operations
- If database creation hangs: Verify Neo4j 5.x syntax uses `TOPOLOGY` clause, not `OPTIONS`
- If property sharding tests fail: Ensure Neo4j 2025.07.1+ images available, sufficient cluster resources (5+ nodes, 4Gi+ memory per server minimum, 8Gi recommended, 2+ CPU cores per server)

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

**GitHub Actions (Updated 2025-08-27)**:
- **Unit Tests**: ✅ Always run automatically on all pushes/PRs
- **Integration Tests**: ⏭️ Optional, on-demand only (trigger with PR label `run-integration-tests`, commit message `[run-integration]`, or manual dispatch)
- **E2E Tests**: Manual workflow dispatch only
- **Release**: Multi-arch builds triggered by git tags

**PR Requirements**: Unit tests must pass, integration tests optional unless requested
**Triggers**:
```bash
# PR label method
gh pr edit --add-label "run-integration-tests"

# Commit message method
git commit -m "feat: cluster changes [run-integration]"

# Manual dispatch: Actions → CI → Run workflow → Check "Run integration tests"
```

**Debug Failed Reconciliation**:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f
kubectl describe neo4jenterprisecluster <name>
# Enable debug logging in deployed operator
kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--mode=dev","--zap-log-level=debug"]}]}}}}'
```

## Key Features

### Plugin Installation Testing
- **Environment Variable Only Plugins**: APOC configuration uses environment variables (no longer supported in neo4j.conf in Neo4j 5.26+)
- **Neo4j Config Plugins**: Graph Data Science, Bloom require neo4j.conf configuration with automatic security settings
- **Dual Deployment Support**: Plugin tests verify both cluster and standalone deployment architectures
- **ConfigMap Validation**: Standalone plugin tests check ConfigMap content, not StatefulSet environment variables
- **Critical Test Fix**: Updated GDS and Bloom plugin tests to check ConfigMap where Neo4j standalone reads configuration

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

## Neo4j Plugin Support

**CRITICAL**: The operator provides comprehensive support for Neo4j plugins with automatic configuration based on plugin type and Neo4j version compatibility.

### Plugin Types and Configuration

**Environment Variable Only Plugins** (Neo4j 5.26+):
- **APOC**: `apoc.export.file.enabled` → `NEO4J_APOC_EXPORT_FILE_ENABLED`
- **APOC Extended**: Similar environment variable mapping
- **Reason**: APOC settings no longer supported in `neo4j.conf` in Neo4j 5.26+

**Neo4j Config Plugins**:
- **Graph Data Science**: Requires `dbms.security.procedures.unrestricted=gds.*`
- **Bloom**: Requires multiple settings (`dbms.bloom.*`, `server.unmanaged_extension_classes`, HTTP auth)
- **GenAI**: Provider-specific configuration through neo4j.conf
- **Neo Semantics (N10s)**: Procedure security configuration
- **GraphQL**: Standard plugin configuration

### Plugin Usage Examples

**APOC Plugin** (Environment Variables):
```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
spec:
  clusterRef: my-cluster
  name: apoc
  version: "5.26.0"
  config:
    # These become NEO4J_APOC_* environment variables
    apoc.export.file.enabled: "true"
    apoc.import.file.enabled: "true"
```

**Graph Data Science Plugin** (Neo4j Config):
```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: gds-plugin
spec:
  clusterRef: my-cluster
  name: graph-data-science
  version: "2.10.0"
  config:
    # These go through neo4j.conf
    gds.enterprise.license_file: "/licenses/gds.license"
  # Security settings automatically applied:
  # - dbms.security.procedures.unrestricted=gds.*
  # - dbms.security.procedures.allowlist=gds.*
```

**Bloom Plugin** (Complex Neo4j Config):
```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: bloom-plugin
spec:
  clusterRef: my-cluster
  name: bloom
  version: "2.15.0"
  config:
    dbms.bloom.license_file: "/licenses/bloom.license"
  # Automatically configured:
  # - dbms.security.procedures.unrestricted=bloom.*
  # - dbms.security.http_auth_allowlist=/,/browser.*,/bloom.*
  # - server.unmanaged_extension_classes=com.neo4j.bloom.server=/bloom
```

**Plugin with Dependencies**:
```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: gds-with-apoc
spec:
  clusterRef: my-cluster
  name: graph-data-science
  version: "2.10.0"
  dependencies:
    - name: apoc
      versionConstraint: ">=5.26.0"
      optional: false
  # Both GDS and APOC will be installed and configured correctly
```

### Key Plugin Features

**Automatic Configuration**:
- Plugin-specific security settings applied automatically
- Environment variables vs neo4j.conf handled correctly
- Procedure allowlists configured per plugin requirements

**Dependency Management**:
- Automatic dependency resolution and installation
- Version constraint validation
- Optional vs required dependency handling

**Plugin Installation Methods**:
- `NEO4J_PLUGINS` environment variable (recommended)
- Automatic jar file management
- Version compatibility validation

**Testing Plugin Installation**:
```bash
# Verify plugin installation
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW PROCEDURES"

# Check plugin-specific procedures
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc'"

# For standalone deployments: Check ConfigMap content (not environment variables)
kubectl get configmap <standalone-name>-config -o yaml | grep -A10 neo4j.conf

# For cluster deployments: Check environment variables
kubectl get statefulset <cluster-name>-server -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NEO4J_PLUGINS")].value}'
```

**Plugin Testing Architecture**:
- **Cluster Tests**: Verify plugin installation via StatefulSet environment variables
- **Standalone Tests**: Verify plugin configuration via ConfigMap where Neo4j reads settings
- **Critical Fix Applied**: Plugin tests now correctly validate configuration source based on deployment type

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

## CRITICAL: Neo4jDatabase Support for Standalone Deployments (Added 2025-08-20)

**MANDATORY FOR PRODUCTION**: The operator now fully supports Neo4jDatabase resources with both Neo4jEnterpriseCluster AND Neo4jEnterpriseStandalone deployments.

**Key Fixes Implemented**:
- **Enhanced DatabaseValidator**: Added dual resource discovery - tries cluster first, then standalone
- **Enhanced Database Controller**: Added standalone-specific reconciliation logic with proper client creation
- **Enhanced Neo4j Client**: Added `NewClientForEnterpriseStandalone()` method for standalone connections
- **Authentication Fix**: Added NEO4J_AUTH environment variable to standalone controller for automatic password setup

**Why This Fix Is Critical**:
1. **API Consistency**: Neo4jDatabase resources now work uniformly across all deployment types
2. **Authentication Automation**: Eliminates manual password changes in standalone deployments
3. **Production Readiness**: Enables automated database creation in both cluster and standalone environments
4. **Developer Experience**: Consistent API behavior regardless of deployment architecture

**Usage Examples**:
```yaml
# Database for cluster deployment
apiVersion: neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-cluster-database
spec:
  clusterRef: my-cluster  # References Neo4jEnterpriseCluster
  name: proddb
  topology:
    primaries: 2
    secondaries: 1

---
# Database for standalone deployment
apiVersion: neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-standalone-database
spec:
  clusterRef: my-standalone  # References Neo4jEnterpriseStandalone
  name: devdb
  ifNotExists: true
```

**Validation Logic**:
- DatabaseValidator attempts cluster lookup first
- If cluster not found, attempts standalone lookup
- Applies appropriate validation rules based on deployment type
- Provides clear error messages for missing references

**Technical Implementation**:
- Database controller detects referenced resource type automatically
- Uses appropriate Neo4j client (cluster vs standalone)
- Maintains backward compatibility with existing cluster deployments

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

## CRITICAL: Edition Field Removal (Added 2025-09-02)

**MANDATORY FOR API SIMPLIFICATION**: The operator no longer requires users to specify an Edition field since only Neo4j Enterprise edition is supported.

**Changes Made**:
- **Removed Edition field** from `Neo4jEnterpriseClusterSpec` and `Neo4jEnterpriseStandaloneSpec` API types
- **Eliminated redundant validation** - edition_validator.go now always returns no errors
- **Updated all examples** - removed `edition: enterprise` from 22+ YAML files
- **Updated all tests** - removed Edition field from integration and unit tests
- **Updated documentation** - removed Edition field references from API docs

**Why This Change Is Critical**:
1. **User Experience**: Eliminates confusion about why users need to specify something that only has one valid option
2. **API Clarity**: Simplifies the API surface - if only enterprise is supported, don't make users declare it
3. **Reduced Complexity**: Less validation code, fewer test cases, cleaner examples
4. **Future-Proof**: When there's only one option, having a field for it is an anti-pattern

**Impact**:
- **Existing deployments**: No breaking changes - operator internally assumes enterprise edition
- **New deployments**: Simpler YAML with no edition field required
- **Tests pass**: All integration tests updated to work without Edition field
- **Documentation**: Updated to reflect simplified API

**Technical Notes**:
- Neo4j client code still checks actual Neo4j server edition (from `CALL dbms.components()`) - this remains unchanged
- CRDs regenerated to remove edition field from OpenAPI schema
- All controller and validation logic updated to not expect Edition field

## Configuration Validation

### Integration Test Configuration
- **Timeouts**: All integration tests use 300-second timeout for CI compatibility
- **Resources**: Minimal CPU (50m-200m), memory limits at 1.5Gi (Neo4j Enterprise requirement for database operations)
- **Storage**: Reduced storage sizes (500Mi-1Gi) to avoid PVC scheduling issues
- **Image Pull**: Tests account for image pull delays in CI environments
- **Memory Validation**: Neo4j Enterprise requires minimum 1.5Gi for database creation and topology operations
- **OOM Prevention**: Tests configured to prevent Out of Memory kills (exit code 137) during database operations

### Integration Test Resource Management (Critical)
**MANDATORY**: All integration tests MUST implement proper resource cleanup to prevent CI failures.

**Required AfterEach Pattern**:
```go
AfterEach(func() {
    // Critical: Clean up resources immediately to prevent CI resource exhaustion
    if cluster != nil {
        By("Cleaning up cluster resource")
        // Remove finalizers first
        if len(cluster.GetFinalizers()) > 0 {
            cluster.SetFinalizers([]string{})
            _ = k8sClient.Update(ctx, cluster)
        }
        // Delete the resource
        _ = k8sClient.Delete(ctx, cluster)
        cluster = nil
    }
    // Clean up any remaining resources in namespace
    if testNamespace != "" {
        cleanupCustomResourcesInNamespace(testNamespace)
    }
})
```

**Key Requirements**:
1. **Always include AfterEach blocks** - Even if tests have inline cleanup
2. **Remove finalizers before deletion** - Ensures resources are actually deleted
3. **Call cleanupCustomResourcesInNamespace()** - Cleans up related resources
4. **Set resources to nil after deletion** - Prevents double cleanup
5. **Don't rely on test suite cleanup alone** - Active cleanup prevents accumulation

**Common Pitfalls to Avoid**:
- ❌ No AfterEach block (causes resource leaks if tests fail)
- ❌ Only deleting main resource without namespace cleanup
- ❌ Relying on inline cleanup at end of test (won't run if test fails)
- ❌ Not removing finalizers (resources stay in Terminating state)
- ❌ Comments saying "cleanup handled by test suite" (not sufficient)

### Template Comparison Fix (Critical)
**Issue**: Original logic used `sts.ResourceVersion != ""` to check if StatefulSet exists
**Problem**: ResourceVersion is populated even for new resources, preventing initial creation
**Solution**: Use `sts.UID != ""` which correctly identifies existing vs new resources
**Impact**: Enables immediate StatefulSet creation instead of being blocked by template comparison

### CRD Separation of Concerns (Critical)

**MANDATORY DESIGN PRINCIPLE**: The operator follows strict separation of concerns between CRDs to prevent configuration conflicts and maintain clear responsibility boundaries.

**Neo4jEnterpriseCluster / Neo4jEnterpriseStandalone CRDs**:
- Infrastructure-level configuration (servers, resources, networking)
- Neo4j server configuration (`neo4j.conf` settings)
- Authentication, TLS, and security settings
- Plugin installations and environment variables
- Backup policies and monitoring configuration
- Image versions and repository settings

**Neo4jDatabase CRD**:
- Database-specific settings only (name, topology, Cypher version)
- Database creation options passed to `CREATE DATABASE` statement
- Initial data import and seeding configuration
- Database-level topology (primaries/secondaries distribution within cluster)

**NEVER Allow Cross-CRD Configuration Overrides**:
- ❌ Neo4jDatabase MUST NOT override cluster/server-level settings
- ❌ Neo4jDatabase MUST NOT modify resource limits, TLS, or authentication
- ❌ Neo4jDatabase MUST NOT change Neo4j server configuration
- ✅ Each CRD manages its own responsibility scope exclusively

**Why This Design Matters**:
1. **Clear Ownership**: Each CRD has a single responsibility
2. **Prevents Conflicts**: Avoids configuration conflicts between resources
3. **Operational Clarity**: Operators know exactly where to configure each setting
4. **Resource Management**: Infrastructure decisions stay at infrastructure level
5. **Security Consistency**: Security settings apply uniformly across all databases

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
10. **CRD Separation**: Never allow cross-CRD configuration overrides
11. **Enterprise Image Validation**: Always validate Neo4j Enterprise images only (`neo4j:X.Y-enterprise`, `neo4j:2025.X.Y-enterprise`) - never allow community images (`neo4j:X.Y`) as they cause licensing and feature failures
12. **Integration Test Cleanup**: MANDATORY AfterEach blocks with finalizer removal and `cleanupCustomResourcesInNamespace()` - prevents CI resource exhaustion and test failures from resource leaks
13. **NEO4J_AUTH Environment Variable**: Standalone deployments require `NEO4J_AUTH` environment variable for automatic password setup - critical for Neo4jDatabase support on standalone deployments
14. **Plugin Configuration Validation**: Environment variable plugins (APOC) check StatefulSet env vars for clusters; Neo4j config plugins (GDS, Bloom) check ConfigMap content for standalone - test configuration source based on deployment type
15. **Status Phase Validation**: Always check `status.phase="Ready"` for clusters before database operations - don't rely solely on conditions as phase is more reliable for readiness
16. **TLS Scheme Consistency**: TLS-enabled clusters must use `bolt+s://` scheme, TLS-disabled use `bolt://` - critical for Neo4j client connections and seed URI functionality
17. **Backup Path Syntax**: Neo4j 5.26+ requires correct `--to-path` syntax for backup operations with automated path creation to prevent backup failures

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
