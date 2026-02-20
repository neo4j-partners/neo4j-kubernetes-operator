# Neo4jPlugin API Reference

The `Neo4jPlugin` Custom Resource Definition (CRD) provides automated plugin installation and management for both Neo4j Enterprise clusters and standalone deployments.

## Overview

- **API Version**: `neo4j.neo4j.com/v1alpha1`
- **Kind**: `Neo4jPlugin`
- **Target Deployments**: Both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`
- **Installation Method**: Neo4j's `NEO4J_PLUGINS` environment variable approach
- **Supported Plugins**: APOC, Graph Data Science, Bloom, GraphQL, GenAI, N10s, and custom plugins
- **Automatic Configuration**: Plugin-specific settings and security policies

## Architecture

**Universal Compatibility**: The `Neo4jPlugin` CRD works seamlessly with both deployment architectures:

- **Cluster Support**: Updates `{cluster-name}-server` StatefulSet with plugin configuration
- **Standalone Support**: Updates `{standalone-name}` StatefulSet with plugin configuration
- **Automatic Detection**: Controller automatically identifies target deployment type
- **Rolling Updates**: Triggers controlled restarts to apply plugin changes
- **Environment Variable Method**: Uses Neo4j's recommended `NEO4J_PLUGINS` installation approach

## Related Resources

- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) - Target cluster deployments
- [`Neo4jEnterpriseStandalone`](neo4jenterprisestandalone.md) - Target standalone deployments
- [`Neo4jDatabase`](neo4jdatabase.md) - Create databases that use plugin functionality
- [Plugin Examples](../../examples/plugins/README.md) - Detailed usage examples

## Plugin Installation Process

The `Neo4jPlugin` controller implements Neo4j's recommended installation approach:

### Installation Steps

1. **Target Discovery**: Automatically detects whether `clusterRef` points to a cluster or standalone
2. **Plugin Collection**: Gathers main plugin and all dependencies into a unified list
3. **Environment Variable Setup**: Configures `NEO4J_PLUGINS` with plugin names and versions
4. **Configuration Application**: Adds plugin-specific settings as `NEO4J_*` environment variables
5. **StatefulSet Update**: Patches the target StatefulSet with new configuration
6. **Rolling Restart**: Triggers controlled pod restarts to apply changes
7. **Verification**: Confirms plugin installation and updates status

### Environment Variable Mapping

**Example Configuration (APOC)**:
```yaml
config:
  "apoc.export.file.enabled": "true"
  "apoc.import.file.enabled": "true"
```

**Applied Environment Variables (APOC)**:
```yaml
env:
- name: NEO4J_PLUGINS
  value: '["apoc"]'
- name: NEO4J_APOC_EXPORT_FILE_ENABLED
  value: 'true'
- name: NEO4J_APOC_IMPORT_FILE_ENABLED
  value: 'true'
