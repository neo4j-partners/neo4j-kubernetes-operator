# Query Monitoring Guide

This guide explains how to set up and use query performance monitoring for your Neo4j Enterprise clusters using the Kubernetes operator.

## Overview

The Neo4j Kubernetes operator provides built-in query monitoring capabilities through the official Neo4j Prometheus exporter. This feature allows you to monitor query performance, track slow queries, and gain insights into your Neo4j cluster's behavior.

## Features

- **Prometheus Integration**: Native Prometheus metrics export
- **Query Performance Metrics**: Track query execution times and patterns
- **Slow Query Detection**: Identify and monitor slow queries
- **Cluster Health Monitoring**: Monitor cluster-wide metrics
- **Custom Metrics**: Support for custom Neo4j metrics

## Configuration

### Basic Query Monitoring Setup

Enable query monitoring in your `Neo4jEnterpriseCluster` resource:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-neo4j-cluster
spec:
  image:
    repo: neo4j/neo4j
    tag: 5.15.0-enterprise
  topology:
    primaries: 3
  storage:
    className: fast-ssd
    size: 10Gi
  queryMonitoring:
    enabled: true
    slowQueryThreshold: "5s"
    explainPlan: true
    indexRecommendations: true
```

### Advanced Query Monitoring Configuration

```yaml
spec:
  queryMonitoring:
    enabled: true
    slowQueryThreshold: "2s"
    explainPlan: true
    indexRecommendations: true
    sampling:
      rate: "0.1"
      maxQueriesPerSecond: 100
    metricsExport:
      prometheus: true
      customEndpoint: "https://metrics.company.com/neo4j"
      interval: "30s"
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | false | Enable query monitoring |
| `slowQueryThreshold` | string | "5s" | Threshold for slow query detection |
| `explainPlan` | boolean | true | Enable query plan explanation |
| `indexRecommendations` | boolean | true | Enable index recommendations |
| `sampling.rate` | string | "1.0" | Query sampling rate (0.0 to 1.0) |
| `sampling.maxQueriesPerSecond` | int32 | 1000 | Maximum queries to sample per second |
| `metricsExport.prometheus` | boolean | true | Export to Prometheus |
| `metricsExport.customEndpoint` | string | "" | Custom metrics endpoint |
| `metricsExport.interval` | string | "30s" | Export interval |

## How It Works

1. **Sidecar Container**: When query monitoring is enabled, the operator adds a Prometheus exporter sidecar container to each Neo4j pod.

2. **Metrics Collection**: The exporter connects to Neo4j via Bolt protocol and collects metrics about:
   - Query execution times
   - Database statistics
   - Connection information
   - Transaction metrics
   - Memory usage

3. **Prometheus Integration**: The exporter exposes metrics on port 2004 with Prometheus annotations for automatic discovery.

4. **Query Analysis**: The exporter can analyze query plans and provide recommendations for optimization.

## Prometheus Configuration

### Service Discovery

The operator automatically adds Prometheus annotations to pods:

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "2004"
  prometheus.io/path: "/metrics"
```

### Prometheus Scrape Configuration

Add this to your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'neo4j'
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
        replacement: /metrics
      - source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        regex: ([^:]+)(?::\d+)?;(\d+)
        replacement: $1:$2
        target_label: __address__
```

## Available Metrics

### Query Performance Metrics

- `neo4j_query_duration_seconds`: Query execution time
- `neo4j_query_count_total`: Total number of queries executed
- `neo4j_slow_query_count_total`: Number of slow queries
- `neo4j_query_plan_cache_hits_total`: Query plan cache hits
- `neo4j_query_plan_cache_misses_total`: Query plan cache misses

### Database Metrics

- `neo4j_database_size_bytes`: Database size in bytes
- `neo4j_database_transactions_total`: Total transactions
- `neo4j_database_connections_active`: Active connections
- `neo4j_database_connections_idle`: Idle connections

### Memory Metrics

- `neo4j_memory_heap_used_bytes`: Used heap memory
- `neo4j_memory_heap_max_bytes`: Maximum heap memory
- `neo4j_memory_pagecache_used_bytes`: Used page cache
- `neo4j_memory_pagecache_max_bytes`: Maximum page cache

### Cluster Metrics

- `neo4j_cluster_members_total`: Total cluster members
- `neo4j_cluster_members_online`: Online cluster members
- `neo4j_cluster_leader`: Current cluster leader
- `neo4j_cluster_raft_term`: Current Raft term

## Grafana Dashboards

### Basic Neo4j Dashboard

Create a Grafana dashboard with these panels:

1. **Query Performance**
   - Query duration percentiles
   - Slow query rate
   - Query throughput

2. **Database Health**
   - Database size
   - Transaction rate
   - Connection count

3. **Memory Usage**
   - Heap memory usage
   - Page cache usage
   - Memory pressure

