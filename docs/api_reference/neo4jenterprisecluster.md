# Neo4jEnterpriseCluster API Reference

The `Neo4jEnterpriseCluster` Custom Resource Definition (CRD) manages Neo4j Enterprise clusters with high availability, automatic failover, and horizontal scaling capabilities.

## Overview

- **API Version**: `neo4j.neo4j.com/v1alpha1`
- **Kind**: `Neo4jEnterpriseCluster`
- **Supported Neo4j Versions**: 5.26.0+ (semver) and 2025.01.0+ (calver)
- **Architecture**: Server-based deployment with unified StatefulSet
- **Minimum Servers**: 2 (required for clustering)
- **Maximum Servers**: 20 (validated limit)

## Architecture

**Server-Based Architecture**: `Neo4jEnterpriseCluster` uses a unified server-based architecture introduced in Neo4j 5.26+:

- **Single StatefulSet**: `{cluster-name}-server` with configurable replica count
- **Server Pods**: Named `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc.
- **Self-Organization**: Servers automatically organize into primary/secondary roles for databases
- **Centralized Backup**: Optional `{cluster-name}-backup-0` pod for centralized backup operations
- **Role Flexibility**: Servers can host multiple databases with different roles

**When to Use**:

- Production workloads requiring high availability
- Multi-database deployments with different topology requirements
- Workloads needing horizontal read scaling
- Enterprise features like advanced backup, security, and monitoring
- Large datasets requiring property sharding (Neo4j 2025.07.1+)

**For single-node deployments**, use [`Neo4jEnterpriseStandalone`](neo4jenterprisestandalone.md) instead.

## Related Resources

- [`Neo4jDatabase`](neo4jdatabase.md) - Create databases within the cluster
- [`Neo4jShardedDatabase`](neo4jshardeddatabase.md) - Create sharded databases for horizontal scaling
- [`Neo4jPlugin`](neo4jplugin.md) - Install plugins (APOC, GDS, etc.)
- [`Neo4jBackup`](neo4jbackup.md) - Schedule automated backups
- [`Neo4jRestore`](neo4jrestore.md) - Restore from backups

## Spec

The `Neo4jEnterpriseClusterSpec` defines the desired state of a Neo4j Enterprise cluster.

### Core Configuration (Required)

| Field | Type | Description |
|---|---|---|
| `image` | [`ImageSpec`](#imagespec) | The Neo4j Docker image configuration |
| `topology` | [`TopologyConfiguration`](#topologyconfiguration) | Cluster topology (number of servers) |
| `storage` | [`StorageSpec`](#storagespec) | Storage configuration for data persistence |
| `auth` | [`AuthSpec`](#authspec) | Authentication configuration |

### Kubernetes Integration

| Field | Type | Description |
|---|---|---|
| `resources` | `corev1.ResourceRequirements` | CPU and memory resources |
| `nodeSelector` | `map[string]string` | Node selector constraints |
| `podSecurityContext` | `*corev1.PodSecurityContext` | Pod-level security settings |
| `securityContext` | `*corev1.SecurityContext` | Container-level security settings |
| `imagePullSecrets` | `[]corev1.LocalObjectReference` | Image pull secrets |
| `affinity` | `*corev1.Affinity` | Pod affinity rules |
| `tolerations` | `[]corev1.Toleration` | Pod tolerations |
| `topologySpreadConstraints` | `[]corev1.TopologySpreadConstraint` | Topology spread constraints |
| `placement` | [`PlacementSpec`](#placementspec) | Advanced placement configuration |
| `priorityClassName` | `string` | Priority class name |
| `serviceAccountName` | `string` | Service account name |
| `sidecarContainers` | `[]corev1.Container` | Additional sidecar containers |
| `initContainers` | `[]corev1.Container` | Init containers |
| `volumes` | `[]corev1.Volume` | Additional volumes |
| `volumeMounts` | `[]corev1.VolumeMount` | Additional volume mounts |

### Neo4j Configuration

| Field | Type | Description |
|---|---|---|
| `config` | `map[string]string` | Custom Neo4j configuration |
| `additionalConfig` | `[]string` | Additional config lines |
| `configMap` | `string` | ConfigMap with Neo4j configuration |
| `logLevel` | `string` | Neo4j log level |
| `jvmOptions` | `[]string` | JVM options |
| `env` | `[]corev1.EnvVar` | Environment variables |
| `envFrom` | `[]corev1.EnvFromSource` | Environment from sources |

### Operations

| Field | Type | Description |
|---|---|---|
| `restoreFromBackup` | [`RestoreSpec`](#restorespec) | Restore from backup configuration |
| `backups` | [`BackupsSpec`](#backupsspec) | Backup configuration |
| `maintenance` | [`MaintenanceSpec`](#maintenancespec) | Maintenance mode configuration |
| `upgradeStrategy` | [`UpgradeStrategySpec`](#upgradestrategyspec) | Upgrade strategy configuration |

### Networking

| Field | Type | Description |
|---|---|---|
| `service` | [`ServiceSpec`](#servicespec) | Service configuration for external access |

### Extensions

| Field | Type | Description |
|---|---|---|
| `tls` | [`TLSSpec`](#tlsspec) | TLS configuration |
| `plugins` | `[]PluginSpec` | **DEPRECATED:** Plugin configuration (use Neo4jPlugin CRD instead) |
| `monitoring` | [`MonitoringSpec`](#monitoringspec) | Monitoring configuration |
| `propertySharding` | [`PropertyShardingSpec`](#propertyshardingspec) | Property sharding configuration (Neo4j 2025.06+) |
| `queryMonitoring` | [`QueryMonitoringSpec`](#querymonitoringspec) | Query monitoring configuration |
| `podManagementPolicy` | `string` | Pod management policy: `"Parallel"` or `"OrderedReady"` |
| `updateStrategy` | `*appsv1.StatefulSetUpdateStrategy` | StatefulSet update strategy |
| `annotations` | `map[string]string` | Additional annotations |
| `labels` | `map[string]string` | Additional labels |
| `podAnnotations` | `map[string]string` | Pod annotations |
| `podLabels` | `map[string]string` | Pod labels |

## Type Definitions

### ImageSpec

| Field | Type | Description |
|---|---|---|
| `repository` | `string` | Image repository (default: `"neo4j"`) |
| `tag` | `string` | Image tag |
| `pullPolicy` | `string` | Pull policy: `"Always"`, `"IfNotPresent"`, `"Never"` |

### TopologyConfiguration

Defines the cluster server topology and role constraints.

| Field | Type | Description |
|---|---|---|
| `servers` | `int32` | **Required**. Number of Neo4j servers (minimum: 2, maximum: 20) |
| `serverModeConstraint` | `string` | Global server mode constraint: `"NONE"` (default), `"PRIMARY"`, `"SECONDARY"` |
| `serverRoles` | [`[]ServerRoleHint`](#serverrolehint) | Per-server role constraints (overrides global constraint) |
| `placement` | [`*PlacementConfig`](#placementconfig) | Advanced placement and scheduling configuration |
| `availabilityZones` | `[]string` | Target availability zones for server distribution |
| `enforceDistribution` | `bool` | Enforce server distribution across topology domains |

**Server Role Management**:
- Servers self-organize into primary/secondary roles at the **database level**
- Role constraints influence which databases a server can host:
  - `NONE`: Server can host databases in any mode (default)
  - `PRIMARY`: Server only hosts databases in primary mode
  - `SECONDARY`: Server only hosts databases in secondary mode
- Use `serverRoles` for granular per-server control

**Validation**:
- Minimum 2 servers required for clustering
- Cannot configure all servers as `SECONDARY` (cluster needs primaries)
- Server indices in `serverRoles` must be within range (0 to servers-1)

### ServerRoleHint

Specifies role constraints for individual servers.

| Field | Type | Description |
|---|---|---|
| `serverIndex` | `int32` | **Required**. Server index (0-based, must be < servers count) |
| `modeConstraint` | `string` | **Required**. Role constraint: `"NONE"`, `"PRIMARY"`, `"SECONDARY"` |

### StorageSpec

| Field | Type | Description |
|---|---|---|
| `size` | `string` | Storage size (e.g., `"10Gi"`) |
| `storageClassName` | `string` | Storage class name |
| `selector` | `*metav1.LabelSelector` | PVC label selector |
| `volumeName` | `string` | Volume name |
| `accessModes` | `[]corev1.PersistentVolumeAccessMode` | Access modes |
| `volumeMode` | `*corev1.PersistentVolumeMode` | Volume mode |
| `dataSource` | `*corev1.TypedLocalObjectReference` | Data source |

### AuthSpec

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Auth provider: `"native"`, `"ldap"`, `"jwt"`, `"kerberos"` (default: `"native"`) |
| `adminSecret` | `string` | Secret containing admin username and password |
| `secretRef` | `string` | Secret containing provider-specific configuration |
| `externalSecrets` | [`*ExternalSecretsConfig`](#externalsecretsconfig) | External secrets configuration |
| `passwordPolicy` | [`*PasswordPolicySpec`](#passwordpolicyspec) | Password policy configuration |

### JWTAuthSpec

| Field | Type | Description |
|---|---|---|
| `secretName` | `string` | Secret containing JWT keys |
| `publicKeyPath` | `string` | Path to public key in secret |
| `audience` | `string` | Expected audience claim |
| `issuer` | `string` | Expected issuer claim |
| `realm` | `string` | Authentication realm |

### LDAPAuthSpec

| Field | Type | Description |
|---|---|---|
| `host` | `string` | LDAP server host |
| `port` | `int32` | LDAP server port |
| `useTLS` | `bool` | Use TLS connection |
| `bindDN` | `string` | Bind DN |
| `bindPasswordSecret` | `string` | Secret containing bind password |
| `userSearchBase` | `string` | User search base |
| `userSearchFilter` | `string` | User search filter |
| `groupSearchBase` | `string` | Group search base |
| `groupSearchFilter` | `string` | Group search filter |

### KerberosAuthSpec

| Field | Type | Description |
|---|---|---|
| `realm` | `string` | Kerberos realm |
| `kdcAddress` | `string` | KDC address |
| `servicePrincipal` | `string` | Service principal |
| `keytabSecret` | `string` | Secret containing keytab |

### TLSSpec

| Field | Type | Description |
|---|---|---|
| `mode` | `string` | TLS mode: `"cert-manager"`, `"manual"`, `"external-secrets"` |
| `issuerRef` | `*cmmeta.ObjectReference` | cert-manager issuer reference |
| `certificateSpec` | `*cmapi.CertificateSpec` | cert-manager certificate spec |
| `secretName` | `string` | TLS secret name (for manual mode) |
| `externalSecrets` | [`*ExternalSecretsSpec`](#externalsecretsspec) | External Secrets configuration |

### ExternalSecretsSpec

| Field | Type | Description |
|---|---|---|
| `secretStoreRef` | `string` | External Secrets store reference |
| `refreshInterval` | `string` | Refresh interval (e.g., `"1h"`) |
| `keyMapping` | `map[string]string` | Key mapping |

### PropertyShardingSpec

Configures property sharding for horizontal scaling of large datasets. Property sharding separates graph structure from properties, distributing properties across multiple databases for better scalability. Available in Neo4j 2025.07.1+ Enterprise only.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | **Required**. Enable property sharding for this cluster |
| `config` | `map[string]string` | Optional advanced property sharding configuration |

**System Requirements** (validated by operator):

- **Neo4j Version**: 2025.07.1+ Enterprise
- **Minimum Servers**: 5 servers (for proper shard distribution)
- **Memory**: 6GB minimum, 12GB+ recommended per server
- **CPU**: 1+ core minimum, 2+ cores recommended per server
- **Authentication**: Admin secret required
- **Storage**: Persistent storage class required

**Required Configuration** (automatically applied when enabled):

- `internal.dbms.sharded_property_database.enabled: "true"`
- `db.query.default_language: "CYPHER_25"`
- `internal.dbms.cluster.experimental_protocol_version.dbms_enabled: "true"`
- `internal.dbms.sharded_property_database.allow_external_shard_access: "false"`

**Performance Tuning Options**:

```yaml
propertySharding:
  enabled: true
  config:
    # Transaction log retention (critical for shard sync)
    db.tx_log.rotation.retention_policy: "14 days"

    # Property synchronization interval
    internal.dbms.sharded_property_database.property_pull_interval: "10ms"

    # Memory optimization
    server.memory.heap.max_size: "12G"
    server.memory.pagecache.size: "6G"

    # Connection pooling for cross-shard queries
    dbms.connector.bolt.thread_pool_min_size: "10"
    dbms.connector.bolt.thread_pool_max_size: "100"
