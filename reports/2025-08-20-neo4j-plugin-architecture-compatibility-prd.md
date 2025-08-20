# Neo4j Plugin Architecture Compatibility PRD

**Document ID**: PRD-2025-08-20-001
**Created**: August 20, 2025
**Status**: Implemented
**Version**: 1.0

## Executive Summary

The Neo4j Kubernetes Operator's Plugin CRD (`Neo4jPlugin`) has been comprehensively updated to support the current server-based architecture and extended to work with both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone deployments. This PRD documents the architectural changes, implementation details, and compatibility improvements.

## Background

### Previous Architecture Issues
The original Neo4jPlugin implementation was designed for an older architecture and had several critical compatibility issues:

1. **Incorrect StatefulSet Naming**: Used `cluster.Name` instead of `cluster.Name + "-server"`
2. **No Standalone Support**: Only worked with Neo4jEnterpriseCluster deployments
3. **Wrong Resource References**: Incorrect PVC and pod label assumptions
4. **Client Connection Issues**: Didn't use appropriate Neo4j client methods for different deployment types

### Current Server-Based Architecture (August 2025)

**Unified Server Deployment**: Neo4j servers self-organize into primary/secondary roles within a single StatefulSet.

#### Neo4jEnterpriseCluster Architecture
```yaml
topology:
  servers: 3  # Single StatefulSet: {cluster-name}-server (replicas: 3)
```

**Resource Naming Patterns**:
- **StatefulSet**: `{cluster-name}-server`
- **Pods**: `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc.
- **Pod Labels**: `app.kubernetes.io/name=neo4j`, `app.kubernetes.io/instance={cluster-name}`
- **Services**:
  - Client: `{cluster-name}-client`
  - Headless: `{cluster-name}-server`
  - Discovery: `{cluster-name}-discovery`

#### Neo4jEnterpriseStandalone Architecture
```yaml
# Fixed single node deployment
```

**Resource Naming Patterns**:
- **StatefulSet**: `{standalone-name}`
- **Pods**: `{standalone-name}-0`
- **Pod Labels**: `app={standalone-name}`
- **Service**: `{standalone-name}-service`

## Product Requirements

### Functional Requirements

#### FR1: Dual Deployment Type Support
**Requirement**: Neo4jPlugin must support both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone deployments.

**Implementation**:
- Automatic deployment type detection via `getTargetDeployment()`
- Unified `DeploymentInfo` abstraction layer
- Type-specific handling for installation and configuration

**Acceptance Criteria**:
- ✅ Plugin can reference Neo4jEnterpriseCluster via `clusterRef`
- ✅ Plugin can reference Neo4jEnterpriseStandalone via `clusterRef`
- ✅ Automatic fallback detection (tries cluster first, then standalone)
- ✅ Clear error messages when deployment not found

#### FR2: Correct Architecture Compatibility
**Requirement**: Plugin controller must use correct resource naming patterns for current architecture.

**Implementation**:
- **StatefulSet Names**:
  - Cluster: `{name}-server`
  - Standalone: `{name}`
- **Pod Label Selectors**:
  - Cluster: `app.kubernetes.io/name=neo4j`, `app.kubernetes.io/instance={cluster-name}`
  - Standalone: `app={standalone-name}`

**Acceptance Criteria**:
- ✅ Plugin installation can restart StatefulSets correctly
- ✅ Plugin wait logic uses correct pod selectors
- ✅ Resource references match actual Kubernetes resources

#### FR3: Appropriate Neo4j Client Creation
**Requirement**: Plugin controller must use correct Neo4j client methods based on deployment type.

**Implementation**:
```go
if deployment.Type == "cluster" {
    client = neo4jclient.NewClientForEnterprise(cluster, k8sClient, adminSecret)
} else {
    client = neo4jclient.NewClientForEnterpriseStandalone(standalone, k8sClient, adminSecret)
}
```

**Acceptance Criteria**:
- ✅ Cluster deployments use `NewClientForEnterprise()`
- ✅ Standalone deployments use `NewClientForEnterpriseStandalone()`
- ✅ Authentication works correctly for both deployment types

#### FR4: Plugin Installation Lifecycle
**Requirement**: Complete plugin installation, configuration, and removal lifecycle.

**Implementation Phases**:
1. **Download**: Job-based plugin download from various sources
2. **Installation**: Copy plugin to Neo4j pods via shared volumes
3. **Restart**: Rolling restart of StatefulSet to load plugins
4. **Verification**: Verify plugin loaded via Neo4j client
5. **Configuration**: Apply plugin-specific configuration
6. **Removal**: Clean plugin removal and dependency cleanup

**Acceptance Criteria**:
- ✅ Plugin downloads from official, community, custom, and URL sources
- ✅ Plugin installation handles both deployment types
- ✅ Neo4j instances restart correctly to load plugins
- ✅ Plugin configuration applied via Neo4j client
- ✅ Plugin removal cleans up all resources

### Non-Functional Requirements

#### NFR1: Backward Compatibility
**Requirement**: Existing Neo4jEnterpriseCluster plugins must continue to work.

**Implementation**:
- Maintained `getTargetCluster()` method for backward compatibility
- Existing plugin definitions continue to work without changes

**Acceptance Criteria**:
- ✅ No breaking changes to existing plugin CRDs
- ✅ Existing cluster plugins work with updated controller

#### NFR2: Performance and Reliability
**Requirement**: Plugin operations must be efficient and reliable.

**Implementation**:
- Resource conflict retry logic with `retry.RetryOnConflict()`
- Proper timeout handling for long-running operations
- Job-based approach for plugin downloads and installations

**Acceptance Criteria**:
- ✅ Plugin installation handles resource conflicts gracefully
- ✅ Timeout limits prevent hanging operations
- ✅ Failed installations can be retried

#### NFR3: Observability
**Requirement**: Plugin operations must be properly observable.

**Implementation**:
- Comprehensive status updates throughout plugin lifecycle
- Structured logging with deployment type context
- Kubernetes events for major plugin lifecycle events

**Acceptance Criteria**:
- ✅ Plugin status reflects current installation state
- ✅ Logs include deployment type and context information
- ✅ Status conditions provide actionable information

## Technical Architecture

### Controller Architecture

```
Neo4jPluginReconciler
├── getTargetDeployment()     # Detects cluster or standalone
├── installPlugin()           # Unified plugin installation
├── configurePlugin()         # Type-aware configuration
├── performPluginInstallation()  # Core installation logic
├── copyPluginToDeployment()  # Plugin file management
├── restartNeo4jInstances()   # StatefulSet restart
├── waitForDeploymentReady()  # Type-aware readiness check
└── removePluginFromDeployment()  # Cleanup logic
```

### DeploymentInfo Abstraction

```go
type DeploymentInfo struct {
    Object    client.Object  // Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone
    Type      string         // "cluster" or "standalone"
    Name      string         // Deployment name
    Namespace string         // Kubernetes namespace
    IsReady   bool          // Ready state
}
```

### Helper Functions

```go
// Resource naming helpers
getStatefulSetName(deployment *DeploymentInfo) string
getPodLabels(deployment *DeploymentInfo) map[string]string
getExpectedReplicas(deployment *DeploymentInfo) int
getPluginsPVCName(deployment *DeploymentInfo) string
```

## Plugin Source Types

### Official Repository
```yaml
source:
  type: official