4. **Cluster Status**
   - Cluster member status
   - Leader election
   - Raft term changes

### Sample Queries

```promql
# Average query duration (95th percentile)
histogram_quantile(0.95, rate(neo4j_query_duration_seconds_bucket[5m]))

# Slow query rate
rate(neo4j_slow_query_count_total[5m])

# Database size growth
rate(neo4j_database_size_bytes[1h])

# Memory usage percentage
(neo4j_memory_heap_used_bytes / neo4j_memory_heap_max_bytes) * 100

# Active connections
neo4j_database_connections_active

# Query plan cache hit rate
rate(neo4j_query_plan_cache_hits_total[5m]) / (rate(neo4j_query_plan_cache_hits_total[5m]) + rate(neo4j_query_plan_cache_misses_total[5m]))
```

## Alerting Rules

### Slow Query Alerts

```yaml
groups:
  - name: neo4j
    rules:
      - alert: Neo4jSlowQueries
        expr: rate(neo4j_slow_query_count_total[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Neo4j slow queries detected"
          description: "{{ $value }} slow queries per second in the last 5 minutes"

      - alert: Neo4jHighQueryDuration
        expr: histogram_quantile(0.95, rate(neo4j_query_duration_seconds_bucket[5m])) > 5
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Neo4j high query duration"
          description: "95th percentile query duration is {{ $value }}s"

      - alert: Neo4jMemoryPressure
        expr: (neo4j_memory_heap_used_bytes / neo4j_memory_heap_max_bytes) > 0.85
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Neo4j memory pressure"
          description: "Heap memory usage is {{ $value | humanizePercentage }}"

      - alert: Neo4jClusterUnhealthy
        expr: neo4j_cluster_members_online < neo4j_cluster_members_total
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Neo4j cluster unhealthy"
          description: "{{ $value }} cluster members are offline"
```

## Troubleshooting

### Exporter Not Starting

Check the exporter container logs:

```bash
kubectl logs <pod-name> -c prometheus-exporter
```

Common issues:
- **Authentication**: Verify Neo4j credentials are correct
- **Network**: Ensure exporter can connect to Neo4j on localhost:7687
- **Permissions**: Check if Neo4j user has monitoring permissions

### Missing Metrics

If metrics are not appearing in Prometheus:

1. **Check exporter status**:
   ```bash
   kubectl exec <pod-name> -c prometheus-exporter -- curl localhost:2004/metrics
   ```

2. **Verify Prometheus configuration**:
   ```bash
   kubectl get pods -l app=prometheus -o yaml
   ```

3. **Check service discovery**:
   ```bash
   kubectl get pods -o jsonpath='{.items[*].metadata.annotations.prometheus\.io/scrape}'
   ```

### High Memory Usage

If the exporter is consuming too much memory:

1. **Reduce sampling rate**:
   ```yaml
   queryMonitoring:
     sampling:
       rate: "0.1"  # Sample only 10% of queries
   ```

2. **Increase export interval**:
   ```yaml
   queryMonitoring:
     metricsExport:
       interval: "60s"  # Export every minute instead of 30s
   ```

## Performance Considerations

### Resource Usage

The Prometheus exporter typically uses:
- **CPU**: 50-200m
- **Memory**: 100-500Mi
- **Network**: Minimal (local communication with Neo4j)

### Optimization Tips

1. **Sampling**: Use sampling for high-traffic clusters
2. **Scrape Interval**: Adjust Prometheus scrape interval based on needs
3. **Metrics Filtering**: Configure Prometheus to only collect needed metrics
4. **Retention**: Set appropriate metrics retention periods

## Integration Examples

### With Grafana

```yaml
# Grafana datasource configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-datasources
data:
  prometheus.yaml: |
    apiVersion: 1
    datasources:
    - name: Prometheus
      type: prometheus
      url: http://prometheus:9090
      access: proxy
      isDefault: true
```

### With AlertManager

```yaml
# AlertManager configuration
global:
  slack_api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'slack-notifications'

receivers:
- name: 'slack-notifications'
  slack_configs:
  - channel: '#neo4j-alerts'
    title: '{{ template "slack.title" . }}'
    text: '{{ template "slack.text" . }}'
```

## Best Practices

1. **Start Small**: Begin with basic monitoring and expand gradually
2. **Set Appropriate Thresholds**: Configure alerts based on your application's needs
3. **Monitor Resource Usage**: Keep an eye on exporter resource consumption
4. **Regular Review**: Periodically review and tune monitoring configuration
5. **Documentation**: Document your monitoring setup and alert procedures

## Next Steps

- [Plugin Management Guide](./plugin-management-guide.md)
- [Performance Tuning Guide](./performance-guide.md)
- [Backup and Restore Guide](./backup-restore-guide.md)
