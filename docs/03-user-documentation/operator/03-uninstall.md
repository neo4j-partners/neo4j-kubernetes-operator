# Uninstall the operator

Remove the operator controller and RBAC. **Neo4j workloads and PVCs are not deleted automatically** when uninstalling the operator (V1 policy: preserve data).

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

## Remove CRD (destructive)

Only when no `Neo4j` resources remain in the cluster:

```bash
kubectl delete -f config/crd/bases/neo4j.com_neo4js.yaml
```

Deleting the CRD removes all `Neo4j` CRs cluster-wide.

## PVC retention

V1 uninstall **preserves PersistentVolumeClaims** ([V1 scope lock](../../00-discovery/13-v1-scope-lock.md)). Delete PVCs explicitly if you want to reclaim storage:

```bash
kubectl delete pvc -n default -l app.kubernetes.io/instance=dev
```

## Remove namespace

```bash
kubectl delete namespace neo4j-operator-system --ignore-not-found
```
