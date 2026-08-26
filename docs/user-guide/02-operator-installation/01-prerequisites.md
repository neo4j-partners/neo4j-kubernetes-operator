# Prerequisites

What must be true before you install the operator.

## Kubernetes cluster

Any conformant Kubernetes **1.28 or later**, including local kind and minikube clusters. The CRD
uses validation rules that older API servers do not evaluate.

Your `kubectl` context needs permission to install a CustomResourceDefinition and to create a
namespace, Deployment, ServiceAccount and the roles the operator binds. Cluster-admin is the
simplest way to get there; a platform team can narrow it down as described in
[Watch scope and RBAC](04-operator-scope.md).

## Tools

| Tool | Purpose | Needed for |
|------|---------|-----------|
| `kubectl` | Applying the CRD, then the `Neo4j` resources you create | Every install |
| `helm` 3.8+ | Installing the chart, published or from a clone | Every install — the chart is the recommended path |
| `docker` and `make` | Building the controller image from source | Choice B only |
| `go` 1.24+ | Running the controller as a local process, without an image | Development only |

The controller image, the chart and the CRD are published with every release, so a first install
needs neither a clone nor a build — that is Choice A in
[Operator installation](readme.md). Building your own is Choice B, and starts at
[Build the operator image](02-build-image.md).

## Storage

Every `Neo4j` resource must declare a data volume under `spec.storage.volumes.data`. The default
choice is `Dynamic`, which asks Kubernetes for a PersistentVolumeClaim, so the cluster needs
either a default StorageClass or an explicit `dynamic.storageClassName`:

```bash
kubectl get storageclass
```

If nothing is marked `(default)` and you do not set a class name, the claim stays Pending and the
instance never starts. The full matrix of volume modes, including reusing an existing PVC, is in
[Storage](../03-neo4j/03-storage.md); runnable manifests are in
[`examples/storage/`](../../../examples/storage/).

## Neo4j image

The operator runs `{spec.image.repository}:{spec.version}` (or `repository@digest` when
`spec.image.digest` is set), defaulting to the `neo4j` repository on Docker Hub. Repositories
must be on the operator allowlist (`allowedImageRepositories` / `--allowed-image-repositories`,
NEO-012). On a restricted network, mirror the image, add the mirror to that allowlist, and set
`spec.image.pullSecrets` if needed.

Both editions are supported. `spec.edition: enterprise` requires `spec.license.accept: "yes"`
(or `"eval"`), which is a statement that you hold a commercial licence or use the image under the
terms Neo4j grants for evaluation and development. `spec.edition: community` needs no licence — the
block is optional and ignored there, and the operator pulls the unsuffixed image tag, which is the
Community build.

Community is confined to `topology.mode: Standalone`, and `features.backup` and
`features.monitoring.prometheus` are rejected on it: clustering, backup and metrics are Enterprise
capabilities, and a Community server refuses to start once its configuration mentions them.

## Namespaces

| Namespace | Contents |
|-----------|----------|
| `neo4j-operator-system` | The operator Deployment, its ServiceAccount and RBAC |
| One or more workload namespaces | Your `Neo4j` resources and the pods, Services and PVCs they own |

The operator only reconciles namespaces it was told to watch, and it is not cluster-wide by
default. The manifests ship with `WATCH_NAMESPACE=default`, which is why examples that omit
`metadata.namespace` work out of the box. The operator install namespace is never on that list
(NEO-016) — a `Neo4j` CR there is refused at start-up if you add it. Adding a workload namespace
means both extending that list and granting the operator rights in it — see
[Watch scope and RBAC](04-operator-scope.md).

## Next

Pick an install path in [Operator installation](readme.md), or go straight to
[Install the operator](03-install.md). Building your own image first:
[Build the operator image](02-build-image.md).

If you would rather follow a complete platform walkthrough, use
[kind (local)](../01-getting-started/local-kind.md), [Azure AKS](../01-getting-started/azure-aks.md)
or [GKE](../01-getting-started/gcp-gke.md).
