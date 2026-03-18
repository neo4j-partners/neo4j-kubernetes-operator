# Prometheus and Grafana Setup Guide

This guide walks you through connecting Prometheus and Grafana to monitor both the Neo4j Operator and your Neo4j Enterprise deployments. It covers installation, configuration, dashboard setup, and alerting — from zero to a fully observable Neo4j environment on Kubernetes.

## Overview

The Neo4j Operator exposes two independent metric streams:

| Source | Port | Description |
|---|---|---|
| **Operator metrics** | 8080 | Reconciliation, backup, upgrade, cluster health, scaling, and security metrics (prefix: `neo4j_operator_`) |
| **Neo4j native metrics** | 2004 | Database engine metrics — transactions, queries, page cache, store size, Bolt connections (prefix: `neo4j_`) |

Both streams are standard Prometheus `/metrics` endpoints. This guide shows how to scrape them, visualise them in Grafana, and set up alerts.

## Prerequisites

- A running Kubernetes cluster (Kind, EKS, GKE, AKS, etc.)
- `kubectl` and `helm` CLI tools installed
- The Neo4j Operator deployed (via Helm or `make operator-setup`)
- At least one `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` resource deployed

---

## Stage 1: Enable Neo4j Metrics

Before Prometheus can scrape Neo4j, you must enable the built-in Prometheus endpoint on your Neo4j deployment.

### Cluster deployments

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  # ... other config ...
  monitoring:
    enabled: true