```

**Notes**:
- APOC settings are applied via environment variables in Neo4j 5.26+
- Bloom/GDS/GenAI settings are applied via ConfigMap (standalone) or runtime configuration (cluster)
- Automatic dependency resolution and security defaults are applied as needed

## API Version

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `clusterRef` | `string` | ✅ | Name of target Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone |
| `name` | `string` | ✅ | Plugin name (e.g., "apoc", "graph-data-science") |
| `version` | `string` | ✅ | Plugin version to install (must match Neo4j version compatibility) |
| `enabled` | `boolean` | ❌ | Enable the plugin (default: `true`) |
| `source` | [`PluginSource`](#pluginsource) | ❌ | Plugin source configuration (default: official repository) |
| `dependencies` | [`[]PluginDependency`](#plugindependency) | ❌ | Plugin dependencies (automatically resolved) |
| `config` | `map[string]string` | ❌ | Plugin-specific configuration (becomes `NEO4J_*` env vars) |
| `license` | [`PluginLicense`](#pluginlicense) | ❌ | License configuration for commercial plugins |
| `security` | [`PluginSecurity`](#pluginsecurity) | ❌ | Security settings and procedure restrictions |
| `resources` | [`PluginResourceRequirements`](#pluginresourcerequirements) | ❌ | Resource requirements for plugin operations |

### PluginSource

| Field | Type | Description |
|-------|------|-------------|
| `type` | `string` | Source type: "official", "community", "custom", "url" |
| `registry` | `PluginRegistry` | Registry configuration for custom sources |
| `url` | `string` | Direct URL for "url" source type |
| `checksum` | `string` | Checksum for URL sources (format: "sha256:hash") |
| `authSecret` | `string` | Secret containing auth for private registries/URLs |

### PluginDependency

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Dependency plugin name |
| `versionConstraint` | `string` | Version constraint (e.g., ">=5.26.0") |
| `optional` | `boolean` | Whether dependency is optional |

### PluginSecurity

| Field | Type | Description |
|-------|------|-------------|
| `allowedProcedures` | `[]string` | List of allowed procedures/functions |
| `deniedProcedures` | `[]string` | List of denied procedures/functions |
| `securityPolicy` | `string` | Security policy: "open", "restricted" |
| `sandbox` | `boolean` | Enable sandbox mode |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current plugin conditions |
| `phase` | `string` | Current phase: `"Pending"`, `"Installing"`, `"Ready"`, `"Failed"`, `"Waiting"` |
| `message` | `string` | Human-readable status message |
| `installedVersion` | `string` | Actually installed plugin version |
| `installationTime` | `*metav1.Time` | When the plugin was successfully installed |
| `health` | [`*PluginHealth`](#pluginhealth) | Plugin health and performance information |
| `usage` | [`*PluginUsage`](#pluginusage) | Plugin usage statistics |
| `observedGeneration` | `int64` | Generation of the most recently observed spec |

### PluginHealth

Plugin health and performance metrics.

| Field | Type | Description |
|-------|------|-------------|
| `status` | `string` | Plugin health status |
| `lastHealthCheck` | `*metav1.Time` | Last health check timestamp |
| `errors` | `[]string` | Error messages from health checks |
| `performance` | [`*PluginPerformance`](#pluginperformance) | Performance metrics |

### PluginPerformance

Plugin performance statistics.

| Field | Type | Description |
|-------|------|-------------|
| `memoryUsage` | `string` | Current memory usage |
| `cpuUsage` | `string` | Current CPU usage |
| `executionCount` | `int64` | Number of procedure executions |
| `avgExecutionTime` | `string` | Average execution time |

### PluginUsage

Plugin usage analytics.

| Field | Type | Description |
|-------|------|-------------|
| `proceduresCalled` | `map[string]int64` | Count of procedure calls by name |
| `lastUsed` | `*metav1.Time` | Last time plugin was used |
| `usageFrequency` | `string` | Usage frequency classification |

## Examples

### APOC Plugin for Cluster

Install APOC plugin on a Neo4jEnterpriseCluster:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: cluster-apoc-plugin
  namespace: default
spec:
  # References a Neo4jEnterpriseCluster
  clusterRef: my-cluster

  # Plugin identification
  name: apoc
  version: "5.26.0"  # Must match Neo4j version
  enabled: true

  # Plugin source (official Neo4j repository)
  source:
    type: official

  # APOC-specific configuration (becomes NEO4J_APOC_* env vars)
  config:
    "apoc.export.file.enabled": "true"
    "apoc.import.file.enabled": "true"
    "apoc.import.file.use_neo4j_config": "true"
    "apoc.trigger.enabled": "true"

  # Security configuration
  security:
    allowedProcedures:
      - "apoc.*"
    securityPolicy: "open"
```

**Result**: Updates `my-cluster-server` StatefulSet with:
- `NEO4J_PLUGINS=["apoc"]`
- `NEO4J_APOC_EXPORT_FILE_ENABLED=true`
- `NEO4J_APOC_IMPORT_FILE_ENABLED=true`
- Security settings for APOC procedures

### Graph Data Science Plugin for Standalone

Install GDS plugin with dependencies on a Neo4jEnterpriseStandalone:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: standalone-gds-plugin
  namespace: default
