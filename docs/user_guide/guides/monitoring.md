# Monitoring

This guide explains how to monitor your Neo4j Enterprise clusters and the operator itself.

## Operator Monitoring

The Neo4j Enterprise Operator includes comprehensive monitoring capabilities for both operational insight and performance optimization.

### Built-in Resource Monitoring

The operator includes a built-in resource monitoring framework that tracks:

```yaml
# Enable resource monitoring in your cluster spec
spec:
  monitoring:
    resourceMonitoring:
      enabled: true
      interval: "30s"
      metrics:
        - "cpu"
        - "memory"
        - "storage"
        - "neo4j_transactions"
```

### Operational Metrics

The operator exposes several operational metrics:

- **Reconciliation Frequency**: Monitor controller performance (~34 reconciliations/minute optimal)
- **ConfigMap Updates**: Track configuration changes and debounce effectiveness
- **Status Updates**: Monitor cluster state transitions and API efficiency
- **Resource Validation**: Track memory validation and resource recommendations

### Controller Performance Monitoring

Monitor operator controller performance using these Kubernetes commands:

```bash
# Monitor controller logs for performance insights
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager -f

# Check reconciliation patterns
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep "reconcile"

# Monitor ConfigMap debounce effectiveness
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep "debounce"
```

## Neo4j Cluster Monitoring

### Prometheus Integration

The operator integrates with Prometheus to expose metrics about your Neo4j cluster. You can enable Prometheus integration by setting the `spec.monitoring.prometheus.enabled` field to `true`.

```yaml
spec:
  monitoring:
    prometheus:
      enabled: true
      serviceMonitor:
        enabled: true
        interval: "30s"
        scrapeTimeout: "10s"
```

The operator will then expose a metrics endpoint that can be scraped by Prometheus. This allows you to monitor key metrics like transaction rates, memory usage, and query performance.

### Key Metrics to Monitor

#### Cluster Health Metrics
- **Cluster Status**: Ready/NotReady state transitions
- **Pod Health**: Individual pod readiness and liveness
- **Resource Utilization**: CPU, memory, and storage usage

#### Neo4j Performance Metrics
- **Transaction Rates**: Transactions per second across cluster members
- **Query Performance**: Slow query detection and execution times
- **Memory Usage**: Heap, page cache, and total memory utilization
- **Storage I/O**: Disk read/write operations and latency

#### Operator Efficiency Metrics
- **Reconciliation Rate**: Controller reconciliation frequency
- **API Call Rate**: Kubernetes API interactions per minute
- **Configuration Updates**: ConfigMap update frequency and success rate

## Grafana Dashboards

The operator includes a pre-built Grafana dashboard for visualizing your Neo4j metrics. You can import this dashboard into your Grafana instance to get a comprehensive overview of your cluster's health and performance.

### Dashboard Components
- **Cluster Overview**: High-level cluster health and status
- **Resource Utilization**: CPU, memory, and storage metrics
- **Neo4j Performance**: Transaction rates, query performance, cache hit ratios
- **Operator Metrics**: Controller performance and reconciliation patterns

## Health Checks

The operator performs regular health checks on your Neo4j cluster to ensure that it is running correctly. You can view the health of your cluster by checking the `status` of the `Neo4jEnterpriseCluster` resource.

### Enhanced Health Checks

The operator now includes enhanced health checking:

```bash
# Check overall cluster health
kubectl get neo4jenterprisecluster -o wide

# View detailed cluster status
kubectl describe neo4jenterprisecluster my-cluster

# Monitor cluster conditions
kubectl get neo4jenterprisecluster my-cluster -o jsonpath='{.status.conditions}'
```

### Health Check Components
- **Pod Readiness**: All Neo4j pods are ready and serving traffic
- **Cluster Connectivity**: Inter-pod communication is functioning
- **Resource Availability**: Sufficient CPU, memory, and storage
- **Configuration Validity**: Neo4j configuration is valid and applied

## Alerting

### Recommended Alerts

Set up alerts for these key conditions:

#### Critical Alerts
- Cluster status transitions to "NotReady"
- Pod crashes or restarts
- High reconciliation frequency (>100/minute indicates issues)
- Memory validation failures

#### Warning Alerts
- Resource utilization above 80%
- Slow query detection
- ConfigMap update failures
- Scaling operation timeouts

### Alert Configuration Example

```yaml
# Example Prometheus alert rules
groups:
- name: neo4j-operator
  rules:
  - alert: HighReconciliationRate
    expr: rate(controller_runtime_reconcile_total[5m]) > 1.5
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "Neo4j operator reconciliation rate is high"
      description: "Reconciliation rate {{ $value }} exceeds normal threshold"

  - alert: ClusterNotReady
    expr: neo4j_cluster_ready == 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "Neo4j cluster is not ready"
```

## Troubleshooting with Monitoring

Use monitoring data to troubleshoot common issues:

### High Resource Usage
1. Check resource monitoring metrics
2. Review memory validation recommendations
3. Analyze Neo4j heap and page cache allocation

### Slow Performance
1. Monitor transaction rates and query performance
2. Check for resource bottlenecks
3. Review controller reconciliation patterns

### Configuration Issues
1. Monitor ConfigMap update success rates
2. Check debounce mechanism effectiveness
3. Review cluster status transitions