```
- Downloads from Neo4j's official plugin repository
- URL pattern: `https://dist.neo4j.org/plugins/{name}/{version}/{name}-{version}.jar`

### Community Repository
```yaml
source:
  type: community
```
- Downloads from Maven Central or GitHub releases
- Fallback mechanism for multiple sources

### Custom Repository
```yaml
source:
  type: custom
  registry:
    url: https://my-repo.example.com
    authSecret: repo-credentials
    tls:
      insecureSkipVerify: false
      caSecret: ca-cert-secret
```

### Direct URL
```yaml
source:
  type: url
  url: https://github.com/example/plugin/releases/download/v1.0.0/plugin.jar
  checksum: sha256:abcd1234...
  authSecret: download-credentials
```

## Security Model

### Plugin Security Configuration
```yaml
security:
  allowedProcedures:
    - "apoc.*"
    - "gds.*"
  deniedProcedures:
    - "apoc.import.file"
  sandbox: true
  securityPolicy: "restricted"
```

### Authentication and Authorization
- Uses existing Neo4j admin credentials from `neo4j-admin-secret`
- Supports custom authentication for plugin repositories
- TLS verification for secure plugin downloads

## Plugin Lifecycle States

```
Plugin Lifecycle:
Pending → Installing → Configuring → Ready
                    ↓
                  Failed
```

### Status Phases
- **Pending**: Plugin created, waiting for target deployment
- **Installing**: Downloading and installing plugin
- **Configuring**: Applying plugin configuration
- **Ready**: Plugin installed and configured successfully
- **Failed**: Installation or configuration failed

### Condition Types
- **Ready**: Plugin is ready for use
- **Installing**: Plugin installation in progress
- **ConfigurationApplied**: Plugin configuration successful

## Dependencies and Plugin Management

### Dependency Resolution
```yaml
dependencies:
  - name: apoc
    versionConstraint: ">=5.26.0"
    optional: false
  - name: graph-algorithms
    versionConstraint: "^1.10.0"
    optional: true
```

### Dependency Installation
- Creates child Neo4jPlugin resources for dependencies
- Waits for dependency readiness before installing main plugin
- Handles optional vs required dependencies