```

**Resource Recommendations**:

*Development:*
```yaml
resources:
  requests:
    memory: 6Gi    # Absolute minimum
    cpu: 1000m     # Basic operation
  limits:
    memory: 8Gi
    cpu: 2000m
```

*Production:*
```yaml
resources:
  requests:
    memory: 12Gi   # Recommended minimum
    cpu: 2000m     # Cross-shard performance
  limits:
    memory: 16Gi   # Allow headroom
    cpu: 4000m     # Handle peak loads
```

**Validation Errors**:

| Error | Cause | Resolution |
|-------|-------|------------|
| `property sharding requires Neo4j 2025.07.1+` | Old Neo4j version | Upgrade to 2025.07.1+ Enterprise |
| `property sharding requires minimum 5 servers` | Insufficient servers | Increase server count to 5+ |
| `property sharding requires minimum 6GB memory` | Insufficient memory | Increase memory to 12GB+ (recommended) |
| `property sharding requires minimum 1 CPU core` | Insufficient CPU | Increase CPU to 2+ cores (recommended) |

For detailed configuration, see the [Property Sharding Guide](../user_guide/property_sharding.md).

### BackupsSpec

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable automatic backups |
| `type` | `string` | Backup type: `"full"`, `"incremental"`, `"auto"` |
| `schedule` | `string` | Cron schedule for backups |
| `retention` | [`RetentionSpec`](#retentionspec) | Backup retention policy |
| `storage` | [`BackupStorageSpec`](#backupstoragespec) | Backup storage configuration |
| `consistencyCheck` | `bool` | Enable consistency check before backup |
| `pauseDatabase` | `bool` | Pause database during backup |
| `parallelism` | `int32` | Backup parallelism |
| `compression` | `string` | Compression type |
| `encryption` | [`*EncryptionSpec`](#encryptionspec) | Backup encryption |
| `includeTransactionLogs` | `bool` | Include transaction logs |
| `backupWindow` | [`*BackupWindowSpec`](#backupwindowspec) | Backup window |
| `fromSecondary` | `bool` | Backup from secondary nodes (servers will self-organize roles) |

### QueryMonitoringSpec

Query performance monitoring and analytics configuration.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable query monitoring (default: `true`) |
| `slowQueryThreshold` | `string` | Slow query threshold (default: `"5s"`) |
| `explainPlan` | `bool` | Enable query plan explanation (default: `true`) |
| `indexRecommendations` | `bool` | Enable index recommendations (default: `true`) |
| `sampling` | [`*QuerySamplingConfig`](#querysamplingconfig) | Query sampling configuration |
| `metricsExport` | [`*QueryMetricsExportConfig`](#querymetricsexportconfig) | Metrics export configuration |

### QuerySamplingConfig

Query sampling configuration for performance monitoring.

| Field | Type | Description |
|---|---|---|
| `rate` | `string` | Sampling rate (0.0 to 1.0) |
| `maxQueriesPerSecond` | `int32` | Maximum queries to sample per second |

### QueryMetricsExportConfig

Metrics export configuration for query monitoring.

| Field | Type | Description |
|---|---|---|
| `prometheus` | `bool` | Export to Prometheus |
| `customEndpoint` | `string` | Export to custom endpoint |
| `interval` | `string` | Export interval |

### PlacementConfig

Advanced placement and scheduling configuration.

| Field | Type | Description |
|---|---|---|
| `topologySpread` | [`*TopologySpreadConfig`](#topologyspreadconfig) | Topology spread constraints |
| `antiAffinity` | [`*PodAntiAffinityConfig`](#podantiaffinityconfig) | Pod anti-affinity rules |
| `nodeSelector` | `map[string]string` | Node selection constraints |
| `requiredDuringScheduling` | `bool` | Hard placement requirements |

### TopologySpreadConfig

Controls how servers are distributed across cluster topology.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable topology spread constraints |
| `topologyKey` | `string` | Topology domain (e.g., `"topology.kubernetes.io/zone"`) |
| `maxSkew` | `int32` | Maximum allowed imbalance between domains |
| `whenUnsatisfiable` | `string` | Action when constraints can't be satisfied |
| `minDomains` | `*int32` | Minimum number of eligible domains |

### PodAntiAffinityConfig

Prevents servers from being scheduled on the same nodes/zones.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable anti-affinity rules |
| `topologyKey` | `string` | Anti-affinity topology domain |
| `type` | `string` | Constraint type: `"required"` or `"preferred"` |

### ServiceSpec

Configures how Neo4j is exposed outside the Kubernetes cluster.

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Service type: `"ClusterIP"`, `"NodePort"`, `"LoadBalancer"` (default: `"ClusterIP"`) |
| `annotations` | `map[string]string` | Service annotations (e.g., for cloud load balancer configuration) |
| `loadBalancerIP` | `string` | Static IP for LoadBalancer service (cloud provider specific) |
| `loadBalancerSourceRanges` | `[]string` | IP ranges allowed to access LoadBalancer |
| `externalTrafficPolicy` | `string` | External traffic policy: `"Cluster"` or `"Local"` |
| `ingress` | [`IngressSpec`](#ingressspec) | Ingress configuration |

### IngressSpec

Configures an Ingress resource for HTTP(S) access to Neo4j Browser.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable Ingress creation |
| `className` | `string` | Ingress class name (e.g., `"nginx"`) |
| `host` | `string` | Hostname for the Ingress |
| `path` | `string` | Path prefix (default: `"/"`) |
| `pathType` | `string` | Path type: `"Prefix"`, `"Exact"`, `"ImplementationSpecific"` |
| `tlsEnabled` | `bool` | Enable TLS on the Ingress |
| `tlsSecretName` | `string` | TLS certificate secret name |
| `annotations` | `map[string]string` | Ingress annotations |

## Status

The `Neo4jEnterpriseClusterStatus` represents the observed state of the cluster.

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | Cluster phase: `"Initializing"`, `"Running"`, `"Failed"`, `"Upgrading"`, `"Scaling"` |
| `ready` | `bool` | Whether the cluster is ready |
| `message` | `string` | Human-readable status message |
| `conditions` | `[]metav1.Condition` | Cluster conditions |
| `replicas` | [`map[string]ReplicaStatus`](#replicastatus) | Status of each replica |
| `clusterID` | `string` | Neo4j cluster ID |
| `endpoints` | [`EndpointStatus`](#endpointstatus) | Service endpoints |
| `version` | `string` | Current Neo4j version |
| `upgradeStatus` | [`*UpgradeStatus`](#upgradestatus) | Upgrade status |
| `lastBackup` | `*metav1.Time` | Last backup timestamp |
| `observedGeneration` | `int64` | Last observed generation |

### EndpointStatus

Service endpoints and connection information.

| Field | Type | Description |
|---|---|---|
| `boltURL` | `string` | Bolt protocol endpoint |
| `httpURL` | `string` | HTTP endpoint for Neo4j Browser |
| `httpsURL` | `string` | HTTPS endpoint for Neo4j Browser |
| `internalURL` | `string` | Internal cluster communication endpoint |
| `connectionExamples` | [`ConnectionExamples`](#connectionexamples) | Example connection strings |

### ConnectionExamples

Example connection strings for various scenarios.

| Field | Type | Description |
|---|---|---|
| `portForward` | `string` | kubectl port-forward command |
| `browserURL` | `string` | Neo4j Browser URL |
| `boltURI` | `string` | Bolt connection URI |
| `neo4jURI` | `string` | Neo4j driver URI |
| `pythonExample` | `string` | Python driver connection example |
| `javaExample` | `string` | Java driver connection example |

### UpgradeStatus

Detailed upgrade progress tracking.

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | Upgrade phase: `"Pending"`, `"InProgress"`, `"Paused"`, `"Completed"`, `"Failed"` |
| `startTime` | `*metav1.Time` | When the upgrade started |
| `completionTime` | `*metav1.Time` | When the upgrade completed |
| `currentStep` | `string` | Current upgrade step description |
| `previousVersion` | `string` | Version before upgrade |
| `targetVersion` | `string` | Version being upgraded to |
| `progress` | [`*UpgradeProgress`](#upgradeprogress) | Upgrade progress statistics |
| `message` | `string` | Additional upgrade details |
| `lastError` | `string` | Last error encountered during upgrade |

### UpgradeProgress

Upgrade progress across servers.

| Field | Type | Description |
|---|---|---|
| `total` | `int32` | Total number of servers to upgrade |
| `upgraded` | `int32` | Number of servers successfully upgraded |
| `inProgress` | `int32` | Number of servers currently being upgraded |
| `pending` | `int32` | Number of servers pending upgrade |
| `servers` | [`*NodeUpgradeProgress`](#nodeupgradeprogress) | Server upgrade details |

### NodeUpgradeProgress

Server-specific upgrade progress.

| Field | Type | Description |
|---|---|---|
| `total` | `int32` | Total number of servers |
| `upgraded` | `int32` | Number of servers successfully upgraded |
| `inProgress` | `int32` | Number of servers currently being upgraded |
| `pending` | `int32` | Number of servers pending upgrade |
| `currentLeader` | `string` | Current leader server |

## Examples

### Basic Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: basic-cluster
spec:
  image:
    repo: neo4j  # Note: field name is 'repo', not 'repository'
    tag: "5.26.0-enterprise"
  topology:
    servers: 3  # Creates StatefulSet basic-cluster-server with 3 replicas
  storage:
    className: standard
    size: 10Gi
  auth:
    adminSecret: neo4j-admin-secret  # Note: field name is 'adminSecret'
  resources:
    requests:
      cpu: "1"
      memory: 4Gi
    limits:
      cpu: "2"
      memory: 8Gi
```

