# Neo4jEnterpriseStandalone API Reference

The `Neo4jEnterpriseStandalone` custom resource represents a single-node Neo4j Enterprise deployment running in single mode (non-clustered). This is ideal for development, testing, and simple production workloads that don't require clustering capabilities.

## Overview

- **API Version**: `neo4j.neo4j.com/v1alpha1`
- **Kind**: `Neo4jEnterpriseStandalone`
- **Metadata**: Standard Kubernetes metadata
- **Spec**: Desired state specification
- **Status**: Current state and conditions

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

```yaml
image:
  repo: neo4j                    # Docker repository
  tag: "5.26-enterprise"         # Neo4j version (5.26+ required)
  pullPolicy: IfNotPresent       # Image pull policy
  pullSecrets: []                # Image pull secrets
```

#### `storage` (StorageSpec)
Defines storage configuration for the Neo4j data volume.

```yaml
storage:
  className: standard            # Storage class name
  size: "10Gi"                  # Storage size
  retentionPolicy: Delete       # PVC retention policy (Delete/Retain)
```

### Optional Fields

#### `edition` (string)
Neo4j edition. Always set to `enterprise`.

```yaml
edition: enterprise
```

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
  - name: NEO4J_PLUGINS
    value: '["apoc", "gds"]'
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
```

**Restricted Configurations**: The following clustering-related configurations are not allowed:
- `dbms.cluster.*`
- `dbms.kubernetes.*`
- `internal.dbms.single_raft_enabled`

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
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
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

#### `plugins` ([]PluginSpec)
Neo4j plugins to install.

```yaml
plugins:
  - name: apoc
    version: "5.26.0"
    enabled: true
  - name: graph-data-science
    version: "2.9.0"
    enabled: true
```

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

```yaml
databaseStatus:
  databaseMode: SINGLE
  databaseName: neo4j
  storageSize: "2.5Gi"
  connectionCount: 5
  healthStatus: "Healthy"
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
    tag: "5.26-enterprise"

  storage:
    className: standard
    size: "10Gi"

  resources:
    requests:
      memory: "2Gi"
      cpu: "500m"
    limits:
      memory: "4Gi"
      cpu: "2"

  tls:
    mode: disabled

  auth:
    provider: native
    adminSecret: neo4j-admin-secret

  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"
```

### Production Standalone with TLS

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: prod-neo4j-standalone
  namespace: production
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"

  storage:
    className: fast-ssd
    size: "50Gi"

  resources:
    requests:
      memory: "4Gi"
      cpu: "1"
    limits:
      memory: "8Gi"
      cpu: "4"

  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer

  auth:
    provider: native
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

  plugins:
    - name: apoc
      version: "5.26.0"
      enabled: true
    - name: graph-data-science
      version: "2.9.0"
      enabled: true

  queryMonitoring:
    enabled: true
    slowQueryThreshold: "1s"
    explainPlan: true

  config:
    server.memory.heap.initial_size: "3G"
    server.memory.heap.max_size: "6G"
    server.memory.pagecache.size: "2G"
    dbms.security.procedures.unrestricted: "gds.*,apoc.*"
    dbms.logs.query.enabled: "true"
    dbms.logs.query.threshold: "500ms"

  env:
    - name: NEO4J_ACCEPT_LICENSE_AGREEMENT
      value: "yes"
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
    kind: Neo4jEnterpriseStandalone
    name: dev-neo4j
  storage:
    type: s3
    bucket: my-backup-bucket
    path: /backups/dev-neo4j
EOF

# Restore from backup
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: restored-neo4j
spec:
  # ... other config ...
  restoreFrom:
    backupRef: dev-neo4j-backup
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
2. **Deploy a new cluster** with minimum topology (1 primary + 1 secondary)
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

# Check pod logs
kubectl logs -l app=<standalone-name>

# Test connectivity
kubectl port-forward svc/<service-name> 7474:7474
curl http://localhost:7474
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

For more advanced configurations and cluster deployments, see the [Neo4jEnterpriseCluster API Reference](neo4jenterprisecluster.md).
