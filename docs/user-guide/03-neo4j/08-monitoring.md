# Monitoring

Neo4j can expose its metrics in Prometheus format, and the operator can register a ServiceMonitor so
a Prometheus Operator installation picks them up without further configuration.

## Enabling metrics

Two fields, and both are needed:

```yaml
spec:
  features:
    monitoring:
      prometheus:
        enabled: true
  connectivity:
    listeners:
      metrics: 2004
```

The feature toggle turns the Prometheus endpoint on inside Neo4j; the listener opens the port. A
listener without the feature is rejected at admission, so you cannot end up with an open port serving
nothing.

Enabling Prometheus also causes the admin Service `<name>-admin` to be created if it does not exist
already, publishing the metrics port under the name `tcp-prometheus`. Metrics are deliberately not
added to the client Service — application clients have no business scraping them, and on a
LoadBalancer that would expose internals.

`spec.features.monitoring.prometheus.endpoint` is ignored: the endpoint follows the listener port.

## Checking the endpoint

```bash
kubectl port-forward -n default svc/dev-admin 2004:2004
curl -s localhost:2004/metrics | head -20
```

You should see Neo4j metric families such as `neo4j_database_*` and JVM metrics. An empty response
usually means the feature flag is missing while the listener is set, or the reverse.

## ServiceMonitor

If you run the Prometheus Operator, let it discover Neo4j automatically:

```yaml
spec:
  features:
    monitoring:
      prometheus:
        enabled: true
      serviceMonitor:
        enabled: true
        interval: 30s
        labels:
          release: kube-prometheus-stack
  connectivity:
    listeners:
      metrics: 2004
```

The operator creates `<name>-servicemonitor` selecting the admin Service, scraping the
`tcp-prometheus` port at `/metrics` every 30 seconds by default. The `labels` field matters more than
it looks: most Prometheus installations only pick up ServiceMonitors carrying a specific label, so
set whatever your `serviceMonitorSelector` requires — commonly the Helm release name.

`interval`, `path`, `port`, `jobLabel`, `targetLabels` and `selector` are all overridable when your
setup differs. Note that the ServiceMonitor is created in the same namespace as the `Neo4j` resource,
so Prometheus must be watching that namespace; `namespaceSelector` is accepted but not applied.

The CustomResourceDefinition for ServiceMonitor has to exist in the cluster. Without the Prometheus
Operator installed, creating the object fails and the resource reports an error — enable this only
where Prometheus Operator is present.

Example: [`examples/standalone/16-servicemonitor.yaml`](../../../examples/standalone/16-servicemonitor.yaml).

## Verifying discovery

```bash
kubectl get servicemonitor -n default

kubectl get endpoints dev-admin -n default
```

If the ServiceMonitor exists but no targets appear in Prometheus, the cause is almost always label
selection: either the ServiceMonitor lacks the label Prometheus requires, or Prometheus is not
configured to watch that namespace.

## What to watch

Start with the metrics that reflect the questions you will actually be asked: page cache hit ratio and
transaction rates for performance, heap usage and garbage collection pauses for stability, and for
clusters the Raft and replication metrics that reveal a member falling behind.

Alongside Neo4j's own metrics, the resource status is a cheap and reliable signal for Kubernetes-level
alerting, since it summarises what the operator observed:

```bash
kubectl get neo4j dev -n default \
  -o jsonpath='{.status.phase} {range .status.conditions[*]}{.type}={.status} {end}{"\n"}'
```

Alert on `Ready=False` persisting, on `ClusterFormed=False` for clustered deployments, and on
`status.serverSummary.ready` dropping below `servers`.

## Not implemented

`spec.features.monitoring.csv`, `.jmx` and `.graphite` exist in the schema and do nothing. If you need
those exporters today, configure them through `spec.config.neo4j` with the corresponding Neo4j
settings, and give metrics their own volume if you write CSV files:

```yaml
spec:
  storage:
    volumes:
      metrics:
        mode: Dynamic
        dynamic:
          size: 10Gi
```

## Next

[Connectivity](04-connectivity.md) · [Operations](09-operations.md) ·
[Operator logs](../04-troubleshooting/02-operator-logs.md)
