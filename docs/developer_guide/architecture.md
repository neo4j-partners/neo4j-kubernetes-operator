# Architecture

This guide provides an overview of the Neo4j Enterprise Operator's architecture and design principles.

## Core Design Principles

The Neo4j Enterprise Operator follows cloud-native best practices with a focus on:

- **Production Stability**: Optimized reconciliation frequency and efficient resource management
- **Performance**: Intelligent rate limiting and status update optimization
- **Observability**: Comprehensive monitoring and operational insights
- **Validation**: Proactive resource validation and recommendations

## Controllers

The operator is built around a set of controllers that manage the lifecycle of Neo4j resources. Each controller is optimized for performance and reliability.

### Neo4jEnterpriseCluster Controller

The main controller (`internal/controller/neo4jenterprisecluster_controller.go`) manages clustered Neo4j Enterprise deployments with the following architectural components:

#### Performance Optimizations
- **Efficient Reconciliation**: Optimized from ~18,000 to ~34 reconciliations per minute
- **Smart Status Updates**: Only updates status when cluster state actually changes
- **ConfigMap Debouncing**: 2-minute debounce mechanism prevents restart loops

#### Core Components
- **ConfigMap Manager** (`internal/controller/configmap_manager.go`): Handles Neo4j configuration with hash-based change detection
- **Topology Scheduler**: Handles pod placement and anti-affinity rules
- **Cluster Topology Validator**: Enforces minimum cluster topology requirements (2+ servers)

### Neo4jEnterpriseStandalone Controller

The standalone controller (`internal/controller/neo4jenterprisestandalone_controller.go`) manages single-node Neo4j Enterprise deployments:

#### Key Features
- **Unified Clustering Infrastructure**: Uses clustering infrastructure with single member (Neo4j 5.26+ approach)
- **Simplified Topology**: Single-node deployment without complex clustering configurations
- **Resource Management**: Handles ConfigMap, Service, and StatefulSet for single-node deployments
- **Status Tracking**: Provides comprehensive status updates for standalone instances

### Other Controllers

- **Neo4jDatabase Controller**: Manages database lifecycle within clusters
- **Neo4jBackup/Restore Controllers**: Handle backup and restore operations (supports both cluster and standalone targets)
- **Neo4jPlugin Controller**: Manages Neo4j plugin installation and configuration

## Custom Resource Definitions (CRDs)

The operator defines a set of CRDs to represent Neo4j resources. The Go type definitions are located in `api/v1alpha1/`.

### Core CRDs

#### Neo4jEnterpriseCluster
- **Purpose**: Manages clustered Neo4j Enterprise deployments requiring high availability
- **Minimum Topology**: Enforces 2+ servers that self-organize into primary/secondary roles
- **Discovery Mode**: Automatically configures V2_ONLY discovery for Neo4j 5.26+ and 2025.x
- **CRITICAL**: Uses `tcp-discovery` port (5000) for V2_ONLY discovery, not `tcp-tx` port (6000)
- **Scaling**: Supports horizontal scaling with topology validation

#### Neo4jEnterpriseStandalone
- **Purpose**: Manages single-node Neo4j Enterprise deployments
- **Use Cases**: Development, testing, simple production workloads
- **Configuration**: Uses unified clustering approach with single member (Neo4j 5.26+)
- **Restrictions**: Fixed at 1 replica, does not support scaling to multiple nodes

### Enhanced CRD Features
- **Resource Validation**: Built-in validation for resource limits and Neo4j configuration
- **Status Conditions**: Comprehensive status reporting with detailed conditions
- **Topology Validation**: Prevents invalid cluster configurations

## Validation Framework

The operator uses a comprehensive validation framework (`internal/validation/`) to ensure resource correctness:

### Validation Components
- **Topology Validator** (`topology_validator.go`): Validates cluster topology and enforces minimum requirements
- **Cluster Validator** (`cluster_validator.go`): Validates cluster configuration and topology
- **Memory Validator** (`memory_validator.go`): Ensures Neo4j memory settings are within container limits
- **Resource Validator** (`resource_validator.go`): Validates CPU, memory, and storage allocation

### Validation Features
- **Proactive Validation**: Catches configuration errors before deployment
- **Topology Enforcement**: Ensures Neo4jEnterpriseCluster meets minimum topology requirements
- **CRD Separation**: Validates that single-node deployments use Neo4jEnterpriseStandalone
- **Resource Recommendations**: Suggests optimal resource allocation
- **Memory Ratio Validation**: Ensures proper heap/page cache ratios
- **Configuration Restrictions**: Prevents clustering configurations in standalone deployments

