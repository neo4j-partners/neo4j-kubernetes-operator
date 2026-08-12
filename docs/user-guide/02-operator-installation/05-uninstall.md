# Uninstall the operator

Removing the operator does not remove your databases. The controller, the `Neo4j` resources and the
data volumes are three separate lifecycles, and you decide how far to go.

| Step | Removes | Keeps |
|------|---------|-------|
| Remove the controller | Operator Deployment, ServiceAccount, RBAC | CRD, `Neo4j` resources, running pods, data |
| Delete a `Neo4j` resource | Its StatefulSet, Services, ConfigMap, generated Secret | PersistentVolumeClaims, unless you opted into deletion |
| Remove the CRD | Every `Neo4j` resource cluster-wide, and everything they own | Nothing |

## Remove the controller

```bash
make undeploy
```

With Helm:

```bash
helm uninstall neo4j-operator --namespace neo4j-operator-system
```

Either way the Deployment and its RBAC go away while Neo4j keeps serving traffic: pods, Services
and volumes are owned by the `Neo4j` resource, not by the controller. What you lose is
reconciliation — spec changes are not applied, failed pods are not replaced, and status stops
being updated. Re-installing the operator picks the resources up again.

## Delete a Neo4j resource

```bash
kubectl delete neo4j dev -n default
```

Owner references garbage-collect the StatefulSet, the Services, the ConfigMap and the generated
auth Secret. PersistentVolumeClaims are the deliberate exception.

## PersistentVolumeClaim retention

Deleting a resource **preserves its PersistentVolumeClaims by default**, so a mistaken
`kubectl delete` never destroys a database. The default is equivalent to:

```yaml
spec:
  storage:
    volumeClaimRetention:
      whenDeleted: Retain
      whenScaled: Retain
```

Reclaim the volumes explicitly when you are sure:

```bash
kubectl delete pvc -n default \
  -l app.kubernetes.io/instance=dev,app.kubernetes.io/managed-by=neo4j-operator,neo4j.com/component=storage
```

For throwaway environments you can opt into deletion instead:

```yaml
spec:
  storage:
    volumeClaimRetention:
      whenDeleted: Delete   # drop PVCs when the Neo4j resource is deleted
      whenScaled: Delete    # drop PVCs of members removed by scale-in
```

Example: [`examples/standalone/18-pvc-delete-on-uninstall.yaml`](../../../examples/standalone/18-pvc-delete-on-uninstall.yaml).

Two rules make this predictable, and both can surprise you:

**The choice is pinned at creation.** The operator records the effective `whenDeleted` in
`status.volumeClaimRetentionWhenDeleted` on first reconcile and keeps honouring that. Patching an
existing resource to `Delete` does not arm wipe-on-delete, precisely so that a late edit cannot
turn a routine delete into data loss. If you need the ephemeral behaviour, delete the resource and
recreate it with the field set.

**Only volumes the operator provisioned are ever deleted.** Deletion targets `Dynamic` PVCs
carrying the instance labels above. A PVC you referenced through
`spec.storage.volumes.*.existing.claimName` is never touched, and neither is a claim that merely
shares an instance label without being managed by this operator.

`whenScaled` is separate: with the default `Retain`, PVCs of members dropped by scale-in stay
behind so the data is recoverable, and you clean them up yourself. See
[Clustering](../03-neo4j/02-clustering.md#scaling-members).

## Remove the CRD

Only once no `Neo4j` resource remains anywhere in the cluster:

```bash
kubectl get neo4j -A
kubectl delete -f config/crd/bases/neo4j.com_neo4js.yaml
```

Deleting a CRD deletes every custom resource of that kind cluster-wide, which cascades to the
StatefulSets, Services and Secrets they own. Check the first command's output carefully — this is
the one destructive step on the page.

## Remove the namespace

```bash
kubectl delete namespace neo4j-operator-system --ignore-not-found
```

## Verify

```bash
kubectl get all -n neo4j-operator-system
kubectl get crd neo4js.neo4j.com
kubectl get pvc -A -l app.kubernetes.io/managed-by=neo4j-operator
```

The last command is the one to trust when you expected data to be gone: a surviving claim means
retention did its job, not that the uninstall failed.
