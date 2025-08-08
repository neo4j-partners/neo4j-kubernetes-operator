# Resource Sizing Guide for Neo4j Enterprise Clusters

This comprehensive guide explains how to properly size CPU and memory resources for Neo4j Enterprise clusters deployed with the Kubernetes operator. Learn how to optimize performance while managing infrastructure costs.

## Table of Contents
- [Quick Start](#quick-start)
- [Understanding Resource Configuration](#understanding-resource-configuration)
- [Automatic Recommendations](#automatic-recommendations)
- [Simple Use Cases](#simple-use-cases)
- [Advanced Configuration](#advanced-configuration)
- [Memory Deep Dive](#memory-deep-dive)
- [CPU Configuration](#cpu-configuration)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)

## Quick Start

For most users, these configurations will work well:

```yaml
# Development/Testing (2 servers, minimal resources)
resources:
  requests: { memory: "2Gi", cpu: "500m" }
  limits: { memory: "4Gi", cpu: "2" }

# Standard Production (3-4 servers)
resources:
  requests: { memory: "4Gi", cpu: "1" }
  limits: { memory: "8Gi", cpu: "4" }

# Large Production (5+ servers)
resources:
  requests: { memory: "3Gi", cpu: "750m" }
  limits: { memory: "6Gi", cpu: "2" }
```

## Understanding Resource Configuration

### Resource Fields Explained

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
spec:
  resources:
    requests:      # Guaranteed resources (reserved)
      memory: "4Gi"   # Minimum memory guaranteed
      cpu: "1"        # Minimum CPU cores guaranteed (1000m)
    limits:        # Maximum resources (ceiling)
      memory: "8Gi"   # Maximum memory pod can use
      cpu: "4"        # Maximum CPU cores pod can use
```

**Key Concepts:**
- **Requests**: Kubernetes guarantees these resources are available
- **Limits**: Pod is throttled/killed if it exceeds these
- **Memory**: Should have requests = limits (Neo4j doesn't handle swapping well)
- **CPU**: Can have limits > requests (allows burst processing)

### How the Operator Uses Resources

1. **Validation**: Ensures minimum 1Gi memory for Neo4j Enterprise
2. **Auto-calculation**: Divides memory between heap, page cache, and OS
3. **Recommendations**: Suggests optimal settings based on cluster size
4. **Prevention**: Blocks configurations that would cause OOM errors

## Automatic Recommendations

The operator provides intelligent recommendations based on your cluster topology:

| Cluster Size | Memory/Pod | CPU Limits | Heap/Cache Split | Use Case |
|-------------|------------|------------|------------------|----------|
| 1 server | 8Gi | 4 cores | 50%/50% | Development only |
| 2 servers | 6Gi | 3 cores | 50%/50% | Limited HA (not recommended) |
| 3-4 servers | 4Gi | 2 cores | 55%/45% | **Standard production** |
| 5-6 servers | 3Gi | 2 cores | 60%/40% | Large clusters |
| 7+ servers | 2Gi | 1 core | 60%/40% | Very large clusters |

**Note**: System reserves 512MB-1GB for OS operations

## Simple Use Cases

### Development Environment

**Goal**: Minimize resource usage for local development

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: dev-cluster
spec:
  topology:
    servers: 2  # Minimum for clustering

  resources:
    requests:
      memory: "1Gi"    # Absolute minimum
      cpu: "250m"      # Quarter core
    limits:
      memory: "1Gi"    # Same as requests
      cpu: "1"         # Allow CPU burst

  # Neo4j auto-configures: ~400MB heap, ~400MB cache
```

### Small Production (< 100GB data, < 100 concurrent users)

**Goal**: Reliable performance with cost efficiency

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: small-prod
spec:
  topology:
    servers: 3  # Minimum for production HA

  resources:
    requests:
      memory: "4Gi"
      cpu: "1"
    limits:
      memory: "4Gi"    # No overcommit
      cpu: "2"         # 2x burst capacity

  # Neo4j auto-configures: ~2GB heap, ~1.5GB cache
```

### Medium Production (100GB-1TB data, 100-1000 users)

**Goal**: Balance performance and availability

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: medium-prod
spec:
  topology:
    servers: 5  # Good availability, odd number for quorum

  resources:
    requests:
      memory: "8Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  # Explicit Neo4j configuration
  config:
    server.memory.heap.max_size: "4G"
    server.memory.heap.initial_size: "4G"
    server.memory.pagecache.size: "3G"
```

### Large Production (1TB+ data, 1000+ users)

**Goal**: Maximum performance and availability

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: large-prod
spec:
  topology:
    servers: 7  # High availability across zones

  resources:
    requests:
      memory: "16Gi"
      cpu: "4"
    limits:
      memory: "16Gi"
      cpu: "8"

  config:
    # Fine-tuned memory settings
    server.memory.heap.max_size: "8G"
    server.memory.heap.initial_size: "8G"
    server.memory.pagecache.size: "6G"

    # Performance tuning (Neo4j 5.26+ settings)
    dbms.memory.transaction.total.max: "2G"
    server.bolt.thread_pool_max_size: "400"
```

## Advanced Configuration

### Manual Memory Tuning

Override automatic memory calculations when you need precise control:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: tuned-cluster
spec:
  topology:
    servers: 4

  resources:
    limits:
      memory: "12Gi"
      cpu: "6"
    requests:
      memory: "12Gi"
      cpu: "3"

  config:
    # Manual memory configuration
    server.memory.heap.initial_size: "4G"
    server.memory.heap.max_size: "6G"      # 50% for heap
    server.memory.pagecache.size: "5G"     # 42% for cache
    # Leaves 1GB (8%) for OS

    # Transaction memory limits (Neo4j recommended)
    dbms.memory.transaction.total.max: "1G"        # Global transaction memory limit
    db.memory.transaction.total.max: "512M"        # Per-database limit
    db.memory.transaction.max: "256M"              # Per-transaction limit

    # Off-heap memory
    dbms.memory.off_heap.max_size: "512M"
```

### JVM and Garbage Collection Tuning

Configure JVM settings for optimal performance:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: jvm-tuned-cluster
spec:
  topology:
    servers: 3

  resources:
    limits:
      memory: "16Gi"

  config:
    # Memory configuration
    server.memory.heap.initial_size: "8G"
    server.memory.heap.max_size: "8G"
    server.memory.pagecache.size: "6G"

    # JVM tuning (Neo4j 5.26+ and 2025.x)
    server.jvm.additional: |
      -XX:+UseG1GC
      -XX:MaxGCPauseMillis=200
      -XX:+ParallelRefProcEnabled
      -XX:+UnlockExperimentalVMOptions
      -XX:+UnlockDiagnosticVMOptions
      -XX:G1NewSizePercent=2
      -XX:G1MaxNewSizePercent=10
      -XX:+G1UseAdaptiveIHOP
      -XX:InitiatingHeapOccupancyPercent=45
      -XX:+UseCompressedOops
      -XX:+UseCompressedClassPointers

    # Bolt thread pool tuning (Neo4j 5.26+ format)
    server.bolt.thread_pool_min_size: "10"
    server.bolt.thread_pool_max_size: "400"
    server.bolt.thread_pool_keep_alive: "5m"
```

**JVM Best Practices:**
- Use G1GC for heaps > 4GB (default in modern JVMs)
- Enable compressed OOPs for heaps up to 31GB (saves ~30% memory)
- Set heap initial = max to avoid resize pauses
- Monitor GC logs to tune pause time goals

### Query-Heavy Workloads

Optimize for complex analytical queries:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: analytics-cluster
spec:
  topology:
    servers: 3

  resources:
    limits:
      memory: "32Gi"  # Large memory for analytics
      cpu: "16"       # High CPU for parallel processing
    requests:
      memory: "32Gi"
      cpu: "8"

  config:
    # Favor heap for query processing
    server.memory.heap.max_size: "20G"     # 62% for complex queries
    server.memory.pagecache.size: "10G"    # 31% for data access

    # Query optimization
    dbms.cypher.runtime: "pipelined"
    dbms.cypher.planner: "cost"
    dbms.memory.transaction.total.max: "4G"

    # Parallelism
    dbms.threads.worker_count: "16"
    internal.dbms.executors.parallel.enabled: "true"
```

### Write-Heavy Workloads

Optimize for high-throughput data ingestion:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: ingestion-cluster
spec:
  topology:
    servers: 5

  resources:
    limits:
      memory: "16Gi"
      cpu: "8"
    requests:
      memory: "16Gi"
      cpu: "4"

  config:
    # Favor page cache for write buffers
    server.memory.heap.max_size: "6G"      # 37% for processing
    server.memory.pagecache.size: "9G"     # 56% for write caching

    # Write optimization
    dbms.checkpoint.interval.time: "30m"
    dbms.checkpoint.interval.tx: "1000000"
    dbms.checkpoint.interval.volume: "1GB"

    # Transaction log
    dbms.tx_log.rotation.retention_policy: "1G size"
    dbms.tx_log.rotation.size: "256M"
```

### Vector Index Workloads (Neo4j 2025.x)

Optimize for semantic search and vector operations:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: vector-search-cluster
spec:
  topology:
    servers: 4

  resources:
    limits:
      memory: "24Gi"  # Extra memory for vector indexes
      cpu: "8"

  config:
    # Memory allocation for vector workloads
    # Formula: Heap + PageCache + 0.25*(Vector Index Size) + OS
    server.memory.heap.initial_size: "8G"
    server.memory.heap.max_size: "8G"      # For query processing
    server.memory.pagecache.size: "10G"    # For graph data
    # Leaves 6GB for OS-managed vector index memory

    # Transaction memory for complex vector queries
    dbms.memory.transaction.total.max: "2G"

    # Vector-specific optimizations
    dbms.cypher.runtime: "parallel"  # For vector operations
    dbms.threads.worker_count: "16"
```

**Vector Index Memory Calculation Example:**
- 10M vectors with 1536 dimensions (float32)
- Index size: ~60GB on disk
- Required OS memory: 0.25 * 60GB = 15GB
- Total container memory: 8GB (heap) + 10GB (page cache) + 15GB (vector) + 2GB (OS) = 35GB

**Best Practices for Vector Workloads:**
1. Use 1:4 memory-to-storage ratio for optimal performance
2. Pre-warm indexes with random queries after startup
3. Monitor OS memory usage (vector indexes use OS cache, not Neo4j page cache)
4. Consider dedicated nodes for vector-heavy workloads

### Mixed Workloads with Read Replicas

Balance reads and writes with dedicated topology:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: mixed-workload
spec:
  topology:
    servers: 6  # Will be allocated by databases

  resources:
    limits:
      memory: "8Gi"
      cpu: "4"
    requests:
      memory: "8Gi"
      cpu: "2"

  config:
    # Balanced configuration
    server.memory.heap.max_size: "4G"
    server.memory.pagecache.size: "3G"

    # Read routing
    dbms.routing.enabled: "true"
    dbms.routing.default_router: "SERVER"
---
# Database with read-heavy topology
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: app-database
spec:
  clusterRef: mixed-workload
  topology:
    primaries: 2    # For writes
    secondaries: 4  # For reads
```

## Memory Deep Dive

### Memory Architecture

```
Container Memory Limit (e.g., 8Gi)
├── JVM Heap (e.g., 4Gi)
│   ├── Query Processing
│   ├── Transaction State
│   └── Cypher Runtime
├── Page Cache (e.g., 3Gi)
│   ├── Database Pages
│   ├── Index Caching
│   └── Write Buffers
└── System/OS (e.g., 1Gi)
    ├── Native Memory
    ├── Network Buffers
    └── File System Cache
```

### Page Cache Sizing Based on Database Size

Neo4j recommends sizing page cache based on actual database size:

```bash
# Check database size in Neo4j
CALL dbms.listPools() YIELD name, currentSize, maxSize
WHERE name CONTAINS 'page'
RETURN name, currentSize, maxSize;

# Or check file system
kubectl exec <pod-name> -- du -sh /var/lib/neo4j/data/databases/
```

**Page Cache Formula (Neo4j Official):**
```
Page Cache Size = Database Size × 1.2 (20% growth buffer)
```

**Examples:**
| Database Size | Recommended Page Cache | Container Memory |
|--------------|------------------------|------------------|
| 10GB | 12GB | 16GB+ |
| 50GB | 60GB | 80GB+ |
| 100GB | 120GB | 160GB+ |
| 500GB | 600GB | 640GB+ |

```yaml
# Example for 50GB database
config:
  server.memory.heap.max_size: "16G"      # For operations
  server.memory.pagecache.size: "60G"     # 1.2 × 50GB
  # Total: 76GB Neo4j + 4GB OS = 80GB container
```

### Calculation Examples

**8Gi Container Memory:**
```yaml
# Automatic calculation:
System Reserved: 1Gi (12.5%)
Available: 7Gi
├── Heap: 3.85Gi (55% of available)
└── Page Cache: 3.15Gi (45% of available)

# Manual override:
config:
  server.memory.heap.max_size: "4G"     # 50%
  server.memory.pagecache.size: "3G"    # 37.5%
  # System: 1Gi (12.5%)
```

### Memory Validation

The operator validates memory to prevent issues:

```yaml
# ❌ WILL FAIL: Neo4j memory exceeds container
resources:
  limits:
    memory: "4Gi"
config:
  server.memory.heap.max_size: "3G"
  server.memory.pagecache.size: "2G"  # Total 5G > 4Gi limit!

# ✅ VALID: Fits within container
resources:
  limits:
    memory: "6Gi"
config:
  server.memory.heap.max_size: "3G"
  server.memory.pagecache.size: "2G"  # Total 5G < 6Gi limit
```

## CPU Configuration

### CPU Units Explained

```yaml
cpu: "1"      # 1 full core (1000 millicores)
cpu: "500m"   # Half core (500 millicores)
cpu: "2.5"    # 2.5 cores (2500 millicores)
```

### CPU Sizing Guidelines

| Workload Type | Requests | Limits | Reasoning |
|---------------|----------|--------|-----------|
| Development | 250m | 1 | Minimal baseline, allow bursts |
| Light Production | 500m | 2 | Steady state with 4x burst |
| Standard Production | 1 | 4 | Good baseline with headroom |
| Query-Heavy | 2 | 8 | Complex queries need CPU |
| Write-Heavy | 1 | 4 | I/O bound more than CPU |
| Large Cluster | 4 | 16 | Coordination overhead |

### CPU Examples

```yaml
# Query-intensive workload
resources:
  requests:
    cpu: "4"      # High baseline for consistent performance
  limits:
    cpu: "8"      # 2x burst for complex queries

# Write-intensive workload
resources:
  requests:
    cpu: "1"      # Lower baseline (I/O bound)
  limits:
    cpu: "4"      # 4x burst for checkpoint operations

# Cost-optimized production
resources:
  requests:
    cpu: "500m"   # Low guarantee saves cost
  limits:
    cpu: "4"      # High ceiling for when needed
```

## Troubleshooting

### Common Issues and Solutions

#### 1. Pod OOMKilled

**Symptom**: Pod restarts with `OOMKilled` reason

**Diagnosis**:
```bash
kubectl describe pod <pod-name>
# Look for: Last State: Terminated, Reason: OOMKilled
```

**Solutions**:
```yaml
# Increase memory limit
resources:
  limits:
    memory: "8Gi"  # Was 4Gi

# Or reduce Neo4j memory usage
config:
  server.memory.heap.max_size: "2G"     # Was 3G
  server.memory.pagecache.size: "1.5G"  # Was 2G
```

#### 2. Slow Query Performance

**Symptom**: Queries take longer than expected

**Diagnosis**:
```bash
# Check CPU throttling
kubectl top pod <pod-name>
# If CPU near limit, being throttled

# Check memory pressure
kubectl exec <pod-name> -- neo4j-admin server memory-recommendation
```

**Solutions**:
```yaml
# Increase CPU limits for burst capacity
resources:
  limits:
    cpu: "8"  # Was 2

# Increase heap for query processing
config:
  server.memory.heap.max_size: "6G"  # Was 3G
```

#### 3. Cluster Formation Failures

**Symptom**: Cluster stuck in "Pending" or pods crash during startup

**Diagnosis**:
```bash
kubectl logs <pod-name> | grep -i memory
# Look for: "insufficient memory" messages
```

**Solutions**:
```yaml
# Ensure minimum memory (1Gi absolute minimum, 2Gi recommended)
resources:
  limits:
    memory: "2Gi"  # Increase from 1Gi
```

#### 4. Node Resource Pressure

**Symptom**: Pods pending with "Insufficient memory" or "Insufficient cpu"

**Diagnosis**:
```bash
kubectl describe node <node-name>
# Check Allocatable vs Requested resources

kubectl get events --field-selector type=Warning
```

**Solutions**:
```yaml
# Option 1: Reduce resource requests
resources:
  requests:
    memory: "2Gi"  # Was 4Gi
    cpu: "500m"    # Was 1

# Option 2: Use node affinity for larger nodes
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["m5.2xlarge", "m5.4xlarge"]
```

### Monitoring Commands

```bash
# Real-time resource usage
kubectl top pods -l neo4j.com/cluster=<cluster-name>

# Historical resource usage (if metrics-server installed)
kubectl describe pod <pod-name> | grep -A 10 "Containers:"

# Neo4j memory recommendation tool (official guidance)
kubectl exec <pod-name> -- neo4j-admin server memory-recommendation --memory=8g --verbose

# Example output:
# NEO4J MANUAL MEMORY RECOMMENDATIONS:
# Assuming the system has 8g of memory:
# server.memory.heap.initial_size=3200m
# server.memory.heap.max_size=3200m
# server.memory.pagecache.size=3600m

# Check actual memory usage in Neo4j
kubectl exec <pod-name> -- cypher-shell -u neo4j -p <password> \
  "CALL dbms.listPools() YIELD name, currentSize, maxSize WHERE name CONTAINS 'heap' OR name CONTAINS 'page' RETURN name, currentSize, maxSize"

# Monitor transaction memory
kubectl exec <pod-name> -- cypher-shell -u neo4j -p <password> \
  "SHOW TRANSACTIONS YIELD currentQuery, allocatedBytes, status RETURN currentQuery, allocatedBytes, status"

# Check for throttling
kubectl get --raw /api/v1/nodes/<node>/proxy/stats/summary | jq '.pods[] | select(.podRef.name=="<pod-name>") | .cpu.usageCoreNanoSeconds'

# Memory pressure indicators
kubectl exec <pod-name> -- cat /proc/meminfo | grep -E "MemFree|MemAvailable|Cached"

# GC activity monitoring
kubectl exec <pod-name> -- jcmd 1 GC.heap_info
kubectl exec <pod-name> -- jcmd 1 VM.native_memory summary
```

## Best Practices

### Production Checklist

- [ ] **Memory requests = limits** (prevent swapping)
- [ ] **Minimum 2Gi memory** for production clusters
- [ ] **Odd number of servers** (3, 5, 7) for better quorum
- [ ] **CPU limits > requests** for burst capacity
- [ ] **Anti-affinity rules** to spread across nodes
- [ ] **Resource monitoring** enabled (metrics-server, Prometheus)
- [ ] **Regular performance testing** with production-like data

### Cost Optimization

1. **Right-size based on actual usage**:
   ```bash
   # Analyze actual usage over time
   kubectl top pods -l neo4j.com/cluster=<name> --use-protocol-buffers
   ```

2. **Use spot/preemptible instances** for non-critical:
   ```yaml
   tolerations:
   - key: "kubernetes.io/spot-instance"
     operator: "Equal"
     value: "true"
     effect: "NoSchedule"
   ```

3. **Scale horizontally** rather than vertically:
   ```yaml
   # Better: 5 servers with 4Gi each
   # Than: 3 servers with 8Gi each
   ```

### Performance Optimization

1. **Profile before optimizing**:
   ```cypher
   PROFILE MATCH (n:Person)-[:KNOWS]->(m:Person)
   RETURN n.name, count(m) as friends
   ORDER BY friends DESC LIMIT 10
   ```

2. **Monitor key metrics**:
   - Page cache hit ratio (target > 90%)
   - Heap usage (should fluctuate, not constantly high)
   - CPU usage (sustained > 80% needs investigation)
   - Query execution time (p99 latency)

3. **Adjust based on workload**:
   - OLTP: Balance heap and cache
   - OLAP: Increase heap for complex queries
   - Bulk loading: Increase page cache
   - Graph algorithms: Maximum heap

### Validation Rules

The operator enforces these rules:

| Rule | Minimum | Recommended | Maximum |
|------|---------|-------------|---------|
| Container Memory | 1Gi | 4Gi+ | Node capacity |
| Heap Size | 256MB | 2Gi+ | 31Gi (JVM limit) |
| Page Cache | 128MB | 1Gi+ | Container - heap - 1Gi |
| CPU | 100m | 1 core+ | Node capacity |
| Servers (cluster) | 2 | 3+ | 20 |

## Advanced Topics

### NUMA Awareness

For large memory systems (>64GB):

```yaml
# Pin to NUMA node
resources:
  limits:
    memory: "64Gi"
    cpu: "32"

affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: topology.kubernetes.io/numa-node
          operator: In
          values: ["0"]  # Pin to NUMA node 0
```

### Huge Pages

For very large heaps (>32GB):

```yaml
resources:
  limits:
    memory: "64Gi"
    hugepages-2Mi: "32Gi"  # Use huge pages for heap

env:
- name: JAVA_OPTS
  value: "-XX:+UseTransparentHugePages -XX:+UseG1GC"
```

### Guaranteed QoS Class

Ensure pod gets Guaranteed QoS:

```yaml
resources:
  requests:
    memory: "8Gi"
    cpu: "4"
  limits:
    memory: "8Gi"  # Same as requests
    cpu: "4"        # Same as requests
# Results in QoS Class: Guaranteed
```

## Alignment with Neo4j Official Recommendations

This guide follows Neo4j's official performance documentation for versions 5.26+ and 2025.x:

### ✅ Neo4j Best Practices Implemented

1. **Memory Configuration**
   - Uses `server.memory.*` settings (not deprecated `dbms.memory.*`)
   - Sets `heap.initial_size = heap.max_size` to avoid GC pauses
   - Reserves ~1GB for OS operations

2. **Page Cache Sizing**
   - Formula: `Page Cache = Database Size × 1.2`
   - Accounts for 20% growth buffer

3. **JVM Tuning**
   - G1GC for heaps > 4GB
   - Compressed OOPs for heaps up to 31GB
   - Proper GC tuning parameters

4. **Transaction Memory**
   - Global limits with `dbms.memory.transaction.total.max`
   - Per-database limits with `db.memory.transaction.total.max`
   - Per-transaction limits with `dbms.memory.transaction.max`

5. **Vector Index Support (2025.x)**
   - Formula: `Heap + PageCache + 0.25×(Vector Index Size) + OS`
   - 1:4 memory-to-storage ratio recommendation

### 📊 Memory Allocation Guidelines

| Component | Neo4j Recommendation | Our Default |
|-----------|---------------------|-------------|
| Heap | 40-60% of available | 50-60% |
| Page Cache | 1.2× database size | 40-50% of available |
| OS Reserve | ~1GB | 512MB-1GB |
| Transaction | Configure explicitly | Examples provided |

### 🔧 Tools for Validation

```bash
# Use Neo4j's official memory recommendation tool
neo4j-admin server memory-recommendation --memory=<container-memory>

# Monitor with Neo4j procedures
CALL dbms.listPools()
SHOW TRANSACTIONS
```

## Summary

Resource sizing is critical for Neo4j performance. Key takeaways:

1. **Start with operator recommendations** based on cluster size
2. **Memory is most critical** - ensure adequate allocation
3. **CPU burst capacity** helps with query spikes
4. **Monitor and adjust** based on actual workload
5. **Validate configurations** before production deployment

For most production deployments, the 3-4 server configuration with 4-8Gi memory per pod provides the best balance of performance, availability, and cost.

**This guide aligns with Neo4j Operations Manual:**
- [Neo4j 5.26 Performance Guide](https://neo4j.com/docs/operations-manual/5/performance/)
- [Neo4j 2025.x Performance Guide](https://neo4j.com/docs/operations-manual/2025.07/performance/)

## Related Documentation

- [Configuration Best Practices](configuration_best_practices.md)
- [Performance Tuning](performance.md)
- [Troubleshooting Guide](troubleshooting.md)
- [Fault Tolerance](fault_tolerance.md)
