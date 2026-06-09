# Configuration

This guide provides a comprehensive overview of the configuration options available for both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` custom resources. The operator allows for a declarative approach to managing your Neo4j deployments, where you define the desired state in a YAML file, and the operator works to make it a reality.

## CRD Specification

The full CRD specifications, which detail every possible configuration field, can be found in the API Reference:
- [Neo4jEnterpriseCluster](../api_reference/neo4jenterprisecluster.md) - For clustered deployments
- [Neo4jEnterpriseStandalone](../api_reference/neo4jenterprisestandalone.md) - For single-node deployments

## Key Configuration Fields

Below are some of the most important fields you will use to configure your cluster. For a complete list, please consult the API reference.

*   `spec.image`: The Neo4j Docker image to use. Requires Neo4j Enterprise 5.26+ or 2025.x. You can specify the repository (e.g., `neo4j`), tag (e.g., `5.26-enterprise`), pull policy, and pull secrets for private registries.

#### Private Registry / Image Pull Secrets

To pull Neo4j images from a private registry (ECR, GCR, ACR, or a private Docker Hub account), create a Kubernetes image pull secret and reference it in your cluster spec:

```bash
# Create the pull secret
kubectl create secret docker-registry my-registry-secret \
  --docker-server=<registry-url> \
  --docker-username=<username> \
  --docker-password=<password>
```

```yaml
spec:
  image:
    repo: my-private-registry.example.com/neo4j
    tag: "2025.01.0-enterprise"
    pullSecrets:
      - my-registry-secret
```

The `pullSecrets` field accepts a list of secret names. Secrets must exist in the same namespace as the cluster. The operator automatically propagates the secrets to the StatefulSet's `imagePullSecrets` field.

**Cloud-managed registries**: For ECR (AWS), GCR (Google Cloud), or ACR (Azure), use workload identity / IRSA to avoid long-lived credentials where possible. The `pullSecrets` field supports any Kubernetes `kubernetes.io/dockerconfigjson` secret.
*   `spec.topology`: (Cluster only) Defines the architecture of your cluster. Specify the total number of servers (minimum 2) that will self-organize into primary and secondary roles based on database requirements. You can optionally configure server role constraints.
*   `spec.storage`: Configures the persistent storage for the cluster, including storage class, size, and [retention policy](#storage-and-pvc-retention).
*   `spec.auth`: Manages authentication, allowing you to specify the provider (native, LDAP, etc.) and the secret containing credentials.
*   `spec.resources`: Allows you to set specific CPU and memory requests and limits for the Neo4j pods, which is crucial for performance tuning.
*   Backups: Use the separate `Neo4jBackup` CRD for backup management — see the [Backup and Restore guide](guides/backup_restore.md).
*   `spec.monitoring`: Enable monitoring, Prometheus metrics exposure, and query logging.

> **Live Diagnostics:** When `enabled: true` and the cluster is `Ready`, the operator
> automatically runs `SHOW SERVERS` and `SHOW DATABASES` and writes results to
> `status.diagnostics`. Two new conditions, `ServersHealthy` and `DatabasesHealthy`,
> reflect cluster health without requiring `kubectl exec`. See the
> [Monitoring Guide](guides/monitoring.md#live-cluster-diagnostics) for full details.

*   **Plugin management**: Use separate Neo4jPlugin CRDs to install plugins like APOC, GDS, Bloom, GenAI, and N10s. The operator automatically handles Neo4j 5.26+ compatibility requirements (see [Neo4jPlugin API Reference](../api_reference/neo4jplugin.md)).
*   `spec.mcp`: Optional Neo4j MCP server deployment for client integrations (HTTP or STDIO). Requires the APOC plugin via Neo4jPlugin; HTTP uses per-request auth and supports Service/Ingress/Route exposure with optional TLS.
*   `spec.tls`: Configure TLS/SSL encryption. Set mode to `cert-manager` and provide an issuerRef for automatic certificate management.
*   `spec.config`: Add custom Neo4j configuration settings as key-value pairs. These are added to neo4j.conf.
*   `spec.env`: Add environment variables to Neo4j pods. Note that NEO4J_AUTH and NEO4J_ACCEPT_LICENSE_AGREEMENT are managed by the operator.

    > **Warning**: Do not set `NEO4J_AUTH` via `spec.env`. The operator builds it from the admin Secret's `username` / `password` keys (referenced via `spec.auth.adminSecret`); overriding via `spec.env` bypasses the Secret-managed flow and the two paths can desync on Secret rotation. Override the Secret itself, not the env var.
*   `spec.service`: Configure service type (ClusterIP, NodePort, LoadBalancer), annotations, and external access settings (Ingress; OpenShift Route).
*   `spec.propertySharding`: (Neo4j 2025.12+) Enable property sharding for horizontal scaling of large datasets. See the [Property Sharding Guide](property_sharding.md) for detailed configuration options.

## Storage and PVC Retention

The `spec.storage` section configures persistent volumes for Neo4j data. The most important field users overlook is `retentionPolicy`, which controls what happens to your data when a cluster or standalone is deleted.

### Retention Policy

| Value | Behavior | Use When |
|-------|----------|----------|
| `Delete` (default) | PVCs are **permanently deleted** when the cluster/standalone is removed | Development, testing, temporary deployments |
| `Retain` | PVCs are **preserved** after deletion and can be manually recovered or reused | Production, valuable data, compliance requirements |

> **Data loss warning:** The default is `Delete`. If you delete a `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` resource without changing this default, **all data on the associated PVCs will be permanently lost**. There is no undo. For production deployments, always set `retentionPolicy: Retain`.

### Configuration

```yaml
spec:
  storage:
    className: premium-rwo        # Your StorageClass
    size: "100Gi"
    retentionPolicy: Retain       # Keep PVCs on deletion (recommended for production)
