# Plugin Management Guide

## Overview

The Neo4j Kubernetes Operator provides comprehensive plugin management capabilities, allowing you to dynamically install, configure, and manage Neo4j plugins including APOC, Graph Data Science (GDS), and custom plugins. This feature supports the full plugin lifecycle with dependency resolution, security configurations, and monitoring.

## Features

- **Official Plugin Support**: APOC, GDS, and other Neo4j official plugins
- **Community Plugins**: Support for community-developed plugins
- **Custom Plugins**: Deploy your own JAR files
- **Dependency Resolution**: Automatic handling of plugin dependencies
- **Security Configuration**: Sandboxing and security constraints
- **Hot Deployment**: Install plugins without cluster restart
- **Version Management**: Plugin versioning and updates
- **Licensing**: Automatic license compliance checking

## Prerequisites

- Neo4j Enterprise Edition cluster
- Sufficient storage for plugin files
- Network access to plugin repositories
- Appropriate RBAC permissions

## Basic Configuration

### 1. APOC Plugin Installation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
  namespace: default
spec:
  clusterRef: my-neo4j-cluster
  name: apoc
  version: "5.26.0"
  enabled: true

  source:
    type: official

  config:
    apoc.export.file.enabled: "true"
    apoc.import.file.enabled: "true"
    apoc.import.file.use_neo4j_config: "true"

  security:
    sandbox: restricted
    allowedProcedures:
    - "apoc.create.*"
    - "apoc.load.*"
    - "apoc.export.*"
```

### 2. Graph Data Science Plugin

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: gds-plugin
  namespace: default
spec:
  clusterRef: my-neo4j-cluster
  name: graph-data-science
  version: "2.6.0"
  enabled: true

  source:
    type: official

  config:
    gds.enterprise.license_file: "/licenses/gds.license"

  resources:
    requests:
      memory: "2Gi"
      cpu: "500m"
    limits:
      memory: "8Gi"
      cpu: "2000m"

  licensing:
    type: enterprise
    licenseSecret: gds-license-secret
```

### 3. Custom Plugin Installation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: custom-plugin
  namespace: default
spec:
  clusterRef: my-neo4j-cluster
  name: my-custom-plugin
  version: "1.0.0"
  enabled: true

  source:
    type: custom
    url: "https://my-repo.com/plugins/my-plugin-1.0.0.jar"
    checksum: sha256:abcdef123456...

  dependencies:
  - name: apoc
    version: ">=5.26.0"

  config:
    my.plugin.property: "value"

  security:
    sandbox: strict
    signatureVerification: true
```

## Advanced Configuration

### Multi-Plugin Setup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: plugin-cluster
  namespace: default
spec:
  edition: enterprise
  image:
    repo: neo4j
    tag: 5.26-enterprise

  plugins:
    enabled: true
    autoUpdate: false

    # Global plugin configuration
    global:
      security:
        sandbox: restricted
        allowUnsigned: false

      resources:
        pluginDirectory: /var/lib/neo4j/plugins
        maxPluginSize: "100Mi"

    # Plugin specifications
    plugins:
    - name: apoc
      version: "5.26.0"
      source:
        type: official
      config:
        apoc.export.file.enabled: "true"

    - name: graph-data-science
      version: "2.6.0"
      source:
        type: official
      licensing:
        licenseSecret: gds-license-secret

    - name: custom-analytics
      version: "1.2.0"
      source:
        type: custom
        url: "s3://my-plugins/analytics-1.2.0.jar"

  auth:
    adminSecret: neo4j-admin-secret
```

### Plugin Repository Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: plugin-repositories
  namespace: default
data:
  repositories.yaml: |
    repositories:
    - name: official
      url: "https://dist.neo4j.org/plugins"
      type: official

    - name: community
      url: "https://plugins.neo4j.com/community"
      type: community

    - name: private
      url: "https://private-repo.company.com/neo4j-plugins"
      type: private
      auth:
        secretRef: repo-credentials
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: community-plugin
spec:
  clusterRef: my-cluster
  name: community-awesome-plugin
  version: "1.0.0"

  source:
    type: repository
    repository: community
```

## Plugin Lifecycle Management

### Installation Process

1. **Plugin Discovery**: Operator resolves plugin location
2. **Dependency Check**: Validates dependencies and compatibility
3. **Security Validation**: Verifies signatures and security constraints
4. **Download**: Downloads plugin JAR file
5. **Installation**: Deploys plugin to cluster nodes
6. **Configuration**: Applies plugin-specific configuration
7. **Verification**: Confirms successful installation

### Update Procedure

```yaml
# Update plugin version
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
spec:
  clusterRef: my-neo4j-cluster
  name: apoc
  version: "5.27.0"  # Updated version

  updatePolicy:
    strategy: rolling  # or 'recreate'
    maxUnavailable: 1

  rollback:
    enabled: true
    timeout: "10m"
