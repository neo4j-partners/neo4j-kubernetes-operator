# Architecture Overview

This guide provides a comprehensive overview of the Neo4j Enterprise Operator's architecture, design principles, and current implementation status as of August 2025.

## Core Design Principles

The Neo4j Enterprise Operator follows cloud-native best practices with a focus on:

- **Production Stability**: Optimized reconciliation frequency and efficient resource management
- **Performance**: Intelligent rate limiting and status update optimization
- **Server-Based Architecture**: Unified server deployments with self-organizing roles
- **Resource Efficiency**: Centralized backup system (70% resource reduction)
- **Observability**: Comprehensive monitoring and operational insights
- **Validation**: Proactive resource validation and recommendations

## Current Architecture (August 2025)

### Server-Based Architecture

The operator has evolved to use a **unified server-based architecture** where Neo4j servers self-organize into primary/secondary roles:

#### Key Changes from Legacy Architecture
- **Before**: Separate primary/secondary StatefulSets with complex orchestration
- **After**: Single `{cluster-name}-server` StatefulSet with self-organizing servers
- **Benefit**: Simplified resource management, improved scaling, reduced complexity

#### Current Implementation
```yaml
# Neo4jEnterpriseCluster topology
topology:
  servers: 3  # Creates: my-cluster-server StatefulSet (replicas: 3)
  # Pods: my-cluster-server-0, my-cluster-server-1, my-cluster-server-2
```

```yaml
# Neo4jEnterpriseStandalone deployment
# Creates: my-standalone StatefulSet (replicas: 1)
# Pod: my-standalone-0
```

### Centralized Backup System

**Major Efficiency Improvement**: Replaced expensive per-pod backup sidecars with centralized backup architecture:

- **Resource Efficiency**: 100m CPU/256Mi memory per cluster vs N×200m CPU/512Mi per sidecar
- **Resource Savings**: ~70% reduction in backup-related resource usage
- **Architecture**: Single `{cluster-name}-backup-0` StatefulSet per cluster
- **Connectivity**: Connects to cluster via client service using Bolt protocol
- **Neo4j 5.26+ Support**: Modern backup syntax with automated path creation

## Custom Resource Definitions (CRDs)

The operator defines six core CRDs located in `api/v1alpha1/`:

### Core Deployment CRDs

#### Neo4jEnterpriseCluster (`neo4jenterprisecluster_types.go`)
- **Purpose**: High-availability clustered Neo4j Enterprise deployments
- **Architecture**: Server-based with `{cluster-name}-server` StatefulSet
- **Minimum Topology**: 2+ servers (enforced by validation)
- **Server Organization**: Servers self-organize into primary/secondary roles for databases
- **Scaling**: Horizontal scaling supported with topology validation
- **Discovery**: LIST resolver with static pod FQDNs; V2_ONLY explicitly set for 5.26.x, implicit for 2025.x+
- **Resource Pattern**: Single StatefulSet replaces complex multi-StatefulSet architecture

**Key Fields**:
```go
type Neo4jEnterpriseClusterSpec struct {
    Image    ImageSpec              `json:"image"`
    Topology TopologyConfiguration  `json:"topology"`  // servers: N
    Storage  StorageSpec           `json:"storage"`
    // ... additional fields
}
```

#### Neo4jEnterpriseStandalone (`neo4jenterprisestandalone_types.go`)
- **Purpose**: Single-node Neo4j Enterprise deployments
- **Architecture**: Uses clustering infrastructure but fixed at 1 replica
- **Use Cases**: Development, testing, simple production workloads
- **StatefulSet**: `{standalone-name}` (no "-server" suffix)
- **Configuration**: Modern clustering approach with single member (Neo4j 5.26+)
- **Restrictions**: Cannot scale beyond 1 replica

### Database Management CRDs

