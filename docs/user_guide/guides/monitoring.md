# Monitoring

This guide explains how to expose Neo4j metrics for Prometheus and where the operator wires things for you. For a complete end-to-end setup with Grafana dashboards and alerting, see the [Prometheus and Grafana Setup Guide](prometheus-grafana-setup.md).

## Enable metrics via `spec.monitoring`

The operator uses Neo4j's built-in Prometheus endpoint (see https://neo4j.com/docs/operations-manual/current/monitoring/metrics/expose/). When `spec.monitoring.enabled` is true, the operator:

- Enables the Neo4j Prometheus endpoint (`server.metrics.prometheus.enabled=true`).
- Binds it to `0.0.0.0:2004` (safe in Kubernetes — pod network isolation prevents external access).
- Exposes port `2004` on the Neo4j container.
- Adds `prometheus.io/*` annotations for scrape-based setups.

Independently of `spec.monitoring`, the operator always disables the JMX and CSV metrics reporters (`server.metrics.jmx.enabled=false`, `server.metrics.csv.enabled=false`) on every Neo4j deployment — JMX is an unauthenticated management surface and CSV writes one file per metric into the pod's ephemeral filesystem. These hardening defaults are emitted regardless of whether monitoring is enabled; re-enable via `spec.config` if you genuinely need them.

### Cluster example

```yaml
spec:
  monitoring:
    enabled: true
```

For clusters, the operator also creates:

- A `Service` named `<cluster>-metrics` (port 2004)
- A `ServiceMonitor` named `<cluster>-monitoring` (if Prometheus Operator CRDs are available)

### Standalone example

```yaml
spec:
  monitoring:
    enabled: true
```

For standalone deployments, the metrics port is added to the `<standalone>-client` Service and a `ServiceMonitor` named `<standalone>-monitoring` is created automatically.

### Full configuration example

```yaml
spec:
  monitoring:
    enabled: true
    slowQueryThreshold: "5s"       # Log queries slower than this (maps to db.logs.query.threshold)
    queryLogLevel: "INFO"          # OFF, INFO, or VERBOSE (maps to db.logs.query.enabled)
    obfuscateLiterals: true        # Mask literal values in query logs (recommended in production)
    explainPlan: false             # Include execution plan in logs (performance impact — avoid in production)
    metricsFilter: "*"             # Enable all Neo4j metrics (default: subset only)
    metricsPrefix: "neo4j"         # Custom prefix for metric names
```

## Prometheus scraping

### Prometheus Operator

The operator auto-creates `ServiceMonitor` resources for both cluster and standalone deployments when `monitoring.enabled: true`. These target the metrics Service on port `metrics` (2004) with a 30-second scrape interval. No manual ServiceMonitor creation is needed.

### Standard Prometheus

Add a scrape config that targets the metrics Service:

```yaml
scrape_configs:
  - job_name: neo4j
    metrics_path: /metrics
    static_configs:
      - targets:
          - <cluster>-metrics.<namespace>.svc.cluster.local:2004
```

Note: for standalone deployments, scrape `<standalone>-client.<namespace>.svc.cluster.local:2004` (the metrics port is added to the same Service that serves Bolt/HTTP). Scrape configs still pointing at the legacy `<standalone>-service` name keep working this release, but that alias is deprecated and will be removed next release — switch to `-client`.

### Operator's own metrics (TokenReview-authenticated)

The Neo4j *operator* exposes its own Prometheus metrics on port 8080 of the operator pod. As of v1.10 the operator's metrics endpoint defaults to **HTTPS with TokenReview-based authentication** (`metrics.secure=true` in the helm chart) — anonymous scrapes are rejected with 401/403. To scrape the operator's own metrics:

1. Bind a ServiceAccount to the `metrics-reader` ClusterRole the operator ships:
   ```bash
   kubectl create clusterrolebinding prom-metrics-reader \
       --clusterrole=metrics-reader \
       --serviceaccount=monitoring:prometheus
   ```
2. On Kubernetes 1.24+, manually create a long-lived token Secret for that SA:
   ```yaml
   apiVersion: v1
   kind: Secret
   type: kubernetes.io/service-account-token
   metadata:
     name: prometheus-metrics-token
     namespace: monitoring
     annotations:
       kubernetes.io/service-account.name: prometheus
   ```
3. Tell the operator's `ServiceMonitor` to use that token by setting `--set metrics.serviceMonitor.bearerTokenSecret.name=prometheus-metrics-token` at install time.