```

### Plugin Removal

```bash
# Remove specific plugin
kubectl delete neo4jplugin apoc-plugin

# Disable plugin without removal
kubectl patch neo4jplugin apoc-plugin --type='merge' -p='{"spec":{"enabled":false}}'
```

## Security Configuration

### Sandbox Modes

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: secure-plugin
spec:
  clusterRef: my-cluster
  name: apoc
  version: "5.26.0"

  security:
    # Sandbox levels: none, restricted, strict
    sandbox: strict

    # Allowed procedures and functions
    allowedProcedures:
    - "apoc.create.node"
    - "apoc.create.relationship"

    allowedFunctions:
    - "apoc.date.*"
    - "apoc.text.*"

    # Restrict file system access
    fileSystemAccess:
      allowedPaths:
      - "/import"
      - "/export"
      readOnly: true

    # Network access restrictions
    networkAccess:
      allowedHosts:
      - "api.company.com"
      allowedPorts:
      - 80
      - 443

    # Signature verification
    signatureVerification: true
    trustedSigners:
    - "neo4j-official"
    - "company-plugins"
```

### License Management

```yaml
# Create license secret
apiVersion: v1
kind: Secret
metadata:
  name: plugin-licenses
  namespace: default
type: Opaque
data:
  gds.license: <base64-encoded-license>
  custom.license: <base64-encoded-license>
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: licensed-plugin
spec:
  clusterRef: my-cluster
  name: graph-data-science

  licensing:
    type: enterprise
    licenseSecret: plugin-licenses
    licenseKey: gds.license

    # License validation
    validation:
      strict: true
      checkExpiry: true
      allowGracePeriod: "7d"
```

## Monitoring and Observability

### Plugin Status

```bash
# Check plugin status
kubectl get neo4jplugin

# Detailed plugin information
kubectl describe neo4jplugin apoc-plugin

# View plugin logs
kubectl logs -l app=neo4j-cluster -c neo4j | grep -i plugin
```

### Plugin Metrics

```yaml
# Enable plugin monitoring
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: monitored-plugin
spec:
  clusterRef: my-cluster
  name: apoc

  monitoring:
    enabled: true
    metrics:
      enabled: true
      prometheus:
        enabled: true
        path: "/metrics"
        port: 7474

    # Plugin-specific health checks
    healthChecks:
    - name: apoc-procedures
      cypherQuery: "CALL apoc.help('apoc') YIELD name RETURN count(name)"
      expectedResult: ">0"
      interval: "30s"
```

### Prometheus Metrics

```promql
# Plugin installation status
neo4j_plugin_installed{plugin="apoc"}

# Plugin procedure calls
neo4j_plugin_procedure_calls_total{plugin="apoc",procedure="apoc.create.node"}

# Plugin errors
neo4j_plugin_errors_total{plugin="apoc"}

# Plugin resource usage
neo4j_plugin_memory_usage_bytes{plugin="gds"}
```

## Advanced Use Cases

### Plugin Development Workflow

```yaml
# Development environment setup
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: dev-plugin
  namespace: development
spec:
  clusterRef: dev-cluster
  name: my-dev-plugin
  version: "dev-SNAPSHOT"

  source:
    type: custom
    # Use local development build
    url: "file:///dev/plugins/my-plugin-dev.jar"

  # Development settings
  development:
    hotReload: true
    debugMode: true

  security:
    sandbox: none  # Relaxed for development

  config:
    my.plugin.debug: "true"
    my.plugin.log.level: "DEBUG"
```

### CI/CD Integration

```yaml
# Pipeline configuration
apiVersion: tekton.dev/v1beta1
kind: Task
metadata:
  name: deploy-plugin
spec:
  steps:
  - name: build-plugin
    image: maven:3.8
    script: |
      mvn clean package -DskipTests

  - name: test-plugin
    image: neo4j:5.26-enterprise
    script: |
      # Run plugin tests

  - name: deploy-plugin
    image: bitnami/kubectl
    script: |
      kubectl apply -f - <<EOF
      apiVersion: neo4j.neo4j.com/v1alpha1
      kind: Neo4jPlugin
      metadata:
        name: $(params.plugin-name)
        namespace: $(params.namespace)
      spec:
        clusterRef: $(params.cluster)
        name: $(params.plugin-name)
        version: $(params.version)
        source:
          type: custom
          url: $(params.plugin-url)
      EOF
```

### Multi-Environment Plugin Management

```yaml
# Base plugin configuration
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- base-plugin.yaml

patchesStrategicMerge:
- env-specific-config.yaml

# Environment-specific overrides
---
# env-specific-config.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
spec:
  config:
    apoc.export.file.enabled: "false"  # Disabled in production
    apoc.import.file.enabled: "false"

  security:
    sandbox: strict  # Stricter in production
```

