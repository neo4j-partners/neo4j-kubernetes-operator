# Split-Brain Recovery Quick Reference

Fast reference guide for detecting and recovering from Neo4j cluster split-brain scenarios.

## Quick Detection

```bash
# Check cluster consistency across all servers
for i in 0 1 2; do
  echo "=== Server $i ==="
  kubectl exec cluster-server-$i -- cypher-shell -u neo4j -p password \
    "SHOW SERVERS YIELD name, state ORDER BY name"
done
```

**✅ Healthy**: All servers show same server list
**❌ Split-Brain**: Different servers show different server lists

## Automatic Recovery

The Neo4j Kubernetes Operator **automatically detects and repairs** split-brain scenarios:

### Monitor Auto-Recovery
```bash
# Watch operator logs for split-brain detection
kubectl logs -f -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -i "split.*brain"

# Check for split-brain events
kubectl get events --field-selector reason=SplitBrainDetected
kubectl get events --field-selector reason=SplitBrainRepaired
```

### Expected Auto-Recovery Logs
```
Starting split-brain detection for cluster production-cluster, expectedServers: 3
Split-brain analysis results: isSplitBrain: true, orphanedPods: 1
Split-brain automatically repaired by restarting orphaned pods: [cluster-server-2]
```

## Manual Recovery (If Auto-Recovery Fails)

### 1. Identify Main Cluster
```bash
# Count servers visible to each pod
kubectl exec cluster-server-0 -- cypher-shell -u neo4j -p password "SHOW SERVERS" | wc -l
kubectl exec cluster-server-1 -- cypher-shell -u neo4j -p password "SHOW SERVERS" | wc -l
kubectl exec cluster-server-2 -- cypher-shell -u neo4j -p password "SHOW SERVERS" | wc -l
```

### 2. Restart Orphaned Servers
```bash
# Restart the server(s) with inconsistent views
kubectl delete pod cluster-server-X

# Wait for rejoin
kubectl wait --for=condition=Ready pod/cluster-server-X --timeout=300s
```

### 3. Verify Recovery
```bash
# All servers should now show consistent cluster membership
kubectl exec cluster-server-0 -- cypher-shell -u neo4j -p password \
  "SHOW SERVERS YIELD name, state ORDER BY name"
```

## Emergency Procedures

### Force Full Cluster Restart
⚠️ **Use only if individual pod restart fails**

```bash
# Delete all server pods (data preserved in PVCs)
kubectl delete pods -l app.kubernetes.io/name=neo4j,neo4j.com/cluster=CLUSTER_NAME

# Monitor reformation
kubectl get pods -l app.kubernetes.io/name=neo4j -w
```

### Trigger Operator Reconciliation
```bash
# Force operator to re-examine cluster
kubectl annotate neo4jenterprisecluster CLUSTER_NAME \
  "operator.neo4j.com/force-reconcile=$(date +%s)"
```

## Common Symptoms

| Symptom | Indicates Split-Brain |
|---------|----------------------|
| Different server counts per pod | ✅ |
| "Insufficient servers" database errors | ✅ |
| Some databases unreachable | ✅ |
| Inconsistent `SHOW DATABASES` output | ✅ |
| Application connection failures | ⚠️ Possible |

## Prevention Quick Tips

### Resource Allocation
```yaml
spec:
  resources:
    requests:
      memory: "4Gi"  # Prevent OOM
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"
```

### Multi-Zone Deployment
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

### Network Resilience
```yaml
spec:
  config:
    # Optimize discovery timeouts
    dbms.kubernetes.discovery.v2.refresh_rate: "10s"
    dbms.cluster.raft.election_timeout: "7s"  # Neo4j 5.26+
```

## Monitoring Commands

```bash
# Health check script
#!/bin/bash
CLUSTER="production-cluster"
EXPECTED=3

for i in $(seq 0 $((EXPECTED-1))); do
  COUNT=$(kubectl exec ${CLUSTER}-server-$i -- cypher-shell -u neo4j -p password \
    "SHOW SERVERS" 2>/dev/null | wc -l)
  echo "Server $i sees $COUNT servers"
  [ "$COUNT" -ne "$EXPECTED" ] && echo "⚠️ Split-brain detected!"
done
```

## Quick Troubleshooting

| Issue | Command | Solution |
|-------|---------|----------|
| Can't connect to Neo4j | `kubectl exec cluster-server-0 -- cypher-shell -u neo4j -p password "RETURN 1"` | Check credentials/network |
| Pod not ready | `kubectl describe pod cluster-server-0` | Check resources/storage |
| Operator not responding | `kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager` | Check operator health |
| RBAC issues | `kubectl auth can-i exec pods --as=system:serviceaccount:neo4j-operator-system:operator` | Fix permissions |

## Emergency Contacts

When automatic recovery fails:
1. **Check operator logs** first
2. **Try manual pod restart**
3. **Full cluster restart** if necessary
4. **Restore from backup** as last resort

**⚠️ Remember**: The operator handles 99% of split-brain scenarios automatically. Manual intervention should be rare.

For detailed procedures, see: [Split-Brain Recovery Guide](../troubleshooting/split-brain-recovery.md)
