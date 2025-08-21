# Neo4j Plugin Examples

This directory contains examples of how to use the Neo4jPlugin CRD with both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone deployments.

## Plugin Installation Method

The Neo4jPlugin controller uses Neo4j's recommended `NEO4J_PLUGINS` environment variable approach with plugin-type-aware configuration:

- **No External Jobs**: Plugins are configured via environment variables, not download jobs
- **Automatic Installation**: Neo4j downloads and installs plugins on startup
- **Rolling Restart**: StatefulSet pods restart to apply new plugin configuration
- **Dependency Management**: Plugin dependencies are automatically included
- **Smart Configuration**: Automatic plugin-specific configuration based on plugin type

## Plugin Types (Neo4j 5.26+ Compatibility)

### Environment Variable Only Plugins
**APOC & APOC Extended**: Configuration via environment variables only
- **Reason**: APOC settings no longer supported in `neo4j.conf` in Neo4j 5.26+
- **Example**: `apoc.export.file.enabled` becomes `NEO4J_APOC_EXPORT_FILE_ENABLED`

### Neo4j Config Plugins
**Graph Data Science, Bloom, GenAI, etc.**: Configuration through neo4j.conf
- **Automatic Security**: Required procedure security settings applied automatically
- **License Support**: License file configuration for Enterprise plugins

## Architecture Compatibility

The Neo4jPlugin controller has been updated to work with the current server-based architecture:

### Neo4jEnterpriseCluster Support
- **StatefulSet Naming**: Uses `{cluster-name}-server` pattern
- **Pod Labels**: `app.kubernetes.io/name=neo4j`, `app.kubernetes.io/instance={cluster-name}`
- **Neo4j Client**: Uses `NewClientForEnterprise()` method
- **Replica Count**: Based on `cluster.Spec.Topology.Servers`

### Neo4jEnterpriseStandalone Support ✅ NEW
- **StatefulSet Naming**: Uses `{standalone-name}` pattern
- **Pod Labels**: `app={standalone-name}`
- **Neo4j Client**: Uses `NewClientForEnterpriseStandalone()` method
- **Replica Count**: Always 1

## Examples

### APOC Plugin (Environment Variables Only)
```yaml
# apoc-plugin-example.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
spec:
  clusterRef: my-cluster  # References Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone
  name: apoc
  version: "5.26.0"
  source:
    type: official
  config:
    # These settings become environment variables (NEO4J_APOC_*)
    # APOC configuration no longer supported in neo4j.conf in Neo4j 5.26+
    apoc.export.file.enabled: "true"
    apoc.import.file.enabled: "true"
    apoc.load.json.enabled: "true"
```

### Graph Data Science Plugin (Neo4j Config)
```yaml
# gds-plugin-example.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: gds-plugin
spec:
  clusterRef: my-cluster
  name: graph-data-science
  version: "2.10.0"
  source:
    type: community
  config:
    # This goes through neo4j.conf
    gds.enterprise.license_file: "/licenses/gds.license"
  # Automatically configured by operator:
  # - dbms.security.procedures.unrestricted=gds.*
  # - dbms.security.procedures.allowlist=gds.*
```

### Bloom Plugin (Complex Neo4j Config)
```yaml
# bloom-plugin-example.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: bloom-plugin
spec:
  clusterRef: my-cluster
  name: bloom
  version: "2.15.0"
  source:
    type: official
  config:
    # License configuration
    dbms.bloom.license_file: "/licenses/bloom.license"
    # Optional: Role-based access
    dbms.bloom.authorization_role: "admin,architect"
  # Automatically configured by operator:
  # - dbms.security.procedures.unrestricted=bloom.*
  # - dbms.security.http_auth_allowlist=/,/browser.*,/bloom.*
  # - server.unmanaged_extension_classes=com.neo4j.bloom.server=/bloom
```

### Plugin with Dependencies
```yaml
# gds-with-apoc-example.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
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
  config:
    gds.enterprise.license_file: "/licenses/gds.license"
  # Both GDS and APOC will be installed and configured correctly
  # - GDS: neo4j.conf configuration
  # - APOC: environment variable configuration
```