spec:
  # References a Neo4jEnterpriseStandalone
  clusterRef: my-standalone

  # Plugin identification
  name: graph-data-science
  version: "2.10.0"
  enabled: true

  # Plugin source - official Neo4j repository
  source:
    type: official

  # Plugin dependencies (automatically included in installation)
  dependencies:
    - name: apoc
      versionConstraint: ">=5.26.0"
      optional: false

  # GDS-specific configuration
  config:
    "gds.enterprise.license_file": "/licenses/gds.license"
    "gds.procedure.allowlist": "gds.*"
    "gds.graph.store.max_size": "2GB"

  # License configuration for enterprise features
  license:
    keySecret: gds-license-secret
    licenseFile: "/licenses/gds.license"

  # Security configuration
  security:
    allowedProcedures:
      - "gds.*"
      - "apoc.load.*"  # APOC dependency procedures
    securityPolicy: "restricted"
    sandbox: false  # GDS requires full access

  # Resource requirements for GDS operations
  resources:
    memoryLimit: "2Gi"    # GDS needs substantial memory
    cpuLimit: "1"         # CPU for graph algorithms
    threadPoolSize: 8     # Parallel processing
```

**Result**: Updates `my-standalone` StatefulSet with:
- `NEO4J_PLUGINS=["apoc", "graph-data-science"]` (dependencies included)
- GDS-specific environment variables
- Security settings for both APOC and GDS procedures
- Enhanced resource allocation

### Custom Plugin Example

Install a plugin from a custom registry:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: custom-plugin
spec:
  clusterRef: my-cluster
  name: my-custom-plugin
  version: "1.0.0"

  # Custom registry source
  source:
    type: custom
    registry:
      url: "https://my-registry.example.com"
      authentication:
        secret: registry-credentials

  # Security settings
  security:
    allowedProcedures:
      - "custom.*"
    sandbox: true
```

### URL Plugin Example

Install a plugin directly from a URL:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: url-plugin
spec:
  clusterRef: my-cluster
  name: direct-download-plugin
  version: "2.0.0"

  # Direct URL source with checksum verification
  source:
    type: url
    url: "https://example.com/plugins/my-plugin-2.0.0.jar"
    checksum: "sha256:abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234"
```

## Supported Plugins

### Official Neo4j Plugins

| Plugin | Name | Description | Configuration Method | Automatic Security |
|--------|------|-------------|---------------------|-------------------|
| **APOC** | `apoc` | Awesome Procedures on Cypher | Environment Variables | ❌ Manual setup |
| **APOC Extended** | `apoc-extended` | Extended APOC procedures | Environment Variables | ❌ Manual setup |
| **Graph Data Science** | `graph-data-science` | Advanced graph algorithms | Neo4j Config + Security | ✅ Auto-configured |
| **Neo4j Streams** | `streams` | Kafka/Pulsar integration | Neo4j Config | ❌ Manual setup |
| **GraphQL** | `graphql` | GraphQL endpoint | Neo4j Config | ❌ Manual setup |

### Enterprise Plugins

| Plugin | Name | Description | License Required | Automatic Security |
|--------|------|-------------|------------------|-------------------|
| **Bloom** | `bloom` | Graph visualization | ✅ Commercial License | ✅ Auto-configured |
| **GenAI** | `genai` | AI/ML integration | ✅ Commercial License | ❌ Manual setup |

## Automatic Security Configuration

**New Feature**: Some plugins require specific security settings to function properly. The operator automatically applies these settings even when no user configuration is provided.

### Plugins with Automatic Security

**Bloom Plugin**:
- Automatically applies required security settings for proper operation
- No manual configuration needed for basic functionality
- Automatically configured settings:
  - `NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED=bloom.*`
  - `NEO4J_DBMS_SECURITY_HTTP_AUTH_ALLOWLIST=/,/browser.*,/bloom.*`
  - `NEO4J_SERVER_UNMANAGED_EXTENSION_CLASSES=com.neo4j.bloom.server=/bloom`

**Graph Data Science Plugin**:
- Automatically applies default security settings
- User security configuration can override defaults
- Automatically configured settings:
  - `NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED=gds.*,apoc.load.*`

### Examples with Automatic Security

**Bloom Plugin (Zero Configuration)**:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: bloom-plugin
spec:
  clusterRef: my-cluster
  name: bloom
  version: "2.15.0"
  # No config or security section needed
  # All required security settings applied automatically
```

**GDS Plugin with Custom Security**:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: gds-plugin
spec:
  clusterRef: my-cluster
  name: graph-data-science
  version: "2.10.0"
  # User security settings override automatic ones
  security:
    sandbox: true  # Uses allowlist instead of unrestricted
    allowedProcedures: ["gds.*", "apoc.load.*"]
    # Results in: NEO4J_DBMS_SECURITY_PROCEDURES_ALLOWLIST=gds.*,apoc.load.*
