# Troubleshooting — operator install

## CRD apply fails: metadata.annotations too long

**Symptom:** `CustomResourceDefinition "neo4js.neo4j.com" is invalid: metadata.annotations: Too long: may not be more than 262144 bytes`

**Cause:** Plain `kubectl apply -f config/crd/bases/neo4j.com_neo4js.yaml` (or `kubectl apply -k config/crd`) uses client-side apply. Kubernetes stores the entire manifest in `kubectl.kubernetes.io/last-applied-configuration`, and the Neo4j CRD OpenAPI schema is ~1.5 MB — above the 256 KiB annotation limit.

**Fix:** Use server-side apply via `make install`:

```bash
make install
# equivalent:
kubectl apply --server-side --force-conflicts -f config/crd/bases/neo4j.com_neo4js.yaml
```

If a previous failed apply left a broken CRD object, delete it first (only when no Neo4j workloads depend on it):

```bash
kubectl delete crd neo4js.neo4j.com --ignore-not-found
make install
```

## CRD not found when applying Neo4j

**Symptom:** `no matches for kind "Neo4j" in version "neo4j.com/v1beta1"`

**Fix:** Install the CRD first:

```bash
make install
kubectl get crd neo4js.neo4j.com
```

## Operator pod not starting

**Symptom:** `neo4j-operator-controller-manager` stays `CrashLoopBackOff` or `Pending`

**Checks:**

```bash
kubectl describe pod -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager
```

Common causes:

- Image `controller:latest` not present on nodes — run `make docker-build` and load into kind, or use `make run` locally.
- RBAC not applied — re-run `kubectl apply -k config/rbac`.

## Neo4j CR accepted but nothing happens

**Checks:**

```bash
kubectl get neo4j -A
kubectl describe neo4j dev -n default
kubectl get sts,svc,secret,pvc -n default -l app.kubernetes.io/instance=dev
```

- Confirm the operator pod is `Running`.
- Check `status.conditions` for `Error` or `Ready=False` messages.
- **Cluster mode** is not supported in Slice 1 — use `topology.mode: Standalone`.

## PVC stays Pending

**Symptom:** Pod `Pending`, PVC `Pending`

**Fix:**

- Ensure a StorageClass exists and is default, or set `spec.storage.volumes.data.dynamic.storageClassName`.
- On kind, install a local path provisioner or use the default standard StorageClass.

## Auth Secret / password

When `spec.auth.generatePassword: true`, the operator creates `{metadata.name}-auth`:

```bash
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d
```

See [Quickstart — Standalone](../neo4j/01-quickstart-standalone.md#connect).

## Ready condition false

Wait for StatefulSet rollout and PVC binding:

```bash
kubectl rollout status statefulset/dev-server -n default
kubectl get neo4j dev -n default -o jsonpath='{.status.conditions[?(@.type=="Ready")]}'
```

Status semantics: [status model](../../02-technical-design/crd-spec/neo4j/status.md) (design reference).
