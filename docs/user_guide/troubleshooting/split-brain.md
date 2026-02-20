# Neo4j Split-Brain Detection and Troubleshooting Guide

This guide provides comprehensive information about the Neo4j Kubernetes Operator's split-brain detection and repair system.

## Overview

Split-brain scenarios occur when Neo4j servers that should form a single cluster instead form multiple independent clusters. This can lead to data inconsistency, database creation failures, and operational issues.

## What is Split-Brain?

In a Neo4j cluster context, split-brain occurs when:
- **Expected**: All 3 servers form one unified cluster
- **Split-Brain**: Server 1 forms its own cluster, while servers 2-3 form another cluster

This results in:
- Multiple independent Neo4j clusters
- Inconsistent data across clusters
- Database creation failures due to "insufficient servers"
- Application connection issues

## Detection System

### Automatic Detection

The operator automatically detects split-brain scenarios during cluster formation verification:

```go
// Runs during every cluster reconciliation
func (r *Neo4jEnterpriseClusterReconciler) verifyNeo4jClusterFormation() {
    splitBrainDetector := NewSplitBrainDetector(r.Client)
    analysis, err := splitBrainDetector.DetectSplitBrain(ctx, cluster)
    // Automatic repair if needed
}
```

### Detection Logic

1. **Multi-Pod Analysis**: Connects to each Neo4j server pod individually
2. **Cluster View Comparison**: Compares what servers each pod can see via `SHOW SERVERS`
3. **Group Analysis**: Groups pods by similar cluster views
4. **Split-Brain Detection**: Identifies multiple cluster groups or missing servers

## Monitoring and Observability

### Kubernetes Events

The operator generates events for monitoring:

```bash
# Check for split-brain events
kubectl get events --field-selector reason=SplitBrainDetected -A
kubectl get events --field-selector reason=SplitBrainRepaired -A
```

Example events:
```
Warning  SplitBrainDetected  Split-brain detected: 2 cluster groups found, 1 orphaned pods
Normal   SplitBrainRepaired  Split-brain automatically repaired by restarting orphaned pods
```

### Log Monitoring

Monitor operator logs for split-brain activity:

```bash
# Monitor split-brain detection
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -E "(split|brain|SplitBrain)"

# Example log messages
# "Starting split-brain detection ... expectedServers: 3"
# "Split-brain analysis results ... isSplitBrain: true, orphanedPods: 1"
# "Split-brain automatically repaired by restarting orphaned pods"
```

### Cluster Health Verification

Verify cluster health manually:

```bash
# Check cluster formation from different pods
kubectl exec <cluster>-server-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
kubectl exec <cluster>-server-1 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
kubectl exec <cluster>-server-2 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"

# All pods should show the same server list
# Example healthy output (all pods see all 3 servers):
# "server-0", "<cluster>-server-0.<cluster>-headless:7687", "Enabled", "Available"
# "server-1", "<cluster>-server-1.<cluster>-headless:7687", "Enabled", "Available"
# "server-2", "<cluster>-server-2.<cluster>-headless:7687", "Enabled", "Available"
```

## Automatic Repair Actions

### Repair Strategies

The system chooses repair actions based on the detected scenario:

1. **`RepairActionRestartPods`** (Preferred)
   - Restarts specific orphaned pods
   - Used when clear orphaned pods are identified
   - Minimal disruption to the main cluster

2. **`RepairActionRestartAll`** (Nuclear Option)
   - Restarts all cluster pods
   - Used when connection failures prevent analysis
   - Complete cluster reformation

3. **`RepairActionWaitForming`** (Monitor)
   - Waits for natural cluster formation
   - Used during normal startup scenarios
   - No immediate action required

4. **`RepairActionInvestigate`** (Manual)
   - Requires manual intervention
   - Used when too many connection failures occur
   - Human analysis needed

### Automatic Repair Process

When split-brain is detected with `RepairActionRestartPods`:

1. **Detection**: Identifies orphaned pods (e.g., `server-1`)
2. **Deletion**: Deletes the orphaned pod
3. **Recreation**: StatefulSet recreates the pod
4. **Rejoin**: New pod joins the main cluster during startup
5. **Verification**: System verifies successful cluster formation

## Troubleshooting Common Issues

### Symptoms of Split-Brain

**Database Creation Failures**:
```
Neo4jError: Could not create database 'mydb'.
Desired number of allocations is '3', but only '2' possible servers found
```

**Pod Status**: All pods running but cluster shows "Forming" or "Initializing"

**Inconsistent Server Views**: Different pods report different server lists

### Diagnostic Commands