#### Neo4jDatabase (`neo4jdatabase_types.go`)
- **Purpose**: Manages database lifecycle within clusters and standalone deployments
- **Dual Support**: Works with both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone
- **Enhanced Validation**: DatabaseValidator supports automatic deployment type detection
- **Neo4j 5.26+ Syntax**: Uses modern `TOPOLOGY` clause for database creation
- **Standalone Fix**: Added NEO4J_AUTH environment variable for automatic authentication

**Key Features**:
```go
type Neo4jDatabaseSpec struct {
    ClusterRef string           `json:"clusterRef"`     // References cluster OR standalone
    Name       string           `json:"name"`           // Database name
    Topology   DatabaseTopology `json:"topology"`       // Primary/secondary counts
    IfNotExists bool            `json:"ifNotExists"`    // CREATE IF NOT EXISTS
}
```

#### Neo4jPlugin (`neo4jplugin_types.go`)
- **Purpose**: Manages Neo4j plugin installation and configuration
- **Dual Architecture Support**: Enhanced for server-based cluster + standalone compatibility
- **Deployment Detection**: Automatic cluster vs standalone recognition
- **Resource Naming**: Handles `{cluster-name}-server` vs `{standalone-name}` patterns
- **Plugin Sources**: Official, community, custom registry, direct URL support

### Backup & Restore CRDs

#### Neo4jBackup (`neo4jbackup_types.go`)
- **Purpose**: Manages backup operations for both clusters and standalone deployments
- **Centralized Architecture**: Uses single backup pod per cluster (not sidecars)
- **Target Support**: Can backup both cluster and standalone deployments
- **Neo4j 5.26+ Support**: Modern backup syntax with `--to-path` parameter

#### Neo4jRestore (`neo4jrestore_types.go`)
- **Purpose**: Manages database restoration from backups
- **Point-in-Time Recovery**: Supports `--restore-until` for precise recovery
- **Cross-Deployment Support**: Can restore to different deployment types

## Controllers Architecture

### Core Controllers (`internal/controller/`)

#### Neo4jEnterpriseCluster Controller (`neo4jenterprisecluster_controller.go`)
**Primary cluster management controller with server-based architecture:**

**Performance Optimizations**:
- **Efficient Reconciliation**: Reduced from ~18,000 to ~34 reconciliations per minute
- **Smart Status Updates**: Only updates when cluster state changes
- **ConfigMap Debouncing**: 2-minute debounce prevents restart loops
- **Resource Version Conflict Handling**: Retry logic for concurrent updates

**Server-Based Implementation**:
- **Single StatefulSet**: Creates `{cluster-name}-server` instead of separate primary/secondary
- **Self-Organizing Servers**: Neo4j servers automatically assign database hosting roles
- **Simplified Resource Management**: Unified pod templates and configuration
- **Certificate DNS**: Includes all server pod names in TLS certificates

**Split-Brain Detection**:
- **Location**: `internal/controller/splitbrain_detector.go`
- **Multi-Pod Analysis**: Connects to each server to compare cluster views
- **Automatic Repair**: Restarts orphaned pods to rejoin main cluster
- **Production Ready**: Comprehensive logging and fallback mechanisms

#### Neo4jEnterpriseStandalone Controller (`neo4jenterprisestandalone_controller.go`)
**Single-node deployment controller:**

**Key Features**:
- **Clustering Infrastructure**: Uses same infrastructure as clusters (Neo4j 5.26+ approach)
- **Single Member Configuration**: Sets up clustering with single server
- **Resource Management**: Handles ConfigMap, Service, and StatefulSet
- **Status Tracking**: Comprehensive status updates for standalone instances

#### Database Controller (`neo4jdatabase_controller.go`)
**Enhanced for dual deployment support:**
- **Automatic Detection**: Tries cluster lookup first, then standalone fallback
- **Neo4j Client Creation**: `NewClientForEnterprise()` vs `NewClientForEnterpriseStandalone()`
- **Authentication Handling**: Manages NEO4J_AUTH for standalone deployments
- **Syntax Support**: Neo4j 5.26+ and 2025.x database creation syntax

