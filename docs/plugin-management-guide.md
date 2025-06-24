# Plugin Management Guide

This guide explains how to manage Neo4j plugins in your Neo4j Enterprise clusters using the Kubernetes operator.

## Overview

The Neo4j Kubernetes operator supports automatic installation and management of Neo4j plugins through init containers. Plugins are downloaded and installed before the main Neo4j container starts, ensuring they are available when Neo4j initializes.

## Supported Plugin Types

The operator supports installing any Neo4j plugin that can be downloaded as a JAR or ZIP file, including:

- **APOC** - Awesome Procedures On Cypher
- **Graph Data Science (GDS)** - Graph algorithms and machine learning
- **Neo4j Streams** - Kafka integration
- **Custom plugins** - Any compatible Neo4j plugin

## Configuration

### Basic Plugin Configuration

Add plugins to your `Neo4jEnterpriseCluster` resource:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-neo4j-cluster
spec:
  image:
    repo: neo4j/neo4j
    tag: 5.15.0-enterprise
  topology:
    primaries: 3
  storage:
    className: fast-ssd
    size: 10Gi
  plugins:
    - name: apoc
      version: 5.15.0
      enabled: true
      source:
        url: https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar
    - name: graph-data-science
      version: 2.4.0
      enabled: true
      source:
        url: https://graphdatascience.ninja/neo4j-graph-data-science-2.4.0.zip
```

### Plugin Configuration Options

Each plugin supports the following configuration:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique name for the plugin |
| `version` | string | Yes | Plugin version |
| `enabled` | boolean | No | Whether to install the plugin (default: true) |
| `source.url` | string | Yes | URL to download the plugin from |
| `config` | map[string]string | No | Plugin-specific configuration |

### Plugin Configuration Example

```yaml
plugins:
  - name: apoc
    version: 5.15.0
    enabled: true
    source:
      url: https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar
    config:
      apoc.import.file.enabled: "true"
      apoc.export.file.enabled: "true"
      apoc.import.file.use_neo4j_config: "false"
```

## How It Works

1. **Init Container Creation**: For each enabled plugin, the operator creates an init container named `install-plugin-{plugin-name}`.

2. **Plugin Download**: The init container downloads the plugin from the specified URL using `curl`.

3. **Plugin Installation**: The plugin is saved to the `/plugins` directory, which is mounted as a shared volume.

4. **Neo4j Startup**: When Neo4j starts, it automatically loads plugins from the `/plugins` directory.

## Common Plugins

### APOC (Awesome Procedures On Cypher)

APOC is a collection of useful procedures and functions for Neo4j.

```yaml
plugins:
  - name: apoc
    version: 5.15.0
    enabled: true
    source:
      url: https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar
    config:
      apoc.import.file.enabled: "true"
      apoc.export.file.enabled: "true"
      apoc.import.file.use_neo4j_config: "false"
```

### Graph Data Science (GDS)

GDS provides graph algorithms and machine learning capabilities.

```yaml
plugins:
  - name: graph-data-science
    version: 2.4.0
    enabled: true
    source:
      url: https://graphdatascience.ninja/neo4j-graph-data-science-2.4.0.zip
    config:
      gds.license: "your-license-key"
```

### Neo4j Streams

Neo4j Streams provides Kafka integration.

```yaml
plugins:
  - name: neo4j-streams
    version: 5.15.0
    enabled: true
    source:
      url: https://github.com/neo4j-contrib/neo4j-streams/releases/download/5.15.0/neo4j-streams-5.15.0.jar
```

## Troubleshooting

### Plugin Installation Failures

If plugin installation fails, check the init container logs:

```bash
kubectl logs <pod-name> -c install-plugin-<plugin-name>
```

Common issues:
- **Network connectivity**: Ensure the pod can reach the plugin URL
- **Invalid URL**: Verify the plugin URL is correct and accessible
- **Plugin compatibility**: Ensure the plugin version is compatible with your Neo4j version

### Plugin Loading Issues

If plugins don't load in Neo4j, check:

1. **Neo4j logs**: Look for plugin loading errors
2. **Plugin directory**: Verify plugins are in `/plugins`
3. **Plugin compatibility**: Check Neo4j and plugin version compatibility

### Disabling Plugins

To disable a plugin without removing it from the configuration:

```yaml
plugins:
  - name: apoc
    version: 5.15.0
    enabled: false  # Plugin will not be installed
    source:
      url: https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar
```

## Best Practices

1. **Version Compatibility**: Always use plugin versions compatible with your Neo4j version
2. **Reliable URLs**: Use stable, reliable URLs for plugin downloads
3. **Configuration**: Use plugin-specific configuration to customize behavior
4. **Testing**: Test plugin installations in a development environment first
5. **Monitoring**: Monitor plugin installation and loading in production

## Security Considerations

- **URL Validation**: Ensure plugin URLs are from trusted sources
- **Network Policies**: Configure network policies to allow plugin downloads
- **Plugin Permissions**: Some plugins may require additional permissions or configuration

## Migration

When upgrading Neo4j or plugins:

1. Update the plugin version in your cluster configuration
2. The operator will automatically reinstall plugins with the new version
3. Test the new plugin version in a development environment first
4. Monitor the upgrade process and verify plugin functionality
