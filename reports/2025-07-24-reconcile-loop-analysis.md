# Neo4j Kubernetes Operator Reconcile Loop Analysis

## Overview

This report provides a comprehensive analysis of the reconciliation loops in the Neo4j Kubernetes Operator, covering both the cluster and standalone controllers. The operator uses the Kubebuilder framework and implements controller-runtime patterns for managing Neo4j Enterprise deployments (v5.26+).

## Controller Architecture

### Main Controllers

1. **Neo4jEnterpriseClusterReconciler** (`internal/controller/neo4jenterprisecluster_controller.go:91`)
   - Manages high-availability Neo4j clusters
   - Supports minimum 2 servers (server-based topology)
   - Handles rolling upgrades and topology placement

2. **Neo4jEnterpriseStandaloneReconciler** (`internal/controller/neo4jenterprisestandalone_controller.go:66`)
   - Manages single-node deployments
   - Fixed single replica (no scaling)
   - Uses clustering infrastructure without `dbms.mode=SINGLE`

3. **Neo4jBackupReconciler** (`internal/controller/neo4jbackup_controller.go:91`)
   - Handles backup operations

4. **Neo4jRestoreReconciler** (`internal/controller/neo4jrestore_controller.go:87`)
   - Manages restore operations

5. **Neo4jDatabaseReconciler** (`internal/controller/neo4jdatabase_controller.go:61`)
   - Manages database lifecycle

6. **Neo4jPluginReconciler** (`internal/controller/plugin_controller.go:57`)
   - Handles plugin management

## Reconciliation Flow

### 1. Entry Point and Resource Fetch

```go
func (r *Neo4jEnterpriseClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch the custom resource
    cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
    if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
        if errors.IsNotFound(err) {
            return ctrl.Result{}, nil  // Resource deleted, nothing to do
        }
        return ctrl.Result{}, err  // Actual error, retry with backoff
    }
```

### 2. Deletion Handling

If `DeletionTimestamp` is set, the controller enters deletion mode:
- Cleans up PVCs based on retention policy
- Removes finalizer
- Returns without further processing

### 3. Validation Phase

The controller performs extensive validation:

```go
// Apply defaults
r.Validator.ApplyDefaults(ctx, cluster)

// Determine if this is create or update
isUpdate := cluster.Generation > 1

// Validate with warnings
if isUpdate {
    result := r.Validator.ValidateUpdateWithWarnings(ctx, currentCluster, cluster)
} else {
    result := r.Validator.ValidateCreateWithWarnings(ctx, cluster)
}

// Emit warnings as events
for _, warning := range result.Warnings {
    r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "TopologyWarning", warning)
}

// Handle validation errors
if len(result.Errors) > 0 {
    _ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Validation failed: %v", err))
    return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
}
```

### 4. Finalizer Management

```go
if !controllerutil.ContainsFinalizer(cluster, ClusterFinalizer) {
    controllerutil.AddFinalizer(cluster, ClusterFinalizer)
    if err := r.Update(ctx, cluster); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{Requeue: true}, nil  // Requeue to continue
}
```

### 5. Upgrade Detection

```go
if r.isUpgradeRequired(ctx, cluster) {
    logger.Info("Image upgrade detected, initiating rolling upgrade")
    return r.handleRollingUpgrade(ctx, cluster)
}
```

### 6. Resource Creation/Update (Main Reconciliation)

The controller creates/updates resources in a specific order:

#### a. TLS Certificate (if enabled)
```go
if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" {
    certificate := resources.BuildCertificateForEnterprise(cluster)
    if err := r.createOrUpdateResource(ctx, certificate, cluster); err != nil {
        _ = r.updateClusterStatus(ctx, cluster, "Failed", "Failed to create Certificate")
        return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
    }
}
```

#### b. External Secrets (if enabled)
- TLS ExternalSecret
- Auth ExternalSecret

#### c. ConfigMap
```go
if err := r.ConfigMapManager.ReconcileConfigMap(ctx, cluster); err != nil {
    _ = r.updateClusterStatus(ctx, cluster, "Failed", "Failed to reconcile ConfigMap")
    return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
}
```

#### d. RBAC Resources
- ServiceAccount for discovery
- Role for endpoints access
- RoleBinding

#### e. Services
```go
services := []*corev1.Service{
    resources.BuildHeadlessServiceForEnterprise(cluster),   // For StatefulSet
    resources.BuildDiscoveryServiceForEnterprise(cluster),  // For K8s discovery
    resources.BuildInternalsServiceForEnterprise(cluster),  // For cluster comms
    resources.BuildClientServiceForEnterprise(cluster),     // For external access
}
```

#### f. Topology Calculation
```go
if r.TopologyScheduler != nil {
    placement, err := r.TopologyScheduler.CalculateTopologyPlacement(ctx, cluster)
    // Apply zone distribution and anti-affinity rules
}
```

