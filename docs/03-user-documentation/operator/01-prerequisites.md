# Prerequisites

Before installing the Neo4j operator, ensure your environment meets the following requirements.

## Kubernetes cluster

- A working cluster (local **kind** / **minikube**, or any conformant Kubernetes **1.28+**).
- `kubectl` configured with cluster-admin or sufficient permissions to:
  - Install CRDs (`CustomResourceDefinition`)
  - Create namespaces, Deployments, ClusterRoles, and ClusterRoleBindings (current dev manifests)

## Tools

| Tool | Purpose | Required for |
|------|---------|--------------|
| `kubectl` | Apply manifests | All installs |
| `make` | Project deployment targets | Recommended |
| `go` 1.22+ | Build manager locally | `make build`, `make run` |
| `docker` | Build controller image | `make docker-build` (optional) |

## Storage

Standalone Neo4j uses a **Dynamic** PersistentVolumeClaim (`spec.storage.volumes.data`).

- The cluster must have a **default StorageClass**, or you must set `storageClassName` on the `Neo4j` CR.
- Verify:

```bash
kubectl get storageclass
```

## Neo4j image

The operator sets the container image to `{repository}:{spec.version}` (default repository `neo4j`).

- Your cluster nodes (or image pull secrets) must be able to pull the Neo4j Enterprise image tag you declare in `spec.version`.
- V1 requires `spec.edition: enterprise` and `spec.license.accept: "yes"`.

## Namespace layout (V1)

| Namespace | Purpose |
|-----------|---------|
| `neo4j-operator-system` | Operator controller (Deployment) |
| Workload namespace | `Neo4j` CR and Neo4j pods |

**V1 target** ([BDR-003](../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)): operator and `Neo4j` CR in the **same namespace**. The Makefile quickstart uses `graph-dev` as a separate workload namespace for local labs; production installs should follow the single-namespace policy when enforced by the controller.

## Next step

[Install the operator](install/readme.md) — [kind](install/local/kind/install.md) · [Azure AKS](install/azure/aks/install.md)