```

**Automatic vs Manual Security Configuration**:

| Configuration Type | Applies When | Environment Variables | Override Behavior |
|-------------------|--------------|----------------------|-------------------|
| **Automatic** | No user `security` section | Applied automatically | Overridden by user config |
| **User-Provided** | User defines `security` section | User settings + automatic defaults | User settings take precedence |
| **Mixed** | User partially configures security | Automatic + user settings merged | User settings override matching keys |

### Community Plugins

| Plugin | Name | Description | Configuration |
|--------|------|-------------|---------------|
| **Neo Semantics (N10s)** | `n10s` | RDF/ontology support | Neo4j Config |
| **Custom Plugins** | `custom` | User-defined plugins | Flexible |

### Plugin-Specific Configuration

**APOC (Environment Variables)**:
```yaml
config:
  "apoc.export.file.enabled": "true"
  "apoc.import.file.enabled": "true"
  "apoc.trigger.enabled": "true"
  "apoc.jobs.pool.num_threads": "4"
```

**Graph Data Science (Neo4j Config)**:
```yaml
config:
  "gds.enterprise.license_file": "/licenses/gds.license"
  "gds.graph.store.max_size": "8GB"
  "gds.procedure.allowlist": "gds.*"
security:
  allowedProcedures:
    - "gds.*"
    - "apoc.load.*"
```

**Bloom (Automatic Security Configuration)**:
```yaml
# Minimal configuration - security settings applied automatically
config:
  "dbms.bloom.license_file": "/licenses/bloom.license"
license:
  keySecret: bloom-license-secret
  licenseFile: "/licenses/bloom.license"
# Automatically applied by operator:
# - NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED=bloom.*
# - NEO4J_DBMS_SECURITY_HTTP_AUTH_ALLOWLIST=/,/browser.*,/bloom.*
# - NEO4J_SERVER_UNMANAGED_EXTENSION_CLASSES=com.neo4j.bloom.server=/bloom
```

## Plugin Status Phases

- **Pending**: Plugin resource created, waiting for processing
- **Waiting**: Waiting for deployment to be ready
- **Installing**: Plugin installation in progress
- **Ready**: Plugin successfully installed and active
- **Failed**: Plugin installation failed

## Supported Plugin Sources

### Official Repository
Neo4j's official plugin repository (recommended for production):
- APOC (Awesome Procedures On Cypher)
- Neo4j Streams
- Neo4j GraphQL

### Community Repository
Community-maintained plugins:
- Graph Data Science (GDS)
- Additional APOC extensions
- Third-party plugins

### Custom Registry
Private plugin registries with authentication support.

### Direct URL
Direct download from URLs with checksum verification.

## Installation Workflow

The `Neo4jPlugin` controller follows this comprehensive workflow:

### Phase 1: Validation and Discovery

1. **Target Validation**: Verifies `clusterRef` points to existing cluster or standalone
2. **Plugin Validation**: Checks plugin name, version compatibility, and source availability
3. **Dependency Analysis**: Resolves plugin dependencies and version constraints
4. **Conflict Detection**: Identifies conflicts with existing plugins

### Phase 2: Configuration Preparation

1. **Plugin Collection**: Assembles main plugin and dependencies into unified list
2. **Environment Variable Mapping**: Converts plugin config to `NEO4J_*` environment variables
3. **Security Configuration**: Applies plugin-specific security settings
4. **License Verification**: Validates commercial plugin licenses (if required)

### Phase 3: Deployment

1. **StatefulSet Update**: Patches target StatefulSet with plugin configuration
2. **Rolling Restart**: Initiates controlled pod restart sequence
3. **Health Monitoring**: Tracks pod restart progress and Neo4j startup
4. **Installation Verification**: Confirms plugin loading via Neo4j procedures

### Phase 4: Status and Monitoring

1. **Status Update**: Sets plugin phase to "Ready" and records installation time
2. **Health Tracking**: Monitors plugin performance and usage
3. **Error Handling**: Captures and reports installation failures
4. **Dependency Tracking**: Maintains dependency relationships

### Example Installation Timeline

```bash
# Plugin creation
kubectl apply -f apoc-plugin.yaml

