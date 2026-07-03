# Troubleshooting — operator install

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