```

This applies identically to both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`.

### What happens with each policy

**With `Delete` (default):**
1. You run `kubectl delete neo4jenterprisecluster my-cluster`
2. The operator deletes the StatefulSet and all associated PVCs
3. The underlying PersistentVolumes are released and reclaimed per the StorageClass `reclaimPolicy`
4. Data is gone

**With `Retain`:**
1. You run `kubectl delete neo4jenterprisecluster my-cluster`
2. The operator deletes the StatefulSet but **leaves PVCs intact**
3. PVCs remain in the namespace with their data
4. You can inspect the data, attach it to a new deployment, or manually delete when ready

### Checking current policy

```bash
# Check the retention policy of a running cluster
kubectl get neo4jenterprisecluster my-cluster -o jsonpath='{.spec.storage.retentionPolicy}'

# List PVCs that would be affected
kubectl get pvc -l app=my-cluster
```

### Recovering retained PVCs

If you deleted a cluster with `Retain` and want to redeploy using the same data, create a new cluster with the same name and storage configuration. The StatefulSet will reattach to the existing PVCs (matched by name).

### Best practices

- **Production**: Always set `retentionPolicy: Retain` and rely on backups (via `Neo4jBackup` CRD) for disaster recovery
- **Development**: `Delete` is fine for ephemeral environments — keeps namespaces clean
- **CI/CD**: Use `Delete` in test pipelines to avoid PVC accumulation
- **Before deletion**: Always verify the retention policy before deleting a cluster: `kubectl get neo4jenterprisecluster <name> -o jsonpath='{.spec.storage.retentionPolicy}'`

## MCP Server