### Cluster with Server Role Constraints

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: role-constrained-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 5
    # Global constraint: all servers default to any role
    serverModeConstraint: NONE
    # Per-server role hints (overrides global constraint)
    serverRoles:
      - serverIndex: 0
        modeConstraint: PRIMARY    # Server-0: only primary databases
      - serverIndex: 1
        modeConstraint: PRIMARY    # Server-1: only primary databases
      - serverIndex: 2
        modeConstraint: SECONDARY  # Server-2: only secondary databases
      - serverIndex: 3
        modeConstraint: SECONDARY  # Server-3: only secondary databases
      - serverIndex: 4
        modeConstraint: NONE       # Server-4: any database mode
    # Advanced placement for high availability
    placement:
      topologySpread:
        enabled: true
        topologyKey: topology.kubernetes.io/zone
        maxSkew: 1
        whenUnsatisfiable: DoNotSchedule
      antiAffinity:
        enabled: true
        topologyKey: kubernetes.io/hostname
        type: required
    availabilityZones:
      - us-east-1a
      - us-east-1b
      - us-east-1c
    enforceDistribution: true
  storage:
    className: fast-ssd
    size: 50Gi
  auth:
    adminSecret: neo4j-admin-secret
```

### Cluster with Centralized Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: monitored-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 3  # Creates monitored-cluster-server StatefulSet
  storage:
    className: fast-ssd
    size: 50Gi
  auth:
    adminSecret: neo4j-admin-secret
  # Centralized backup configuration
  backups:
    defaultStorage:
      type: s3
      bucket: neo4j-backups
      path: production/
    cloud:
      provider: aws
      identity:
        provider: aws
        serviceAccount: neo4j-backup-sa  # Uses IAM roles for pods
  # Enhanced query monitoring
  queryMonitoring:
    enabled: true
    slowQueryThreshold: "1s"
    explainPlan: true
    indexRecommendations: true
    sampling:
      rate: "0.1"
      maxQueriesPerSecond: 100
    metricsExport:
      prometheus: true
      interval: "30s"
```

