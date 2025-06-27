# Performance

This guide explains how to tune the performance of your Neo4j Enterprise clusters.

## Resource Allocation

One of the most important factors for performance is resource allocation. You can configure the CPU and memory resources for your Neo4j pods using the `spec.resources` field in the `Neo4jEnterpriseCluster` resource. It is crucial to set both `requests` and `limits` for predictable performance.

```yaml
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
      limits:
        cpu: "4"
        memory: "8Gi"
```

## JVM Tuning

For advanced use cases, you can tune the JVM settings for your Neo4j pods using environment variables in the `spec.env` field. This allows you to control settings like heap size, garbage collection, and more.

```yaml
    env:
      - name: NEO4J_dbms_memory_heap_initial__size
        value: "4G"
      - name: NEO4J_dbms_memory_heap_max__size
        value: "4G"
```

## Autoscaling

The operator supports autoscaling to automatically adjust the size of your cluster based on the workload. This is a powerful feature for managing performance and cost in dynamic environments. You can configure autoscaling based on CPU and memory utilization.

See the [API Reference](../../api_reference/neo4jenterprisecluster.md) for more details on the `autoScaling` spec.
