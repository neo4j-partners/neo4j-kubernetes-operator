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
| `go` 1.24+ | Build manager locally | `make build`, `make run` |
| `docker` | Build controller image | `make docker-build` (optional) |

## Storage

`Neo4j` requires `spec.storage.volumes.data` (`Dynamic` or `Existing`).

- **Dynamic:** cluster needs a default StorageClass, or set `dynamic.storageClassName` on the CR.
- **Existing:** provide a PVC (`claimName`), raw `volume`, or `volumeClaimTemplate` (optional `selector`).
- **Aux volumes** (`backups`, `logs`, `metrics`, `import`, `licenses`): `Share` / `Dynamic` / `Existing`.

Verify StorageClasses:

```bash
kubectl get storageclass
```

Examples: [`examples/storage/`](../../../examples/storage/).

## Neo4j image

The operator sets the container image to `{repository}:{spec.version}` (default repository `neo4j`).

- Your cluster nodes (or image pull secrets) must be able to pull the Neo4j Enterprise image tag you declare in `spec.version`.
- V1 requires `spec.edition: enterprise` and `spec.license.accept: "yes"`.

## Namespace layout (V1)

| Namespace | Purpose |
|-----------|---------|
| `neo4j-operator-system` | Operator controller (Deployment) |
| Workload namespace | `Neo4j` CR and Neo4j pods — **`default`** if `metadata.namespace` is omitted |

**V1 target** ([BDR-003](../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)): operator and `Neo4j` CR in the **same namespace** when using the dedicated operator namespace. Samples and quickstarts omit `metadata.namespace` so the `Neo4j` CR is created in **`default`** unless you set another namespace explicitly.

## Next step

Start with a platform quickstart:

- [kind (local)](../quickstart/local-kind/install.md)
- [Azure AKS](../quickstart/azure-aks/install.md)

Or install the operator on an existing cluster: [02-installation.md](02-installation.md).