```

When `monitoring.enabled: true`, the operator automatically:
- Sets `server.metrics.prometheus.enabled=true` and binds to `0.0.0.0:2004`
- Exposes container port 2004 on every Neo4j pod
- Adds `prometheus.io/*` annotations for annotation-based scraping
- Creates a dedicated `my-cluster-metrics` Service (port 2004)
- Creates a `ServiceMonitor` named `my-cluster-monitoring` (if Prometheus Operator CRDs exist)
- Creates a `PrometheusRule` named `my-cluster-query-alerts` with default alert rules

### Standalone deployments

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: my-standalone
spec:
  # ... other config ...
  monitoring:
    enabled: true
```

For standalone, the metrics port is added to the existing `my-standalone-service` Service — no separate metrics Service is created.

### Verify metrics are exposed

```bash
# Port-forward to any Neo4j pod and check the /metrics endpoint
kubectl port-forward pod/my-cluster-server-0 2004:2004 &
curl -s http://localhost:2004/metrics | head -20
```

You should see lines like `neo4j_bolt_connections_opened_total`, `neo4j_db_query_execution_latency_millis`, etc.

---

## Stage 2: Install the kube-prometheus-stack

The [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) Helm chart installs Prometheus, Grafana, Alertmanager, and the Prometheus Operator in one step.

### Add the Helm repository

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

### Install with recommended settings

Create a values file `prometheus-stack-values.yaml`:

```yaml
# prometheus-stack-values.yaml
prometheus:
  prometheusSpec:
    # Discover ServiceMonitors in all namespaces
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorNamespaceSelector: {}
    # Discover PrometheusRules in all namespaces
    ruleSelectorNilUsesHelmValues: false
    ruleNamespaceSelector: {}
    # Discover PodMonitors in all namespaces (optional)
    podMonitorSelectorNilUsesHelmValues: false
    podMonitorNamespaceSelector: {}
    # Storage (optional but recommended for persistence)
    storageSpec:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi

grafana:
  # Enable persistence so dashboards survive restarts
  persistence:
    enabled: true
    size: 5Gi
  # Default admin credentials
  adminUser: admin
  adminPassword: admin
  # Auto-provision dashboard JSON files from ConfigMaps
  sidecar:
    dashboards:
      enabled: true
      label: grafana_dashboard
      labelValue: "1"
      searchNamespace: ALL

alertmanager:
  enabled: true
```

Install:

```bash
helm install prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  -f prometheus-stack-values.yaml
```

### Verify installation

```bash
# Check all monitoring pods are running
kubectl get pods -n monitoring

# Access Grafana (default: admin/admin)
kubectl port-forward -n monitoring svc/prometheus-stack-grafana 3000:80 &
# Open http://localhost:3000

# Access Prometheus UI
kubectl port-forward -n monitoring svc/prometheus-stack-kube-prom-prometheus 9090:9090 &
# Open http://localhost:9090
```

---

## Stage 3: Connect the Operator to Prometheus

### Option A: Prometheus Operator (ServiceMonitor — recommended)

If you installed the operator via Helm, enable the ServiceMonitor:

```bash
helm upgrade neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator \
  --set metrics.enabled=true \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.serviceMonitor.interval=30s
```

Or set it in your `values.yaml`:

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: 30s
    scrapeTimeout: 10s
    labels: {}  # Add labels if your Prometheus uses label selectors
```

This creates a `ServiceMonitor` that tells Prometheus to scrape the operator's `/metrics` endpoint on port 8080.

The Neo4j cluster/standalone `ServiceMonitor` is created automatically when `monitoring.enabled: true` — no extra Helm config needed.

### Option B: Annotation-based scraping (no Prometheus Operator)

If you use plain Prometheus without the Operator, add scrape configs to your `prometheus.yml`:

```yaml
scrape_configs:
  # Scrape Neo4j Operator metrics
  - job_name: neo4j-operator
    metrics_path: /metrics
    static_configs:
      - targets:
          - neo4j-operator-controller-manager-metrics.neo4j-operator.svc.cluster.local:8080

  # Scrape Neo4j cluster metrics (one target per cluster)
  - job_name: neo4j-cluster
    metrics_path: /metrics
    static_configs:
      - targets:
          - my-cluster-metrics.default.svc.cluster.local:2004
        labels:
          cluster: my-cluster

  # OR use annotation-based auto-discovery
  - job_name: neo4j-pods
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        target_label: __address__
        regex: (.+)
        replacement: ${1}
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
```

### Verify scrape targets

In the Prometheus UI (`http://localhost:9090`):

1. Go to **Status → Targets**
2. You should see targets for:
   - `neo4j-operator` (operator metrics, port 8080)
   - `neo4j-cluster` or `neo4j-pods` (Neo4j metrics, port 2004)
3. All targets should show **State: UP**

Test a query in the Prometheus expression browser:

```promql
# Operator metric
neo4j_operator_cluster_healthy

# Neo4j native metric
neo4j_bolt_connections_opened_total
```

---

## Stage 4: Import Grafana Dashboards

### Dashboard 1: Neo4j Operator Overview

Create this ConfigMap to auto-provision a dashboard via Grafana's sidecar:

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: neo4j-operator-dashboard
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
data:
  neo4j-operator-overview.json: |
    {
      "annotations": { "list": [] },
      "editable": true,
      "fiscalYearStartMonth": 0,
      "graphTooltip": 1,
      "links": [],
      "panels": [
        {
          "title": "Cluster Health",
          "type": "stat",
          "gridPos": { "h": 4, "w": 6, "x": 0, "y": 0 },
          "targets": [{
            "expr": "neo4j_operator_cluster_healthy",
            "legendFormat": "{{ cluster_name }}"
          }],
          "fieldConfig": {
            "defaults": {
              "mappings": [
                { "type": "value", "options": { "0": { "text": "UNHEALTHY", "color": "red" }, "1": { "text": "HEALTHY", "color": "green" } } }
              ],
              "thresholds": { "steps": [{ "color": "red", "value": null }, { "color": "green", "value": 1 }] }
            }
          }
        },
        {
          "title": "Cluster Phase",
          "type": "state-timeline",
          "gridPos": { "h": 4, "w": 18, "x": 6, "y": 0 },
          "targets": [{
            "expr": "neo4j_operator_cluster_phase == 1",
            "legendFormat": "{{ cluster_name }} - {{ phase }}"
          }]
        },
        {
          "title": "Server Health",
          "type": "table",
          "gridPos": { "h": 6, "w": 12, "x": 0, "y": 4 },
          "targets": [{
            "expr": "neo4j_operator_server_health",
            "format": "table",
            "instant": true
          }],
          "transformations": [
            { "id": "organize", "options": { "excludeByName": { "Time": true, "__name__": true, "job": true, "instance": true }, "renameByName": { "cluster_name": "Cluster", "namespace": "Namespace", "server_name": "Server", "server_address": "Address", "Value": "Health" } } }
          ],
          "fieldConfig": {
            "overrides": [{
              "matcher": { "id": "byName", "options": "Health" },
              "properties": [{ "id": "mappings", "value": [{ "type": "value", "options": { "0": { "text": "Degraded", "color": "red" }, "1": { "text": "Healthy", "color": "green" } } }] }]
            }]
          }
        },
        {
          "title": "Replica Count",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 12, "y": 4 },
          "targets": [
            { "expr": "neo4j_operator_cluster_replicas_total", "legendFormat": "{{ cluster_name }} {{ role }}" }
          ]
        },
        {
          "title": "Reconciliation Rate",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 0, "y": 10 },
          "targets": [
            { "expr": "rate(neo4j_operator_reconcile_total[5m])", "legendFormat": "{{ cluster_name }} {{ operation }} {{ result }}" }
          ]
        },
        {
          "title": "Reconciliation Duration (p99)",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 12, "y": 10 },
          "targets": [
            { "expr": "histogram_quantile(0.99, rate(neo4j_operator_reconcile_duration_seconds_bucket[5m]))", "legendFormat": "{{ cluster_name }} {{ operation }} p99" }
          ],
          "fieldConfig": { "defaults": { "unit": "s" } }
        },
        {
          "title": "Backup Status",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 0, "y": 16 },
          "targets": [
            { "expr": "rate(neo4j_operator_backup_total[1h])", "legendFormat": "{{ cluster_name }} {{ result }}" }
          ]
        },
        {
          "title": "Backup Size",
          "type": "stat",
          "gridPos": { "h": 6, "w": 6, "x": 12, "y": 16 },
          "targets": [
            { "expr": "neo4j_operator_backup_size_bytes", "legendFormat": "{{ cluster_name }}" }
          ],
          "fieldConfig": { "defaults": { "unit": "bytes" } }
        },
        {
          "title": "Backup Duration (p95)",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 6, "x": 18, "y": 16 },
          "targets": [
            { "expr": "histogram_quantile(0.95, rate(neo4j_operator_backup_duration_seconds_bucket[1h]))", "legendFormat": "{{ cluster_name }} p95" }
          ],
          "fieldConfig": { "defaults": { "unit": "s" } }
        },
        {
          "title": "Split Brain Events",
          "type": "stat",
          "gridPos": { "h": 4, "w": 6, "x": 0, "y": 22 },
          "targets": [
            { "expr": "neo4j_operator_split_brain_detected_total", "legendFormat": "{{ cluster_name }}" }
          ],
          "fieldConfig": { "defaults": { "thresholds": { "steps": [{ "color": "green", "value": null }, { "color": "red", "value": 1 }] } } }
        },
        {
          "title": "Upgrade Activity",
          "type": "timeseries",
          "gridPos": { "h": 4, "w": 9, "x": 6, "y": 22 },
          "targets": [
            { "expr": "rate(neo4j_operator_upgrade_total[1h])", "legendFormat": "{{ cluster_name }} {{ result }}" }
          ]
        },
        {
          "title": "Resource Version Conflicts",
          "type": "timeseries",
          "gridPos": { "h": 4, "w": 9, "x": 15, "y": 22 },
          "targets": [
            { "expr": "rate(neo4j_operator_resource_version_conflicts_total[5m])", "legendFormat": "{{ resource_type }}" }
          ]
        }
      ],
      "schemaVersion": 39,
      "templating": {
        "list": [
          {
            "name": "namespace",
            "type": "query",
            "query": "label_values(neo4j_operator_cluster_healthy, namespace)",
            "multi": true,
            "includeAll": true
          },
          {
            "name": "cluster",
            "type": "query",
            "query": "label_values(neo4j_operator_cluster_healthy{namespace=~\"$namespace\"}, cluster_name)",
            "multi": true,
            "includeAll": true
          }
        ]
      },
      "time": { "from": "now-1h", "to": "now" },
      "title": "Neo4j Operator Overview",
      "uid": "neo4j-operator-overview"
    }
EOF
```

### Dashboard 2: Neo4j Database Performance

This dashboard uses Neo4j's native Prometheus metrics exposed on port 2004:

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: neo4j-database-dashboard
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
data:
  neo4j-database-performance.json: |
    {
      "annotations": { "list": [] },
      "editable": true,
      "graphTooltip": 1,
      "panels": [
        {
          "title": "Active Transactions",
          "description": "Per-database metric: neo4j.database.<db>.transaction.active",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 8, "x": 0, "y": 0 },
          "targets": [
            { "expr": "{__name__=~\"neo4j_database_.+_transaction_active\"}", "legendFormat": "{{ instance }} {{ __name__ }}" }
          ]
        },
        {
          "title": "Transaction Rate",
          "description": "Per-database metric: neo4j.database.<db>.transaction.committed/rollbacks",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 8, "x": 8, "y": 0 },
          "targets": [
            { "expr": "rate({__name__=~\"neo4j_database_.+_transaction_committed_total\"}[5m])", "legendFormat": "committed {{ instance }}" },
            { "expr": "rate({__name__=~\"neo4j_database_.+_transaction_rollbacks_total\"}[5m])", "legendFormat": "rollback {{ instance }}" }
          ]
        },
        {
          "title": "Bolt Connections",
          "description": "Global metric: neo4j.dbms.bolt.connections_running/idle",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 8, "x": 16, "y": 0 },
          "targets": [
            { "expr": "neo4j_dbms_bolt_connections_running", "legendFormat": "running {{ instance }}" },
            { "expr": "neo4j_dbms_bolt_connections_idle", "legendFormat": "idle {{ instance }}" }
          ]
        },
        {
          "title": "Page Cache Hit Ratio",
          "description": "Global metric: neo4j.page_cache.hit_ratio — target >95%",
          "type": "gauge",
          "gridPos": { "h": 6, "w": 8, "x": 0, "y": 6 },
          "targets": [
            { "expr": "neo4j_page_cache_hit_ratio", "legendFormat": "{{ instance }}" }
          ],
          "fieldConfig": {
            "defaults": {
              "unit": "percentunit",
              "min": 0, "max": 1,
              "thresholds": { "steps": [{ "color": "red", "value": null }, { "color": "yellow", "value": 0.9 }, { "color": "green", "value": 0.95 }] }
            }
          }
        },
        {
          "title": "Page Cache Usage",
          "description": "Global metric: neo4j.page_cache.usage_ratio — 100% means increase pagecache.size",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 8, "x": 8, "y": 6 },
          "targets": [
            { "expr": "neo4j_page_cache_usage_ratio", "legendFormat": "usage {{ instance }}" }
          ],
          "fieldConfig": { "defaults": { "unit": "percentunit" } }
        },
        {
          "title": "Store Size",
          "description": "Per-database: neo4j.database.<db>.store.size.total (5.26) / store.size.full (2025.x+)",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 8, "x": 16, "y": 6 },
          "targets": [
            { "expr": "{__name__=~\"neo4j_database_.+_store_size_(total|full)\"}", "legendFormat": "{{ instance }} {{ __name__ }}" }
          ],
          "fieldConfig": { "defaults": { "unit": "bytes" } }
        },
        {
          "title": "Query Execution Time (p99)",
          "description": "Per-database histogram: neo4j.database.<db>.db.query.execution.latency.millis",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 0, "y": 12 },
          "targets": [
            { "expr": "histogram_quantile(0.99, rate(neo4j_db_query_execution_latency_millis_bucket[5m]))", "legendFormat": "p99 {{ instance }}" }
          ],
          "fieldConfig": { "defaults": { "unit": "ms" } }
        },
        {
          "title": "Query Success / Failure Rate",
          "description": "Per-database: neo4j.database.<db>.db.query.execution.success/failure",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 12, "y": 12 },
          "targets": [
            { "expr": "rate({__name__=~\"neo4j_database_.+_db_query_execution_success_total\"}[5m])", "legendFormat": "success {{ instance }}" },
            { "expr": "rate({__name__=~\"neo4j_database_.+_db_query_execution_failure_total\"}[5m])", "legendFormat": "failure {{ instance }}" }
          ]
        },
        {
          "title": "JVM Heap Usage",
          "description": "Global: neo4j.vm.heap.used / committed / max",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 0, "y": 18 },
          "targets": [
            { "expr": "neo4j_vm_heap_used", "legendFormat": "used {{ instance }}" },
            { "expr": "neo4j_vm_heap_max", "legendFormat": "max {{ instance }}" }
          ],
          "fieldConfig": { "defaults": { "unit": "bytes" } }
        },
        {
          "title": "GC Pause Time",
          "description": "Global: neo4j.vm.gc.time.<gc_name>",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 12, "y": 18 },
          "targets": [
            { "expr": "rate({__name__=~\"neo4j_vm_gc_time_.+\"}[5m])", "legendFormat": "{{ __name__ }} {{ instance }}" }
          ],
          "fieldConfig": { "defaults": { "unit": "ms" } }
        },
        {
          "title": "Cluster Replication (Raft)",
          "description": "Per-database cluster metric: neo4j.database.<db>.cluster.raft.append_index / applied_index",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 0, "y": 24 },
          "targets": [
            { "expr": "{__name__=~\"neo4j_database_.+_cluster_raft_append_index\"}", "legendFormat": "append {{ instance }}" },
            { "expr": "{__name__=~\"neo4j_database_.+_cluster_raft_applied_index\"}", "legendFormat": "applied {{ instance }}" }
          ]
        },
        {
          "title": "Cluster Discovery / Raft Leader",
          "description": "Per-database cluster metric: neo4j.database.<db>.cluster.raft.is_leader (1=leader, 0=follower)",
          "type": "timeseries",
          "gridPos": { "h": 6, "w": 12, "x": 12, "y": 24 },
          "targets": [
            { "expr": "{__name__=~\"neo4j_database_.+_cluster_raft_is_leader\"}", "legendFormat": "leader {{ instance }}" }
          ]
        }
      ],
      "schemaVersion": 39,
      "templating": {
        "list": [
          {
            "name": "instance",
            "type": "query",
            "query": "label_values(neo4j_dbms_bolt_connections_running, instance)",
            "multi": true,
            "includeAll": true
          }
        ]
      },
      "time": { "from": "now-1h", "to": "now" },
      "title": "Neo4j Database Performance",
      "uid": "neo4j-database-performance"
    }
EOF
```

After applying, refresh Grafana. The dashboards appear under **Dashboards → Browse**.

---

## Stage 5: Configure Alerting

### Operator alerts (auto-created)

When `monitoring.enabled: true`, the operator automatically creates a `PrometheusRule` named `<cluster>-query-alerts` with default alert rules for query performance. No manual configuration needed.

### Custom alerting rules

Create additional alerts for operator-level concerns:

```bash
kubectl apply -f - <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: neo4j-operator-alerts
  namespace: monitoring
  labels:
    prometheus: kube-prometheus
    app.kubernetes.io/name: neo4j-operator
spec:
  groups:
    - name: neo4j-operator-health
      rules:
        # Cluster unhealthy for 5 minutes
        - alert: Neo4jClusterUnhealthy
          expr: neo4j_operator_cluster_healthy == 0
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "Neo4j cluster {{ $labels.cluster_name }} is unhealthy"
            description: "Cluster {{ $labels.cluster_name }} in namespace {{ $labels.namespace }} has been unhealthy for 5 minutes."

        # Individual server degraded
        - alert: Neo4jServerDegraded
          expr: neo4j_operator_server_health == 0
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Neo4j server {{ $labels.server_name }} is degraded"
            description: "Server {{ $labels.server_name }} ({{ $labels.server_address }}) in cluster {{ $labels.cluster_name }} has been degraded for 5 minutes."

        # Split brain detected
        - alert: Neo4jSplitBrainDetected
          expr: increase(neo4j_operator_split_brain_detected_total[10m]) > 0
          labels:
            severity: critical
          annotations:
            summary: "Split brain detected in cluster {{ $labels.cluster_name }}"
            description: "A split-brain scenario was detected in cluster {{ $labels.cluster_name }}. The operator is attempting automatic recovery."

        # Reconciliation failures spiking
        - alert: Neo4jReconciliationFailures
          expr: rate(neo4j_operator_reconcile_total{result="failure"}[10m]) > 0.1
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "High reconciliation failure rate for {{ $labels.cluster_name }}"
            description: "Cluster {{ $labels.cluster_name }} is experiencing >0.1 failures/sec over the last 10 minutes."

        # Backup hasn't succeeded recently
        - alert: Neo4jBackupStale
          expr: time() - (neo4j_operator_backup_duration_seconds_count > 0) > 86400
          for: 1h
          labels:
            severity: warning
          annotations:
            summary: "No recent backup for cluster {{ $labels.cluster_name }}"
            description: "No successful backup recorded for {{ $labels.cluster_name }} in over 24 hours."

        # Slow reconciliation
        - alert: Neo4jSlowReconciliation
          expr: histogram_quantile(0.99, rate(neo4j_operator_reconcile_duration_seconds_bucket[10m])) > 30
          for: 15m
          labels:
            severity: warning
          annotations:
            summary: "Slow reconciliation for {{ $labels.cluster_name }}"
            description: "p99 reconciliation duration for {{ $labels.cluster_name }} exceeds 30 seconds."

    - name: neo4j-database-health
      rules:
        # Page cache hit ratio too low
        - alert: Neo4jLowPageCacheHitRatio
          expr: neo4j_page_cache_hit_ratio < 0.75
          for: 15m
          labels:
            severity: warning
          annotations:
            summary: "Low page cache hit ratio on {{ $labels.instance }}"
            description: "Page cache hit ratio is {{ $value | humanizePercentage }} — consider increasing `server.memory.pagecache.size`."

        # High transaction rollback rate (per-database metrics use neo4j_database_<db>_transaction_* naming)
        - alert: Neo4jHighRollbackRate
          expr: rate({__name__=~"neo4j_database_.+_transaction_rollbacks_total"}[5m]) / (rate({__name__=~"neo4j_database_.+_transaction_committed_total"}[5m]) + rate({__name__=~"neo4j_database_.+_transaction_rollbacks_total"}[5m])) > 0.1
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "High rollback rate on {{ $labels.instance }}"
            description: "More than 10% of transactions are rolling back."

        # JVM heap pressure
        - alert: Neo4jHighHeapUsage
          expr: neo4j_vm_heap_used / neo4j_vm_heap_max > 0.9
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "High JVM heap usage on {{ $labels.instance }}"
            description: "Heap usage is above 90%. Consider increasing heap size."
EOF
```

### Wire Alertmanager to Slack (optional)

```yaml
# alertmanager-config.yaml
apiVersion: v1
kind: Secret
metadata:
  name: alertmanager-prometheus-stack-kube-prom-alertmanager
  namespace: monitoring
stringData:
  alertmanager.yaml: |
    global:
      resolve_timeout: 5m
    route:
      receiver: slack
      group_by: [alertname, cluster_name]
      group_wait: 30s
      group_interval: 5m
      repeat_interval: 4h
      routes:
        - match:
            severity: critical
          receiver: slack
    receivers:
      - name: slack
        slack_configs:
          - api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'
            channel: '#neo4j-alerts'
            title: '{{ .GroupLabels.alertname }}'
            text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

---

## Stage 6: Verify the Complete Pipeline

### Checklist

```bash
# 1. Neo4j metrics endpoint is live
kubectl port-forward pod/my-cluster-server-0 2004:2004 &
curl -s http://localhost:2004/metrics | grep neo4j_bolt

# 2. Operator metrics endpoint is live
kubectl port-forward -n neo4j-operator svc/neo4j-operator-controller-manager-metrics 8080:8080 &
curl -s http://localhost:8080/metrics | grep neo4j_operator

# 3. ServiceMonitors exist
kubectl get servicemonitors -A | grep neo4j

# 4. Prometheus is scraping targets
# Visit http://localhost:9090/targets — all neo4j targets should be UP

# 5. Grafana dashboards are loaded
# Visit http://localhost:3000 → Dashboards → Browse
# Look for "Neo4j Operator Overview" and "Neo4j Database Performance"

# 6. Alerts are registered
# Visit http://localhost:9090/alerts — you should see neo4j-operator-health rules
```

### Useful PromQL queries for ad-hoc investigation

```promql
# Which clusters are unhealthy right now?
neo4j_operator_cluster_healthy == 0

# What phase is each cluster in?
neo4j_operator_cluster_phase == 1

# Reconciliation error rate by cluster
sum by (cluster_name) (rate(neo4j_operator_reconcile_total{result="failure"}[5m]))

# Backup success/failure ratio
sum by (cluster_name, result) (rate(neo4j_operator_backup_total[1h]))

# Cypher execution p95 latency
histogram_quantile(0.95, rate(neo4j_operator_cypher_execution_duration_seconds_bucket[5m]))

# Resource conflicts (indicates contention)
rate(neo4j_operator_resource_version_conflicts_total[5m])

# Neo4j query latency p99
histogram_quantile(0.99, rate(neo4j_db_query_execution_latency_millis_bucket[5m]))

# Page cache efficiency (global metric)
neo4j_page_cache_hit_ratio

# Bolt connection pool (global metrics — note the dbms prefix)
neo4j_dbms_bolt_connections_running
neo4j_dbms_bolt_connections_idle

# Raft replication lag (per-database cluster metric — database name embedded in metric name)
{__name__=~"neo4j_database_.+_cluster_raft_append_index"} - {__name__=~"neo4j_database_.+_cluster_raft_applied_index"}
```

---

## Troubleshooting

### Prometheus shows "0 active targets" for Neo4j

1. Verify `monitoring.enabled: true` in your CR
2. Check the ServiceMonitor exists: `kubectl get servicemonitor -A | grep monitoring`
3. Ensure Prometheus is configured to discover all ServiceMonitors (see `serviceMonitorSelectorNilUsesHelmValues: false` above)
4. Check the `<cluster>-metrics` Service has endpoints: `kubectl get endpoints my-cluster-metrics`

### Operator metrics not appearing

1. Verify the ServiceMonitor is enabled: `kubectl get servicemonitor -A | grep neo4j-operator`
2. Check the operator metrics Service exists: `kubectl get svc -n neo4j-operator | grep metrics`
3. Port-forward and test directly: `kubectl port-forward -n neo4j-operator svc/<operator-svc> 8080:8080`

### Grafana dashboards not appearing

1. Confirm the ConfigMap has the label `grafana_dashboard: "1"`
2. Check the Grafana sidecar is configured to search all namespaces
3. Restart the Grafana pod to force re-scan: `kubectl rollout restart deployment -n monitoring prometheus-stack-grafana`

### Metrics show stale data

- The operator records metrics on every reconcile cycle (~30s default)
- If the cluster is not in `Ready` phase, diagnostics metrics like `server_health` are not updated
- Check reconcile logs: `kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager | grep -i reconcile`

---

## Complete Operator Metrics Reference

All metrics use the prefix `neo4j_operator_` and are registered via the controller-runtime Prometheus registry.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `cluster_healthy` | Gauge | cluster_name, namespace | 1=healthy, 0=unhealthy |
| `cluster_replicas_total` | Gauge | cluster_name, namespace, role | Replicas by role (primary/secondary) |
| `cluster_phase` | Gauge | cluster_name, namespace, phase | 1 for active phase, 0 for others |
| `split_brain_detected_total` | Counter | cluster_name, namespace | Split-brain detection events |
| `server_health` | Gauge | cluster_name, namespace, server_name, server_address | 1=Enabled+Available, 0=degraded |
| `reconcile_total` | Counter | cluster_name, namespace, operation, result | Reconciliation attempts |
| `reconcile_duration_seconds` | Histogram | cluster_name, namespace, operation | Reconciliation duration |
| `upgrade_total` | Counter | cluster_name, namespace, result | Upgrade attempts |
| `upgrade_duration_seconds` | Histogram | cluster_name, namespace, phase | Upgrade duration per phase |
| `backup_total` | Counter | cluster_name, namespace, result | Backup attempts |
| `backup_duration_seconds` | Histogram | cluster_name, namespace | Backup duration |
| `backup_size_bytes` | Gauge | cluster_name, namespace | Last backup size |
| `cypher_executions_total` | Counter | cluster_name, namespace, operation, result | Cypher executions by the operator |
| `cypher_execution_duration_seconds` | Histogram | cluster_name, namespace, operation | Cypher execution duration |
| `security_operations_total` | Counter | cluster_name, namespace, operation, result | Security ops (user, role, grant) |
| `resource_version_conflicts_total` | Counter | resource_type, namespace | K8s resource version conflicts |
| `conflict_retry_attempts` | Histogram | resource_type, namespace | Retry attempts per conflict |
| `conflict_retry_duration_seconds` | Histogram | resource_type, namespace | Time spent retrying conflicts |
| `disaster_recovery_status` | Gauge | cluster_name, namespace, primary_region, secondary_region | 1=DR ready, 0=not ready |
| `failover_total` | Counter | cluster_name, namespace, result | Failover events |
| `replication_lag_seconds` | Gauge | cluster_name, namespace, primary_region, secondary_region | Cross-region replication lag |
| `manual_scaler_enabled` | Gauge | cluster_name, namespace | 1=manual scaling on |
| `scale_events_total` | Counter | cluster_name, namespace, node_type, direction | Scale up/down events |
| `primary_count` | Gauge | cluster_name, namespace | Current primary count |
| `secondary_count` | Gauge | cluster_name, namespace | Current secondary count |
| `scaling_validation_total` | Counter | cluster_name, namespace, validation_type, result | Scaling validation attempts |