### Create Scheduled Backup (Separate Resource)

```yaml
# Note: Backups are now managed via Neo4jBackup CRD
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-cluster-backup
spec:
  target:
    kind: Cluster
    name: monitored-cluster
  storage:
    type: s3
    bucket: neo4j-backups
    path: daily/
  schedule: "0 2 * * *"  # Daily at 2 AM
  retention:
    maxAge: "30d"
    maxCount: 30
  options:
    compress: true
    backupType: FULL
```

### Cluster with LoadBalancer Service

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: public-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 5  # Creates public-cluster-server StatefulSet with 5 replicas
  storage:
    className: standard
    size: 20Gi
  auth:
    adminSecret: neo4j-admin-secret
  # LoadBalancer service configuration
  service:
    type: LoadBalancer
    annotations:
      # AWS NLB example
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
    loadBalancerSourceRanges:
      - "10.0.0.0/8"      # Corporate network
      - "172.16.0.0/12"   # VPN range
    externalTrafficPolicy: Local  # Preserve client IPs
```

### Cluster with Ingress

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: ingress-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 3  # Creates ingress-cluster-server StatefulSet
  storage:
    className: fast-ssd
    size: 20Gi
  auth:
    adminSecret: neo4j-admin-secret
  # TLS configuration
  tls:
    mode: cert-manager
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer
  # Ingress configuration
  service:
    ingress:
      enabled: true
      className: nginx
      host: neo4j.example.com
      tlsSecretName: neo4j-tls
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
        nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
```