```bash
# 1. Check cluster status
kubectl get neo4jenterprisecluster <cluster-name> -n <namespace>

# 2. Check pod status
kubectl get pods -l neo4j.com/cluster=<cluster-name> -n <namespace>

# 3. Check recent events
kubectl get events -n <namespace> --sort-by=.metadata.creationTimestamp

# 4. Check operator logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager --tail=50

# 5. Test connectivity between pods
kubectl exec <pod-1> -- nc -z <pod-2>.<cluster>-headless 7687
```

### Manual Verification Steps

1. **Verify Pod-to-Pod Connectivity**:
```bash
kubectl exec <cluster>-server-0 -- nc -z <cluster>-server-1.<cluster>-headless 7687
kubectl exec <cluster>-server-0 -- nc -z <cluster>-server-2.<cluster>-headless 7687
```

2. **Check Neo4j Cluster Formation**:
```bash
kubectl exec <cluster>-server-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS YIELD name, address, state, health"
```

3. **Verify Service Discovery**:
```bash
kubectl get endpoints <cluster>-discovery -n <namespace> -o yaml
# Should show all server pods as addresses
```

### Manual Repair (If Automatic Fails)

If automatic repair doesn't work:

1. **Force Pod Restart**:
```bash
# Delete specific orphaned pod
kubectl delete pod <cluster>-server-X -n <namespace>

# Or restart all pods (nuclear option)
kubectl delete pods -l neo4j.com/cluster=<cluster-name> -n <namespace>
```

2. **Check Resource Constraints**:
```bash
# Verify resource availability
kubectl describe nodes
kubectl get pvc -n <namespace>
```

3. **Network Troubleshooting**:
```bash
# Check DNS resolution
kubectl exec <pod> -- nslookup <cluster>-discovery.<namespace>.svc.cluster.local

# Check service endpoints
kubectl get endpoints <cluster>-discovery -n <namespace>
```

## Prevention Best Practices

### Cluster Configuration

1. **Resource Allocation**:
```yaml
resources:
  requests:
    memory: "1Gi"  # Minimum for Neo4j Enterprise
    cpu: "200m"
  limits:
    memory: "2Gi"
```

2. **Network Policies**: Ensure Neo4j discovery traffic is allowed
3. **Storage Classes**: Use fast, reliable storage classes
4. **Node Affinity**: Consider spreading pods across nodes

### Monitoring Setup

1. **Event Monitoring**:
```bash
# Set up monitoring for split-brain events
kubectl get events --watch --field-selector reason=SplitBrainDetected
```

2. **Log Aggregation**: Collect operator logs for analysis
3. **Alerting**: Set up alerts for split-brain detection events

### Testing Procedures

Use the integration tests to verify split-brain handling:

```bash
# Run split-brain specific tests
ginkgo run --focus "Split-Brain Detection" ./test/integration/

# Test scenarios:
# - Normal cluster formation without split-brain
# - Split-brain detection and automatic repair
# - Pod failure recovery scenarios
```

## Advanced Troubleshooting

### Debug Mode

Enable debug logging for detailed analysis:

```bash
# Deploy operator with debug logging in development cluster
make operator-setup
kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--mode=dev","--zap-log-level=debug"]}]}}}}'

# Or patch deployment for debug logging
kubectl patch deployment neo4j-operator-controller-manager -n neo4j-operator-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--zap-log-level=debug"]}]}}}}'
```

### Custom Repair Actions

For complex scenarios, you may need custom repair:

1. **Analyze Cluster State**: Use `SHOW SERVERS` and `SHOW DATABASES`
2. **Identify Root Cause**: Network, resources, configuration
3. **Targeted Repair**: Restart specific pods or modify configuration
4. **Verification**: Ensure all servers see each other

### Performance Considerations

Split-brain detection adds minimal overhead:
- Runs only during cluster reconciliation
- Uses lightweight connections with timeouts
- Fallback to legacy checks if detection fails

## Getting Help

If split-brain issues persist:

1. **Collect Diagnostics**:
   - Operator logs with debug level
   - Cluster events and status
   - Pod logs and status
   - Network connectivity tests

2. **Check Documentation**:
   - Review CLAUDE.md for latest guidance
   - Check integration test scenarios

3. **Community Support**:
   - Report issues with diagnostic information
   - Include split-brain detection logs
   - Describe cluster topology and symptoms

## Summary

The split-brain detection and repair system provides:

✅ **Automatic Detection**: Proactive identification of split-brain scenarios
✅ **Smart Repair**: Targeted pod restarts to resolve issues
✅ **Production Ready**: Comprehensive logging and event generation
✅ **Minimal Impact**: Non-disruptive operation during normal cluster formation
✅ **Comprehensive Testing**: Integration tests cover various scenarios

This system significantly improves Neo4j cluster reliability and reduces operational overhead by automatically handling one of the most common cluster formation issues.
