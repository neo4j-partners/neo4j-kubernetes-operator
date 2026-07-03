# Quickstart — Standalone Neo4j

Deploy a minimal single-instance Neo4j (Enterprise) in **Standalone** mode.

**V1 supported** (Slice 1): Dynamic storage, generated password, ClusterIP Bolt + HTTP.

## 1. Install the operator

If not already done: [Install the operator](../operator/install/readme.md) ([kind](../operator/install/local/kind/install.md) · [Azure AKS](../operator/install/azure/aks/install.md)).

## 2. Apply the sample

```bash
make sample-standalone
```

Or manually:

```bash
kubectl create namespace graph-dev
kubectl apply -f config/samples/neo4j_v1beta1_neo4j.yaml
```

Sample manifest (abbreviated):

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
  namespace: graph-dev
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

## 3. Watch progress

```bash
kubectl get neo4j dev -n graph-dev -w
kubectl get pods -n graph-dev -l app.kubernetes.io/instance=dev
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

## 4. Check status

```bash
kubectl get neo4j dev -n graph-dev -o wide
kubectl get neo4j dev -n graph-dev -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`
- `status.endpoints.bolt`: in-cluster Bolt URI

## Connect

Retrieve credentials:

```bash
kubectl get secret dev-auth -n graph-dev -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo
```

Port-forward Bolt:

```bash
kubectl port-forward -n graph-dev svc/dev 7687:7687
```

Use `neo4j://localhost:7687` with user `neo4j` and the password from the Secret.

Browser HTTP (optional):

```bash
kubectl port-forward -n graph-dev svc/dev 7474:7474
# Open http://localhost:7474
```

## Customize

| Goal | Field |
|------|-------|
| Larger disk | `spec.storage.volumes.data.dynamic.size` |
| StorageClass | `spec.storage.volumes.data.dynamic.storageClassName` |
| Existing password Secret | `spec.auth.passwordSecretRef.name` (disable `generatePassword`) |
| Neo4j config | `spec.config.neo4j` (key-value → `neo4j.conf`) |
| JVM flags | `spec.config.jvm.additionalArguments` |

Full API: [CRD spec](../../02-technical-design/crd-spec/neo4j/spec.md) · [Cheatsheet](../reference/api-cheatsheet.md)

## Clean up

```bash
kubectl delete neo4j dev -n graph-dev
kubectl delete namespace graph-dev
```

PVCs may remain until explicitly deleted — see [Uninstall](../operator/03-uninstall.md).