## Management Commands

### Basic Operations

```bash
# Create a cluster
kubectl apply -f cluster.yaml

# List clusters
kubectl get neo4jenterprisecluster

# Get cluster details
kubectl describe neo4jenterprisecluster my-cluster

# Check cluster status
kubectl get neo4jenterprisecluster my-cluster -o yaml

# Port forward for local access
kubectl port-forward svc/my-cluster-client 7474:7474 7687:7687
```

### Cluster Operations

```bash
# Scale cluster (change server count)
kubectl patch neo4jenterprisecluster my-cluster -p '{"spec":{"topology":{"servers":5}}}'

# Update Neo4j version
kubectl patch neo4jenterprisecluster my-cluster -p '{"spec":{"image":{"tag":"5.27.0-enterprise"}}}'

# Check cluster health
kubectl exec my-cluster-server-0 -- cypher-shell -u neo4j -p password "SHOW SERVERS"

# Monitor server pods
kubectl get pods -l app=my-cluster
kubectl logs my-cluster-server-0 -c neo4j
```

## Usage Patterns

### Multi-Database Architecture

```yaml
# 1. Create cluster with role-optimized servers
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: multi-db-cluster
spec:
  topology:
    servers: 6
    serverRoles:
      - serverIndex: 0
        modeConstraint: PRIMARY    # Dedicated for write workloads
      - serverIndex: 1
        modeConstraint: PRIMARY
      - serverIndex: 2
        modeConstraint: SECONDARY  # Dedicated for read workloads
      - serverIndex: 3
        modeConstraint: SECONDARY
      - serverIndex: 4
        modeConstraint: NONE       # Mixed workloads
      - serverIndex: 5
        modeConstraint: NONE
  # ... other configuration

---
# 2. Create databases with specific topologies
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: user-database
spec:
  clusterRef: multi-db-cluster
  name: users
  topology:
    primaries: 2    # Uses servers 0-1 (PRIMARY constraint)
    secondaries: 2  # Uses servers 2-3 (SECONDARY constraint)

---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: analytics-database
spec:
  clusterRef: multi-db-cluster
  name: analytics
  topology:
    primaries: 1    # Uses server 4 or 5 (NONE constraint)
    secondaries: 3  # Uses remaining servers
```

