# Monitoring

This guide explains how to expose Neo4j metrics for Prometheus and where the operator wires things for you.

## Enable metrics via `spec.queryMonitoring`

The operator uses Neo4j's built-in Prometheus endpoint (see https://neo4j.com/docs/operations-manual/current/monitoring/metrics/expose/). When `spec.queryMonitoring.enabled` is true, the operator:

- Enables the Neo4j Prometheus endpoint (`server.metrics.prometheus.enabled=true`).
- Binds it to `0.0.0.0:2004`.
- Exposes port `2004` on the Neo4j container.
- Adds `prometheus.io/*` annotations for scrape-based setups.

### Cluster example

```yaml
spec:
  queryMonitoring:
    enabled: true
```

For clusters, the operator also creates a `Service` named `<cluster>-metrics` and attempts to create a `ServiceMonitor` named `<cluster>-query-monitoring` if the Prometheus Operator CRDs are available.

### Standalone example

```yaml
spec:
  queryMonitoring:
    enabled: true
```

For standalone deployments, the metrics port is added to the `<standalone>-service` Service.

## Prometheus scraping

### Prometheus Operator

If you use Prometheus Operator, the ServiceMonitor created for clusters will target the `<cluster>-metrics` Service (port `metrics`). For standalone deployments, create your own ServiceMonitor pointing at the `<standalone>-service` Service.

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

Note: for standalone deployments, scrape `<standalone>-service.<namespace>.svc.cluster.local:2004` (the metrics port is added to the same Service that serves Bolt/HTTP).

## Customizing metrics settings

If you override the metrics endpoint in `spec.config`, keep the Service port aligned:

```yaml
spec:
  queryMonitoring:
    enabled: true
  config:
    server.metrics.prometheus.endpoint: "0.0.0.0:2004"
```

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

When `spec.queryMonitoring.enabled: true` and the cluster is in `Ready` phase, the
operator automatically collects live diagnostics by running `SHOW SERVERS` and
`SHOW DATABASES` against the cluster. Results are written to `status.diagnostics`
and two new Kubernetes conditions without requiring `kubectl exec` into pods.

### Prerequisites

```yaml
spec:
  queryMonitoring:
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

Set `spec.queryMonitoring.enabled: false` (or omit the `queryMonitoring` section entirely).
The `status.diagnostics` field will remain at its last-known value but will not be updated.
