# Migration Guide: Neo4j Kubernetes Operator

This guide helps you migrate from previous versions of the Neo4j Kubernetes Operator to the latest version with the new CRD structure.

## Overview of Changes

The Neo4j Kubernetes Operator now separates single-node and clustered deployments into two distinct CRDs:

- **`Neo4jEnterpriseCluster`**: For clustered deployments requiring high availability
- **`Neo4jEnterpriseStandalone`**: For single-node deployments in single mode

## ⚠️ Breaking Changes

### 1. Single-Node Clusters No Longer Supported

**Previous behavior** (no longer supported):
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: single-node-cluster
spec:
  topology:
    servers: 1
```

**New behavior** - Choose one of these options:

**Option A: Migrate to Neo4jEnterpriseStandalone** (recommended for development/testing):
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: single-node-standalone
spec:
  # Same configuration as before, but without topology
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: standard
    size: "10Gi"
  # ... other configuration
```

**Option B: Migrate to Minimal Cluster** (recommended for production):
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: minimal-cluster
spec:
  topology:
    servers: 2  # Minimum required
  # ... other configuration
```

### 2. Minimum Cluster Topology Requirements

`Neo4jEnterpriseCluster` now enforces minimum topology requirements:

- **Minimum**: 2 servers (self-organize into primary/secondary roles)
- **Recommended**: 3+ servers for production fault tolerance

**Invalid configurations** (will fail validation):
```yaml
# ❌ This will fail validation
topology:
  servers: 1
```

**Valid configurations**:
```yaml
# ✅ Minimum cluster topology
topology:
  servers: 2

# ✅ Larger cluster
topology:
  servers: 5
```

### 3. Discovery Mode Changes

All Neo4j 5.26+ deployments now use `V2_ONLY` discovery mode automatically. You no longer need to configure this manually.

## Migration Scenarios

### Scenario 1: Single-Node Development Environment

**Before**:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: dev-neo4j
spec:
  topology:
    servers: 1
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: standard
    size: "10Gi"
  resources:
    requests:
      memory: "2Gi"
      cpu: "500m"
  tls:
    mode: disabled
```

**After**:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: dev-neo4j
spec:
  # Remove topology field
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: standard
    size: "10Gi"
  resources:
    requests:
      memory: "2Gi"
      cpu: "500m"
  tls:
    mode: disabled
```

### Scenario 2: Production Single-Node → Minimal Cluster

**Before**:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: prod-neo4j
spec:
  topology:
    servers: 1
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: fast-ssd
    size: "50Gi"
  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
  tls:
    mode: cert-manager
    issuerRef:
      name: prod-issuer
      kind: ClusterIssuer
```

**After**:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: prod-neo4j
spec:
  topology:
    servers: 2  # Minimum cluster topology
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: fast-ssd
    size: "50Gi"
  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
  tls:
    mode: cert-manager
    issuerRef:
      name: prod-issuer
      kind: ClusterIssuer
```

### Scenario 3: Existing Multi-Node Clusters

**No changes required** - existing multi-node clusters will continue to work as before:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: prod-cluster
spec:
  topology:
    servers: 5
  # ... rest of configuration unchanged
```

## Step-by-Step Migration Process

### 1. Assessment Phase

First, identify what you currently have:

```bash
# List all existing clusters
kubectl get neo4jenterprisecluster -A

# Check topology of each cluster
kubectl get neo4jenterprisecluster -A -o custom-columns=NAME:.metadata.name,NAMESPACE:.metadata.namespace,SERVERS:.spec.topology.servers
```

### 2. Backup Your Data

Before making any changes, create backups:

```bash
# Create backup for each cluster
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: migration-backup-$(date +%Y%m%d)
  namespace: <your-namespace>
spec:
  target:
    kind: Cluster
    name: <your-cluster-name>
  storage:
    type: pvc
    pvc:
      name: migration-backup-pvc
      storageClassName: standard
      size: 50Gi
EOF
```

