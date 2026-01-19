# Split-Brain Recovery Guide

This guide provides comprehensive troubleshooting and recovery procedures for Neo4j cluster split-brain scenarios when using the Neo4j Kubernetes Operator with server-based architecture.

## Overview

Split-brain occurs when Neo4j cluster servers lose communication and form separate, independent clusters instead of one unified cluster. This can lead to data inconsistencies and cluster instability if not properly detected and resolved.

The Neo4j Kubernetes Operator includes **automatic split-brain detection and repair** to prevent and resolve these issues proactively.

## Understanding Split-Brain Scenarios

### What is Split-Brain?

Split-brain happens when:
1. Network partitions separate cluster servers
2. Servers cannot communicate with each other
3. Multiple independent "clusters" form within the same deployment
4. Each partition believes it is the authoritative cluster

### Common Causes

- **Network partitions** between Kubernetes nodes
- **Resource constraints** causing pod communication failures
- **DNS resolution issues** preventing server discovery
- **Storage problems** affecting cluster state persistence
- **Configuration errors** in discovery or networking

## Automatic Split-Brain Detection

The operator includes comprehensive split-brain detection that runs automatically during cluster health checks.

### Detection Process

1. **Multi-Pod Analysis**: Connects to each server pod individually
2. **Cluster View Comparison**: Compares each server's view of cluster membership
3. **Inconsistency Detection**: Identifies servers with conflicting cluster views
4. **Automatic Repair**: Restarts orphaned pods to rejoin the main cluster

### Detection Logs

Monitor operator logs for split-brain detection:

```bash
# Check for split-brain detection logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -i "split.*brain"

# Expected detection logs:
# Starting split-brain detection for cluster production-cluster, expectedServers: 3
# Split-brain analysis results: isSplitBrain: true, orphanedPods: 1, repairAction: RestartPods
# Split-brain automatically repaired by restarting orphaned pods: [production-cluster-server-2]
```

### Kubernetes Events

The operator generates events for split-brain scenarios:

```bash
# Check for split-brain events
kubectl get events --field-selector reason=SplitBrainDetected
kubectl get events --field-selector reason=SplitBrainRepaired

# Example events:
# Warning   SplitBrainDetected   Neo4jEnterpriseCluster/production-cluster   Split-brain detected: 1 orphaned servers
# Normal    SplitBrainRepaired   Neo4jEnterpriseCluster/production-cluster   Split-brain repaired: restarted orphaned pods
```

## Manual Split-Brain Detection

### Verify Cluster Health

1. **Check Server Status**:
   ```bash
   # Connect to each server and check cluster membership
   for i in 0 1 2; do
     echo "=== Server $i ==="
     kubectl exec production-cluster-server-$i -- cypher-shell -u neo4j -p password \
       "SHOW SERVERS YIELD name, state, health ORDER BY name"
     echo
   done
   ```

2. **Compare Cluster Views**:
   Look for inconsistencies in server lists between different pods. In a healthy cluster, all servers should see the same cluster membership.

3. **Check Database Allocation**:
   ```bash
   # Verify database distribution consistency
   kubectl exec production-cluster-server-0 -- cypher-shell -u neo4j -p password \
     "SHOW DATABASES YIELD name, currentStatus, role, address"
   ```

### Identify Split-Brain Symptoms

**Indicators of Split-Brain:**
- Different server counts reported by different pods
- Inconsistent database allocations across servers
- Some servers showing as "offline" from others' perspectives
- Database creation failures with "insufficient servers" errors
- Application connection failures to some databases

## Repair Strategies

### Automatic Repair (Recommended)

The operator automatically repairs split-brain scenarios by:

1. **Detection**: Identifying orphaned servers with inconsistent cluster views
2. **Analysis**: Determining the main cluster and orphaned servers
3. **Restart**: Gracefully restarting orphaned pods to rejoin the main cluster
4. **Verification**: Confirming successful cluster reformation

**No manual intervention required** - the operator handles this automatically.

### Manual Repair Procedures

If automatic repair fails or you need to intervene manually:

#### 1. Identify the Main Cluster

```bash
# Check which partition has the majority of servers
kubectl exec production-cluster-server-0 -- cypher-shell -u neo4j -p password \
  "SHOW SERVERS YIELD name, state ORDER BY name"

# Count active servers in each partition
kubectl exec production-cluster-server-1 -- cypher-shell -u neo4j -p password \
  "SHOW SERVERS YIELD name, state ORDER BY name"
```