#### Plugin Controller (`plugin_controller.go`)
**Manages plugin lifecycle with architecture compatibility:**
- **DeploymentInfo Abstraction**: Unified handling of cluster/standalone types
- **Resource Naming**: Correct StatefulSet names (`{cluster-name}-server` vs `{standalone-name}`)
- **Pod Labels**: Applies appropriate labels for each deployment type
- **Plugin Sources**: Official, community, custom registries, direct URLs

#### Backup Controller (`neo4jbackup_controller.go`)
**Centralized backup management:**
- **Architecture**: Single backup StatefulSet per cluster
- **Resource Efficiency**: 70% reduction in backup resource usage
- **Cross-Deployment Support**: Backs up both clusters and standalone deployments
- **Modern Syntax**: Neo4j 5.26+ compatible backup commands

#### Restore Controller (`neo4jrestore_controller.go`)
**Database restoration management:**
- **Point-in-Time Recovery**: Supports precise timestamp restoration
- **Flexible Targets**: Can restore to different deployment types
- **Validation**: Ensures target deployment compatibility

## Validation Framework (`internal/validation/`)

### Comprehensive Validation Architecture

#### Core Validators:
- **TopologyValidator** (`topology_validator.go`): Cluster topology and server count validation
- **ClusterValidator** (`cluster_validator.go`): Cluster-specific configuration validation
- **MemoryValidator** (`memory_validator.go`): Neo4j memory settings vs container limits
- **ResourceValidator** (`resource_validator.go`): CPU, memory, and storage validation
- **TLSValidator** (`tls_validator.go`): TLS/SSL configuration validation
- **DatabaseValidator** (`database_validator.go`): Database creation and topology validation

#### Enhanced Validation Features:
- **Dual CRD Validation**: Separate validation rules for cluster vs standalone
- **Server-Based Topology**: Validates server counts instead of primary/secondary counts
- **Resource Recommendations**: Suggests optimal resource allocation
- **Configuration Restrictions**: Prevents clustering settings in standalone deployments
- **Neo4j Version Compatibility**: Validates settings against Neo4j 5.26+ and 2025.x

### Database Validator Enhancements
- **Automatic Deployment Detection**: Tries cluster first, then standalone
- **Appropriate Client Creation**: Uses correct client type for deployment
- **Clear Error Messages**: Distinguishes between cluster and standalone validation failures

## Neo4j Version Compatibility

### Supported Versions
- **Neo4j 5.26.x**: Last semver LTS release (5.26.0, 5.26.1, etc.) — no 5.27+ semver versions exist
- **Neo4j 2025.x+**: Calver format (2025.01.0, 2025.02.0, etc.)

### Version-Specific Configuration

#### Discovery Configuration (LIST resolver, injected by startup script):

| Setting | 5.26.x (SemVer) | 2025.x+ / 2026.x+ (CalVer) |
|---|---|---|
| `dbms.cluster.discovery.resolver_type` | `LIST` | `LIST` |
| `dbms.cluster.discovery.version` | `V2_ONLY` (explicit) | *(omitted — V2 is only protocol)* |
| Endpoints key | `dbms.cluster.discovery.v2.endpoints` | `dbms.cluster.endpoints` |
| Endpoint port | **6000** (tcp-tx) | **6000** (tcp-tx) |
| Bootstrap hint | `internal.dbms.cluster.discovery.system_bootstrapping_strategy=me/other` | *(not used)* |

Port 5000 (`tcp-discovery`) is the **deprecated V1 discovery port — never used by this operator**.
CalVer detection: `ParseVersion()` → `IsCalver` (`major >= 2025`) covers 2026.x+ automatically.

#### Modern Configuration Standards:
- **Memory**: `server.memory.*` (not deprecated `dbms.memory.*`)
- **TLS/SSL**: `server.https.*` and `server.bolt.*` (not `dbms.connector.*`)
- **Database Format**: `db.format: "block"` (not deprecated formats)
- **Discovery**: managed entirely by operator startup script — do not set in `spec.config`

### Database Creation Syntax

