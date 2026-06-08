# Performance Tuning Guide

This comprehensive guide covers performance optimization strategies for Neo4j clusters deployed using the Neo4j Kubernetes Operator, focusing on server-based architecture and Neo4j 5.26+/2025.x versions.

## Overview

Neo4j performance in Kubernetes environments depends on several key factors:
- **Resource allocation** (CPU, memory, storage)
- **Cluster topology** and database distribution
- **Storage configuration** and I/O optimization
- **Network configuration** and discovery performance
- **Neo4j-specific tuning** parameters

## Server-Based Architecture Performance

The Neo4j Kubernetes Operator uses a server-based architecture where servers self-organize into roles based on database requirements.

### Optimal Server Configurations

| Use Case | Servers | Resource Profile | Database Strategy |
|----------|---------|------------------|-------------------|
| **Development** | 2 | 2 CPU, 4Gi RAM | Single database with simple topology |
| **Small Production** | 3 | 4 CPU, 8Gi RAM | Multiple databases with 1-2 primaries |
| **High Performance** | 5-7 | 8+ CPU, 16Gi+ RAM | Read-heavy databases with replicas |
| **Enterprise Scale** | 7+ | 16+ CPU, 32Gi+ RAM | Complex multi-database topologies |

### Memory Configuration

Neo4j Enterprise requires careful memory tuning:

```yaml
spec:
  resources:
    requests:
      memory: "8Gi"    # Minimum for production
      cpu: "4"
    limits:
      memory: "16Gi"   # Allow headroom for operations
      cpu: "8"

  # Neo4j memory settings
  config:
    # Heap memory (operator default: 60% of container memory for >=4Gi)
    server.memory.heap.initial_size: "9g"
    server.memory.heap.max_size: "9g"

    # Page cache (operator default: 30% of container memory for >=4Gi)
    server.memory.pagecache.size: "4g"

    # Global transaction state memory limit
    dbms.memory.transaction.total.max: "2g"
```

## Storage Performance Optimization

### Storage Classes and Types

**Recommended Storage Classes by Use Case:**

```yaml
# High Performance (NVMe SSD)
storage:
  className: "fast-ssd"        # AWS: gp3, GCP: pd-ssd, Azure: Premium_LRS
  size: "500Gi"

# Balanced Performance
storage:
  className: "standard-ssd"    # AWS: gp2, GCP: pd-standard, Azure: StandardSSD_LRS
  size: "1Ti"

# Cost-Optimized (for development)
storage:
  className: "standard"        # AWS: gp2, GCP: pd-standard, Azure: Standard_LRS
  size: "100Gi"
```

### Storage Performance Settings

```yaml
spec:
  config:
    # Transaction log settings for performance
    db.tx_log.rotation.retention_policy: "1 days"
    db.tx_log.rotation.size: "250M"

    # Checkpoint settings
    db.checkpoint.interval.time: "15m"
    db.checkpoint.interval.tx: "100000"

    # Store files optimization
    db.store.files.preallocate: "true"
```

## CPU Performance Optimization

### CPU Allocation Strategy

```yaml
spec:
  resources:
    requests:
      cpu: "4"        # Guaranteed CPU for consistent performance
    limits:
      cpu: "8"        # Burst capacity for peak loads

  config:
    # Worker thread pool (Neo4j 5.26+: server.* namespace)
    server.threads.worker_count: "8"           # 2x CPU cores

    # Bolt connection thread pool
    server.bolt.thread_pool_max_size: "400"
```

### JVM Performance Tuning

```yaml
spec:
  config:
    # GC optimization for Neo4j (JDK 17/21 — these flags are merged with the
    # operator's built-in G1GC tuning, not replaced)
    server.jvm.additional: >
      -XX:+UseG1GC
      -XX:MaxGCPauseMillis=200
      -XX:G1HeapRegionSize=32m
      -XX:+ParallelRefProcEnabled
      -XX:+UseStringDeduplication
```

## Network and Discovery Performance

### Cluster Communication Optimization

```yaml
spec:
  config:
    # Cluster communication timeouts (Neo4j 5.26+)
    dbms.cluster.raft.election_timeout: "7s"
    dbms.cluster.raft.leader_failure_detection_window: "30s"
```

> **Note:** Discovery settings (resolver type, endpoints, resolution timeout)
> are managed entirely by the operator (LIST discovery with static pod FQDNs).
> The config validator rejects user overrides such as
> `dbms.cluster.discovery.v2.endpoints`, `dbms.cluster.endpoints`, or
> `dbms.cluster.discovery.resolver_type`.

### Service Configuration for Performance

```yaml
spec:
  service:
    type: ClusterIP
    annotations:
      # AWS Load Balancer optimization
      service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout: "3600"
      # GCP optimization
      cloud.google.com/backend-config: '{"ports": {"7687":"neo4j-backend-config"}}'
```

## Database-Level Performance Optimization

### Database Topology for Performance

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jDatabase
metadata:
  name: high-performance-db
spec:
  clusterRef: production-cluster
  name: performance-db

  # Optimal topology for read-heavy workloads
  topology:
    primaries: 2      # Multiple primaries for write scalability
    secondaries: 3    # Read replicas for query distribution

  wait: true
  ifNotExists: true