## Monitoring and Observability

The operator includes a comprehensive monitoring framework for operational insights:

### Resource Monitoring
- **Resource Monitor** (`internal/monitoring/resource_monitor.go`): Real-time resource utilization tracking
- **Performance Metrics**: Controller performance and reconciliation efficiency
- **Operational Insights**: ConfigMap update patterns and debounce effectiveness

### Status Management
- **Enhanced Status Updates**: Detailed cluster state tracking
- **Condition Management**: Comprehensive status conditions with proper transitions
- **Event Recording**: Structured events for debugging and monitoring

## Resource Management

The operator includes intelligent resource management capabilities:

### Resource Recommendations
- **Resource Recommendation Engine** (`internal/resources/resource_recommendation.go`): Suggests optimal resource allocation
- **Memory Optimization**: Automatic heap and page cache sizing recommendations
- **Scaling Guidance**: Intelligent scaling recommendations based on usage patterns

### Configuration Management
- **Hash-based Change Detection**: Prevents unnecessary ConfigMap updates
- **Debounce Mechanism**: Reduces configuration churn and restart loops
- **Content Normalization**: Ensures consistent configuration formatting
- **Dual CRD Support**: Handles configuration for both clustered and standalone deployments
- **V2_ONLY Discovery Fix**: Automatically configures correct discovery port for Neo4j 5.26+ and 2025.x
  - Neo4j 5.26.x: `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery`
  - Neo4j 2025.x: `dbms.kubernetes.discovery.service_port_name=tcp-discovery`
  - Implementation: `internal/resources/cluster.go:getKubernetesDiscoveryParameter()`
- **Unified Clustering**: All deployments use clustering infrastructure (no special single mode)

## RBAC and Security

The operator's permissions are defined in `config/rbac/` following security best practices:

- **Principle of Least Privilege**: Minimal required permissions
- **ClusterRole Design**: Scoped permissions for cross-namespace operations
- **Service Account Security**: Dedicated service accounts with specific roles

## Performance Architecture

### Reconciliation Optimization
- **Rate Limiting**: Intelligent rate limiting prevents API server overload
- **Status Update Efficiency**: Only updates when state actually changes
- **Event Filtering**: Reduces unnecessary reconciliation triggers

### Caching Strategy
- **Informer Caching**: Optimized Kubernetes informer usage
- **Direct Client Mode**: Ultra-fast startup with direct API calls
- **Selective Watching**: Only watches resources that trigger reconciliation

### Startup Modes
The operator supports multiple startup modes for different environments:

- **Production Mode**: Standard settings with full caching
- **Development Mode**: Optimized cache settings for development
- **Minimal Mode**: Ultra-fast startup with minimal caching

## Integration Points

### External Systems
- **Cert-Manager**: TLS certificate management integration
- **Prometheus**: Metrics collection and monitoring
- **External Secrets**: Secret management integration
- **Storage Classes**: Persistent volume integration

### Kubernetes Integration
- **Network Policies**: Pod-to-pod communication security
- **Service Mesh**: Istio/Linkerd compatibility
- **Ingress Controllers**: External traffic routing

## Extensibility

The operator is designed for extensibility:

- **Plugin System**: Support for Neo4j plugin management
- **Custom Metrics**: Extensible monitoring framework
- **Webhook Integration**: Admission webhook support
- **Event Handlers**: Pluggable event handling system
- **Dual CRD Architecture**: Supports both clustered and standalone deployment patterns
- **Migration Support**: Provides tools and guidance for migrating between deployment types

## Configuration Management

### Neo4j Version Compatibility

The operator supports Neo4j 5.26+ and 2025.x+ versions. Key configuration considerations:

1. **Memory Settings**: Always use `server.memory.*` prefix (not deprecated `dbms.memory.*`)
2. **TLS/SSL Settings**: Use `server.https.*` and `server.bolt.*` (not deprecated `dbms.connector.*`)
3. **Discovery Settings**: Use `dbms.cluster.discovery.resolver_type` (not deprecated `type`)
4. **Database Format**: Use `db.format: "block"` (not deprecated "standard" or "high_limit")

### Automatic Configuration

