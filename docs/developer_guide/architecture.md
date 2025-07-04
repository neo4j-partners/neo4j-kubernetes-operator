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

The main controller (`internal/controller/neo4jenterprisecluster_controller.go`) includes several key architectural components:

#### Performance Optimizations
- **Efficient Reconciliation**: Optimized from ~18,000 to ~34 reconciliations per minute
- **Smart Status Updates**: Only updates status when cluster state actually changes
- **ConfigMap Debouncing**: 2-minute debounce mechanism prevents restart loops

#### Core Components
- **ConfigMap Manager** (`internal/controller/configmap_manager.go`): Handles Neo4j configuration with hash-based change detection
- **Scaling Status Manager** (`internal/controller/scaling_status_manager.go`): Manages autoscaling status and operations
- **Topology Scheduler**: Handles pod placement and anti-affinity rules

### Other Controllers

- **Neo4jDatabase Controller**: Manages database lifecycle within clusters
- **Neo4jBackup/Restore Controllers**: Handle backup and restore operations
- **Neo4jPlugin Controller**: Manages Neo4j plugin installation and configuration

## Custom Resource Definitions (CRDs)

The operator defines a set of CRDs to represent Neo4j resources. The Go type definitions are located in `api/v1alpha1/`.

### Enhanced CRD Features
- **Resource Validation**: Built-in validation for resource limits and Neo4j configuration
- **Status Conditions**: Comprehensive status reporting with detailed conditions
- **Autoscaling Support**: HPA integration with Neo4j-specific metrics

## Validation Framework

The operator uses a comprehensive validation framework (`internal/validation/`) to ensure resource correctness:

### Validation Components
- **Cluster Validator** (`cluster_validator.go`): Validates cluster configuration and topology
- **Memory Validator** (`memory_validator.go`): Ensures Neo4j memory settings are within container limits
- **Resource Validator** (`resource_validator.go`): Validates CPU, memory, and storage allocation

### Validation Features
- **Proactive Validation**: Catches configuration errors before deployment
- **Resource Recommendations**: Suggests optimal resource allocation
- **Memory Ratio Validation**: Ensures proper heap/page cache ratios

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
- **HPA Integration**: Horizontal Pod Autoscaler support
- **Network Policies**: Pod-to-pod communication security
- **Service Mesh**: Istio/Linkerd compatibility
- **Ingress Controllers**: External traffic routing

## Extensibility

The operator is designed for extensibility:

- **Plugin System**: Support for Neo4j plugin management
- **Custom Metrics**: Extensible monitoring framework
- **Webhook Integration**: Admission webhook support
- **Event Handlers**: Pluggable event handling system