#### 2. Restart Orphaned Servers

```bash
# Restart the server(s) that show inconsistent cluster views
kubectl delete pod production-cluster-server-2

# Wait for pod to restart and rejoin
kubectl wait --for=condition=Ready pod/production-cluster-server-2 --timeout=300s
```

#### 3. Verify Cluster Recovery

```bash
# Confirm all servers show consistent cluster membership
kubectl exec production-cluster-server-0 -- cypher-shell -u neo4j -p password \
  "SHOW SERVERS YIELD name, state, health ORDER BY name"

# Check database status
kubectl exec production-cluster-server-0 -- cypher-shell -u neo4j -p password \
  "SHOW DATABASES"
```

#### 4. Force Cluster Reformation (Last Resort)

If standard restart doesn't work, use cluster-wide restart:

```bash
# Delete all server pods simultaneously (data preserved in PVCs)
kubectl delete pods -l app.kubernetes.io/name=neo4j,neo4j.com/cluster=production-cluster

# Monitor cluster reformation
kubectl get pods -l app.kubernetes.io/name=neo4j -w
```

⚠️ **Warning**: Cluster-wide restart should only be used as a last resort and may cause temporary service interruption.

## Prevention Strategies

### Network Resilience

1. **Node Affinity Configuration**:
   ```yaml
   spec:
     topology:
       servers: 3
       placement:
         antiAffinity:
           enabled: true
           type: preferred    # Allow scheduling on same node if necessary
           topologyKey: kubernetes.io/hostname
   ```

2. **Multi-Zone Deployment**:
   ```yaml
   spec:
     topology:
       servers: 3
       placement:
         topologySpread:
           enabled: true
           topologyKey: topology.kubernetes.io/zone
           maxSkew: 1
   ```

### Resource Allocation

```yaml
spec:
  resources:
    requests:
      memory: "4Gi"    # Adequate memory to prevent OOM
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"
```

### Network Configuration

```yaml
spec:
  config:
    # Optimize discovery timeouts
    dbms.kubernetes.discovery.v2.refresh_rate: "10s"
    dbms.cluster.discovery.resolution_timeout: "30s"

    # Cluster communication resilience (Neo4j 5.26+)
    dbms.cluster.raft.election_timeout: "7s"
    dbms.cluster.raft.leader_failure_detection_window: "30s"
```

## Monitoring and Alerting

### Prometheus Metrics

Monitor these key metrics for early split-brain detection:

```yaml
# Cluster health metrics
neo4j_cluster_servers_total
neo4j_cluster_servers_online
neo4j_database_allocation_inconsistency

# Alert rules
groups:
- name: neo4j.split-brain
  rules:
  - alert: Neo4jSplitBrainDetected
    expr: neo4j_cluster_servers_online < neo4j_cluster_servers_total
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "Neo4j cluster split-brain detected"
      description: "Cluster {{ $labels.cluster }} has {{ $value }} online servers out of {{ neo4j_cluster_servers_total }} total servers"
```

### Log Monitoring

Set up log monitoring for split-brain events:

```bash
# Alert on split-brain detection logs
kubectl logs -f -n neo4j-operator-system deployment/neo4j-operator-controller-manager | \
  grep -E "(split.*brain|Split.*Brain)" --line-buffered | \
  while read line; do
    echo "ALERT: $line"
    # Send to monitoring system
  done
```

### Health Check Automation

```bash
#!/bin/bash
# Automated cluster health check script

CLUSTER_NAME="production-cluster"
NAMESPACE="default"

check_cluster_health() {
  local expected_servers=3
  local consistent_views=0

  for i in $(seq 0 $((expected_servers-1))); do
    local server_count=$(kubectl exec ${CLUSTER_NAME}-server-$i -n $NAMESPACE -- \
      cypher-shell -u neo4j -p password \
      "SHOW SERVERS YIELD name" 2>/dev/null | wc -l)

    if [ "$server_count" -eq "$expected_servers" ]; then
      ((consistent_views++))
    fi
  done

  if [ "$consistent_views" -eq "$expected_servers" ]; then
    echo "✅ Cluster health: OK"
    return 0
  else
    echo "❌ Split-brain detected: $consistent_views/$expected_servers servers have consistent views"
    return 1
  fi
}

# Run health check
if ! check_cluster_health; then
  echo "🔄 Triggering operator reconciliation..."
  kubectl annotate neo4jenterprisecluster $CLUSTER_NAME -n $NAMESPACE \
    "troubleshooting.neo4j.com/reconcile=$(date +%s)" --overwrite
fi
```

