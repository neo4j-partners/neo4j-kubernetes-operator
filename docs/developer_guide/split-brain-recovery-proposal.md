# Split-Brain Recovery Proposal

## Overview

This proposal outlines how the Neo4j Kubernetes Operator could be enhanced to automatically detect and recover from split-brain scenarios, particularly for TLS-enabled clusters.

## Current State

- The operator does NOT currently detect or fix split-brain scenarios
- No cluster health validation beyond basic pod readiness
- No automatic node rejoining capabilities
- Manual intervention required when clusters split

## Proposed Solution

### 1. Split-Brain Detection

Add cluster health monitoring to the reconciliation loop:

```go
// internal/controller/cluster_health_checker.go
type ClusterHealthChecker struct {
    client neo4j.Client
}

func (c *ClusterHealthChecker) CheckClusterHealth(cluster *v1alpha1.Neo4jEnterpriseCluster) (*ClusterHealth, error) {
    // Query each pod's view of the cluster
    servers := make(map[string][]ServerInfo)

    for _, pod := range pods {
        result, err := c.queryServers(pod)
        if err != nil {
            continue
        }
        servers[pod.Name] = result
    }

    // Detect split-brain
    clusters := c.identifyClusters(servers)
    if len(clusters) > 1 {
        return &ClusterHealth{
            Status: "split-brain",
            Clusters: clusters,
        }, nil
    }

    return &ClusterHealth{Status: "healthy"}, nil
}

func (c *ClusterHealthChecker) queryServers(pod *v1.Pod) ([]ServerInfo, error) {
    // Execute: SHOW SERVERS
    // Parse results
}
```

### 2. Automatic Recovery

Implement recovery strategies when split-brain is detected:

```go
// internal/controller/cluster_recovery.go
type ClusterRecovery struct {
    client kubernetes.Interface
}

func (r *ClusterRecovery) RecoverFromSplitBrain(cluster *v1alpha1.Neo4jEnterpriseCluster, health *ClusterHealth) error {
    // Strategy 1: Rolling restart with delays
    if cluster.Spec.TLS != nil {
        return r.rollingRestartWithDelay(cluster, 30*time.Second)
    }

    // Strategy 2: Targeted node restart
    // Identify nodes in smaller clusters
    minorityNodes := r.identifyMinorityNodes(health)
    for _, node := range minorityNodes {
        if err := r.restartPod(node); err != nil {
            return err
        }
        // Wait for node to rejoin
        time.Sleep(20 * time.Second)
    }

    return nil
}
```

### 3. Integration with Reconciler

Add health checking to the main reconciliation loop:

```go
// internal/controller/neo4jenterprisecluster_controller.go
func (r *Neo4jEnterpriseClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ... existing code ...

    // After cluster is running, check health
    if cluster.Status.Phase == "Running" {
        health, err := r.healthChecker.CheckClusterHealth(&cluster)
        if err != nil {
            return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
        }

        if health.Status == "split-brain" {
            log.Info("Split-brain detected", "clusters", len(health.Clusters))

            // Update status
            cluster.Status.Phase = "Recovering"
            cluster.Status.Message = fmt.Sprintf("Split-brain detected: %d clusters", len(health.Clusters))

            // Attempt recovery
            if err := r.recovery.RecoverFromSplitBrain(&cluster, health); err != nil {
                log.Error(err, "Failed to recover from split-brain")
                return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
            }

            // Requeue to check if recovery succeeded
            return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
        }
    }
}
```

### 4. Configuration Options

Add recovery configuration to the CRD:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
spec:
  recovery:
    enabled: true
    splitBrainRecovery:
      enabled: true
      strategy: "rolling-restart"  # or "targeted-restart"
      maxAttempts: 3
      retryDelay: "30s"
```

## Implementation Steps

1. **Phase 1**: Implement cluster health checking
   - Add Neo4j client to query SHOW SERVERS
   - Detect split-brain conditions
   - Report in cluster status

2. **Phase 2**: Implement basic recovery
   - Rolling restart with configurable delays
   - Status reporting during recovery

3. **Phase 3**: Advanced recovery strategies
   - Targeted node restarts
   - Preserve data consistency
   - Handle edge cases

## Challenges

1. **Neo4j Client Access**: Need to handle authentication and TLS connections
2. **Data Consistency**: Ensure no data loss during recovery
3. **Recovery Loops**: Prevent infinite recovery attempts
4. **Performance Impact**: Minimize disruption to running workloads

## Alternative Approaches

### 1. Prevention-Based Approach
Instead of recovery, prevent split-brain:
- Sequential pod startup for TLS clusters
- Pre-flight TLS validation
- Configurable startup delays

### 2. Manual Recovery Assistance
Provide tools but require manual triggering:
- `kubectl neo4j recover-cluster <name>`
- Detailed split-brain diagnostics
- Step-by-step recovery guide

### 3. Webhook-Based Validation
Use admission webhooks to:
- Validate cluster readiness before marking pods ready
- Ensure proper cluster formation before allowing traffic

## Recommendation

Start with Phase 1 (detection and reporting) to:
- Understand frequency of split-brain issues
- Gather data on recovery patterns
- Build confidence before automation

Then gradually add automated recovery based on real-world usage patterns.

## Example Implementation Timeline

- **Week 1-2**: Implement cluster health checking
- **Week 3-4**: Add status reporting and metrics
- **Week 5-6**: Implement basic rolling restart recovery
- **Week 7-8**: Testing and edge case handling
- **Week 9-10**: Documentation and user guides
