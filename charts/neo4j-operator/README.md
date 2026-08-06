# Neo4j Operator Helm chart

Install / upgrade / uninstall the Neo4j **operator controller** (OP-2-001-PKG-02).

This is not the Neo4j *workload* chart (`helm-charts/neo4j`).

## Prerequisites

- Helm 3.8+
- Kubernetes 1.28+
- Operator image reachable by the cluster (`image.repository` / `tag`)

## Install

The Neo4j CRD OpenAPI schema is large; install it with **server-side apply** first:

```bash
# from repository root
make install

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace \
  --set image.repository=YOUR_REGISTRY/neo4j-operator \
  --set image.tag=YOUR_TAG
```

Or one shot:

```bash
make helm-install IMG=YOUR_REGISTRY/neo4j-operator:YOUR_TAG
```

## Values

| Key | Default | Meaning |
|-----|---------|---------|
| `image.repository` / `tag` | ACR `…/neo4j-operator:latest` | Controller image |
| `watchNamespaces` | `[default]` | Namespaces in `WATCH_NAMESPACE` (+ Role/RoleBinding each) |
| `serviceAccount.create` / `name` | `true` / `""` | When `create: false`, `name` is **required** (no silent fallback to `default`) |
| `replicaCount` | `1` | Controller replicas |
| `resources` / `tolerations` / `nodeSelector` | see `values.yaml` | Scheduling |

Watch multiple namespaces:

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  -n neo4j-operator-system --create-namespace \
  --set watchNamespaces={default,team-a,team-b}
```

## Upgrade

```bash
helm upgrade neo4j-operator ./charts/neo4j-operator -n neo4j-operator-system
# After API changes, also refresh the CRD:
make install
```

`helm upgrade` rolls the controller only; Neo4j CRs and PVCs are not deleted.

## Uninstall

```bash
helm uninstall neo4j-operator -n neo4j-operator-system
```

Leaves the CRD and any Neo4j workloads/PVCs in place (preserve-data default).