## Troubleshooting Common Issues

### Split-Brain Detection Not Working

1. **Check Operator Logs**:
   ```bash
   kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager --tail=100
   ```

2. **Verify RBAC Permissions**:
   ```bash
   kubectl auth can-i get pods --as=system:serviceaccount:neo4j-operator-system:neo4j-operator-controller-manager
   kubectl auth can-i exec pods --as=system:serviceaccount:neo4j-operator-system:neo4j-operator-controller-manager
   ```

3. **Check Neo4j Connectivity**:
   ```bash
   # Test if operator can connect to Neo4j
   kubectl exec production-cluster-server-0 -- cypher-shell -u neo4j -p password "RETURN 'test'"
   ```

### False Split-Brain Detection

If the operator incorrectly identifies split-brain:

1. **Check Resource Constraints**:
   ```bash
   kubectl describe pods -l app.kubernetes.io/name=neo4j
   kubectl top pods -l app.kubernetes.io/name=neo4j
   ```

2. **Verify Network Connectivity**:
   ```bash
   # Test inter-pod communication
   kubectl exec production-cluster-server-0 -- nc -zv production-cluster-server-1 5000
   ```

3. **Review Configuration**:
   ```bash
   kubectl get neo4jenterprisecluster production-cluster -o yaml | grep -A 20 "spec:"
   ```

### Recovery Failures

If automatic recovery fails:

1. **Check Pod Status**:
   ```bash
   kubectl get pods -l app.kubernetes.io/name=neo4j
   kubectl describe pod production-cluster-server-0
   ```

2. **Review Events**:
   ```bash
   kubectl get events --sort-by=.metadata.creationTimestamp | tail -20
   ```

3. **Inspect Storage**:
   ```bash
   kubectl get pvc -l app.kubernetes.io/name=neo4j
   kubectl describe pvc data-production-cluster-server-0
   ```

## Emergency Recovery Procedures

### Complete Cluster Reset

⚠️ **Use only as a last resort - may cause data loss**

```bash
# 1. Scale down the cluster
kubectl patch neo4jenterprisecluster production-cluster --type='json' \
  -p='[{"op": "replace", "path": "/spec/topology/servers", "value": 0}]'

# 2. Wait for pods to terminate
kubectl wait --for=delete pod -l app.kubernetes.io/name=neo4j --timeout=300s

# 3. Clean up cluster state (if necessary)
# Note: This may cause data loss - only do if cluster is completely corrupted
# kubectl delete pvc -l app.kubernetes.io/name=neo4j

# 4. Scale back up
kubectl patch neo4jenterprisecluster production-cluster --type='json' \
  -p='[{"op": "replace", "path": "/spec/topology/servers", "value": 3}]'

# 5. Monitor recovery
kubectl get pods -l app.kubernetes.io/name=neo4j -w
```

### Data Recovery from Backups

If split-brain causes data corruption:

```bash
# 1. Create restoration cluster
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: recovery-cluster
spec:
  topology:
    servers: 3
  # ... same configuration as original cluster
EOF

# 2. Restore from backup
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: split-brain-recovery
spec:
  clusterRef: recovery-cluster
  backupRef: latest-backup
  options:
    force: true
EOF

# 3. Verify data integrity
kubectl exec recovery-cluster-server-0 -- cypher-shell -u neo4j -p password \
  "MATCH (n) RETURN count(n) as node_count"
```

## Best Practices Summary

1. **Prevention**:
   - Use adequate resource allocation
   - Deploy across multiple zones
   - Configure proper network policies
   - Monitor cluster health continuously

2. **Detection**:
   - Rely on automatic split-brain detection
   - Set up monitoring and alerting
   - Regular health checks

3. **Recovery**:
   - Trust automatic repair mechanisms
   - Manual intervention only when necessary
   - Always verify cluster health after recovery

4. **Monitoring**:
   - Monitor operator logs for split-brain events
   - Set up Kubernetes event alerting
   - Track cluster consistency metrics

For additional troubleshooting help, see:
- [General Troubleshooting Guide](../guides/troubleshooting.md)
- [TLS Configuration Issues](../configuration/tls.md)
- [Performance Troubleshooting](../performance.md)
- [Backup/Restore Issues](backup_restore.md)
