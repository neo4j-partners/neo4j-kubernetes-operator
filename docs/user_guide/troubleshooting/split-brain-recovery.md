# Split-Brain Recovery Guide

This guide helps you identify and recover from split-brain scenarios where Neo4j cluster nodes form multiple independent clusters instead of joining a single unified cluster.

## Table of Contents
- [Identifying Split-Brain](#identifying-split-brain)
- [Common Causes](#common-causes)
- [Recovery Methods](#recovery-methods)
  - [Method 1: Targeted Pod Restart](#method-1-targeted-pod-restart)
  - [Method 2: Rolling Restart](#method-2-rolling-restart)
  - [Method 3: Scale Down and Up](#method-3-scale-down-and-up)
  - [Method 4: Force Rejoin](#method-4-force-rejoin)
- [Prevention Strategies](#prevention-strategies)
- [TLS-Specific Considerations](#tls-specific-considerations)

## Identifying Split-Brain

### Step 1: Check Cluster Status

First, verify all pods are running:

```bash
kubectl get pods -l neo4j.com/cluster=<cluster-name>
```

Expected output:
```
NAME                           READY   STATUS    RESTARTS   AGE
my-cluster-primary-0           1/1     Running   0          10m
my-cluster-primary-1           1/1     Running   0          10m
my-cluster-primary-2           1/1     Running   0          10m
my-cluster-secondary-0         1/1     Running   0          10m
my-cluster-secondary-1         1/1     Running   0          10m
```

### Step 2: Check Cluster Formation

Query each primary node to see its view of the cluster:

```bash
# Check primary-0's view
kubectl exec <cluster-name>-primary-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"

# Check primary-1's view
kubectl exec <cluster-name>-primary-1 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"

# Check primary-2's view
kubectl exec <cluster-name>-primary-2 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
```

**Split-brain indicators:**
- Different nodes report different cluster members
- Total unique servers across all queries exceeds expected count
- Some nodes only see themselves

### Step 3: Identify Split Clusters

Example of split-brain output:

```bash
# Primary-0 sees 3 nodes
name, address, state, health, hosting
"abc123...", "primary-0...:7687", "Enabled", "Available", ["system"]
"def456...", "primary-2...:7687", "Enabled", "Available", ["system"]
"ghi789...", "secondary-0...:7687", "Enabled", "Available", ["neo4j"]

# Primary-1 sees 2 different nodes
name, address, state, health, hosting
"jkl012...", "primary-1...:7687", "Enabled", "Available", ["system"]
"mno345...", "secondary-1...:7687", "Enabled", "Available", ["neo4j"]
```

This shows two separate clusters: (primary-0, primary-2, secondary-0) and (primary-1, secondary-1).

## Common Causes

1. **TLS Certificate Issues**: Nodes can't establish trusted connections during formation
2. **Network Partition**: Temporary network issues during startup
3. **Timing Issues**: Race conditions during parallel pod startup
4. **DNS Resolution**: Delays in service endpoint propagation
5. **Resource Constraints**: Insufficient CPU/memory causing startup delays

## Recovery Methods

### Method 1: Targeted Pod Restart

Best for: Small split clusters (1-2 nodes separated)

1. **Identify minority cluster nodes** (nodes in the smaller cluster):
   ```bash
   # From the example above, primary-1 and secondary-1 are in the minority cluster
   ```

2. **Delete minority pods one at a time**:
   ```bash
   # Delete and wait for each pod to rejoin
   kubectl delete pod <cluster-name>-primary-1

   # Wait for pod to be running again
   kubectl wait --for=condition=ready pod/<cluster-name>-primary-1 --timeout=300s

   # Verify it joined the main cluster
   kubectl exec <cluster-name>-primary-1 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"

   # If successful, continue with other minority nodes
   kubectl delete pod <cluster-name>-secondary-1
   ```

3. **Verify unified cluster**:
   ```bash
   kubectl exec <cluster-name>-primary-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
   # Should show all 5 nodes
   ```

### Method 2: Rolling Restart

Best for: Multiple split clusters or unknown split patterns

1. **Restart secondary StatefulSet first**:
   ```bash
   kubectl rollout restart statefulset <cluster-name>-secondary

   # Monitor progress
   kubectl rollout status statefulset <cluster-name>-secondary
   ```

2. **Wait for secondaries to stabilize**:
   ```bash
   kubectl wait --for=condition=ready pod -l neo4j.com/cluster=<cluster-name>,neo4j.com/role=secondary --timeout=300s
   ```

3. **Restart primary StatefulSet**:
   ```bash
   kubectl rollout restart statefulset <cluster-name>-primary

   # Monitor progress
   kubectl rollout status statefulset <cluster-name>-primary
   ```

4. **Verify cluster formation**:
   ```bash
   # Check from multiple nodes to ensure consistency
   for i in 0 1 2; do
     echo "Checking primary-$i:"
     kubectl exec <cluster-name>-primary-$i -- cypher-shell -u neo4j -p <password> "SHOW SERVERS" | wc -l
   done
   # Should show 6 lines (header + 5 nodes) for each
   ```

### Method 3: Scale Down and Up

Best for: Severe split-brain with data consistency concerns

**⚠️ WARNING**: This method causes temporary downtime. Ensure you have backups.

1. **Scale down secondaries**:
   ```bash
   kubectl scale statefulset <cluster-name>-secondary --replicas=0

   # Wait for termination
   kubectl wait --for=delete pod -l neo4j.com/cluster=<cluster-name>,neo4j.com/role=secondary --timeout=300s
   ```

2. **Scale down primaries to 1**:
   ```bash
   kubectl scale statefulset <cluster-name>-primary --replicas=1

   # Wait for termination
   kubectl wait --for=delete pod <cluster-name>-primary-2 <cluster-name>-primary-1 --timeout=300s
   ```

3. **Verify single node cluster**:
   ```bash
   kubectl exec <cluster-name>-primary-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
   # Should show only primary-0
   ```

4. **Scale back up gradually**:
   ```bash
   # Add primaries first
   kubectl scale statefulset <cluster-name>-primary --replicas=3
   kubectl wait --for=condition=ready pod <cluster-name>-primary-1 <cluster-name>-primary-2 --timeout=300s

   # Then add secondaries
   kubectl scale statefulset <cluster-name>-secondary --replicas=2
   kubectl wait --for=condition=ready pod -l neo4j.com/cluster=<cluster-name>,neo4j.com/role=secondary --timeout=300s
   ```

### Method 4: Force Rejoin

Best for: Experienced users comfortable with Neo4j internals

1. **Connect to a minority node**:
   ```bash
   kubectl exec -it <cluster-name>-primary-1 -- bash
   ```

2. **Stop Neo4j**:
   ```bash
   neo4j stop
   ```

3. **Clear cluster state** (⚠️ Use with caution):
   ```bash
   # Remove cluster state
   rm -rf /var/lib/neo4j/data/cluster-state
   ```

4. **Restart Neo4j**:
   ```bash
   neo4j start
   ```

5. **Exit and verify**:
   ```bash
   exit
   kubectl exec <cluster-name>-primary-1 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
   ```

## Prevention Strategies

### 1. Increase Cluster Formation Timeouts

Add to your cluster spec:

```yaml
spec:
  config:
    # Increase discovery timeouts for cluster formation
    dbms.cluster.discovery.v2.initial_timeout: "10s"
    dbms.cluster.discovery.v2.retry_timeout: "20s"

    # Note: The operator already sets these optimal values:
    # dbms.cluster.raft.membership.join_timeout: "10m"  (DO NOT reduce this!)
    # dbms.cluster.raft.binding_timeout: "1d"
```

### 2. Resource Allocation

Ensure adequate resources:

```yaml
spec:
  resources:
    requests:
      cpu: "1"
      memory: "2Gi"
    limits:
      cpu: "4"
      memory: "8Gi"
```

### 3. Pod Disruption Budgets

Create PDBs to prevent accidental split-brain:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: <cluster-name>-primary-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      neo4j.com/cluster: <cluster-name>
      neo4j.com/role: primary
```

## TLS-Specific Considerations

TLS-enabled clusters are more prone to split-brain due to certificate validation timing. Additional steps for TLS clusters:

### 1. Verify Certificate Status

```bash
# Check certificate is ready
kubectl get certificate <cluster-name>-tls -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'

# Check certificate is mounted
kubectl exec <cluster-name>-primary-0 -- ls -la /ssl/
```

### 2. Check TLS Connectivity

```bash
# Test TLS connection between nodes
kubectl exec <cluster-name>-primary-0 -- openssl s_client -connect <cluster-name>-primary-1.<cluster-name>-headless:5000 -servername <cluster-name>-primary-1 < /dev/null
```

### 3. TLS-Specific Recovery

For TLS clusters, add delays between pod restarts:

```bash
# Delete pods with 30s delay between each
for pod in primary-1 secondary-1; do
  kubectl delete pod <cluster-name>-$pod
  echo "Waiting 30s for TLS initialization..."
  sleep 30
  kubectl wait --for=condition=ready pod/<cluster-name>-$pod --timeout=300s
done
```

## Monitoring and Alerting

### Create a Monitoring Script

```bash
#!/bin/bash
# save as check-cluster-health.sh

CLUSTER_NAME=$1
PASSWORD=$2
EXPECTED_NODES=$3

if [ -z "$CLUSTER_NAME" ] || [ -z "$PASSWORD" ] || [ -z "$EXPECTED_NODES" ]; then
  echo "Usage: $0 <cluster-name> <password> <expected-nodes>"
  exit 1
fi

# Check each primary
for i in 0 1 2; do
  NODE_COUNT=$(kubectl exec ${CLUSTER_NAME}-primary-$i -- cypher-shell -u neo4j -p $PASSWORD "SHOW SERVERS" 2>/dev/null | grep -c "7687")
  echo "Primary-$i sees $NODE_COUNT nodes"

  if [ "$NODE_COUNT" -ne "$EXPECTED_NODES" ]; then
    echo "WARNING: Split-brain detected on primary-$i!"
  fi
done
```

### Run Regular Health Checks

```bash
# Make executable
chmod +x check-cluster-health.sh

# Run check (example for 5-node cluster)
./check-cluster-health.sh my-cluster mypassword 5

# Add to cron for regular monitoring
*/5 * * * * /path/to/check-cluster-health.sh my-cluster mypassword 5 >> /var/log/neo4j-health.log 2>&1
```

## When to Contact Support

Seek additional help if:
- Split-brain persists after multiple recovery attempts
- Data inconsistencies are detected
- Nodes crash during recovery procedures
- Performance degradation after recovery

## Quick Reference Card

| Scenario | Recommended Method | Downtime | Risk Level |
|----------|-------------------|----------|------------|
| 1-2 nodes split | Targeted Restart | Minimal | Low |
| Multiple splits | Rolling Restart | Minimal | Medium |
| Persistent split | Scale Down/Up | Yes | Medium |
| Data corruption | Support + Backup Restore | Yes | High |

## Summary

Split-brain recovery requires careful identification and systematic resolution. Always:
1. Identify the split clusters before taking action
2. Use the least disruptive method first
3. Verify successful recovery from multiple nodes
4. Implement prevention strategies to avoid recurrence
5. Monitor cluster health regularly

For TLS-enabled clusters, expect higher split-brain likelihood and use appropriate delays during recovery procedures.
