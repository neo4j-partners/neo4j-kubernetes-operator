# Neo4jEnterpriseCluster API Reference

The `Neo4jEnterpriseCluster` Custom Resource Definition (CRD) manages Neo4j Enterprise clusters with high availability, automatic failover, and horizontal scaling capabilities.

## Overview

- **API Version**: `neo4j.neo4j.com/v1alpha1`
- **Kind**: `Neo4jEnterpriseCluster`
- **Supported Neo4j Versions**: 5.26.x (last semver LTS) and 2025.01.0+ (CalVer)
- **Architecture**: Server-based deployment with unified StatefulSet
- **Minimum Servers**: 2 (required for clustering)
- **Maximum Servers**: 20 (validated limit)

## Architecture

**Server-Based Architecture**: `Neo4jEnterpriseCluster` uses a unified server-based architecture introduced in Neo4j 5.26.x:

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
- Large datasets requiring property sharding (Neo4j 2025.12+)

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
| `resources` | `*corev1.ResourceRequirements` | CPU and memory resources |
| `env` | `[]corev1.EnvVar` | Environment variables for Neo4j pods |
| `nodeSelector` | `map[string]string` | Node selector constraints |
| `tolerations` | `[]corev1.Toleration` | Pod tolerations |
| `affinity` | `*corev1.Affinity` | Pod affinity rules |
| `securityContext` | [`*SecurityContextSpec`](#securitycontextspec) | Pod/container security overrides |

### Neo4j Configuration

| Field | Type | Description |
|---|---|---|
| `config` | `map[string]string` | Custom Neo4j configuration |

### Operations

| Field | Type | Description |
|---|---|---|
| `restoreFrom` | [`RestoreSpec`](#restorespec) | Restore from backup configuration |
| `backups` | [`BackupsSpec`](#backupsspec) | Backup configuration |
| `upgradeStrategy` | [`UpgradeStrategySpec`](#upgradestrategyspec) | Upgrade strategy configuration |

### Networking

| Field | Type | Description |
|---|---|---|
| `service` | [`ServiceSpec`](#servicespec) | Service configuration for external access |

### Extensions

| Field | Type | Description |
|---|---|---|
| `tls` | [`TLSSpec`](#tlsspec) | TLS configuration |
| `ui` | [`UISpec`](#uispec) | Neo4j UI configuration |
| `mcp` | [`MCPServerSpec`](#mcpserverspec) | MCP server deployment and exposure settings |
| `propertySharding` | [`PropertyShardingSpec`](#propertyshardingspec) | Property sharding configuration (Neo4j 2025.12+) |
| `queryMonitoring` | [`QueryMonitoringSpec`](#querymonitoringspec) | Query monitoring configuration |

## Type Definitions

### ImageSpec

| Field | Type | Description |
|---|---|---|
| `repo` | `string` | Image repository (default: `"neo4j"`) |
| `tag` | `string` | Image tag |
| `pullPolicy` | `string` | Pull policy: `"Always"`, `"IfNotPresent"`, `"Never"` |
| `pullSecrets` | `[]string` | Image pull secrets |

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
| `className` | `string` | Storage class name |
| `size` | `string` | Storage size (e.g., `"10Gi"`) |
| `retentionPolicy` | `string` | PVC retention policy: `"Delete"` (default) or `"Retain"` |
| `backupStorage` | [`*BackupStorageSpec`](#backupstoragespec) | Additional storage for backups |

### BackupStorageSpec

| Field | Type | Description |
|---|---|---|
| `className` | `string` | Storage class name for backup volumes |
| `size` | `string` | Storage size for backup volumes |

### AuthSpec

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Auth provider: `"native"`, `"ldap"`, `"jwt"`, `"kerberos"` (default: `"native"`) |
| `adminSecret` | `string` | Secret containing admin username and password |
| `secretRef` | `string` | Secret containing provider-specific configuration |
| `externalSecrets` | [`*ExternalSecretsConfig`](#externalsecretsconfig) | External secrets configuration |
| `passwordPolicy` | [`*PasswordPolicySpec`](#passwordpolicyspec) | Password policy configuration |
| `jwt` | [`*JWTAuthSpec`](#jwtauthspec) | JWT authentication configuration |
| `ldap` | [`*LDAPAuthSpec`](#ldapauthspec) | LDAP authentication configuration |
| `kerberos` | [`*KerberosAuthSpec`](#kerberosauthspec) | Kerberos authentication configuration |

### JWTAuthSpec

| Field | Type | Description |
|---|---|---|
| `validation` | [`*JWTValidationSpec`](#jwtvalidationspec) | JWT validation settings |
| `claimsMapping` | `map[string]string` | Claims mapping |

### JWTValidationSpec

| Field | Type | Description |
|---|---|---|
| `jwksUrl` | `string` | JWKS endpoint URL |
| `issuer` | `string` | JWT issuer |
| `audience` | `[]string` | JWT audience |

### LDAPAuthSpec

| Field | Type | Description |
|---|---|---|
| `server` | [`*LDAPServerSpec`](#ldapserverspec) | LDAP server settings |
| `userSearch` | [`*LDAPSearchSpec`](#ldapsearchspec) | User search settings |
| `groupSearch` | [`*LDAPSearchSpec`](#ldapsearchspec) | Group search settings |

### LDAPServerSpec

| Field | Type | Description |
|---|---|---|
| `urls` | `[]string` | LDAP server URLs |
| `tls` | `bool` | Enable TLS for LDAP connection |
| `insecureSkipVerify` | `bool` | Skip TLS certificate verification |

### LDAPSearchSpec

| Field | Type | Description |
|---|---|---|
| `baseDN` | `string` | Search base DN |
| `filter` | `string` | Search filter |
| `scope` | `string` | Search scope: `"base"`, `"one"`, `"sub"` |

### KerberosAuthSpec

| Field | Type | Description |
|---|---|---|
| `realm` | `string` | Kerberos realm |
| `servicePrincipal` | `string` | Service principal |
| `keytab` | [`*KerberosKeytabSpec`](#kerberoskeytabspec) | Keytab configuration |

### KerberosKeytabSpec

| Field | Type | Description |
|---|---|---|
| `secretRef` | `string` | Secret containing keytab file |
| `key` | `string` | Key in secret containing keytab (default: `"keytab"`) |

### SecurityContextSpec

| Field | Type | Description |
|---|---|---|
| `podSecurityContext` | `*corev1.PodSecurityContext` | Pod-level security settings |
| `containerSecurityContext` | `*corev1.SecurityContext` | Container-level security settings |

### TLSSpec

| Field | Type | Description |
|---|---|---|
| `mode` | `string` | TLS mode: `"cert-manager"` (default) or `"disabled"` |
| `issuerRef` | [`*IssuerRef`](#issuerref) | cert-manager issuer reference |
| `certificateSecret` | `string` | TLS secret name (manual certificates) |
| `externalSecrets` | [`*ExternalSecretsConfig`](#externalsecretsconfig) | External Secrets configuration |
| `duration` | `*string` | Certificate duration (e.g., `"2160h"`) |
| `renewBefore` | `*string` | Renewal window before expiry (e.g., `"360h"`) |
| `subject` | [`*CertificateSubject`](#certificatesubject) | Certificate subject fields |
| `usages` | `[]string` | Certificate usages |

### IssuerRef

| Field | Type | Description |
|---|---|---|
| `name` | `string` | **Required.** Issuer resource name |
| `kind` | `string` | Issuer resource kind. Default: `"ClusterIssuer"`. Use `"Issuer"` for namespace-scoped issuers, or any external issuer kind (e.g. `"AWSPCAClusterIssuer"`, `"VaultIssuer"`, `"GoogleCASClusterIssuer"`) |
| `group` | `string` | API group of the issuer. Default: `cert-manager.io`. Set to the external issuer's API group when using third-party issuers (e.g. `"awspca.cert-manager.io"`) |

**Examples**:

```yaml
# Standard cert-manager ClusterIssuer (default)
issuerRef:
  name: ca-cluster-issuer
  kind: ClusterIssuer

# Namespace-scoped Issuer
issuerRef:
  name: my-issuer
  kind: Issuer

# AWS Private CA (external issuer)
issuerRef:
  name: aws-pca-issuer
  kind: AWSPCAClusterIssuer
  group: awspca.cert-manager.io

# HashiCorp Vault
issuerRef:
  name: vault-issuer
  kind: VaultIssuer
  group: cert.cert-manager.io
```

### CertificateSubject

| Field | Type | Description |
|---|---|---|
| `organizations` | `[]string` | Organization names |
| `countries` | `[]string` | Country codes |
| `organizationalUnits` | `[]string` | Organizational units |
| `localities` | `[]string` | Localities |
| `provinces` | `[]string` | Provinces/States |

### ExternalSecretsConfig

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable External Secrets integration |
| `secretStoreRef` | [`*SecretStoreRef`](#secretstoreref) | SecretStore or ClusterSecretStore reference |
| `refreshInterval` | `string` | Refresh interval (e.g., `"1h"`) |
| `data` | [`[]ExternalSecretData`](#externalsecretdata) | External secret data mappings |

### SecretStoreRef

| Field | Type | Description |
|---|---|---|
| `name` | `string` | SecretStore name |
| `kind` | `string` | `"SecretStore"` or `"ClusterSecretStore"` |

### ExternalSecretData

| Field | Type | Description |
|---|---|---|
| `secretKey` | `string` | Target secret key |
| `remoteRef` | [`*ExternalSecretRemoteRef`](#externalsecretremoteref) | Remote secret reference |

### ExternalSecretRemoteRef

| Field | Type | Description |
|---|---|---|
| `key` | `string` | External secret key |
| `property` | `string` | Property within the secret |
| `version` | `string` | Secret version |

### PropertyShardingSpec

Configures property sharding for horizontal scaling of large datasets. Property sharding separates graph structure from properties, distributing properties across multiple databases for better scalability. Available in Neo4j 2025.12+ Enterprise.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable property sharding for this cluster (default: `false`) |
| `config` | `map[string]string` | Advanced property sharding configuration |

**System Requirements** (validated by operator):

- **Neo4j Version**: 2025.12+ Enterprise
- **Minimum Servers**: 2 servers (3+ recommended for HA graph shard primaries)
- **Memory**: 4GB minimum, 8GB+ recommended per server
- **CPU**: 1+ core minimum, 2+ cores recommended per server
- **Authentication**: Admin secret required
- **Storage**: Persistent storage class required

**Required Configuration** (automatically applied when enabled):

- `internal.dbms.sharded_property_database.enabled: "true"`
- `db.query.default_language: "CYPHER_25"`
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
    server.bolt.thread_pool_min_size: "10"
    server.bolt.thread_pool_max_size: "100"
```

**Resource Recommendations**:

*Development:*
```yaml
resources:
  requests:
    memory: 4Gi    # Absolute minimum for dev/test
    cpu: 2000m     # 2 cores for cross-shard queries
  limits:
    memory: 8Gi    # Recommended for production
    cpu: 2000m
```

*Production:*
```yaml
resources:
  requests:
    memory: 4Gi    # Minimum requirement
    cpu: 2000m     # Cross-shard performance
  limits:
    memory: 8Gi    # Recommended for production
    cpu: 4000m     # Handle peak loads
```

**Validation Errors**:

| Error | Cause | Resolution |
|-------|-------|------------|
| `property sharding requires Neo4j 2025.12+` | Old Neo4j version | Upgrade to 2025.12+ Enterprise |
| `spec.topology.servers in body should be greater than or equal to 2` | Invalid server count | Increase server count to 2+ (3+ recommended for HA) |
| `property sharding requires minimum 4GB memory` | Insufficient memory | Increase memory to 8GB+ (recommended) |
| `property sharding requires minimum 1 CPU core` | Insufficient CPU | Increase CPU to 2+ cores (recommended) |

For detailed configuration, see the [Property Sharding Guide](../user_guide/property_sharding.md).

### BackupsSpec

| Field | Type | Description |
|---|---|---|
| `defaultStorage` | [`*StorageLocation`](#storagelocation) | Default storage location for backups |
| `cloud` | [`*CloudBlock`](#cloudblock) | Cloud provider configuration (credentials/identity) |

### StorageLocation

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Storage type: `"s3"`, `"gcs"`, `"azure"`, `"pvc"` |
| `bucket` | `string` | Bucket name (for cloud storage) |
| `path` | `string` | Path within bucket or PVC |
| `pvc` | [`*PVCSpec`](#pvcspec) | PVC configuration (for `pvc` type) |
| `cloud` | [`*CloudBlock`](#cloudblock) | Cloud provider configuration |

### PVCSpec

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of existing PVC to use |
| `storageClassName` | `string` | Storage class name |
| `size` | `string` | Size for new PVC (e.g., `"100Gi"`) |

### CloudBlock

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Cloud provider: `"aws"`, `"gcp"`, `"azure"` |
| `identity` | [`*CloudIdentity`](#cloudidentity) | Cloud identity configuration |

### CloudIdentity

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| `serviceAccount` | `string` | Service account name for cloud identity |
| `autoCreate` | [`*AutoCreateSpec`](#autocreatespec) | Auto-create service account and annotations |

### AutoCreateSpec

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable auto-creation of service account (default: `true`) |
| `annotations` | `map[string]string` | Annotations to apply to auto-created service account |

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
| `route` | [`RouteSpec`](#routespec) | OpenShift Route configuration |

### IngressSpec

Configures an Ingress resource for HTTP(S) access to Neo4j Browser.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable Ingress creation |
| `className` | `string` | Ingress class name (e.g., `"nginx"`) |
| `annotations` | `map[string]string` | Ingress annotations |
| `host` | `string` | Hostname for the Ingress |
| `tlsSecretName` | `string` | TLS secret name for Ingress |

### RouteSpec

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable OpenShift Route creation |
| `host` | `string` | Hostname for the Route (optional) |
| `path` | `string` | Path for the Route (default: `"/"`) |
| `annotations` | `map[string]string` | Route annotations |
| `tls` | [`*RouteTLSSpec`](#routetlsspec) | TLS settings for the Route |
| `targetPort` | `int32` | Target service port (default: `7474`) |

### RouteTLSSpec

| Field | Type | Description |
|---|---|---|
| `termination` | `string` | TLS termination: `"edge"`, `"reencrypt"`, `"passthrough"` |
| `insecureEdgeTerminationPolicy` | `string` | `"None"`, `"Allow"`, `"Redirect"` |
| `secretName` | `string` | Secret containing certificate (reencrypt/passthrough) |

### MCPServerSpec

Optional MCP server deployment for the cluster. MCP requires the APOC plugin.
HTTP transport uses per-request auth and can be exposed via Service/Ingress/Route on the `/mcp` path. STDIO transport reads credentials from a secret and does not expose a Service.
For client configuration, see the [MCP Client Setup Guide](../user_guide/guides/mcp_client_setup.md).

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable MCP server deployment (default: `false`) |
| `image` | [`*ImageSpec`](#imagespec) | MCP server image (defaults to the operator MCP image repo and `OPERATOR_VERSION` tag, or `latest`) |
| `transport` | `string` | Transport mode: `"http"` (default) or `"stdio"` |
| `readOnly` | `bool` | Disable write tools when `true` (default: `true`) |
| `telemetry` | `bool` | Enable anonymous telemetry (default: `false`) |
| `database` | `string` | Default Neo4j database |
| `schemaSampleSize` | `*int32` | Schema sampling size |
| `logLevel` | `string` | MCP log level |
| `logFormat` | `string` | MCP log format |
| `http` | [`*MCPHTTPConfig`](#mcphttpconfig) | HTTP transport configuration |
| `auth` | [`*MCPAuthSpec`](#mcpauthspec) | STDIO auth configuration |
| `replicas` | `*int32` | MCP pod replicas (default: `1`) |
| `resources` | `*corev1.ResourceRequirements` | Resource requirements |
| `env` | `[]corev1.EnvVar` | Extra environment variables for MCP |
| `securityContext` | [`*SecurityContextSpec`](#securitycontextspec) | Pod/container security overrides |

### MCPHTTPConfig

| Field | Type | Description |
|---|---|---|
| `host` | `string` | HTTP bind host (default: `0.0.0.0`) |
| `port` | `int32` | HTTP bind port (default: `8080`, or `8443` when TLS enabled) |
| `allowedOrigins` | `string` | CORS allowed origins (comma-separated or `*`) |
| `tls` | [`*MCPTLSSpec`](#mcptlsspec) | TLS settings for HTTP transport |
| `service` | [`*MCPServiceSpec`](#mcpservicespec) | Service exposure settings |

### MCPTLSSpec

| Field | Type | Description |
|---|---|---|
| `mode` | `string` | TLS mode: `"disabled"` (default), `"secret"`, `"cert-manager"` |
| `secretName` | `string` | Secret with `tls.crt` and `tls.key` (for `secret` mode) |
| `issuerRef` | [`*IssuerRef`](#issuerref) | cert-manager issuer reference |
| `duration` | `*string` | Certificate duration |
| `renewBefore` | `*string` | Certificate renew window |
| `subject` | [`*CertificateSubject`](#certificatesubject) | Certificate subject details |
| `usages` | `[]string` | Certificate usages |

### MCPServiceSpec

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Service type: `"ClusterIP"`, `"NodePort"`, `"LoadBalancer"` |
| `annotations` | `map[string]string` | Service annotations |
| `loadBalancerIP` | `string` | Static LoadBalancer IP |
| `loadBalancerSourceRanges` | `[]string` | Allowed source ranges |
| `externalTrafficPolicy` | `string` | External traffic policy: `"Cluster"` or `"Local"` |
| `port` | `int32` | Service port for MCP HTTP |
| `ingress` | [`*IngressSpec`](#ingressspec) | Ingress configuration (uses `/mcp` path) |
| `route` | [`*RouteSpec`](#routespec) | OpenShift Route configuration |

### MCPAuthSpec

| Field | Type | Description |
|---|---|---|
| `secretName` | `string` | Secret with username/password keys |
| `usernameKey` | `string` | Username key name (default: `username`) |
| `passwordKey` | `string` | Password key name (default: `password`) |

### UISpec

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable Neo4j UI deployment |
| `ingress` | [`*IngressSpec`](#ingressspec) | UI ingress configuration |
| `resources` | `*corev1.ResourceRequirements` | Resource requirements for UI pods |
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
kubectl patch neo4jenterprisecluster my-cluster -p '{"spec":{"image":{"tag":"2025.01.0-enterprise"}}}'

# Check cluster health
kubectl exec my-cluster-server-0 -- cypher-shell -u neo4j -p password "SHOW SERVERS"

# Monitor server pods
kubectl get pods -l neo4j.com/cluster=my-cluster
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
# Property Sharding cluster (Neo4j 2025.12+)
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: sharding-cluster
spec:
  image:
    repo: neo4j
    tag: 2025.12-enterprise
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
kubectl get pods -l neo4j.com/cluster=my-cluster
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

# Force reconciliation with a no-op annotation change
kubectl annotate neo4jenterprisecluster my-cluster \
  troubleshooting.neo4j.com/reconcile="$(date +%s)" --overwrite
```

For more information:

- [Configuration Best Practices](../user_guide/guides/configuration_best_practices.md)
- [Property Sharding Guide](../user_guide/property_sharding.md)
- [Split-Brain Recovery Guide](../user_guide/troubleshooting/split-brain-recovery.md)
- [Resource Sizing Guide](../user_guide/guides/resource_sizing.md)
- [Fault Tolerance Guide](../user_guide/guides/fault_tolerance.md)
