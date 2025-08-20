# Neo4j Plugin Examples

This directory contains examples of how to use the Neo4jPlugin CRD with both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone deployments.

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

### Cluster Plugin Example
```yaml
# cluster-plugin-example.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: cluster-apoc-plugin
spec:
  clusterRef: my-cluster  # References Neo4jEnterpriseCluster
  name: apoc
  version: "5.26.0"
  source:
    type: official
```

### Standalone Plugin Example
```yaml
# standalone-plugin-example.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: standalone-gds-plugin
spec:
  clusterRef: my-standalone  # References Neo4jEnterpriseStandalone
  name: graph-data-science
  version: "2.10.0"
  source:
    type: community
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

## Key Improvements

✅ **Fixed Architecture Compatibility**: Works with current server-based StatefulSet naming
✅ **Dual Deployment Support**: Supports both cluster and standalone deployments
✅ **Correct Client Creation**: Uses appropriate Neo4j client methods
✅ **Proper Resource References**: Handles StatefulSet and PVC naming correctly
✅ **Enhanced Error Handling**: Better error messages for missing deployments
✅ **Backward Compatibility**: Existing cluster plugins continue to work

## Technical Details

The plugin controller now:

1. **Detects deployment type** automatically (cluster vs standalone)
2. **Uses correct StatefulSet names**:
   - Cluster: `{name}-server`
   - Standalone: `{name}`
3. **Creates appropriate Neo4j clients**:
   - Cluster: `NewClientForEnterprise()`
   - Standalone: `NewClientForEnterpriseStandalone()`
4. **Handles plugin installation** via job-based approach compatible with EmptyDir plugin volumes
5. **Manages plugin lifecycle** including dependencies, configuration, and removal