### Development vs Production

```yaml
# Development cluster (minimal resources)
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: dev-cluster
spec:
  topology:
    servers: 2  # Minimum for clustering
  storage:
    className: standard
    size: 10Gi
  resources:
    requests:
      cpu: 200m
      memory: 1Gi
    limits:
      cpu: 1
      memory: 2Gi
  tls:
    mode: disabled

---
# Property Sharding cluster (Neo4j 2025.07.1+)
apiVersion: neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: sharding-cluster
spec:
  image:
    repo: neo4j
    tag: 2025.07.1-enterprise
  topology:
    servers: 7  # Sufficient for property sharding workloads
  storage:
    className: fast-ssd
    size: 100Gi
  resources:
    requests:
      cpu: 2
      memory: 8Gi
    limits:
      cpu: 4
      memory: 16Gi
  propertySharding:
    enabled: true
    config:
      db.tx_log.rotation.retention_policy: "14 days"
      internal.dbms.sharded_property_database.property_pull_interval: "5ms"
      server.memory.heap.max_size: "12G"
      server.memory.pagecache.size: "6G"
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer

---
# Production cluster (high availability)
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: prod-cluster
spec:
  topology:
    servers: 5
    placement:
      topologySpread:
        enabled: true
        topologyKey: topology.kubernetes.io/zone
        maxSkew: 1
      antiAffinity:
        enabled: true
        type: required
    availabilityZones: [us-east-1a, us-east-1b, us-east-1c]
    enforceDistribution: true
  storage:
    className: fast-ssd
    size: 100Gi
  resources:
    requests:
      cpu: 2
      memory: 8Gi
    limits:
      cpu: 4
      memory: 16Gi
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
  auth:
    passwordPolicy:
      minLength: 12
      requireSpecialChars: true
```