#### Neo4j 5.26+ (Cypher 5):
```cypher
CREATE DATABASE name [IF NOT EXISTS]
[TOPOLOGY n PRIMAR{Y|IES} [m SECONDAR{Y|IES}]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]
```

#### Neo4j 2025.x (Cypher 25):
```cypher
CREATE DATABASE name [IF NOT EXISTS]
[[SET] DEFAULT LANGUAGE CYPHER {5|25}]
[[SET] TOPOLOGY n PRIMARIES [m SECONDARIES]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]
```

## Resource Management Architecture

### Intelligent Resource Handling

#### Resource Builders (`internal/resources/`):
- **ClusterBuilder** (`cluster.go`): Server-based StatefulSet creation
- **StandaloneBuilder** (`standalone.go`): Single-node deployment resources
- **ConfigMapBuilder**: Unified configuration for both deployment types
- **ServiceBuilder**: Client and discovery services
- **BackupBuilder**: Centralized backup StatefulSet

#### Server-Based Resource Patterns:
- **StatefulSet Naming**: `{cluster-name}-server` for clusters, `{standalone-name}` for standalone
- **Pod Naming**: `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc.
- **Service Names**: `{cluster-name}-client`, `{cluster-name}-discovery`
- **Backup Resources**: `{cluster-name}-backup-0` (centralized)

### Performance Optimizations

#### Reconciliation Efficiency:
- **Rate Limiting**: Intelligent rate limiting prevents API server overload
- **Status Update Efficiency**: Only updates when state actually changes
- **Event Filtering**: Reduces unnecessary reconciliation triggers
- **ConfigMap Hashing**: Hash-based change detection prevents unnecessary updates

#### Startup Optimization:
- **Parallel Pod Management**: All server pods start simultaneously
- **Minimum Primaries = 1**: First pod forms cluster immediately
- **PublishNotReadyAddresses**: Discovery includes pending pods
- **Resource Version Conflict Retry**: Handles concurrent updates gracefully

## Security Architecture

### RBAC Configuration (`config/rbac/`)

#### Core RBAC Resources:
- **Principle of Least Privilege**: Minimal required permissions
- **ClusterRole Design**: Cross-namespace operations support
- **Service Account Security**: Dedicated accounts with specific roles

#### Discovery RBAC (Critical):
Each cluster gets automatic RBAC creation:
- **ServiceAccount**: `{cluster-name}-discovery`
- **Role**: Services and endpoints permissions
- **RoleBinding**: Links account to role
- **Endpoints Permission**: **CRITICAL** for cluster formation

### TLS/SSL Support:
- **Cert-Manager Integration**: Automatic certificate provisioning
- **SSL Policy Configuration**: Separate policies for `https`, `bolt`, and `cluster` scopes
- **Trust All for Cluster**: `dbms.ssl.policy.cluster.trust_all=true` for formation
- **Certificate DNS Names**: Includes all server pod names

## Monitoring & Observability

### Resource Monitoring (`internal/monitoring/`):
- **ResourceMonitor** (`resource_monitor.go`): Real-time utilization tracking
- **Performance Metrics**: Controller performance and reconciliation efficiency
- **Operational Insights**: ConfigMap update patterns and debounce effectiveness

### Status Management:
- **Enhanced Status Updates**: Detailed cluster state tracking
- **Condition Management**: Comprehensive status conditions with proper transitions
- **Event Recording**: Structured events for debugging and monitoring
- **Connection Examples**: Automatic generation of connection strings

### QueryMonitor and Live Diagnostics

The `QueryMonitoringSpec` field (`spec.queryMonitoring`) drives two distinct
responsibilities inside the cluster controller:

**1. Infrastructure setup** (`ReconcileQueryMonitoring`):
Creates Kubernetes resources for metrics collection:
- `{cluster-name}-metrics` Service — exposes port 2004 for Prometheus scraping
- `{cluster-name}-query-monitoring` ServiceMonitor — tells the Prometheus Operator to scrape the metrics service
- Neo4j config flags (`server.metrics.prometheus.enabled=true`, `prometheus.io/*` annotations)

Runs on every reconcile regardless of cluster phase.

**2. Live diagnostics** (`CollectDiagnostics`):
Runs `SHOW SERVERS` and `SHOW DATABASES` via the Bolt client when the cluster is `Ready`:
- Writes results to `status.diagnostics` (`ClusterDiagnosticsStatus`)
- Sets `ServersHealthy` condition (`True` when all servers are `state=Enabled` and `health=Available`)
- Sets `DatabasesHealthy` condition (`True` when all user databases have `status=online`; the `system` database is excluded)
- Updates `neo4j_operator_server_health` Prometheus gauge per server (labels: `cluster_name`, `namespace`, `server_name`, `server_address`)
- Non-fatal: collection errors are surfaced in `status.diagnostics.collectionError` and the conditions are set to `Unknown` with reason `DiagnosticsUnavailable`

The diagnostics Bolt client is created fresh per-reconcile and closed with `defer`. It
never shares state with the cluster formation or upgrade clients.

**Architecture invariant:** All status writes in `CollectDiagnostics` use
`retry.RetryOnConflict` to handle concurrent updates without panicking.

**Condition constants** (defined in `internal/controller/conditions.go`):
- `ConditionTypeServersHealthy = "ServersHealthy"`
- `ConditionTypeDatabasesHealthy = "DatabasesHealthy"`
- Reason values: `AllServersHealthy`, `ServerDegraded`, `AllDatabasesOnline`, `DatabaseOffline`, `DiagnosticsUnavailable`

## Integration Architecture

### External System Integration:
- **Cert-Manager**: TLS certificate lifecycle management
- **Prometheus**: Metrics collection and alerting
- **External Secrets**: Secret management integration
- **Storage Classes**: Persistent volume provisioning
- **Cloud Providers**: AWS, GCP, Azure LoadBalancer optimizations

### Kubernetes Integration:
- **Network Policies**: Pod-to-pod communication security
- **Service Mesh**: Istio/Linkerd compatibility
- **Ingress Controllers**: External traffic routing with connection examples
- **Node Affinity**: Topology spread and anti-affinity rules

## Testing Architecture

### Test Strategy:
- **Unit Tests**: Controller logic and helper functions
- **Integration Tests**: Full workflow testing with envtest
- **End-to-End Tests**: Real cluster testing with Kind
- **Performance Tests**: Reconciliation efficiency validation

### Test Infrastructure:
- **Ginkgo/Gomega**: BDD-style testing framework
- **Envtest**: Kubernetes API server for integration testing
- **Kind Clusters**: Development and test cluster automation
- **Test Cleanup**: Automatic finalizer removal and namespace cleanup

## Migration & Compatibility

### Legacy Architecture Migration:
- **Backward Compatibility**: Existing clusters continue to work
- **Gradual Migration**: No breaking changes for existing deployments
- **Resource Name Updates**: New deployments use server-based naming
- **Configuration Migration**: Automatic handling of deprecated settings

### Future Extensibility:
- **Plugin System**: Neo4j plugin management framework
- **Custom Metrics**: Extensible monitoring capabilities
- **Event Handling**: Pluggable event system for custom integrations
- **Multi-Architecture**: Support for different deployment patterns

## Development Best Practices

### Code Organization:
- **Controller Pattern**: Standard Kubernetes controller pattern
- **Builder Pattern**: Resource builders for clean separation
- **Validation Framework**: Centralized validation with clear error messages
- **Testing Strategy**: Comprehensive test coverage with multiple levels

### Performance Considerations:
- **Memory Usage**: Optimized for large-scale deployments
- **API Efficiency**: Minimal API calls with intelligent caching
- **Resource Creation**: Parallel resource creation where possible
- **Error Handling**: Graceful error handling with proper recovery

This architecture provides a solid foundation for managing Neo4j Enterprise deployments in Kubernetes with high performance, reliability, and operational simplicity.