#### g. StatefulSets
```go
// Create single server StatefulSet for all cluster members
serverSts := resources.BuildServerStatefulSetForEnterprise(cluster)
if err := r.createOrUpdateResource(ctx, serverSts, cluster); err != nil {
    _ = r.updateClusterStatus(ctx, cluster, "Failed", "Failed to create server StatefulSet")
    return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
}

// Create centralized backup StatefulSet when backups are enabled
if cluster.Spec.Backups != nil {
    backupSts := resources.BuildBackupStatefulSet(cluster)
    if backupSts != nil {
        if err := r.createOrUpdateResource(ctx, backupSts, cluster); err != nil {
            _ = r.updateClusterStatus(ctx, cluster, "Failed", "Failed to create backup StatefulSet")
            return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
        }
    }
}
```

### 7. Additional Features

- Query Performance Monitoring reconciliation
- Plugin management

### 8. Status Update

```go
statusChanged := r.updateClusterStatus(ctx, cluster, "Ready", "Cluster is ready")
if statusChanged {
    r.Recorder.Event(cluster, "Normal", "ClusterReady", "Neo4j Enterprise cluster is ready")
}
return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
```

## Key Variables and Components

### Important Variables

1. **cluster/standalone**: The custom resource being reconciled
2. **ctx**: Context for cancellation and logging
3. **logger**: Contextual logger from controller-runtime
4. **r.RequeueAfter**: Configurable requeue duration (30s default, 1s in test mode)
5. **topologyPlacement**: Zone distribution and anti-affinity configuration

### Key Components

1. **ConfigMapManager**: Handles configuration updates with debouncing
2. **TopologyScheduler**: Manages pod placement across availability zones
3. **Validator**: Performs create/update validation with warnings
4. **Recorder**: Kubernetes event recorder for user visibility

## Resource Creation Pattern

The `createOrUpdateResource` method implements a sophisticated pattern for resource management:

```go
func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateResource(ctx context.Context, obj client.Object, owner client.Object) error {
    // 1. Set owner reference for garbage collection
    controllerutil.SetControllerReference(owner, obj, r.Scheme)

    // 2. Handle StatefulSet immutable fields
    if sts, ok := obj.(*appsv1.StatefulSet); ok {
        desiredSpec := *sts.Spec.DeepCopy()

        _, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
            if sts.ResourceVersion != "" {
                // Update only mutable fields:
                // - Replicas, UpdateStrategy, Template
                // Preserve immutable fields:
                // - Selector, ServiceName, VolumeClaimTemplates
            }
        })
    }
}
```

## Error Handling and Recovery

### Error Categories

1. **Transient Errors**: Return error to trigger exponential backoff
2. **Validation Failures**: Update status, emit event, requeue with delay
3. **Not Found Errors**: Return nil (resource deleted)
4. **Permanent Failures**: Update status to "Failed", emit warning event

### Retry Mechanisms

```go
// Controller-level rate limiting
RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
    // Exponential backoff: 5s initial, 30s max
    workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Second, 30*time.Second),
    // Bucket rate limiter: max 10 reconciliations per minute
    &workqueue.TypedBucketRateLimiter[reconcile.Request]{
        Limiter: rate.NewLimiter(rate.Every(6*time.Second), 10),
    },
)

// Status update retry with conflict resolution
err := retry.RetryOnConflict(retry.DefaultBackoff, updateFunc)
```

## Status Management

The operator maintains comprehensive status information:

```go
type Neo4jEnterpriseClusterStatus struct {
    Phase      string              // Initializing, Ready, Failed, Upgrading
    Message    string              // Human-readable status message
    Conditions []metav1.Condition  // Standard K8s conditions

    // Upgrade tracking
    UpgradeStatus *UpgradeStatus

    // Topology information
    TopologyStatus *TopologyStatus
}
```

Status updates use optimistic concurrency control:
1. Fetch latest resource version
2. Apply status changes
3. Retry on conflict with exponential backoff

## Critical Design Decisions

### 1. V2_ONLY Discovery
- Uses `tcp-discovery` port (5000) exclusively
- Single ClusterIP discovery service
- Requires endpoints RBAC permissions

### 2. Parallel Cluster Formation
- `ParallelPodManagement` for all StatefulSets
- `MIN_PRIMARIES=1` configuration
- All pods start simultaneously
- First pod forms cluster, others join

### 3. Memory Management
- Automatic calculation based on container resources
- 60% heap, 30% page cache for high-memory deployments
- Centralized backup StatefulSet (`{cluster}-backup-0`) with fixed 100m CPU / 256Mi memory limits

### 4. TLS Cluster Formation
- `trust_all=true` for cluster SSL policy
- Comprehensive certificate with all pod DNS names
- Extended timeouts for certificate propagation

### 5. Resource Update Strategy
- Immutable field preservation for StatefulSets
- ConfigMap updates trigger pod restarts
- Debounced configuration changes (2-minute minimum)

## Performance Optimizations

1. **Status Update Optimization**: Skip updates if status unchanged
2. **Parallel Resource Creation**: Server StatefulSet and centralized backup StatefulSet created independently
3. **Event Deduplication**: Only emit events on actual status changes
4. **Resource Caching**: Controller-runtime's shared cache reduces API calls

## Conclusion

The Neo4j Kubernetes Operator implements a robust reconciliation loop that handles complex scenarios including cluster formation, rolling upgrades, topology placement, and error recovery. The design prioritizes reliability through idempotent operations, comprehensive error handling, and clear status reporting. The separation between cluster and standalone controllers allows for optimized logic while sharing common patterns and utilities.
