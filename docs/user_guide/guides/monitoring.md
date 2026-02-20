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
