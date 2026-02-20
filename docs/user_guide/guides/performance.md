# Performance

This guide explains how to tune the performance of your Neo4j Enterprise clusters.

## Operator Performance Optimizations

The Neo4j Enterprise Operator has been optimized for production environments with several key performance improvements:

### Reconciliation Efficiency
- **Optimized Rate Limiting**: The controller uses intelligent rate limiting to prevent excessive API calls (~34 reconciliations per minute vs. 18,000+)
- **Status Update Optimization**: Status updates only occur when cluster state actually changes, reducing unnecessary API server load
- **ConfigMap Debouncing**: 2-minute debounce mechanism prevents restart loops from configuration changes

### Resource Management
- **Memory Validation**: Automatic validation ensures Neo4j memory settings don't exceed available resources
- **Resource Recommendations**: Built-in recommendations for optimal CPU and memory allocation based on cluster size
- **Efficient Monitoring**: Lightweight resource monitoring with minimal overhead

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

### Memory Validation and Recommendations

The operator includes intelligent memory validation that:
- Ensures Neo4j heap settings don't exceed available container memory
- Provides automatic recommendations for optimal memory allocation
- Validates memory ratios between heap, page cache, and system overhead

```yaml
    resources:
      requests:
        memory: "4Gi"    # Minimum for stable operation
      limits:
        memory: "8Gi"    # Allows 4-6GB for Neo4j heap + page cache
```

## JVM Tuning

For advanced use cases, you can tune the JVM settings for your Neo4j pods using environment variables in the `spec.env` field. This allows you to control settings like heap size, garbage collection, and more.

```yaml
    env:
      - name: NEO4J_server_memory_heap_initial__size
        value: "4G"
      - name: NEO4J_server_memory_heap_max__size
        value: "4G"
      - name: NEO4J_server_memory_pagecache_size
        value: "2G"
```

## Performance Monitoring

The operator includes built-in performance monitoring capabilities:

### Resource Monitoring
- Real-time tracking of CPU, memory, and storage utilization
- Neo4j-specific metrics including transaction rates and query performance
- Automatic detection of resource constraints and bottlenecks

### Operational Insights
- ConfigMap update frequency and debounce effectiveness
- Controller reconciliation patterns and efficiency metrics
- Cluster health and readiness status tracking

## Best Practices

### For Production Deployments
1. **Set appropriate resource limits**: Ensure containers have sufficient CPU and memory
2. **Use persistent storage**: Configure appropriate storage classes for your workload
3. **Enable monitoring**: Monitor both Kubernetes and Neo4j metrics
4. **Plan capacity**: Monitor trends and scale manually as needed

### For Development Environments
1. **Use smaller resource allocations**: Optimize for development machine resources
2. **Enable debug logging**: Set log levels for troubleshooting
3. **Use local storage**: Consider local storage for faster development cycles