## Best Practices

1. **Resource Planning**: Allocate sufficient memory (≥4Gi) and CPU for Neo4j Enterprise workloads
2. **High Availability**: Use odd number of servers (3, 5) and distribute across availability zones
3. **Server Role Optimization**: Use role constraints to optimize server usage for specific workload patterns
4. **Storage**: Use fast SSD storage classes (`fast-ssd`, `premium-ssd`) for production workloads
5. **Security**: Always enable TLS and use strong password policies in production
6. **Monitoring**: Enable query monitoring and centralized logging for performance insights
7. **Backup Strategy**: Use centralized backup with appropriate retention policies
8. **Scaling**: Plan for growth - scaling up is easier than scaling down

## Troubleshooting

### Common Issues

```bash
# Check cluster formation
kubectl exec my-cluster-server-0 -- cypher-shell -u neo4j -p password "SHOW SERVERS"

# Check pod status
kubectl get pods -l app=my-cluster
kubectl describe pod my-cluster-server-0

# Check operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Verify split-brain detection
kubectl get events --field-selector reason=SplitBrainDetected
```

### Resource Conflicts

```bash
# Check resource version conflicts
kubectl get events --field-selector reason=UpdateConflict

# Force reconciliation
kubectl annotate neo4jenterprisecluster my-cluster \
  operator.neo4j.com/force-reconcile="$(date)"
```

For more information:

- [Configuration Best Practices](../user_guide/guides/configuration_best_practices.md)
- [Property Sharding Guide](../user_guide/property_sharding.md)
- [Split-Brain Recovery Guide](../user_guide/troubleshooting/split-brain-recovery.md)
- [Resource Sizing Guide](../user_guide/guides/resource_sizing.md)
- [Fault Tolerance Guide](../user_guide/guides/fault_tolerance.md)