The `ServiceMonitor` template wires `scheme: https`, `tlsConfig.insecureSkipVerify: true` (controller-runtime serves a self-signed cert by default; swap for a cert-manager bundle in production), and the bearer token reference automatically when `metrics.secure=true`.

Set `metrics.secure=false` in helm values to revert to plain HTTP without authn — closes the door on the November 2025 security review #5 remediation, but is sometimes required for legacy scrapers that can't carry bearer tokens.

## Customizing metrics settings

### Metrics filter

By default, Neo4j only exposes a subset of its metrics. To enable all metrics or select specific categories:

```yaml
spec:
  monitoring:
    enabled: true
    metricsFilter: "*"  # Enable all metrics
```

Or select specific categories with glob patterns:

```yaml
spec:
  monitoring:
    enabled: true
    metricsFilter: "*bolt*,*transaction*,*page_cache*,*cluster.raft*"
```

### Query log security

In production environments, enable literal obfuscation to prevent sensitive data (passwords, PII) from appearing in query logs:

```yaml
spec:
  monitoring:
    enabled: true
    obfuscateLiterals: true
    queryLogLevel: "INFO"           # Log only slow queries, not all queries
    slowQueryThreshold: "2s"
```

## Version-specific metrics

Neo4j metric names are identical across 5.26.x and 2025.x+ CalVer releases, but some metrics are only available in newer versions:

| Metric Category | 5.26.x | 2025.x+ |
|---|---|---|
| Core metrics (Bolt, transactions, page cache, JVM) | Yes | Yes |
| CPU usage (`vm.cpu_load.*`) | No | 2025.01+ |
| Raft snapshot metrics (`cluster.raft.snapshot_*`) | No | 2025.01+ |
| Virtual threads (`vm.threads.virtual`) | No | 2025.05+ |
| Raft election/queue metrics | No | 2025.02+ |
| Store copy download metrics | No | 2025.02+ |
| Deadlock rollback counter | No | 2026.01+ |
| Page cache async IO metrics | No | 2026.02+ |
| Discovery v1 metrics (`cluster.discovery.cluster.*`) | Deprecated | Removed |

### Prometheus metric naming

Neo4j converts metric names for Prometheus: dots become underscores, counter metrics get a `_total` suffix. The `# HELP` comment preserves the original name.

Examples:

- `neo4j.dbms.bolt.connections_running` → `neo4j_dbms_bolt_connections_running`
- `neo4j.database.<db>.transaction.committed` → `neo4j_database_<db>_transaction_committed_total`
- `neo4j.page_cache.hit_ratio` → `neo4j_page_cache_hit_ratio`

## Aura Fleet Management (cloud monitoring)

