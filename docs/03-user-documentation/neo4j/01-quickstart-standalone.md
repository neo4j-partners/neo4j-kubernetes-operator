# Quickstart — Standalone Neo4j

Deploy a minimal single-instance Neo4j (Enterprise) in **Standalone** mode.

**Supported:** Dynamic or Existing data storage, aux volumes, generated/BYO password, ClusterIP (or NodePort/LoadBalancer), optional BYO TLS.

Assumes the operator is already installed. If not, pick a platform quickstart:

- [kind (local)](../quickstart/local-kind/install.md)
- [Azure AKS](../quickstart/azure-aks/install.md)

Operator install only: [operator/02-installation.md](../operator/02-installation.md).

Neo4j documentation index: [neo4j/readme.md](readme.md).

## Namespace

The sample manifest omits `metadata.namespace` — the `Neo4j` CR is created in the **`default`** namespace. Set `metadata.namespace` explicitly to deploy elsewhere.

## 1. Apply the sample

```bash
make sample-standalone
```

Or manually:

```bash
kubectl apply -f config/samples/neo4j_v1beta1_neo4j.yaml
```

Sample manifest (abbreviated):

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
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

More storage patterns: [`examples/storage/`](../../../examples/storage/).

## 2. Watch progress

```bash
kubectl get neo4j dev -n default -w
kubectl get pods -n default -l app.kubernetes.io/instance=dev
```

Expected objects:

| Resource | Name |
|----------|------|
| StatefulSet | `dev-server` |
| Headless Service | `dev-server` |
| Client Service | `dev` |
| Auth Secret | `dev-auth` (operator-generated) |
| ConfigMap | `dev-config` |
| PVC | `data-dev-server-0` |

## 3. Check status

```bash
kubectl get neo4j dev -n default -o wide
kubectl get neo4j dev -n default -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`
- `status.endpoints.bolt`: in-cluster Bolt URI

## Connect

Retrieve credentials:

```bash
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo
```

Port-forward Bolt:

```bash
kubectl port-forward -n default svc/dev 7687:7687
```

Use `neo4j://localhost:7687` with user `neo4j` and the password from the Secret.

Browser HTTP (optional):

```bash
kubectl port-forward -n default svc/dev 7474:7474
# Open http://localhost:7474
```

## Customize

| Goal | Field |
|------|-------|
| Larger disk | `spec.storage.volumes.data.dynamic.size` |
| StorageClass | `spec.storage.volumes.data.dynamic.storageClassName` (omit = cluster default) |
| Existing PVC | `spec.storage.volumes.data.mode: Existing` + `existing.claimName` |
| Aux volumes | `spec.storage.volumes.{backups,logs,metrics,import,licenses}` |
| Secret mounts | `spec.storage.secretMounts` |
| Existing password Secret | `spec.auth.passwordSecretRef.name` (disable `generatePassword`) |
| Neo4j config | `spec.config.neo4j` (key-value → `neo4j.conf`) |
| JVM flags | `spec.config.jvm.additionalArguments` |
| Target namespace | `metadata.namespace` on the CR |

Full API: [CRD spec](../../02-technical-design/crd-spec/neo4j/spec.md) · [Cheatsheet](../reference/api-cheatsheet.md)

## Clean up

```bash
kubectl delete neo4j dev -n default
```

PVCs may remain until explicitly deleted — see [Uninstall](../operator/03-uninstall.md).