```

### Query Performance Configuration

```yaml
spec:
  config:
    # Query logging settings
    db.logs.query.enabled: "INFO"
    db.logs.query.threshold: "1s"
    db.logs.query.parameter_logging_enabled: "true"

    # Query cache size — entries per database (Neo4j 5.26+)
    server.memory.query_cache.per_db_cache_num_entries: "1000"

    # Transaction timeout (kills long-running transactions)
    dbms.transaction.timeout: "120s"
```

## Monitoring and Performance Analysis

### Key Performance Metrics

Monitor these critical metrics for performance optimization:

1. **Resource Utilization**:
   ```bash
   # Clusters
   kubectl top pods -l neo4j.com/cluster=<cluster-name>
   # Standalone
   kubectl top pods -l app=<standalone-name>
   kubectl top nodes
   ```

2. **Neo4j-Specific Metrics**:
   - Page cache hit ratio (target: >95%)
   - Transaction throughput (TPS)
   - Query execution times
   - GC pause times

3. **Kubernetes Metrics**:
   - Pod CPU/Memory usage
   - Storage IOPS and latency
   - Network throughput

### Performance Monitoring Setup

```yaml
spec:
  monitoring:
    enabled: true
  config:
    # Prometheus metrics endpoint (overrides default if needed)
    server.metrics.prometheus.endpoint: "0.0.0.0:2004"
```

## Performance Testing and Benchmarking

### Load Testing Strategies

1. **Connection Pool Optimization**:
   ```yaml
   # Application connection settings
   NEO4J_URI: "neo4j://cluster-client:7687"
   NEO4J_MAX_CONNECTION_POOL_SIZE: "100"
   NEO4J_CONNECTION_TIMEOUT: "30s"
   NEO4J_MAX_TRANSACTION_RETRY_TIME: "30s"
   ```

2. **Concurrent Operations Testing**:
   ```bash
   # Test concurrent read operations
   for i in {1..10}; do
     kubectl exec -it cluster-server-0 -- cypher-shell -u neo4j -p password \
       "MATCH (n) RETURN count(n)" &
   done
   ```

### Benchmark Scenarios

| Scenario | Description | Key Metrics |
|----------|-------------|-------------|
| **Write Heavy** | High insert/update rate | TPS, transaction latency |
| **Read Heavy** | Complex analytical queries | Query response time, cache hit ratio |
| **Mixed Workload** | OLTP + analytics | Overall throughput, resource utilization |
| **Failover** | Node failure scenarios | Recovery time, data consistency |

## Troubleshooting Performance Issues

### Common Performance Problems

1. **High Memory Usage**:
   ```bash
   # Check memory allocation
   kubectl exec cluster-server-0 -- cypher-shell -u neo4j -p password \
     "CALL dbms.listConfig() YIELD name, value WHERE name CONTAINS 'memory'"
   ```

2. **Slow Query Performance**:
   ```bash
   # Inspect currently-running queries (slow ones surface here; the query log
   # configured via db.logs.query.* captures completed slow queries on disk)
   kubectl exec cluster-server-0 -- cypher-shell -u neo4j -p password \
     "SHOW TRANSACTIONS YIELD transactionId, currentQuery, elapsedTime ORDER BY elapsedTime DESC LIMIT 10"
   ```

3. **Storage I/O Bottlenecks**:
   ```bash
   # Check storage performance
   kubectl describe pv $(kubectl get pv | grep cluster-server | awk '{print $1}')
   ```

### Performance Optimization Checklist

- [ ] **Memory**: Heap ~60% / page cache ~30% of container memory (operator default for >=4Gi), leaving a system reserve
- [ ] **Storage**: Using SSD storage class with adequate IOPS
- [ ] **CPU**: Request/limit ratio allows for burst capacity
- [ ] **Network**: Cluster communication timeouts optimized
- [ ] **Queries**: Slow query logging enabled and monitored
- [ ] **Caching**: Page cache hit ratio >95%
- [ ] **GC**: GC pause times <200ms
- [ ] **Topology**: Database placement optimized for workload

## Advanced Performance Configurations

### Multi-Zone Performance

```yaml
spec:
  topology:
    servers: 6
    placement:
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1
      antiAffinity:
        enabled: true
        type: preferred
        topologyKey: kubernetes.io/hostname

  config:
    # Cross-zone communication tuning (Neo4j 5.26+). Discovery resolution itself
    # is operator-managed; tune RAFT failure detection to tolerate cross-zone latency.
    dbms.cluster.raft.leader_failure_detection_window: "60s"
```

### Resource Quotas and Limits

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: neo4j-performance-quota
spec:
  hard:
    requests.cpu: "32"
    requests.memory: "128Gi"
    limits.cpu: "64"
    limits.memory: "256Gi"
    persistentvolumeclaims: "10"
```

## Best Practices Summary

1. **Resource Planning**: Always allocate adequate resources for Neo4j Enterprise requirements
2. **Storage Selection**: Use high-performance storage classes for production workloads
3. **Memory Tuning**: Carefully balance heap and page cache allocation
4. **Monitoring**: Implement comprehensive monitoring for proactive performance management
5. **Testing**: Regular performance testing under realistic load conditions
6. **Scaling**: Scale servers based on actual database hosting requirements
7. **Optimization**: Continuously tune configuration based on workload patterns

For additional performance guidance, see:
- [Configuration Best Practices](configuration.md#best-practices-for-specconfig)
- [Resource Sizing Guide](guides/resource_sizing.md)
- [Monitoring Guide](guides/monitoring.md)
- [Backup and Restore](guides/backup_restore.md)