## Troubleshooting

### Common Issues

#### Plugin Installation Fails

**Symptoms**: Plugin shows as "Failed" status

**Solutions**:

1. Check plugin compatibility:

   ```bash
   kubectl describe neo4jplugin <plugin-name>
   ```

2. Verify network connectivity:

   ```bash
   kubectl exec -it neo4j-cluster-0 -- curl -I <plugin-url>
   ```

3. Check resource constraints:

   ```bash
   kubectl describe pod neo4j-cluster-0
   ```

#### Plugin Not Loading

**Symptoms**: Plugin installed but procedures unavailable

**Solutions**:

1. Check plugin directory:

   ```bash
   kubectl exec -it neo4j-cluster-0 -- ls -la /var/lib/neo4j/plugins
   ```

2. Verify plugin configuration:

   ```bash
   kubectl exec -it neo4j-cluster-0 -- grep -i plugin /var/lib/neo4j/conf/neo4j.conf
   ```

3. Check Neo4j logs:

   ```bash
   kubectl logs neo4j-cluster-0 | grep -i plugin
   ```

#### Dependency Conflicts

**Symptoms**: Plugin dependencies cannot be resolved

**Solutions**:

1. Check dependency tree:

   ```bash
   kubectl get neo4jplugin -o yaml | grep -A 10 dependencies
   ```

2. Update plugin versions:

   ```yaml
   dependencies:
   - name: apoc
     version: ">=5.26.0,<6.0.0"  # Version range
   ```

### Debugging Commands

```bash
# View all plugins
kubectl get neo4jplugin -o wide

# Plugin events
kubectl get events --field-selector involvedObject.kind=Neo4jPlugin

# Plugin operator logs
kubectl logs -f deployment/neo4j-operator-controller-manager -n neo4j-operator-system | grep plugin

# Check plugin procedures in Neo4j
kubectl exec -it neo4j-cluster-0 -- cypher-shell -u neo4j -p password \
  "CALL dbms.procedures() YIELD name WHERE name STARTS WITH 'apoc' RETURN name LIMIT 10"
```

## Best Practices

### 1. Version Management

```yaml
# Use specific versions, not latest
spec:
  name: apoc
  version: "5.26.0"  # Not "latest"

  updatePolicy:
    autoUpdate: false  # Manual updates for stability
```

### 2. Security Configuration

```yaml
# Always use appropriate sandbox levels
security:
  sandbox: restricted  # Default for most plugins
  signatureVerification: true

  # Principle of least privilege
  allowedProcedures:
  - "apoc.load.json"  # Only what's needed
```

### 3. Resource Management

```yaml
# Set appropriate resource limits
resources:
  requests:
    memory: "512Mi"
    cpu: "250m"
  limits:
    memory: "2Gi"
    cpu: "1000m"
```

### 4. Testing Strategy

- Test plugins in development environment first
- Use dependency ranges for flexibility
- Implement plugin-specific health checks
- Monitor plugin performance impact

### 5. Documentation

```yaml
metadata:
  annotations:
    neo4j.neo4j.com/plugin-description: "APOC for data import/export"
    neo4j.neo4j.com/plugin-documentation: "https://neo4j.com/docs/apoc"
    neo4j.neo4j.com/plugin-maintainer: "platform-team@company.com"
```

## Integration Examples

### With GitOps

```yaml
# ArgoCD Application
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: neo4j-plugins
spec:
  source:
    repoURL: https://github.com/company/neo4j-plugins
    path: plugins
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: neo4j-production
```

### With Helm

```yaml
# values.yaml
plugins:
  enabled: true

  apoc:
    enabled: true
    version: "5.26.0"
    config:
      apoc.export.file.enabled: true

  gds:
    enabled: true
    version: "2.6.0"
    license:
      secretName: gds-license
```

## Performance Considerations

### Plugin Resource Impact

1. **Memory Usage**: Plugins consume additional heap space
2. **CPU Overhead**: Procedure calls have execution overhead
3. **Storage**: Plugin JARs require disk space
4. **Network**: Plugin downloads affect startup time

### Optimization Tips

```yaml
# Optimize plugin loading
spec:
  config:
    # Lazy loading for better startup time
    dbms.jvm.additional: "-XX:+UnlockExperimentalVMOptions"

  # Resource allocation
  resources:
    requests:
      memory: "4Gi"  # Increase heap for plugins
```

## Next Steps

- [Query Monitoring Guide](./query-monitoring-guide.md)

- [Performance Tuning Guide](./performance-tuning-guide.md)
- [Backup and Restore Guide](../backup-restore-guide.md)