The operator automatically manages:
- Cluster discovery settings (`dbms.cluster.discovery.resolver_type: "K8S"`)
- Discovery version (`dbms.cluster.discovery.version: "V2_ONLY"` for 5.26+, default for 2025.x)
- **CRITICAL V2_ONLY Discovery Fix**: Correct port configuration for cluster formation
  - Neo4j 5.26.x: `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery`
  - Neo4j 2025.x: `dbms.kubernetes.discovery.service_port_name=tcp-discovery`
  - Uses port 5000 (cluster) not port 6000 (discovery - disabled in V2_ONLY)
- Kubernetes-specific endpoints and advertised addresses
- Network bindings for pods

### Kubernetes Discovery Mechanism

Neo4j's Kubernetes discovery (`resolver_type=K8S`) works as follows:

1. **Service Discovery**: Neo4j queries the Kubernetes API to find services matching the configured label selector
2. **Endpoint Resolution**: For each matching service, Neo4j retrieves the Endpoints resource to get individual pod IPs
3. **Cluster Formation**: Neo4j uses these pod IPs to establish direct connections between cluster members

**Important Discovery Behavior**: When Neo4j discovers services, it logs the service hostname (e.g., `my-cluster-discovery.default.svc.cluster.local:5000`). This is **expected behavior** - Neo4j discovers the service first, then internally queries its endpoints to resolve individual pod IPs.

### Cluster Formation Strategy

The operator implements an optimized cluster formation strategy that achieves 100% success rate:

**Key Design Decisions**:
1. **Parallel Pod Management**: All pods (primaries and secondaries) start simultaneously using `ParallelPodManagement`
2. **Minimum Primaries = 1**: Always set to 1, allowing the first pod to form the initial cluster
3. **No Secondary Delay**: Secondaries start immediately with primaries, discovering and joining the cluster
4. **PublishNotReadyAddresses**: Discovery service includes pods in endpoints even before they're ready

**Why This Works**:
- **Fast Cluster Formation**: First pod forms cluster immediately without waiting
- **Reliable Discovery**: All pods discover each other via Kubernetes endpoints
- **No Split Brain**: Single cluster forms naturally as pods join the first-formed cluster
- **Simplified Logic**: No complex timing or sequencing required

**Implementation Details**:
- StatefulSets use `PodManagementPolicy: ParallelPodManagement` (line 135 in `cluster.go`)
- Startup script sets `MIN_PRIMARIES=1` and `dbms.cluster.minimum_initial_system_primaries_count=${MIN_PRIMARIES}`
- Discovery service has `PublishNotReadyAddresses: true` for early pod discovery
- Raft timeouts: `dbms.cluster.raft.membership.join_timeout=10m` and `dbms.cluster.raft.binding_timeout=1d`

**TLS-Specific Optimizations**:
- **Trust All for Cluster SSL**: `dbms.ssl.policy.cluster.trust_all=true` prevents certificate validation issues during formation
- **Maintained Parallel Startup**: TLS doesn't change the pod management policy
- **No Special Delays**: TLS handshakes complete within normal formation timeouts
- **RBAC for Endpoints**: Operator has endpoints permission for proper discovery

**Service Architecture**:
- **Discovery Service**: ClusterIP service with `neo4j.com/clustering=true` label
- **Shared Services**: No per-pod services needed (matches Neo4j Helm chart pattern)
- **ClusterIP Type**: Discovery service is regular ClusterIP, not headless (deliberate for stability)

**RBAC Requirements**: The discovery ServiceAccount needs permissions to:
- `get`, `list`, `watch` services (to find matching services)
- `get`, `list`, `watch` endpoints (to resolve pod IPs behind services) **[CRITICAL]**

Without endpoints permission, Neo4j can discover services but cannot resolve individual pods, preventing cluster formation.

The operator automatically creates these RBAC resources for each cluster, including:
- ServiceAccount: `{cluster-name}-discovery`
- Role: `{cluster-name}-discovery` with services and endpoints permissions
- RoleBinding: `{cluster-name}-discovery`

### Configuration Validation

The validation framework (`internal/validation/`) ensures:
- Deprecated settings are identified and warned about
- Clustering settings are blocked in standalone deployments
- Memory settings are validated against container resources
- Required settings are present for the deployment type

For detailed configuration guidelines, see the [Configuration Best Practices Guide](../user_guide/guides/configuration_best_practices.md).
