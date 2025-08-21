# Neo4jEnterpriseCluster

This document provides a reference for the `Neo4jEnterpriseCluster` Custom Resource Definition (CRD). This resource is used for creating and managing Neo4j Enterprise clusters with high availability.

**Important**: `Neo4jEnterpriseCluster` uses a server-based architecture where servers self-organize into primary and secondary roles automatically. The minimum cluster configuration requires **2 or more servers**.

For single-node deployments, use [`Neo4jEnterpriseStandalone`](neo4jenterprisestandalone.md) instead.

## Spec

The `Neo4jEnterpriseClusterSpec` defines the desired state of a Neo4j Enterprise cluster.

### Core Configuration (Required)

| Field | Type | Description |
|---|---|---|
| `image` | [`ImageSpec`](#imagespec) | The Neo4j Docker image configuration |
| `edition` | `string` | Neo4j edition: `"enterprise"` or `"community"` |
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

| Field | Type | Description |
|---|---|---|
| `servers` | `int32` | Number of servers in the cluster (minimum: 2) |
| `serverModeConstraint` | `string` | Optional server mode constraint: `"PRIMARY"`, `"SECONDARY"`, or empty for automatic role assignment |

**Validation**: Must have at least 2 servers for clustering. Servers automatically organize into primary and secondary roles based on Neo4j's cluster management.

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

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable query monitoring |
| `sampleRate` | `float32` | Query sampling rate (0.0-1.0) |
| `logSlowQueries` | `bool` | Log slow queries |
| `slowQueryThreshold` | `string` | Slow query threshold (e.g., `"500ms"`) |
| `killLongRunningQueries` | `bool` | Kill long-running queries |
| `longRunningQueryThreshold` | `string` | Long-running query threshold |
| `exportMetrics` | `bool` | Export query metrics |
| `metricsEndpoint` | `string` | Metrics export endpoint |

### PlacementSpec

| Field | Type | Description |
|---|---|---|
| `affinity` | `*corev1.Affinity` | Server affinity rules |
| `tolerations` | `[]corev1.Toleration` | Server tolerations |
| `nodeSelector` | `map[string]string` | Server node selector |
| `topologySpreadConstraints` | `[]corev1.TopologySpreadConstraint` | Server topology constraints |
| `enforceDistribution` | `bool` | Enforce even distribution across availability zones |
| `availabilityZones` | `[]string` | Target availability zones for server placement |

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

## Examples

### Basic Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: basic-cluster
spec:
  edition: enterprise
  image:
    repository: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 3  # 3 servers will self-organize into primary/secondary roles
  storage:
    size: 10Gi
  auth:
    provider: secrets
    secret: neo4j-auth
  resources:
    requests:
      cpu: "1"
      memory: 4Gi
    limits:
      cpu: "2"
      memory: 8Gi
```

### Cluster with Backup and Monitoring

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: monitored-cluster
spec:
  edition: enterprise
  image:
    repository: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 3  # 3 servers will self-organize into primary/secondary roles
  storage:
    size: 50Gi
  auth:
    provider: secrets
    secret: neo4j-auth
  backups:
    enabled: true
    schedule: "0 2 * * *"  # Daily at 2 AM
    type: full
    retention:
      keepDaily: 7
      keepWeekly: 4
      keepMonthly: 6
    storage:
      provider: s3
      bucket: neo4j-backups
      region: us-east-1
      credentialsSecret: s3-credentials
    fromSecondary: true
  queryMonitoring:
    enabled: true
    sampleRate: 0.1
    logSlowQueries: true
    slowQueryThreshold: "1s"
    exportMetrics: true
  monitoring:
    enabled: true
    serviceMonitor:
      enabled: true
      labels:
        prometheus: kube-prometheus
```

### Cluster with LoadBalancer Service

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: public-cluster
spec:
  edition: enterprise
  image:
    repository: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 5  # 5 servers will self-organize into primary/secondary roles
  storage:
    size: 20Gi
  auth:
    provider: secrets
    secret: neo4j-auth
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
  edition: enterprise
  image:
    repository: neo4j
    tag: "5.26.0-enterprise"
  topology:
    servers: 3  # 3 servers will self-organize into primary/secondary roles
  storage:
    size: 20Gi
  auth:
    provider: secrets
    secret: neo4j-auth
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
      tlsEnabled: true
      tlsSecretName: neo4j-tls
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
        nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
```

For more information on configuration best practices, see the [Configuration Best Practices Guide](../user_guide/guides/configuration_best_practices.md).
