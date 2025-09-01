# Neo4jEnterpriseStandalone API Reference

The `Neo4jEnterpriseStandalone` custom resource manages single-node Neo4j Enterprise deployments for development, testing, and simple production workloads that don't require clustering capabilities.

## Overview

- **API Version**: `neo4j.neo4j.com/v1alpha1`
- **Kind**: `Neo4jEnterpriseStandalone`
- **Supported Neo4j Versions**: 5.26.0+ (semver) and 2025.01.0+ (calver)
- **Architecture**: Single StatefulSet with unified clustering infrastructure
- **Database Support**: Compatible with Neo4jDatabase CRD for automated database creation
- **Plugin Support**: Full compatibility with Neo4jPlugin CRD

## Architecture

**Unified Infrastructure**: `Neo4jEnterpriseStandalone` uses the same clustering infrastructure as cluster deployments but with a single server:

- **Single StatefulSet**: `{standalone-name}` with 1 replica
- **Single Pod**: Named `{standalone-name}-0`
- **Database Compatibility**: Supports Neo4jDatabase resources for automated database creation
- **Plugin Compatibility**: Full support for Neo4jPlugin installation and management
- **Backup Integration**: Compatible with Neo4jBackup/Restore for data protection

## Related Resources

- [`Neo4jDatabase`](neo4jdatabase.md) - Create databases within the standalone instance
- [`Neo4jPlugin`](neo4jplugin.md) - Install plugins (APOC, GDS, etc.)
- [`Neo4jBackup`](neo4jbackup.md) - Schedule automated backups
- [`Neo4jRestore`](neo4jrestore.md) - Restore from backups
- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) - For clustered deployments

## When to Use Neo4jEnterpriseStandalone

Use `Neo4jEnterpriseStandalone` when you need:
- **Development environments** with a single Neo4j instance
- **Testing scenarios** that don't require clustering
- **Simple production workloads** with basic availability requirements
- **Migration from Community Edition** to Enterprise features without clustering
- **Cost-effective deployments** that don't need high availability

> **Note**: For production workloads requiring high availability, use `Neo4jEnterpriseCluster` instead.

## Spec Fields

### Required Fields

#### `image` (ImageSpec)
Specifies the Neo4j Docker image to use.

| Field | Type | Description |
|---|---|---|
| `repo` | `string` | **Required**. Docker repository (default: `"neo4j"`) |
| `tag` | `string` | **Required**. Neo4j version tag (5.26+ required) |
| `pullPolicy` | `string` | Image pull policy: `"Always"`, `"IfNotPresent"` (default), `"Never"` |
| `pullSecrets` | `[]string` | Image pull secrets for private registries |

```yaml
image:
  repo: neo4j                    # Docker repository
  tag: "5.26.0-enterprise"       # Neo4j version (5.26+ required)
  pullPolicy: IfNotPresent       # Image pull policy
  pullSecrets: []                # Image pull secrets
```

#### `storage` (StorageSpec)
Defines storage configuration for the Neo4j data volume.