## Monitoring and Observability

### Metrics (Future Enhancement)
- Plugin installation success/failure rates
- Plugin installation duration
- Plugin resource usage

### Health Checks
```yaml
status:
  health:
    status: healthy
    lastHealthCheck: "2025-08-20T10:30:00Z"
    performance:
      memoryUsage: "128Mi"
      cpuUsage: "50m"
      executionCount: 1250
```

### Usage Statistics
```yaml
status:
  usage:
    proceduresCalled:
      "apoc.create.node": 450
      "apoc.load.json": 123
    lastUsed: "2025-08-20T10:25:00Z"
    usageFrequency: "high"
```

## Deployment Examples

### Cluster Plugin Example
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: cluster-apoc-plugin
spec:
  clusterRef: my-cluster  # References Neo4jEnterpriseCluster
  name: apoc
  version: "5.26.0"
  enabled: true
  source:
    type: official
  config:
    "apoc.export.file.enabled": "true"
  security:
    allowedProcedures: ["apoc.*"]
    sandbox: false
```

### Standalone Plugin Example
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: standalone-gds-plugin
spec:
  clusterRef: my-standalone  # References Neo4jEnterpriseStandalone
  name: graph-data-science
  version: "2.10.0"
  enabled: true
  source:
    type: community
  dependencies:
    - name: apoc
      versionConstraint: ">=5.26.0"
  license:
    keySecret: gds-license-secret
  security:
    allowedProcedures: ["gds.*"]
    sandbox: true
```

## Implementation Status

### Completed Features ✅
- [x] Dual deployment type support (cluster + standalone)
- [x] Correct StatefulSet naming patterns
- [x] Type-appropriate Neo4j client creation
- [x] Plugin installation lifecycle
- [x] Dependency management
- [x] Security configuration
- [x] Plugin removal and cleanup
- [x] Status reporting and observability
- [x] Multiple plugin source types
- [x] Resource conflict handling
- [x] Backward compatibility
- [x] Documentation and examples

### Future Enhancements 🔮
- [ ] Plugin health monitoring and automatic restart
- [ ] Plugin resource usage metrics
- [ ] Plugin performance optimization
- [ ] Plugin versioning and upgrade strategies
- [ ] Plugin marketplace integration
- [ ] Advanced dependency conflict resolution

## Testing Strategy

### Unit Tests
- Controller logic testing with mocked Kubernetes clients
- Helper function validation
- Error handling scenarios

### Integration Tests
- End-to-end plugin installation on cluster deployments
- End-to-end plugin installation on standalone deployments
- Plugin dependency resolution testing
- Plugin removal and cleanup testing

### Manual Testing Scenarios
1. **Basic Plugin Installation**
   - Deploy cluster, install APOC plugin, verify functionality
   - Deploy standalone, install GDS plugin, verify functionality

2. **Plugin Dependencies**
   - Install plugin with required dependencies
   - Install plugin with optional dependencies
   - Test dependency failure scenarios

3. **Plugin Sources**
   - Test official repository downloads
   - Test community repository downloads
   - Test custom repository with authentication
   - Test direct URL downloads with checksums

4. **Error Scenarios**
   - Target deployment not found
   - Plugin download failures
   - Neo4j connection failures
   - Resource conflicts during installation

## Risk Assessment

### High Risk Areas
- **Plugin Compatibility**: Plugins may not be compatible with specific Neo4j versions
- **Resource Conflicts**: Concurrent plugin installations on same deployment
- **Network Dependencies**: Plugin downloads require internet access

### Mitigation Strategies
- **Version Validation**: Validate plugin compatibility before installation
- **Resource Locking**: Implement plugin installation queuing per deployment
- **Offline Support**: Support for air-gapped environments with custom repositories

## Success Metrics

### Technical Metrics
- **Installation Success Rate**: >95% plugin installations succeed
- **Installation Time**: <5 minutes for typical plugin installations
- **Error Recovery**: Failed installations can be retried successfully

### User Experience Metrics
- **API Compatibility**: 100% backward compatibility with existing plugins
- **Documentation Coverage**: Complete examples for all supported scenarios
- **Error Clarity**: Clear, actionable error messages for all failure modes

## Conclusion

The Neo4jPlugin architecture compatibility update successfully modernizes the plugin system to work with the current server-based architecture while extending support to standalone deployments. This implementation provides a robust, scalable foundation for Neo4j plugin management in Kubernetes environments.

The dual deployment support ensures that both cluster and standalone users can benefit from the rich Neo4j plugin ecosystem, while the improved architecture compatibility resolves previous installation and management issues.

---

**Document Maintainers**: Neo4j Kubernetes Operator Team
**Review Cycle**: Quarterly
**Next Review**: November 20, 2025