# Phase progression
# 0s:  Phase: Pending
# 5s:  Phase: Installing (StatefulSet updated)
# 30s: Phase: Installing (pods restarting)
# 60s: Phase: Ready (plugin verified)
```

### Configuration Examples by Plugin Type

**APOC Plugin (Environment Variables)**:
```yaml
# Input configuration
config:
  "apoc.export.file.enabled": "true"
  "apoc.import.file.enabled": "true"
  "apoc.trigger.enabled": "true"

# Applied environment variables
env:
- name: NEO4J_PLUGINS
  value: '["apoc"]'
- name: NEO4J_APOC_EXPORT_FILE_ENABLED
  value: 'true'
- name: NEO4J_APOC_IMPORT_FILE_ENABLED
  value: 'true'
- name: NEO4J_APOC_TRIGGER_ENABLED
  value: 'true'
```

**Graph Data Science (Environment + Config)**:
```yaml
# Input configuration
config:
  "gds.enterprise.license_file": "/licenses/gds.license"
  "gds.graph.store.max_size": "4GB"
security:
  allowedProcedures: ["gds.*", "apoc.*"]

# Applied configuration
env:
- name: NEO4J_PLUGINS
  value: '["apoc", "graph-data-science"]'
- name: NEO4J_GDS_ENTERPRISE_LICENSE_FILE
  value: '/licenses/gds.license'
- name: NEO4J_GDS_GRAPH_STORE_MAX_SIZE
  value: '4GB'
- name: NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED
  value: 'gds.*,apoc.*'
```

**Multiple Plugins with Dependencies**:
```yaml
# Multiple Neo4jPlugin resources
# Result: Combined environment variables
env:
- name: NEO4J_PLUGINS
  value: '["apoc", "graph-data-science", "streams"]'
- name: NEO4J_APOC_EXPORT_FILE_ENABLED
  value: 'true'
- name: NEO4J_GDS_ENTERPRISE_LICENSE_FILE
  value: '/licenses/gds.license'
- name: NEO4J_STREAMS_SINK_TOPIC_CYPHER_NODES
  value: 'CREATE (n:Node {id: event.id})'
```

## Advanced Plugin Configurations

### Custom Plugin from URL

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: custom-plugin
spec:
  clusterRef: my-cluster
  name: my-custom-plugin
  version: "1.0.0"

  # Custom plugin source
  source:
    type: url
    url: "https://my-registry.example.com/plugins/my-plugin-1.0.0.jar"
    checksum: "sha256:abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234"
    authSecret: custom-registry-credentials

  # Custom configuration
  config:
    "custom.plugin.setting1": "value1"
    "custom.plugin.setting2": "value2"

  # Security restrictions
  security:
    allowedProcedures:
      - "custom.*"
    securityPolicy: "restricted"
    sandbox: true
```

### Plugin with Private Registry

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: private-registry-auth
type: Opaque
data:
  username: <base64-encoded-username>
  password: <base64-encoded-password>
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: private-plugin
spec:
  clusterRef: enterprise-cluster
  name: enterprise-plugin
  version: "2.0.0"

  source:
    type: custom
    registry:
      url: "https://private-registry.company.com"
      authSecret: private-registry-auth
      tls:
        insecureSkipVerify: false
        caSecret: private-registry-ca
```

### Production Plugin Setup with Monitoring

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: production-apoc
  labels:
    environment: production
    plugin-type: essential
spec:
  clusterRef: prod-cluster
  name: apoc
  version: "5.26.0"

  # Production configuration
  config:
    "apoc.export.file.enabled": "true"
    "apoc.import.file.enabled": "false"  # Disabled for security
    "apoc.trigger.enabled": "true"
    "apoc.jobs.pool.num_threads": "8"
    "apoc.spatial.geocode.provider": "osm"

  # Enhanced security
  security:
    allowedProcedures:
      - "apoc.export.*"
      - "apoc.trigger.*"
      - "apoc.periodic.*"
      - "apoc.meta.*"
    securityPolicy: "restricted"
    sandbox: false

  # Resource allocation
  resources:
    memoryLimit: "512Mi"
    cpuLimit: "200m"
    threadPoolSize: 8
```

### Multi-Plugin Setup for Analytics Workload