| Field | Type | Description |
|---|---|---|
| `className` | `string` | **Required**. Storage class name |
| `size` | `string` | **Required**. Storage size (e.g., `"10Gi"`) |
| `retentionPolicy` | `string` | PVC retention policy: `"Delete"` (default), `"Retain"` |
| `backupStorage` | [`*BackupStorageSpec`](#backupstoragespec) | Additional storage for backups |

```yaml
storage:
  className: standard            # Storage class name
  size: "10Gi"                  # Storage size
  retentionPolicy: Delete       # PVC retention policy (Delete/Retain)
```

### Optional Fields

#### `resources` (ResourceRequirements)
Resource limits and requests for the Neo4j pod.

```yaml
resources:
  requests:
    memory: "2Gi"
    cpu: "500m"
  limits:
    memory: "4Gi"
    cpu: "2"
```

#### `env` ([]EnvVar)
Environment variables for the Neo4j container.

```yaml
env:
  - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
    value: "yes"
  # Note: Use Neo4jPlugin CRD for plugin management instead of NEO4J_PLUGINS
```

#### `config` (map[string]string)
Custom Neo4j configuration. The operator uses unified clustering infrastructure for standalone deployments (Neo4j 5.26+ approach).

```yaml
config:
  server.memory.heap.initial_size: "1G"
  server.memory.heap.max_size: "2G"
  server.memory.pagecache.size: "1G"
  dbms.security.procedures.unrestricted: "gds.*,apoc.*"
  dbms.logs.query.enabled: "true"
  dbms.logs.query.threshold: "1s"
  # Neo4j 5.26+ configuration syntax
  server.default_listen_address: "0.0.0.0"
  server.discovery.advertised_address: "$(hostname -f)"
```

**Automatically Managed**: The following configurations are managed by the operator:
- `dbms.cluster.*` - Clustering settings (uses unified infrastructure)
- `dbms.kubernetes.*` - Kubernetes discovery settings
- `server.bolt.listen_address` - Network listeners
- `server.http.listen_address` - HTTP endpoints
- `NEO4J_AUTH` - Authentication setup (when using adminSecret)

**Critical for Standalone**: The operator automatically configures clustering infrastructure but ensures single-node operation.

#### `tls` (TLSSpec)
TLS/SSL configuration for secure connections.

```yaml
tls:
  mode: cert-manager            # cert-manager or disabled
  issuerRef:
    name: ca-cluster-issuer
    kind: ClusterIssuer
```

#### `auth` (AuthSpec)
Authentication configuration.

```yaml
auth:
  provider: native              # native, ldap, kerberos, jwt
  adminSecret: neo4j-admin-secret
  passwordPolicy:
    minLength: 8
    requireUppercase: true
    requireNumbers: true
```

#### `service` (ServiceSpec)
Service configuration for external access.

```yaml
service:
  type: ClusterIP              # ClusterIP, NodePort, LoadBalancer
  annotations:                 # Service annotations (e.g., for cloud LB)
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
  loadBalancerIP: "10.0.0.100"     # Static IP for LoadBalancer
  loadBalancerSourceRanges:        # IP ranges allowed to access
    - "10.0.0.0/8"
    - "192.168.0.0/16"
  externalTrafficPolicy: Local     # Cluster or Local
  ingress:                         # Ingress configuration
    enabled: true
    className: nginx
    host: neo4j.example.com
    tlsSecretName: neo4j-tls
```

#### `persistence` (PersistenceSpec)
Persistence configuration for standalone deployments.

```yaml
persistence:
  enabled: true                 # Enable persistent storage
  retentionPolicy: Delete       # Delete or Retain PVCs on deletion
  accessModes:
    - ReadWriteOnce
```

#### `backups` (BackupsSpec)
Default backup configuration.

```yaml
backups:
  defaultStorage:
    type: s3
    bucket: my-backup-bucket
    path: /neo4j-backups
  cloud:
    provider: aws
    identity:
      serviceAccount: neo4j-backup-sa
```

#### `ui` (UISpec)
Neo4j Browser configuration.

```yaml
ui:
  enabled: true
  resources:
    requests:
      memory: "100Mi"
      cpu: "100m"
```

#### `restoreFrom` (RestoreSpec)
Restore from backup during creation.

```yaml
restoreFrom:
  backupRef: my-backup-name
  pointInTime: "2023-12-01T10:00:00Z"
```

#### `plugins` ([]PluginSpec) - DEPRECATED
**DEPRECATED:** Use the Neo4jPlugin CRD instead for plugin management.

The embedded plugin configuration is deprecated. Use separate Neo4jPlugin resources:

```yaml
# Instead of embedded plugins, use Neo4jPlugin CRD
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: my-apoc-plugin
spec:
  clusterRef: my-standalone  # References the Neo4jEnterpriseStandalone
  name: apoc
  version: "5.26.0"
  config:
    "apoc.export.file.enabled": "true"
    "apoc.import.file.enabled": "true"
```

**Plugin Installation**: The Neo4jPlugin controller automatically:
1. Updates the standalone StatefulSet with `NEO4J_PLUGINS` environment variable
2. Adds plugin-specific configuration as `NEO4J_*` environment variables
3. Triggers a rolling restart to apply plugin changes
4. Verifies plugin installation and marks status as Ready

See the [Neo4jPlugin API reference](neo4jplugin.md) for complete documentation.

#### `queryMonitoring` (QueryMonitoringSpec)
Query performance monitoring.

```yaml
queryMonitoring:
  enabled: true
  slowQueryThreshold: "5s"
  explainPlan: true
  indexRecommendations: true
```

## Status Fields

The `Neo4jEnterpriseStandalone` status provides information about the current state of the deployment.

### Primary Status Fields

#### `phase` (string)
Current deployment phase:
- `Pending`: Deployment is being created
- `Running`: Deployment is running and ready
- `Failed`: Deployment has failed
- `ValidationFailed`: Spec validation failed

#### `ready` (boolean)
Indicates if the standalone deployment is ready for connections.

#### `conditions` ([]Condition)
Detailed conditions about the deployment state.

#### `endpoints` (EndpointStatus)
Connection endpoints for the Neo4j instance.

```yaml
endpoints:
  bolt: "bolt://standalone-neo4j-service.default.svc.cluster.local:7687"
  http: "http://standalone-neo4j-service.default.svc.cluster.local:7474"
  https: "https://standalone-neo4j-service.default.svc.cluster.local:7473"
```

#### `version` (string)
Current Neo4j version running.

#### `podStatus` (StandalonePodStatus)
Information about the Neo4j pod.

```yaml
podStatus:
  podName: standalone-neo4j-0
  podIP: "10.244.0.100"
  nodeName: "worker-node-1"
  phase: Running
  restartCount: 0
```

#### `databaseStatus` (StandaloneDatabaseStatus)
Information about the Neo4j database.

| Field | Type | Description |
|---|---|---|
| `databaseMode` | `string` | Database mode (should show unified infrastructure mode) |
| `databaseName` | `string` | Active database name (usually `"neo4j"`) |
| `lastBackupTime` | `*metav1.Time` | When the last backup was completed |
| `storageSize` | `string` | Current storage usage |
| `connectionCount` | `int32` | Number of active connections |
| `lastHealthCheck` | `*metav1.Time` | When the last health check was performed |
| `healthStatus` | `string` | Current health status |

```yaml
databaseStatus:
  databaseMode: "UNIFIED"  # Reflects unified clustering infrastructure
  databaseName: "neo4j"
  storageSize: "2.5Gi"
  connectionCount: 5
  healthStatus: "Healthy"
  lastHealthCheck: "2025-01-20T10:30:00Z"
```

## Examples

### Basic Standalone Deployment

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: dev-neo4j
  namespace: development
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"       # Use specific version

  storage:
    className: standard
    size: "10Gi"
    retentionPolicy: Delete        # Clean up PVC on deletion

  resources:
    requests:
      memory: "2Gi"                # Minimum for Neo4j Enterprise
      cpu: "500m"
    limits:
      memory: "4Gi"
      cpu: "2"

  auth:
    adminSecret: neo4j-admin-secret  # Contains username/password

  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

  # Optional: Custom Neo4j configuration
  config:
    server.memory.heap.initial_size: "1G"
    server.memory.heap.max_size: "2G"
    server.memory.pagecache.size: "1G"
```

### Standalone with LoadBalancer Service

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: public-neo4j
  namespace: production
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"

  storage:
    className: fast-ssd
    size: "50Gi"
    retentionPolicy: Retain        # Keep data on deletion

  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  # LoadBalancer service for external access
  service:
    type: LoadBalancer
    annotations:
      # Example for AWS NLB
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
    loadBalancerSourceRanges:
      - "10.0.0.0/8"      # Corporate network
      - "192.168.0.0/16"  # VPN range
    externalTrafficPolicy: Local  # Preserve client IPs

  auth:
    adminSecret: neo4j-admin-secret
    passwordPolicy:
      minLength: 12
      requireSpecialChars: true

  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

  # Production configuration
  config:
    server.memory.heap.initial_size: "3G"
    server.memory.heap.max_size: "6G"
    server.memory.pagecache.size: "2G"
    dbms.logs.query.enabled: "true"
    dbms.logs.query.threshold: "1s"
```

### Standalone with Ingress

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: web-neo4j
  namespace: production
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"

  storage:
    className: standard
    size: "20Gi"

  # TLS configuration
  tls:
    mode: cert-manager
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer

  # Ingress for HTTPS access
  service:
    ingress:
      enabled: true
      className: nginx
      host: neo4j.example.com
      tlsEnabled: true
      tlsSecretName: neo4j-tls
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
        nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"

  auth:
    provider: native
    adminSecret: neo4j-admin-secret

  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"
```

### Production Standalone with TLS and Plugins

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: prod-neo4j-standalone
  namespace: production
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"

  storage:
    className: fast-ssd
    size: "50Gi"
    retentionPolicy: Retain        # Persist data

  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer

  auth:
    adminSecret: neo4j-admin-secret
    passwordPolicy:
      minLength: 12
      requireSpecialChars: true

  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: nlb

  persistence:
    enabled: true
    retentionPolicy: Retain

  queryMonitoring:
    enabled: true
    slowQueryThreshold: "1s"
    explainPlan: true
    indexRecommendations: true

  config:
    server.memory.heap.initial_size: "3G"
    server.memory.heap.max_size: "6G"
    server.memory.pagecache.size: "2G"
    dbms.logs.query.enabled: "true"
    dbms.logs.query.threshold: "500ms"

  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"

---
# Install APOC plugin using Neo4jPlugin CRD
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: prod-apoc-plugin
  namespace: production
spec:
  clusterRef: prod-neo4j-standalone  # References the standalone
  name: apoc
  version: "5.26.0"
  config:
    "apoc.export.file.enabled": "true"
    "apoc.import.file.enabled": "true"
    "apoc.import.file.use_neo4j_config": "true"

---
# Create a database using Neo4jDatabase CRD
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: prod-app-database
  namespace: production
spec:
  clusterRef: prod-neo4j-standalone  # References the standalone
  name: appdb
  ifNotExists: true
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
      - "CREATE INDEX user_name IF NOT EXISTS FOR (u:User) ON (u.name)"
```

## Management Commands

### Basic Operations

```bash
# Create a standalone deployment
kubectl apply -f standalone-neo4j.yaml

# List standalone deployments
kubectl get neo4jenterprisestandalone

# Describe a standalone deployment
kubectl describe neo4jenterprisestandalone dev-neo4j

# Get logs
kubectl logs -l app=dev-neo4j

# Port forward for local access
kubectl port-forward svc/dev-neo4j-service 7474:7474 7687:7687
```

### Scaling and Updates

```bash
# Update configuration
kubectl edit neo4jenterprisestandalone dev-neo4j

# Update Neo4j version
kubectl patch neo4jenterprisestandalone dev-neo4j -p '{"spec":{"image":{"tag":"5.27-enterprise"}}}'

# Check status
kubectl get neo4jenterprisestandalone dev-neo4j -o yaml
```

### Database and Plugin Management

```bash
# Create a database in the standalone instance
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: app-database
spec:
  clusterRef: dev-neo4j  # References the standalone
  name: appdb
  ifNotExists: true
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
EOF

# Install APOC plugin
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
spec:
  clusterRef: dev-neo4j  # References the standalone
  name: apoc
  version: "5.26.0"
  config:
    "apoc.export.file.enabled": "true"
EOF
```

### Backup and Restore

```bash
# Create a backup
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: dev-neo4j-backup
spec:
  target:
    kind: Neo4jEnterpriseStandalone  # Note: correct target kind
    name: dev-neo4j
  storage:
    type: s3
    bucket: my-backup-bucket
    path: /backups/dev-neo4j
EOF

# Restore from backup
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-dev-neo4j
spec:
  targetCluster: dev-neo4j        # Target standalone instance
  databaseName: neo4j
  source:
    type: backup
    backupRef: dev-neo4j-backup
  options:
    replaceExisting: true
EOF
```

## Migration Guide

### From Neo4jEnterpriseCluster (Single-Node)

If you previously used `Neo4jEnterpriseCluster` with 1 primary and 0 secondaries, migrate to `Neo4jEnterpriseStandalone`:

**Before (no longer supported):**
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: single-node-cluster
spec:
  topology:
    primaries: 1
    secondaries: 0
  # ... other config ...
```

**After:**
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: single-node-standalone
spec:
  # ... same config without topology ...
```

### To Neo4jEnterpriseCluster (Clustered)

To upgrade from standalone to clustered deployment:

1. **Create a backup** of your standalone deployment
2. **Deploy a new cluster** with minimum topology (2 servers)
3. **Restore the backup** to the new cluster
4. **Update application connections** to the new cluster endpoints
5. **Delete the old standalone** deployment

## Troubleshooting

### Common Issues

#### Deployment Stuck in Pending
```bash
# Check pod status
kubectl get pods -l app=<standalone-name>
kubectl describe pod <pod-name>

# Check PVC
kubectl get pvc -l app=<standalone-name>
kubectl describe pvc <pvc-name>
```

#### Validation Errors
```bash
# Check for validation errors
kubectl describe neo4jenterprisestandalone <name>

# Common validation issues:
# - Invalid Neo4j version (must be 5.26+)
# - Clustering configurations in spec.config
# - Invalid storage size format
```

#### Connection Issues
```bash
# Check service endpoints
kubectl get svc -l app=<standalone-name>
kubectl describe svc <service-name>

# Check pod status
kubectl get pods -l app=<standalone-name>
kubectl describe pod <standalone-name>-0

# Check pod logs
kubectl logs <standalone-name>-0 -c neo4j

# Test connectivity
kubectl port-forward svc/<service-name> 7474:7474 7687:7687
curl http://localhost:7474

# Test database connectivity
kubectl exec <standalone-name>-0 -c neo4j -- \
  cypher-shell -u neo4j -p <password> "RETURN 'Connected!' as status"
```

### Performance Tuning

#### Memory Configuration
```yaml
config:
  # Adjust based on available memory
  server.memory.heap.initial_size: "2G"
  server.memory.heap.max_size: "4G"
  server.memory.pagecache.size: "2G"

  # For memory-intensive workloads
  dbms.memory.transaction.total.max: "1G"
```

#### Storage Optimization
```yaml
storage:
  className: fast-ssd          # Use SSD storage class
  size: "50Gi"                 # Size for your data requirements

# For high IOPS workloads
config:
  dbms.checkpoint.interval.time: "15s"
  dbms.checkpoint.interval.tx: "100000"
```

## Best Practices

1. **Use appropriate resource limits** based on your workload
2. **Enable TLS** for production deployments
3. **Configure backups** for data protection
4. **Monitor query performance** with query monitoring
5. **Use specific Neo4j versions** instead of latest tags
6. **Set proper password policies** for security
7. **Configure resource quotas** in production namespaces
8. **Use correct configuration settings** - see [Configuration Best Practices](../user_guide/guides/configuration_best_practices.md)

## Usage Patterns

### Development Environment

```yaml
# Minimal development setup
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: dev-neo4j
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
  storage:
    className: standard
    size: 5Gi
    retentionPolicy: Delete
  resources:
    requests:
      cpu: 200m
      memory: 1Gi
    limits:
      cpu: 1
      memory: 2Gi
  auth:
    adminSecret: dev-auth-secret
  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"
```

### Testing with Multiple Databases

```yaml
# Standalone for testing multiple databases
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: test-neo4j
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
  storage:
    className: standard
    size: 20Gi
  resources:
    requests:
      memory: 2Gi
      cpu: 500m
    limits:
      memory: 4Gi
      cpu: 2
  auth:
    adminSecret: test-auth-secret

---
# Test database 1
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: test-users-db
spec:
  clusterRef: test-neo4j
  name: users
  ifNotExists: true

---
# Test database 2
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: test-products-db
spec:
  clusterRef: test-neo4j
  name: products
  ifNotExists: true
```

### Migration from Cluster to Standalone

```bash
# 1. Create backup from cluster
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: cluster-migration-backup
spec:
  target:
    kind: Neo4jEnterpriseCluster
    name: old-cluster
  storage:
    type: s3
    bucket: migration-backups
    path: /cluster-to-standalone/
EOF

# 2. Wait for backup completion
kubectl wait --for=condition=Ready neo4jbackup/cluster-migration-backup --timeout=600s

# 3. Create standalone with restore
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: migrated-standalone
spec:
  # ... standalone configuration ...
  restoreFrom:
    backupRef: cluster-migration-backup
```

## Best Practices

1. **Resource Allocation**: Use at least 1.5Gi memory for Neo4j Enterprise database operations
2. **Storage**: Use fast SSD storage classes for production workloads
3. **Authentication**: Always use adminSecret for secure credential management
4. **TLS**: Enable TLS for production deployments using cert-manager
5. **Backup Strategy**: Implement regular backups with appropriate retention policies
6. **Plugin Management**: Use Neo4jPlugin CRD instead of deprecated embedded configuration
7. **Database Management**: Use Neo4jDatabase CRD for automated database creation and schema setup
8. **Monitoring**: Enable query monitoring for performance insights
9. **Configuration**: Use Neo4j 5.26+ configuration syntax (server.* instead of dbms.connector.*)
10. **Version Pinning**: Always use specific version tags instead of latest

## When to Use Cluster Instead

Consider migrating to [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) when you need:

- **High Availability**: Automatic failover and redundancy
- **Horizontal Scaling**: Multiple servers for read/write scaling
- **Multi-Database Topologies**: Different databases with optimized distribution
- **Production Workloads**: Enhanced reliability and performance
- **Load Distribution**: Separation of read and write workloads

For more information:
- [Neo4jEnterpriseCluster API Reference](neo4jenterprisecluster.md)
- [Neo4jDatabase API Reference](neo4jdatabase.md)
- [Neo4jPlugin API Reference](neo4jplugin.md)
- [Backup and Restore Guide](../user_guide/guides/backup_restore.md)
- [Configuration Best Practices](../user_guide/guides/configuration_best_practices.md)