### 3. Migration Strategy A: In-Place Migration (Minimal Downtime)

For clusters that need to stay clustered:

1. **Update the cluster topology**:
   ```bash
   kubectl patch neo4jenterprisecluster <cluster-name> -p '{"spec":{"topology":{"servers":2}}}'
   ```

2. **Wait for the additional server to be ready**:
   ```bash
   kubectl wait neo4jenterprisecluster <cluster-name> --for=condition=Ready --timeout=600s
   ```

3. **Verify the cluster is healthy**:
   ```bash
   kubectl get neo4jenterprisecluster <cluster-name> -o yaml
   ```

### 4. Migration Strategy B: Blue-Green Migration (Zero Downtime)

For production environments requiring zero downtime:

1. **Create new deployment** with correct topology:
   ```bash
   # Create new cluster with minimum topology
   kubectl apply -f new-cluster.yaml
   ```

2. **Wait for new cluster to be ready**:
   ```bash
   kubectl wait neo4jenterprisecluster <new-cluster-name> --for=condition=Ready --timeout=600s
   ```

3. **Restore data to new cluster**:
   ```bash
   kubectl apply -f restore-to-new-cluster.yaml
   ```

4. **Update application connections**:
   ```bash
   # Update your application's connection string
   # From: bolt://old-cluster-client:7687
   # To: bolt://new-cluster-client:7687
   ```

5. **Remove old cluster**:
   ```bash
   kubectl delete neo4jenterprisecluster <old-cluster-name>
   ```

### 5. Migration Strategy C: Single-Node to Standalone

For development/testing environments:

1. **Create standalone deployment**:
   ```bash
   kubectl apply -f standalone-deployment.yaml
   ```

2. **Wait for standalone to be ready**:
   ```bash
   kubectl wait neo4jenterprisestandalone <standalone-name> --for=condition=Ready --timeout=300s
   ```

3. **Migrate data** (if needed):
   ```bash
   # Export data from old cluster
   kubectl exec -it <old-cluster-pod> -- neo4j-admin dump --to=/tmp/export.dump

   # Import data to standalone
   kubectl exec -it <standalone-pod> -- neo4j-admin load --from=/tmp/export.dump
   ```

4. **Update application connections**:
   ```bash
   # Update connection string to standalone service
   ```

5. **Remove old cluster**:
   ```bash
   kubectl delete neo4jenterprisecluster <old-cluster-name>
   ```

## Configuration Migration

### Environment Variables

No changes required - environment variables work the same way in both CRDs.

### Custom Configuration

**Neo4jEnterpriseCluster**:
- Clustering configurations are automatically set
- V2_ONLY discovery is automatically configured
- User configurations are merged with cluster defaults

**Neo4jEnterpriseStandalone**:
- Uses unified clustering infrastructure with single member (Neo4j 5.26+)
- Fixed at 1 replica, clustering scale-out is not supported
- User configurations are merged with standalone defaults

### TLS Configuration

No changes required - TLS configuration works the same way in both CRDs.

### Authentication

No changes required - authentication configuration works the same way in both CRDs.

## Validation and Testing

### 1. Validate New Deployments

```bash
# Check deployment status
kubectl get neo4jenterprisecluster
kubectl get neo4jenterprisestandalone

# Check pod status
# Clusters
kubectl get pods -l neo4j.com/cluster=<cluster-name>
# Standalone
kubectl get pods -l app=<standalone-name>

# Check service endpoints
kubectl get svc -l app.kubernetes.io/name=neo4j
```

### 2. Test Connectivity

```bash
# Test cluster connectivity
kubectl port-forward svc/<cluster-name>-client 7474:7474 7687:7687

# Test standalone connectivity
kubectl port-forward svc/<standalone-name>-service 7474:7474 7687:7687

# Test with Neo4j Browser
open http://localhost:7474
```

### 3. Verify Data Integrity