## Plugin Sources

### Official Repository
```yaml
source:
  type: official
```
Downloads from Neo4j's official plugin repository.

### Community Repository
```yaml
source:
  type: community
```
Downloads from Maven Central or GitHub releases.

### Custom Repository
```yaml
source:
  type: custom
  registry:
    url: https://my-repo.example.com
    authSecret: repo-credentials
```

### Direct URL
```yaml
source:
  type: url
  url: https://github.com/example/plugin/releases/download/v1.0.0/plugin.jar
  checksum: sha256:abcd1234...
```

## Usage Instructions

1. **Deploy your Neo4j instance** (cluster or standalone)
   ```bash
   kubectl apply -f examples/clusters/minimal-cluster.yaml
   # or
   kubectl apply -f examples/standalone/single-node-standalone.yaml
   ```

2. **Wait for deployment to be ready**
   ```bash
   kubectl get neo4jenterprisecluster my-cluster
   # or
   kubectl get neo4jenterprisestandalone my-standalone
   ```

3. **Apply the plugin**
   ```bash
   kubectl apply -f examples/plugins/cluster-plugin-example.yaml
   # or
   kubectl apply -f examples/plugins/standalone-plugin-example.yaml
   ```

4. **Monitor plugin installation**
   ```bash
   kubectl get neo4jplugin
   kubectl describe neo4jplugin cluster-apoc-plugin
   ```

## Verification Commands

### Check Plugin Installation
```bash
# Verify plugins are loaded
kubectl exec <cluster-name>-server-0 -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW PROCEDURES"

# Check APOC procedures specifically
kubectl exec <cluster-name>-server-0 -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc'"

# Check GDS procedures
kubectl exec <cluster-name>-server-0 -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'gds'"

# Verify plugin status
kubectl get neo4jplugin
kubectl describe neo4jplugin <plugin-name>
```

### Check Environment Variables
```bash
# Verify NEO4J_PLUGINS environment variable
kubectl get statefulset <cluster-name>-server -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NEO4J_PLUGINS")].value}'

# Check APOC environment variables
kubectl get statefulset <cluster-name>-server -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NEO4J_APOC_EXPORT_FILE_ENABLED")].value}'

# Check GDS security settings
kubectl get statefulset <cluster-name>-server -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED")].value}'
```

## Supported Plugins

### Environment Variable Only (Neo4j 5.26+)
- **apoc**: Awesome Procedures on Cypher
- **apoc-extended**: Extended APOC procedures

### Neo4j Config
- **graph-data-science** (gds): Graph algorithms and machine learning
- **bloom**: Graph visualization and exploration
- **genai**: Generative AI integrations (OpenAI, Vertex AI, etc.)
- **n10s** (neosemantics): RDF and semantic web support
- **graphql**: GraphQL endpoint generation

## Key Improvements

✅ **Neo4j 5.26+ Compatible**: Proper APOC environment variable configuration
✅ **Plugin-Type Aware**: Automatic configuration based on plugin requirements
✅ **Security Auto-Configuration**: Required procedure security settings applied automatically
✅ **Neo4j Best Practices**: Uses official `NEO4J_PLUGINS` environment variable approach
✅ **No External Dependencies**: Eliminates job-based installation and PVC requirements
✅ **Automatic Plugin Management**: Neo4j handles plugin download and installation
✅ **Dual Deployment Support**: Supports both cluster and standalone deployments
✅ **Dependency Handling**: Automatically includes plugin dependencies
✅ **Smart Configuration**: Environment variables for APOC, neo4j.conf for others

## Technical Details

The plugin controller now:

1. **Detects deployment type** automatically (cluster vs standalone)
2. **Uses correct StatefulSet names**:
   - Cluster: `{name}-server`
   - Standalone: `{name}`
3. **Updates StatefulSet environment**:
   - Adds `NEO4J_PLUGINS=["plugin1", "plugin2"]`
   - Adds `NEO4J_*` configuration variables
4. **Triggers rolling restart** for plugin installation
5. **Waits for readiness** to ensure plugins are loaded
6. **Manages plugin lifecycle** including dependencies, configuration, and removal