```yaml
# APOC Foundation
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: analytics-apoc
spec:
  clusterRef: analytics-cluster
  name: apoc
  version: "5.26.0"
  config:
    "apoc.export.file.enabled": "true"
    "apoc.import.file.enabled": "true"
    "apoc.periodic.enabled": "true"

---
# Graph Data Science for Analytics
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: analytics-gds
spec:
  clusterRef: analytics-cluster
  name: graph-data-science
  version: "2.10.0"
  dependencies:
    - name: apoc
      versionConstraint: "5.26.0"
      optional: false
  config:
    "gds.enterprise.license_file": "/licenses/gds.license"
    "gds.graph.store.max_size": "16GB"
    "gds.procedure.allowlist": "gds.*"
  license:
    keySecret: gds-enterprise-license
    licenseFile: "/licenses/gds.license"
  resources:
    memoryLimit: "8Gi"
    cpuLimit: "4"
    threadPoolSize: 16

---
# Neo4j Streams for Real-time Data
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: analytics-streams
spec:
  clusterRef: analytics-cluster
  name: streams
  version: "5.26.0"
  config:
    "streams.sink.enabled": "true"
    "streams.sink.topic.nodes": "graph-nodes"
    "streams.sink.topic.relationships": "graph-relationships"
    "kafka.bootstrap.servers": "kafka.analytics.svc.cluster.local:9092"
```

## Troubleshooting

### Common Issues and Solutions

**Plugin Not Loading**:
```bash
# Check plugin installation status
kubectl get neo4jplugin <plugin-name> -o yaml

# Verify Neo4j logs for plugin loading
kubectl logs <pod-name> -c neo4j | grep -i "plugin\|apoc\|gds"

# Check available procedures
kubectl exec <pod-name> -c neo4j -- \
  cypher-shell -u neo4j -p password "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc'"
```

**Dependency Conflicts**:
```bash
# Check for version mismatches
kubectl describe neo4jplugin <plugin-name>

# Common conflicts:
# - APOC version doesn't match Neo4j version
# - GDS requires specific APOC version
# - Multiple plugins trying to load same dependency
```

**Resource Constraints**:
```bash
# Check pod resource usage
kubectl top pod <pod-name> --containers

# Monitor during plugin installation
kubectl logs <pod-name> -c neo4j | grep -i "outofmemory\|heap"

# Common issues:
# - Insufficient memory for GDS operations
# - CPU limits too low for parallel plugin loading
```

**License Issues (Commercial Plugins)**:
```bash
# Verify license secret exists
kubectl get secret <license-secret> -o yaml

# Check license file mounting
kubectl exec <pod-name> -c neo4j -- ls -la /licenses/

# Verify license in Neo4j
kubectl exec <pod-name> -c neo4j -- \
  cypher-shell -u neo4j -p password "SHOW PROCEDURES YIELD name WHERE name CONTAINS 'bloom'"
```

**Network and Source Issues**:
```bash
# Test plugin source connectivity
kubectl run test-plugin --rm -it --image=curlimages/curl -- \
  curl -I "https://repo1.maven.org/maven2/org/neo4j/procedure/apoc/"

# Check custom registry access
kubectl get secret <registry-auth-secret> -o yaml
```

### Performance Monitoring

```bash
# Monitor plugin performance
kubectl get neo4jplugin <plugin-name> -o jsonpath='{.status.health.performance}'

# Check plugin usage statistics
kubectl get neo4jplugin <plugin-name> -o jsonpath='{.status.usage.proceduresCalled}'

# View plugin health status
kubectl describe neo4jplugin <plugin-name> | grep -A 10 "Health:"
```

## Best Practices

1. **Version Compatibility**: Always match plugin versions with Neo4j version
2. **Dependency Management**: Let the controller handle dependency resolution
3. **Resource Planning**: Allocate sufficient memory for plugin operations (especially GDS)
4. **Security Configuration**: Use appropriate procedure allowlists and security policies
5. **License Management**: Store commercial plugin licenses in secure secrets
6. **Installation Order**: Install base plugins (APOC) before dependent plugins (GDS)
7. **Monitoring**: Regularly check plugin health and performance metrics
8. **Updates**: Test plugin updates in development before production deployment
9. **Configuration**: Use environment variables for APOC, neo4j.conf for other plugins
10. **Troubleshooting**: Enable debug logging for plugin installation issues

For detailed plugin-specific guides, see:
- [Plugin Examples](../../examples/plugins/README.md)
- [Troubleshooting Guide](../user_guide/guides/troubleshooting.md)
- [Performance Tuning](../user_guide/guides/performance.md)
