# Your first Neo4j

Create one Standalone instance, understand what the operator built, and connect to it.

This page assumes the operator is already running. If it is not, follow
[kind (local)](local-kind.md), [Azure AKS](azure-aks.md), [GKE](gcp-gke.md) or [EKS](aws-eks.md)
first, or install it on an existing cluster from
[Operator installation](../02-operator-installation/readme.md).

## 1. Create the resource

Save this as `dev.yaml`. It is the smallest manifest the operator accepts: an Enterprise version,
an explicit licence acceptance, a topology mode, a data volume, and a password strategy.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
  namespace: default
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 10Gi
  auth:
    generatePassword: true
```

```bash
kubectl apply -f dev.yaml
```

The same manifest is available as [`examples/standalone/01-minimal.yaml`](../../../examples/standalone/01-minimal.yaml).

If your cluster has no default StorageClass, add
`spec.storage.volumes.data.dynamic.storageClassName` — without it the PersistentVolumeClaim never
binds and the instance stays Pending.

## 2. Watch it come up

```bash
kubectl get neo4j dev -n default -w
kubectl get pods -n default -l app.kubernetes.io/instance=dev
```

First boot takes a couple of minutes, most of it pulling the Neo4j Enterprise image.

## 3. What the operator created

| Resource | Name | Purpose |
|----------|------|---------|
| StatefulSet | `dev-server` | The Neo4j pod, one replica in Standalone |
| Headless Service | `dev-server` | Stable per-pod DNS |
| Client Service | `dev` | Where your applications connect |
| ConfigMap | `dev-config` | Rendered `neo4j.conf` settings |
| Secret | `dev-auth` | Generated initial password |
| PersistentVolumeClaim | `data-dev-server-0` | The data directory |

Every object carries `app.kubernetes.io/instance=dev` and is owned by the `Neo4j` resource, so
deleting the resource garbage-collects them — except PersistentVolumeClaims, which are preserved
by default. See [Deleting a Neo4j resource](../03-neo4j/09-operations.md#deleting-a-neo4j-resource).

## 4. Read the status

The operator reports progress through conditions. Automation should gate on those rather than on
`status.phase`, because a phase is a summary while a condition names a cause.

```bash
kubectl get neo4j dev -n default -o wide

kubectl get neo4j dev -n default \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

A healthy Standalone instance reports `status.phase: Running`, `Ready=True`, `Installed=True` and
`StorageReady=True`, and fills in `status.credentials.secretName` and `status.endpoints`.

If a condition is `False`, its `reason` is a stable identifier you can look up in the
[error catalog](../05-reference/errors.md), and `kubectl describe neo4j dev -n default` shows the
matching Events.

## 5. Connect

Read the generated password. The Secret holds a single `NEO4J_AUTH` key in `user/password` form.

```bash
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo
```

Forward the Bolt port and connect with any driver or with `cypher-shell`:

```bash
kubectl port-forward -n default svc/dev 7687:7687
# then, in another shell
cypher-shell -a neo4j://localhost:7687 -u neo4j -p '<password>' "RETURN 1"
```

For Neo4j Browser, forward HTTP as well and open `http://localhost:7474`:

```bash
kubectl port-forward -n default svc/dev 7474:7474
```

Inside the cluster, applications use the client Service directly —
`neo4j://dev.default.svc.cluster.local:7687`. The operator publishes ready-to-paste URIs under
`status.endpoints`:

```bash
kubectl get neo4j dev -n default -o jsonpath='{.status.endpoints.neo4j}{"\n"}'
```

## 6. Where to go next

| Goal | Page |
|------|------|
| Change disk size, StorageClass, or use an existing PVC | [Storage](../03-neo4j/03-storage.md) |
| Expose Neo4j outside the cluster | [Connectivity](../03-neo4j/04-connectivity.md) |
| Use your own password Secret, or enable TLS | [Security](../03-neo4j/05-security.md) |
| Set `neo4j.conf` keys or JVM flags | [Configuration](../03-neo4j/06-configuration.md) |
| Install APOC or Graph Data Science | [Plugins](../03-neo4j/07-plugins.md) |
| Move to a multi-member cluster | [Clustering](../03-neo4j/02-clustering.md) |
| Understand what is and is not implemented | [What works today](feature-status.md) |

## Clean up

```bash
kubectl delete neo4j dev -n default
```

The PersistentVolumeClaim survives on purpose. Remove it explicitly when you no longer need the
data:

```bash
kubectl delete pvc data-dev-server-0 -n default
```