For a hosted monitoring experience, you can register your deployment with [Neo4j Aura Fleet Management](https://neo4j.com/docs/aura/fleet-management/). This lets you view topology, status, and metrics for all self-managed Neo4j instances alongside your Aura-managed instances in the Aura console.

The operator handles plugin installation and token registration automatically. See the [Aura Fleet Management Guide](../aura_fleet_management.md) for setup instructions.

## Complete Metrics Reference

The operator registers the following Prometheus metrics. All metrics use the prefix `neo4j_operator_` (composed from the `neo4j_operator` subsystem in `internal/metrics/metrics.go`).

### Cluster metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_cluster_healthy` | Gauge | `cluster_name`, `namespace` | `1` when cluster is healthy, `0` otherwise |
| `neo4j_operator_cluster_replicas_total` | Gauge | `cluster_name`, `namespace`, `role` (`primary`/`secondary`) | Current replica counts by role |
| `neo4j_operator_cluster_phase` | Gauge | `cluster_name`, `namespace`, `phase` | `1` for the current phase, `0` for all others (phases: `Pending`, `Forming`, `Ready`, `Failed`, `Degraded`, `Upgrading`) |
| `neo4j_operator_split_brain_detected_total` | Counter | `cluster_name`, `namespace` | Total split-brain detection events |
| `neo4j_operator_server_health` | Gauge | `cluster_name`, `namespace`, `server_name`, `server_address` | `1` = Enabled+Available; `0` = degraded |

### Reconcile metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_reconcile_total` | Counter | `cluster_name`, `namespace`, `operation`, `result` (`success`/`failure`) | Total reconciliation attempts |
| `neo4j_operator_reconcile_duration_seconds` | Histogram | `cluster_name`, `namespace`, `operation` | Reconciliation loop duration |

### Upgrade metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_upgrade_total` | Counter | `cluster_name`, `namespace`, `result` (`success`/`failure`) | Total upgrade attempts |
| `neo4j_operator_upgrade_duration_seconds` | Histogram | `cluster_name`, `namespace`, `phase` | Duration per upgrade phase |

### Backup metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_backup_total` | Counter | `cluster_name`, `namespace`, `result` (`success`/`failure`) | Total backup attempts |
| `neo4j_operator_backup_duration_seconds` | Histogram | `cluster_name`, `namespace` | Backup job duration |
| `neo4j_operator_backup_size_bytes` | Gauge | `cluster_name`, `namespace` | Size of the last successful backup in bytes |

### Cypher execution metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_cypher_executions_total` | Counter | `cluster_name`, `namespace`, `operation`, `result` (`success`/`failure`) | Total Cypher statement executions by the operator |
| `neo4j_operator_cypher_execution_duration_seconds` | Histogram | `cluster_name`, `namespace`, `operation` | Duration of operator-issued Cypher statements |

### Security operation metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_security_operations_total` | Counter | `cluster_name`, `namespace`, `operation`, `result` (`success`/`failure`) | Total security operations (user, role, grant) |

### Resource conflict metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_resource_version_conflicts_total` | Counter | `resource_type`, `namespace` | Total Kubernetes resource version conflicts encountered |
| `neo4j_operator_conflict_retry_attempts` | Histogram | `resource_type`, `namespace` | Retry attempts needed to resolve each conflict |
| `neo4j_operator_conflict_retry_duration_seconds` | Histogram | `resource_type`, `namespace` | Time spent retrying due to resource version conflicts |

### Disaster recovery metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_disaster_recovery_status` | Gauge | `cluster_name`, `namespace`, `primary_region`, `secondary_region` | `1` = DR ready, `0` = not ready |
| `neo4j_operator_failover_total` | Counter | `cluster_name`, `namespace`, `result` (`success`/`failure`) | Total failovers performed |
| `neo4j_operator_replication_lag_seconds` | Gauge | `cluster_name`, `namespace`, `primary_region`, `secondary_region` | Replication lag in seconds |

### Scaling metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `neo4j_operator_manual_scaler_enabled` | Gauge | `cluster_name`, `namespace` | `1` = manual scaling enabled, `0` = disabled |
| `neo4j_operator_scale_events_total` | Counter | `cluster_name`, `namespace`, `node_type`, `direction` (`up`/`down`) | Total manual scale events |
| `neo4j_operator_primary_count` | Gauge | `cluster_name`, `namespace` | Current number of primary nodes |
| `neo4j_operator_secondary_count` | Gauge | `cluster_name`, `namespace` | Current number of secondary nodes |
| `neo4j_operator_scaling_validation_total` | Counter | `cluster_name`, `namespace`, `validation_type`, `result` (`success`/`failure`) | Total scaling validation attempts |

## Live Cluster Diagnostics

When `spec.monitoring.enabled: true` and the cluster is in `Ready` phase, the
operator automatically collects live diagnostics by running `SHOW SERVERS` and
`SHOW DATABASES` against the cluster. Results are written to `status.diagnostics`
and two new Kubernetes conditions without requiring `kubectl exec` into pods.

### Prerequisites

```yaml
spec:
  monitoring:
    enabled: true
```

The operator creates a Neo4j client connection for diagnostics only after the cluster
reaches `Ready` phase. No extra configuration is needed — diagnostics run automatically
on every reconcile cycle.

### Viewing Diagnostic Status

```bash
# View the full diagnostics sub-object
kubectl get neo4jenterprisecluster <name> -o json | jq '.status.diagnostics'

# Quick server state overview
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{range .status.diagnostics.servers[*]}{.name}{"\t"}{.state}{"\t"}{.health}{"\n"}{end}'

# Quick database status overview
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{range .status.diagnostics.databases[*]}{.name}{"\t"}{.status}{"\n"}{end}'

# When diagnostics were last collected
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.diagnostics.lastCollected}'

# Any collection error
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.diagnostics.collectionError}'
```

### Diagnostics Status Fields

| Field | Type | Description |
|---|---|---|
| `status.diagnostics.servers[]` | Array | One entry per server from `SHOW SERVERS` |
| `status.diagnostics.servers[].name` | string | Server display name |
| `status.diagnostics.servers[].address` | string | Bolt address |
| `status.diagnostics.servers[].state` | string | Lifecycle state: `Enabled`, `Cordoned`, `Deallocating` |
| `status.diagnostics.servers[].health` | string | Health status: `Available` or `Unavailable` |
| `status.diagnostics.servers[].hostingDatabases` | int | Number of databases hosted |
| `status.diagnostics.databases[]` | Array | One entry per database from `SHOW DATABASES` |
| `status.diagnostics.databases[].name` | string | Database name |
| `status.diagnostics.databases[].status` | string | Current status: `online`, `offline`, `quarantined` |
| `status.diagnostics.databases[].requestedStatus` | string | Desired status |
| `status.diagnostics.databases[].role` | string | Role on last-contacted server: `primary`, `secondary` |
| `status.diagnostics.databases[].default` | bool | True for the default database |
| `status.diagnostics.users[]` | Array | Bounded summary of users from `SHOW USERS` (user, roles, suspended, homeDatabase) |
| `status.diagnostics.userCount` | int | Total users observed, even when the `users` list is truncated |
| `status.diagnostics.roles[]` | Array | Bounded summary of roles from `SHOW ROLES` (role, immutable) |
| `status.diagnostics.roleCount` | int | Total roles observed, even when the `roles` list is truncated |
| `status.diagnostics.lastCollected` | RFC3339 | Timestamp of last successful collection |
| `status.diagnostics.collectionError` | string | Error from last collection attempt; empty on success |

### Diagnostic Conditions

The operator maintains two standard Kubernetes conditions on the cluster resource:

| Condition | `True` when | `False` when | `Unknown` when |
|---|---|---|---|
| `ServersHealthy` | All servers are `state=Enabled` **and** `health=Available` | Any server is Cordoned, Deallocating, or Unavailable | Diagnostics cannot be collected |
| `DatabasesHealthy` | All user databases have `status=online` | Any database has `requestedStatus=online` but `status≠online` | Diagnostics cannot be collected |

> **Note:** The `system` database is excluded from the `DatabasesHealthy` check because it has special internal lifecycle behavior.

Check conditions directly:

```bash
# Check ServersHealthy condition
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.conditions[?(@.type=="ServersHealthy")]}'

# Check DatabasesHealthy condition
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.conditions[?(@.type=="DatabasesHealthy")]}'

# Watch all conditions
kubectl get neo4jenterprisecluster <name> -o jsonpath='{.status.conditions}' | jq .
```

### Prometheus Server Health Metric

The operator exposes a per-server health gauge when diagnostics are enabled:

| Metric | Labels | Value |
|---|---|---|
| `neo4j_operator_server_health` | `cluster_name`, `namespace`, `server_name`, `server_address` | `1` = Enabled+Available; `0` = degraded |

**Example PrometheusRule alert:**

```yaml
groups:
  - name: neo4j-operator
    rules:
      - alert: Neo4jServerDegraded
        expr: neo4j_operator_server_health == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Neo4j server {{ $labels.server_name }} in cluster {{ $labels.cluster_name }} is degraded"
          description: "Server {{ $labels.server_name }} at {{ $labels.server_address }} has been unhealthy for 5 minutes"
```

Enable the ServiceMonitor to scrape this metric (see [Prometheus scraping](#prometheus-scraping) above).

### Troubleshooting Diagnostics Collection

**`status.diagnostics.collectionError` is set:**

This means the operator could not reach the cluster via Bolt. Common causes:

| Cause | Check |
|---|---|
| Cluster not yet `Ready` | Diagnostics only run when `status.phase=Ready`; check cluster phase |
| Auth secret missing or wrong | Check `spec.auth.adminSecret`; verify the secret exists |
| Network policy blocking Bolt | Verify the operator pod can reach port 7687 of the cluster service |
| Cluster overloaded | The Bolt client uses a 10s timeout; check Neo4j pod resource usage |

**`ServersHealthy=False`:**

```bash
# Get details on which servers are degraded
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.conditions[?(@.type=="ServersHealthy")].message}'

# Check server pods directly
kubectl get pods -l neo4j.com/cluster=<name>
```

**`DatabasesHealthy=False`:**

```bash
# Get details on which databases are offline
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.conditions[?(@.type=="DatabasesHealthy")].message}'

# Exec into a pod to investigate
kubectl exec <cluster-name>-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p <password> "SHOW DATABASES"
```

### Disabling Diagnostics

Set `spec.monitoring.enabled: false` (or omit the `monitoring` section entirely).
The `status.diagnostics` field will remain at its last-known value but will not be updated.
