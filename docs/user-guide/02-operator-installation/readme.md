# Operator installation

You install the operator once per Kubernetes cluster: the `Neo4j` CustomResourceDefinition, plus a
controller Deployment in its own namespace. Everything after that is `Neo4j` resources — one per
Neo4j deployment, Standalone or Cluster — covered in [Neo4j](../03-neo4j/readme.md).

There are two ways in, and the question that separates them is whether you need to build anything.

| | Choice A — install the published release | Choice B — build from source |
|---|---|---|
| You need | `kubectl` and `helm` 3.8+ | the same, plus a clone of this repository, `docker` and `make` |
| The controller image comes from | `ghcr.io/neo4j-partners/neo4j-kubernetes-operator`, public | your build, pushed to your own registry or loaded into the node |
| Roughly | 5 minutes | 15 minutes |
| Choose it when | you want to run the operator | you changed the operator, your registry is air-gapped, or you are developing against it |

Both end in the same place: the controller Running in `neo4j-operator-system`, watching the
`default` namespace. Neither installs Neo4j itself.

Check the [prerequisites](01-prerequisites.md) first — Kubernetes version, StorageClass, and the
Neo4j license you will need on the resources you create later.

## Choice A — install the published release

Each release publishes three artefacts: the controller image, the Helm chart as an OCI artefact,
and the CRD as a downloadable asset. Take the version from the
[latest release](https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases) without its
leading `v`:

```bash
VERSION=1.0.0-rc1

# 1. The CRD, server-side. Not part of the chart — see below.
kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

# 2. The operator. The same command installs and upgrades.
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace
```

There is no image to specify: the chart defaults to the published controller image at its own
version, and both artefacts are public, so no registry credentials are involved.

**The CRD is deliberately outside the chart.** It is around 1.5 MB, close to the etcd object limit,
and Helm never upgrades what it finds in `crds/` — so a chart that owned it could not deliver a
schema change, and a `helm uninstall` could take every `Neo4j` resource in the cluster with it.
Applying it yourself also means it must be applied server-side: the client-side form stores the
whole schema in an annotation and fails on its size limit.

Confirm the controller came up before going further:

```bash
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

Then continue with [Install the operator](03-install.md) for the values worth setting, what to check
when the Deployment does not become Available, and how to run the controller outside the cluster.

## Choice B — build the image and install from source

Two steps, one page each. [Build the operator image](02-build-image.md) covers the build, the
architecture trap on Apple silicon, the ways to make an image reachable — loading it into kind or
minikube, pushing it to a registry, or mirroring the published one — and how to point the install
at the result. [Install the operator](03-install.md) then covers both install paths from a clone:
the chart in `charts/neo4j-operator` with your image values, or the raw manifests under `config/`.

If you are only developing, you can skip the image entirely and run the controller as a local
process against your kubeconfig, described in
[Build the operator image](02-build-image.md#skipping-the-image-entirely).

## Two things to settle before you install

**The operator reconciles the `default` namespace only.** You list the namespaces it should watch,
and its own namespace is never one of them. Widening that scope means changing an environment
variable and the RBAC together, so read [Watch scope and RBAC](04-operator-scope.md) first if you
are installing in a shared cluster.

**Removal keeps your data by default.** Uninstalling the operator leaves your `Neo4j` resources and
their volumes in place, and deleting a `Neo4j` keeps its PersistentVolumeClaims.
[Uninstall the operator](05-uninstall.md) lists exactly what goes and what stays.

## Pages in this section

| Page | Covers |
|------|--------|
| [1. Prerequisites](01-prerequisites.md) | Kubernetes version and permissions, tools, StorageClass, the Neo4j image and license, namespaces |
| [2. Build the operator image](02-build-image.md) | Building, cross-building, and getting the image to the nodes — Choice B only |
| [3. Install the operator](03-install.md) | The CRD, manifests and chart, values, controller scheduling, verification |
| [4. Watch scope and RBAC](04-operator-scope.md) | Which namespaces are reconciled, and the roles that grant it |
| [5. Uninstall the operator](05-uninstall.md) | Removing the operator, the CRD, and what happens to your data |

## Next

Create your first instance: [Your first Neo4j](../01-getting-started/first-neo4j.md).
