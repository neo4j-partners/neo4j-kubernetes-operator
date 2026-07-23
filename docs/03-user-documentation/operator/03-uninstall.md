# Uninstall the operator

Remove the operator controller and RBAC. **Neo4j workloads and PVCs are not deleted automatically** when uninstalling the operator (preserve-data by default).

## Remove controller and RBAC

```bash
make undeploy
```

Or manually:

```bash
kubectl delete -k config/manager --ignore-not-found
kubectl delete -k config/rbac --ignore-not-found
```

This removes the Deployment and ServiceAccount in `neo4j-operator-system`. It does **not** remove:

- The CRD (`neo4j.com_neo4js`)
- Existing `Neo4j` custom resources
- StatefulSets, Services, Secrets, or PVCs created by the operator

## Delete Neo4j workloads

Delete each `Neo4j` CR; owned objects are garbage-collected via owner references:

```bash
kubectl delete neo4j dev -n default
```

## PVC retention

Default uninstall **preserves PersistentVolumeClaims** (`storage.volumeClaimRetention.whenDeleted` defaults to `Retain`, OP-2-005-UNINST-01).

For ephemeral labs, opt into wipe:

```yaml
spec:
  storage:
    volumeClaimRetention:
      whenDeleted: Delete   # also set whenScaled: Delete to drop PVCs on scale-down
```

That sets StatefulSet `persistentVolumeClaimRetentionPolicy` and, on CR delete, removes Dynamic PVCs labeled for the instance. **`Existing.claimName` PVCs are never deleted.**

Example: [`examples/standalone/18-pvc-delete-on-uninstall.yaml`](../../examples/standalone/18-pvc-delete-on-uninstall.yaml).

Manual reclaim when using Retain:

```bash
kubectl delete pvc -n default -l app.kubernetes.io/instance=dev,app.kubernetes.io/component=storage
```

## Remove CRD (destructive)

Only when no `Neo4j` resources remain in the cluster:

```bash
kubectl delete -f config/crd/bases/neo4j.com_neo4js.yaml
```

Deleting the CRD removes all `Neo4j` CRs cluster-wide.

## Remove namespace

```bash
kubectl delete namespace neo4j-operator-system --ignore-not-found
```