```bash
# Connect to Neo4j and run basic queries
cypher-shell -u neo4j -p <password> -a bolt://localhost:7687

# Run test queries
MATCH (n) RETURN count(n) as nodeCount;
MATCH ()-[r]->() RETURN count(r) as relationshipCount;
```

## Common Issues and Solutions

### Issue 1: Validation Errors

**Error**: `Neo4jEnterpriseCluster requires minimum cluster topology`

**Solution**: Either add another server or migrate to `Neo4jEnterpriseStandalone`:

```bash
# Option 1: Add server
kubectl patch neo4jenterprisecluster <name> -p '{"spec":{"topology":{"servers":2}}}'

# Option 2: Migrate to standalone
kubectl apply -f standalone-replacement.yaml
```

### Issue 2: Pod Restart Loops

**Error**: Pods restarting due to discovery configuration

**Solution**: Ensure you're using Neo4j 5.26+ and let the operator handle discovery configuration automatically.

### Issue 3: Connection Issues

**Error**: Applications can't connect to new endpoints

**Solution**: Update application connection strings:

```bash
# For clusters
# Old: bolt://single-node-cluster-client:7687
# New: bolt://minimal-cluster-client:7687

# For standalone
# Old: bolt://single-node-cluster-client:7687
# New: bolt://standalone-neo4j-service:7687
```

### Issue 4: Storage Migration

**Error**: Storage not accessible after migration

**Solution**: Ensure PVCs are properly preserved:

```bash
# Check PVC status
# Clusters
kubectl get pvc -l neo4j.com/cluster=<cluster-name>
# Standalone
kubectl get pvc neo4j-data-<standalone-name>-0

# If needed, manually migrate data
kubectl exec -it <old-pod> -- neo4j-admin dump --to=/tmp/migration.dump
kubectl cp <old-pod>:/tmp/migration.dump ./migration.dump
kubectl cp ./migration.dump <new-pod>:/tmp/migration.dump
kubectl exec -it <new-pod> -- neo4j-admin load --from=/tmp/migration.dump
```

## Rollback Procedures

### Rollback from Cluster to Single-Node

If you need to rollback a cluster migration:

1. **Create backup** of current cluster
2. **Deploy old single-node configuration** (if supported in your operator version)
3. **Restore data** from backup
4. **Update application connections**

### Rollback from Standalone to Cluster

If you need to rollback a standalone migration:

1. **Create backup** of standalone deployment
2. **Deploy new cluster** with minimum topology
3. **Restore data** to cluster
4. **Update application connections**

## Best Practices

1. **Always backup** before migration
2. **Test in staging** environment first
3. **Use blue-green deployment** for zero downtime
4. **Monitor during migration** for issues
5. **Update monitoring** and alerting for new endpoints
6. **Document changes** for your team
7. **Plan rollback procedures** in advance

## Getting Help

If you encounter issues during migration:

1. **Check logs**:
   ```bash
   kubectl logs -l app.kubernetes.io/name=neo4j-operator
   kubectl logs -l neo4j.com/cluster=<cluster-name>
   kubectl logs -l app=<standalone-name>
   ```

2. **Check status**:
   ```bash
   kubectl describe neo4jenterprisecluster <name>
   kubectl describe neo4jenterprisestandalone <name>
   ```