The operator can deploy an optional Neo4j MCP server alongside a cluster or standalone deployment. It uses the **official `mcp/neo4j` image** ([Docker Hub](https://hub.docker.com/r/mcp/neo4j), [source](https://github.com/neo4j/mcp)) — the supported Neo4j product MCP server.

The MCP server runs as a separate Deployment and connects to the Neo4j service inside the namespace.
For client configuration and HTTP/STDIO usage, see the [MCP Client Setup Guide](guides/mcp_client_setup.md).

### Requirements

*   **APOC**: MCP requires APOC for the `get-schema` tool. Install APOC using the Neo4jPlugin CRD (see [Neo4jPlugin API Reference](../api_reference/neo4jplugin.md)).
*   **Image**: If `spec.mcp.image` is omitted the operator defaults to `mcp/neo4j:latest`. Pin a version with `spec.mcp.image.tag`.

### Transport Modes

*   **HTTP (default)**: No static credentials in the MCP pod. Each client request carries a Basic Auth or Bearer token `Authorization` header; the server uses those credentials to connect to Neo4j per-request. The operator creates a Service (`<name>-mcp:8080`) and optionally an Ingress or OpenShift Route. The endpoint path is `/mcp` (fixed).
    *   **Benefits**: per-request auth, multi-user, works well with desktop clients (Claude Desktop, VSCode).
*   **STDIO (in-cluster only)**: The operator injects `NEO4J_USERNAME` and `NEO4J_PASSWORD` from the admin secret (or a custom secret via `spec.mcp.auth`). No Service/Ingress/Route is created. Use for in-cluster automation.

### TLS for HTTP

The official image supports container-level TLS. Provide a Kubernetes TLS secret via `spec.mcp.http.tls.secretName`; the operator mounts it and sets `NEO4J_MCP_HTTP_TLS_ENABLED=true`.

Default ports: `8080` (no TLS) or `8443` (with TLS). Override with `spec.mcp.http.port`.

> **Tip**: For most deployments, terminate TLS at the Ingress layer and leave `spec.mcp.http.tls` unset.

### Example: Cluster MCP (HTTP)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: graph-prod
spec:
  image:
    repo: neo4j
    tag: 2025.01.0-enterprise
  topology:
    servers: 3
  storage:
    className: standard
    size: 50Gi
  mcp:
    enabled: true
    # image defaults to mcp/neo4j:latest — no need to specify
    transport: http
    readOnly: true
    http:
      service:
        type: ClusterIP
```

### Example: Cluster MCP (HTTP with Ingress)

```yaml
  mcp:
    enabled: true
    transport: http
    readOnly: true
    http:
      service:
        type: ClusterIP
        ingress:
          enabled: true
          host: neo4j-mcp.example.com
          className: nginx
          tlsSecretName: neo4j-mcp-tls
```

### Example: Standalone MCP (STDIO)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseStandalone
metadata:
  name: graph-dev
spec:
  image:
    repo: neo4j
    tag: 5.26.0-enterprise
  storage:
    className: standard
    size: 10Gi
  auth:
    adminSecret: neo4j-admin-secret
  mcp:
    enabled: true
    transport: stdio
    # auth defaults to the cluster admin secret; override if needed:
    # auth:
    #   secretName: my-readonly-user
```

## Best practices for `spec.config`

`spec.config` is a free-form `map[string]string` that lands in `neo4j.conf`. A lot of older example material still floats around using the pre-5.x key namespace. Use the current keys below and you'll avoid the most common foot-guns.

### Memory

Use the `server.memory.*` namespace. The pre-5.x `dbms.memory.*` keys still appear in many older tutorials but are deprecated:

```yaml
config:
  server.memory.heap.initial_size: "2G"
  server.memory.heap.max_size: "4G"
  server.memory.pagecache.size: "2G"
```

Don't use `dbms.memory.*` — those keys have been deprecated since Neo4j 5.0.

### Query log

Neo4j 5.x+ uses the `db.logs.query.*` namespace; the validator rejects the legacy `dbms.logs.query.enabled` form at apply time:

```yaml
config:
  db.logs.query.enabled: "INFO"
  db.logs.query.threshold: "1s"
  db.logs.query.parameter_logging_enabled: "true"
```

### TLS — operator-managed, off-limits in `spec.config`

TLS is configured via `spec.tls`, not `spec.config`. Setting any of the following keys in `spec.config` is rejected by the validator at apply time:

- `server.bolt.tls_level` (operator emits `REQUIRED` when TLS is enabled)
- `dbms.ssl.policy.{bolt,https,cluster}.*` (full SSL policy block is operator-managed)
- `server.directories.certificates`

The pre-5.x `dbms.connector.{https,bolt}.*` keys are deprecated and superseded by the `server.*` namespace (`spec.tls` replaces the TLS-related ones). Reason for rejecting the keys above: Neo4j runs with `server.config.strict_validation.enabled=false`, so duplicate keys silently override each other; the validator blocks user values that would shadow operator-managed ones. See the [TLS certificates guide](tls_configuration.md) for the full TLS surface.

### Cluster discovery — operator-managed, off-limits in `spec.config`

The operator emits version-specific discovery config at pod startup (LIST resolver, static pod FQDNs, port 6000). All of the following are rejected by the validator:

- `dbms.cluster.discovery.resolver_type`
- `dbms.cluster.discovery.v2.endpoints` (operator-managed for Neo4j 5.26.x)
- `dbms.cluster.endpoints` (operator-managed for Neo4j 2025.x+)
- `dbms.kubernetes.label_selector` (K8s discovery is not used; LIST resolver only)

See the [Clustering guide](clustering.md) for what the operator writes for each Neo4j version.

### Deprecated / removed keys to avoid

| Key | Status | Use instead |
|---|---|---|
| `dbms.mode=SINGLE` | Removed in 5.x | (no replacement — standalone is just `Neo4jEnterpriseStandalone`) |
| `dbms.memory.*` | Deprecated | `server.memory.*` |
| `dbms.connector.*` | Deprecated | `server.bolt.*` / `server.http.*` / `server.https.*` (or `spec.tls`) |
| `causal_clustering.*` | Removed in 5.x | `dbms.cluster.*` |
| `db.format=standard` / `db.format=high_limit` | Deprecated since 5.23 | `db.format=block` |
| `server.groups` | Deprecated | `initial.server.tags` |
| `dbms.logs.query.*` | Deprecated namespace | `db.logs.query.*` |
| `dbms.cluster.role` | Removed in 5.0 | `SHOW DATABASES` / `SHOW SERVERS` |
| `metrics.bolt.*` | Deprecated | (removed metric category) |

### Sample production cluster config

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  topology:
    servers: 5   # self-organise into primary/secondary
  config:
    # Memory
    server.memory.heap.initial_size: "8G"
    server.memory.heap.max_size: "16G"
    server.memory.pagecache.size: "8G"

    # Query logging
    db.logs.query.enabled: "INFO"
    db.logs.query.threshold: "1s"
    db.logs.query.parameter_logging_enabled: "true"

    # Transactions
    db.transaction.timeout: "5m"
    db.lock.acquisition.timeout: "2m"

    # Checkpointing
    db.checkpoint.interval.time: "15m"
    db.checkpoint.interval.tx: "100000"
```

### Sample development standalone config

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseStandalone
metadata:
  name: dev-instance
spec:
  config:
    server.memory.heap.initial_size: "1G"
    server.memory.heap.max_size: "2G"
    server.memory.pagecache.size: "512M"

    db.logs.query.enabled: "INFO"   # enum: OFF | INFO | VERBOSE (not a boolean)
    dbms.security.procedures.unrestricted: "gds.*,apoc.*"
    dbms.security.allow_csv_import_from_file_urls: "true"
```

### Configuration the operator writes for you

You don't need to set these yourself — the operator injects them at pod startup based on `spec.topology` and the Neo4j version:

**Cluster deployments**
- LIST discovery with static pod FQDNs (`{cluster}-server-{n}.{cluster}-headless.{ns}.svc.cluster.local:6000`)
- Version-specific endpoint key (`dbms.cluster.discovery.v2.endpoints` for 5.26.x, `dbms.cluster.endpoints` for 2025.x+)
- `dbms.cluster.discovery.version=V2_ONLY` (5.26.x only — V2 is the only protocol in CalVer)
- ME/OTHER bootstrap strategy (server-0 is the preferred bootstrapper)
- RAFT and routing port advertisement

**Standalone deployments**
- Unified clustering infrastructure (no `dbms.mode=SINGLE`)
- Single-member cluster configuration
- Listen-address bindings

## Migrating from older Neo4j versions

If you're moving from Neo4j 4.x or an early 5.x release:

1. `dbms.memory.*` → `server.memory.*`
2. `dbms.connector.*` → `server.bolt.*` / `server.http.*` / `server.https.*` (and TLS via `spec.tls`)
3. Remove any `dbms.mode=SINGLE` — there is no replacement; use `Neo4jEnterpriseStandalone` instead
4. `causal_clustering.*` → `dbms.cluster.*` (most discovery keys are now operator-managed anyway)
5. `db.format=standard` / `db.format=high_limit` → `db.format=block` for new databases
6. `dbms.logs.query.*` → `db.logs.query.*`

See the [Migration Guide](migration_guide.md) for operator-level migration steps (removed CRD fields, etc.).

## References

- [Neo4j 5.26 configuration settings](https://neo4j.com/docs/operations-manual/5/configuration/configuration-settings/)
- [Neo4j 2025.x configuration settings](https://neo4j.com/docs/operations-manual/2025.06/configuration/configuration-settings/)
- [Neo4j upgrade guide](https://neo4j.com/docs/upgrade-migration-guide/current/)
