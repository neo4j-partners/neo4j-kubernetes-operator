# Neo4jPlugin

This document provides a reference for the `Neo4jPlugin` Custom Resource Definition (CRD). This resource is used to install and manage plugins in both Neo4j Enterprise clusters and standalone deployments.

## Architecture Compatibility

The `Neo4jPlugin` CRD supports both deployment types:

- **Neo4jEnterpriseCluster**: Server-based architecture with StatefulSet named `{cluster-name}-server`
- **Neo4jEnterpriseStandalone**: Single-node deployments with StatefulSet named `{standalone-name}`

The plugin controller automatically detects the deployment type and applies the appropriate configuration.

## Implementation Overview

The `Neo4jPlugin` controller uses Neo4j's recommended `NEO4J_PLUGINS` environment variable approach for plugin installation:

1. **Environment Variable Configuration**: Plugins are configured via the `NEO4J_PLUGINS` environment variable in the Neo4j StatefulSet
2. **Automatic Download**: Neo4j automatically downloads and installs plugins on startup
3. **Dependency Handling**: Plugin dependencies are automatically included in the plugins list
4. **Configuration Management**: Plugin-specific configuration is added as `NEO4J_*` environment variables
5. **StatefulSet Restart**: The StatefulSet is updated with a rolling restart to apply plugin changes

This approach follows Neo4j's Docker plugin installation best practices and eliminates the need for external jobs or volume mounts.

## API Version

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `clusterRef` | `string` | Yes | Name of the target Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone |
| `name` | `string` | Yes | Plugin name (e.g., "apoc", "graph-data-science") |
| `version` | `string` | Yes | Plugin version to install |
| `enabled` | `boolean` | No | Whether the plugin is enabled (default: true) |
| `source` | `PluginSource` | No | Plugin source configuration (default: official) |
| `dependencies` | `[]PluginDependency` | No | Plugin dependencies |
| `config` | `map[string]string` | No | Plugin-specific configuration |
| `license` | `PluginLicense` | No | License configuration for commercial plugins |
| `security` | `PluginSecurity` | No | Security settings |
| `resources` | `PluginResources` | No | Resource requirements |

### PluginSource

| Field | Type | Description |
|-------|------|-------------|
| `type` | `string` | Source type: "official", "community", "custom", "url" |
| `registry` | `PluginRegistry` | Registry configuration for custom sources |
| `url` | `string` | Direct URL for "url" source type |
| `checksum` | `string` | Checksum for URL sources (format: "sha256:hash") |

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
| `securityPolicy` | `string` | Security policy: "open", "restricted" |
| `sandbox` | `boolean` | Enable sandbox mode |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | `string` | Current phase: "Pending", "Installing", "Ready", "Failed", "Waiting" |
| `message` | `string` | Human-readable status message |
| `lastUpdated` | `metav1.Time` | Last status update timestamp |
| `installedVersion` | `string` | Actually installed plugin version |
| `downloadJobName` | `string` | Name of the download job |
| `installJobName` | `string` | Name of the install job |

## Examples

### Cluster Plugin Example

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

  # Plugin configuration
  name: apoc
  version: "5.26.0"
  enabled: true

  # Plugin source - official Neo4j repository
  source:
    type: official

  # APOC-specific configuration
  config:
    "apoc.export.file.enabled": "true"
    "apoc.import.file.enabled": "true"
    "apoc.import.file.use_neo4j_config": "true"
```

### Standalone Plugin Example

Install Graph Data Science plugin on a Neo4jEnterpriseStandalone:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: standalone-gds-plugin
  namespace: default
spec:
  # References a Neo4jEnterpriseStandalone
  clusterRef: my-standalone

  # Plugin configuration
  name: graph-data-science
  version: "2.10.0"
  enabled: true

  # Plugin source - community repository
  source:
    type: community

  # Plugin dependencies (automatically included in NEO4J_PLUGINS)
  dependencies:
    - name: apoc
      versionConstraint: ">=5.26.0"
      optional: false

  # Plugin configuration
  config:
    "gds.enterprise.license_file": "/licenses/gds.license"

  # License configuration
  license:
    keySecret: gds-license-secret
    licenseFile: "/licenses/gds.license"

  # Security configuration
  security:
    allowedProcedures:
      - "gds.*"
      - "apoc.load.*"
    securityPolicy: "restricted"
    sandbox: true

  # Resource requirements
  resources:
    memoryLimit: "1Gi"
    cpuLimit: "500m"
    threadPoolSize: 4
```

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

## Plugin Installation Process

1. **Download Phase**: Plugin files are downloaded from the specified source
2. **Dependency Resolution**: Required dependencies are resolved and installed
3. **Installation Phase**: Plugin is installed to the Neo4j plugins directory
4. **Configuration**: Plugin-specific configuration is applied
5. **Restart**: Neo4j pods are restarted to load the new plugin
6. **Verification**: Plugin installation is verified

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

## How Plugin Installation Works

When you create a `Neo4jPlugin` resource, the controller performs the following steps:

1. **Validates the target deployment** - Checks that the referenced cluster or standalone exists and is ready
2. **Collects plugins** - Gathers the main plugin and all its dependencies into a single list
3. **Updates StatefulSet** - Modifies the Neo4j StatefulSet with:
   - `NEO4J_PLUGINS` environment variable containing the plugin list (e.g., `["apoc", "graph-data-science"]`)
   - `NEO4J_*` environment variables for plugin configuration (e.g., `NEO4J_APOC_EXPORT_FILE_ENABLED=true`)
4. **Triggers rolling restart** - StatefulSet pods restart with new plugin configuration
5. **Waits for readiness** - Ensures all pods are running and Neo4j is responsive
6. **Marks as Ready** - Sets plugin status to "Ready" when installation is complete

### Environment Variables Applied

For this APOC plugin configuration:
```yaml
config:
  "apoc.export.file.enabled": "true"
  "apoc.import.file.enabled": "true"
```

The controller adds these environment variables to the Neo4j container:
```yaml
env:
- name: NEO4J_PLUGINS
  value: '["apoc"]'
- name: NEO4J_APOC_EXPORT_FILE_ENABLED
  value: 'true'
- name: NEO4J_APOC_IMPORT_FILE_ENABLED
  value: 'true'
```

## Best Practices

1. **Version Pinning**: Always specify exact plugin versions for reproducible deployments
2. **Dependency Management**: Explicitly declare plugin dependencies
3. **Security**: Use sandbox mode and restrict procedures for untrusted plugins
4. **Resource Limits**: Set appropriate resource limits for plugin operations
5. **Testing**: Test plugins in development environments before production deployment

## Troubleshooting

Common issues and solutions:

- **Plugin not loading**: Check Neo4j logs for plugin errors
- **Dependency conflicts**: Verify compatible plugin versions
- **Resource constraints**: Ensure sufficient memory/CPU for plugin operations
- **Network issues**: Verify connectivity to plugin sources