3. **Community support**:
   - [GitHub Issues](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues)
   - [Neo4j Community](https://community.neo4j.com/)

## Upgrading to v1.6.0-alpha (API Stabilization)

v1.6.0-alpha includes breaking changes to the `v1alpha1` API in preparation for graduating to `v1beta1`. These changes fix field naming inconsistencies, remove deprecated fields, and resolve bugs.

### Field renames

| Resource | Old Field | New Field | Action |
|----------|-----------|-----------|--------|
| `Neo4jRestore` | `spec.targetCluster` | `spec.clusterRef` | Rename in YAML |
| Auth TrustStore | `spec.auth.trustStore.secretRef` | `spec.auth.trustStore.name` | Rename in YAML |
| Kerberos Keytab | `spec.auth.kerberos.keytab.secretRef` | `spec.auth.kerberos.keytab.name` | Rename in YAML |

Example — before:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
spec:
  targetCluster: my-cluster
  databaseName: neo4j
  source:
    type: backup
    backupRef: daily-backup
```

After:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
spec:
  clusterRef: my-cluster
  databaseName: neo4j
  source:
    type: backup
    backupRef: daily-backup
```

### Removed fields

| Field | Replacement |
|-------|-------------|
| `spec.auth.provider` | `spec.auth.authenticationProviders` (list) |
| `spec.auth.secretRef` | Use provider-specific typed configs (`spec.auth.ldap`, `spec.auth.oidc`, etc.) |
| `spec.persistence` (standalone) | `spec.storage.retentionPolicy` (already present on StorageSpec) |
| `spec.route` (standalone top-level) | `spec.service.route` (matching cluster pattern) |

Example — migrating deprecated auth fields:

Before:
```yaml
spec:
  auth:
    provider: ldap
    secretRef: ldap-config
```

After:
```yaml
spec:
  auth:
    authenticationProviders: ["ldap"]
    authorizationProviders: ["ldap"]
    ldap:
      host: "ldap://ldap.example.com:389"
      authentication:
        userDNTemplate: "uid={0},ou=users,dc=example,dc=com"
```

Example — migrating standalone persistence:

Before:
```yaml
spec:
  storage:
    className: standard
    size: "10Gi"
  persistence:
    enabled: true
    retentionPolicy: Delete
    accessModes: ["ReadWriteOnce"]
```

After:
```yaml
spec:
  storage:
    className: standard
    size: "10Gi"
    retentionPolicy: Delete
```

Example — migrating standalone route:

Before:
```yaml
spec:
  route:
    enabled: true
    host: neo4j.apps.example.com
```

After:
```yaml
spec:
  service:
    type: ClusterIP
    route:
      enabled: true
      host: neo4j.apps.example.com
```

### Encryption algorithm rename

If you use backup encryption with ChaCha20, update the algorithm name:

| Old Value | New Value |
|-----------|-----------|
| `ChaCha20` | `ChaCha20Poly1305` |

### Unified secret reference type

`TrustStoreSpec`, `AuraTokenSecretRef`, and `KerberosKeytabSpec` have been replaced by a single `SecretKeyRef` type with `name` and `key` fields. The JSON structure for `AuraFleetManagement.tokenSecretRef` is unchanged (fields were already `name`/`key`). For `trustStore` and `kerberos.keytab`, the `secretRef` field is now `name`.

### Quick upgrade checklist

1. Search your manifests for `targetCluster:` and replace with `clusterRef:`
2. Search for `auth.provider:` / `auth.secretRef:` and migrate to `authenticationProviders`/`authorizationProviders` with typed provider configs
3. Search standalone manifests for `spec.route:` and move to `spec.service.route:`
4. Search standalone manifests for `spec.persistence:` and move retention policy to `spec.storage.retentionPolicy:`
5. Search for `trustStore.secretRef:` and rename to `trustStore.name:`
6. Search for `kerberos.keytab.secretRef:` and rename to `kerberos.keytab.name:`
7. If using `algorithm: ChaCha20` in backup encryption, change to `algorithm: ChaCha20Poly1305`
8. Apply updated CRDs before deploying the new operator version

## What's Next

After completing your migration:

1. **Update monitoring** dashboards and alerts
2. **Update documentation** and runbooks
3. **Train your team** on the new CRD structure
4. **Consider proper resource configuration** for cluster deployments
5. **Implement proper backup** strategies for your deployment type

For more information, see:
- [Neo4jEnterpriseCluster API Reference](../api_reference/neo4jenterprisecluster.md)
- [Neo4jEnterpriseStandalone API Reference](../api_reference/neo4jenterprisestandalone.md)
- [Getting Started Guide](getting_started.md)
- [Troubleshooting Guide](guides/troubleshooting.md)
