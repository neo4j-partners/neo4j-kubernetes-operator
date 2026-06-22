# Storage Expansion

This guide explains how to expand the persistent storage of a running `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` without downtime.

## Overview

As datasets grow, you may need to increase the storage allocated to your Neo4j deployment. The operator handles this automatically when you update `spec.storage.size` — no manual PVC patching or cluster recreation required. This works for both cluster and standalone deployments.

**What happens under the hood:**

1. You increase `spec.storage.size` in the cluster spec.
2. The operator detects that existing PVCs are smaller than the desired size.
3. It validates that the StorageClass supports volume expansion.
4. It patches each PVC with the new size.
5. It orphan-deletes the StatefulSet (pods keep running — zero downtime).
6. On the next reconcile, the StatefulSet is recreated with updated VolumeClaimTemplates.
7. The cluster transitions through `Expanding` → `Ready`.

## Prerequisites

Your StorageClass must have `allowVolumeExpansion: true`. Most cloud provider default StorageClasses support this. Verify with:

```bash
kubectl get storageclass <your-storage-class> -o jsonpath='{.allowVolumeExpansion}'
```

If it returns `false` or empty, patch it:

```bash
kubectl patch storageclass <your-storage-class> -p '{"allowVolumeExpansion": true}'
```

> **Note:** Not all storage provisioners support online volume expansion. Check your provider's documentation. Most CSI drivers (AWS EBS, GCP PD, Azure Disk) do support it.

## Expanding Data Storage

```bash
# Check current storage size
kubectl get neo4jenterprisecluster my-cluster -o jsonpath='{.spec.storage.size}'
# 100Gi

# Expand to 200Gi
kubectl patch neo4jenterprisecluster my-cluster \
  --type=merge \
  -p '{"spec":{"storage":{"size":"200Gi"}}}'

# Watch the expansion progress
kubectl get neo4jenterprisecluster my-cluster -w
# NAME         PHASE       ...
# my-cluster   Ready       ...  <- before
# my-cluster   Expanding   ...  <- patching PVCs + recreating StatefulSet
# my-cluster   Ready       ...  <- done
```

## Expanding Standalone Storage

The same approach works for `Neo4jEnterpriseStandalone`:

```bash
kubectl patch neo4jenterprisestandalone my-standalone \
  --type=merge \
  -p '{"spec":{"storage":{"size":"200Gi"}}}'
```

The operator expands the single PVC and recreates the StatefulSet with zero downtime.

## Monitoring Expansion

### Events

```bash
# Watch storage expansion events
kubectl get events --field-selector reason=StorageExpansionStarted
kubectl get events --field-selector reason=StorageExpansionCompleted
kubectl get events --field-selector reason=StorageExpansionFailed
```

### Cluster Status

The cluster phase changes during expansion:

| Phase | Meaning |
|---|---|
| `Ready` | Normal operation |
| `Expanding` | PVCs being patched and StatefulSet being recreated |
| `Ready` | Expansion complete |
| `Failed` | Expansion failed (check events for details) |

### PVC Status

You can verify the expansion at the PVC level:

```bash
# Check PVC sizes (spec.resources shows the requested size)
kubectl get pvc -l neo4j.com/cluster=my-cluster -o custom-columns=NAME:.metadata.name,REQUESTED:.spec.resources.requests.storage,CAPACITY:.status.capacity.storage

# Example output during expansion:
# NAME                          REQUESTED   CAPACITY
# data-my-cluster-server-0      200Gi       100Gi    <- expanding (CSI driver working)
# data-my-cluster-server-1      200Gi       200Gi    <- done
```

> **Note:** The `CAPACITY` column reflects the actual filesystem size, which is updated asynchronously by the CSI driver. The operator does not wait for capacity to catch up — it only patches the request. Some providers (especially Azure) may take several minutes.

## Limitations

### Storage Shrink Is Not Supported

Reducing `spec.storage.size` below the current PVC size is rejected by the operator. Kubernetes does not support PVC shrink. If you attempt it, you'll see:

```
PHASE: Failed
EVENT: StorageExpansionFailed - Storage shrink rejected: spec.storage.size (50Gi)
       is smaller than existing data PVCs; PVC shrink is not supported by Kubernetes
```

To recover, set `spec.storage.size` back to the current size or larger.

### StorageClass Changes Are Not Supported

Changing `spec.storage.className` after creation requires manual migration (different operation, out of scope for this feature).

### Filesystem Expansion Timing

The operator patches PVC `.spec.resources.requests` but does not wait for `.status.capacity` to reflect the new size. The actual filesystem resize is handled asynchronously by the CSI driver and kubelet. For most providers this takes seconds to a few minutes.

## Troubleshooting

### "StorageClass does not allow volume expansion"

Your StorageClass has `allowVolumeExpansion: false` or unset. Patch it:

```bash
kubectl patch storageclass <name> -p '{"allowVolumeExpansion": true}'
```

Then retry the expansion (the operator will pick it up on the next reconcile).

### Expansion Stuck in "Expanding" Phase

1. Check events: `kubectl get events --field-selector involvedObject.name=my-cluster`
2. Check PVC conditions: `kubectl describe pvc data-my-cluster-server-0`
3. Look for `FileSystemResizePending` condition — this means the CSI driver is waiting for a pod restart to complete the resize (some drivers require this).

### PVCs Not Found

If the operator can't find PVCs to expand, check that PVCs have the expected labels:

```bash
# Server PVCs
kubectl get pvc -l neo4j.com/cluster=my-cluster -l neo4j.com/role=server

# Standalone PVCs
kubectl get pvc -l neo4j.com/cluster=my-standalone -l neo4j.com/role=data
```

For clusters created before labeling was added, the operator falls back to name-based discovery (`data-{cluster-name}-server-{ordinal}`).

## Example

See [`examples/clusters/storage-expansion.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/storage-expansion.yaml) for a complete example.
