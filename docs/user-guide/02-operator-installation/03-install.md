# Install the operator

Install the operator's CustomResourceDefinitions — `Neo4j`, `Neo4jBackup`, `Neo4jBackupSchedule` and
`Neo4jRestore` — and the controller into `neo4j-operator-system`. The procedure is identical on every
Kubernetes distribution; only where the image comes from differs, which is the choice described in
[Operator installation](readme.md).

Before starting, make sure the [prerequisites](01-prerequisites.md) are met and, if you built your
own image, that it is reachable from the cluster.

The chart does **not** contain the CRDs (the `Neo4j` schema alone is ~1.5 MB, and Helm never upgrades
`crds/`), so every install is two steps: apply the CRDs, then install the chart. Pick one recipe
below and run the whole block — each is complete on its own.

## Install — from the published release (remote)

Nothing to build and no image to name: the chart defaults to the published controller image at this
version, and both artefacts are public. `VERSION` is the release tag without its leading `v`.

```bash
VERSION=1.0.0

# 1. CRDs — all four kinds, bundled in one asset. Server-side apply is required:
#    the Neo4j schema alone exceeds the client-side annotation size limit.
kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

# 2. Operator — the same command installs and upgrades.
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version "${VERSION}" \
  --namespace neo4j-operator-system --create-namespace
```

That is a complete install — nothing else is required. The operator watches the `default` namespace
out of the box, so a `Neo4j` CR applied there is reconciled immediately; to watch other namespaces
set `watchNamespaces` (see [Watch scope and RBAC](04-operator-scope.md)). Re-apply the CRDs on
upgrade — the chart never touches them.

## Install — from a local clone (Helm)

The same two steps against your working tree, run from the repository root: the CRDs from the
checked-in kustomize base, the chart from the local path.

```bash
# 1. CRDs — all four, server-side. `make install` runs this same apply.
kubectl apply --server-side --force-conflicts -k config/crd/bases

# 2. Operator from the working-tree chart. Defaults to the published image at
#    Chart.appVersion, so it works with no --set flags at all.
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace
```

To run your own build and widen the scope, add `--set` flags:

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace \
  --set image.repository=myregistry.example.com/neo4j-operator \
  --set image.tag=1.0.0 \
  --set 'watchNamespaces={default,team-a}'
```

## Install — from a clone, raw manifests (no Helm)

Four applies, in this order: the CRDs, the namespace, the roles and bindings the controller uses in
the namespaces it watches, then the controller Deployment.

```bash
kubectl apply --server-side --force-conflicts -k config/crd/bases
kubectl apply -f config/default/namespace.yaml
kubectl apply -k config/rbac
kubectl apply -k config/manager
```

If you are replacing an install that predates namespaced watch scope, remove the cluster-wide grant
it left behind — nothing else does, and it would keep the operator readable across the whole
Kubernetes cluster:

```bash
kubectl delete clusterrolebinding neo4j-operator-manager-rolebinding --ignore-not-found
kubectl delete clusterrole neo4j-operator-manager-role --ignore-not-found
```

The Deployment runs whatever `config/manager/manager.yaml` names, `controller:latest` out of the
box. These manifests substitute nothing, so pointing them at your own build means editing that
file — see
[Point the install at your image](02-build-image.md#point-the-install-at-your-image). The chart
takes that reference as a value instead, which is why the chart is the better choice for anything
but a scratch cluster.

The Deployment ships with `WATCH_NAMESPACE=default` (workload namespace only). Examples that omit
`metadata.namespace` land in `default` and are reconciled. The operator namespace is never watched
(NEO-016). To reconcile other namespaces, read
[Watch scope and RBAC](04-operator-scope.md) before you go further — the environment variable and
the roles have to be changed together.

Values worth knowing, on either chart:

| Value | Effect |
|-------|--------|
| `image.repository`, `image.tag`, `image.digest` | Which controller image runs; a digest wins over a tag |
| `allowedImageRepositories` | Operand Neo4j image repos allowed on CRs (NEO-012); add ACR/ECR mirrors here |
| `maxConcurrentReconciles` | Concurrent Neo4j reconciles (default 2, maximum 16; NEO-014) |
| `metrics.enabled` | Operator `/metrics` on HTTPS with Kubernetes auth (off by default; NEO-017). Do not enable via `extraArgs` |
| `metrics.serviceMonitor.enabled` | Optional Prometheus Operator ServiceMonitor for the controller (requires `metrics.enabled`) |
| `imagePullSecrets` | Pull Secret for the operator image itself |
| `watchNamespaces` | Workload namespaces to reconcile (must not include the Helm release namespace) |
| `logging.level`, `logging.devel`, `logging.file.*` | Verbosity, encoder, and optional log file inside the pod |
| `resources`, `nodeSelector`, `tolerations`, `affinity` | Placement and sizing of the controller pod |
| `extraArgs` | Extra manager flags, appended after the defaults (not for metrics — use `metrics.enabled`) |

The chart's own `README.md` under `charts/neo4j-operator/` documents every value with its default.

## Scheduling the controller

If your Neo4j nodes are tainted, the controller has to tolerate the same taints or it will sit
Pending and reconcile nothing. The shipped manifests already tolerate a `dedicated=neo4j`
`NoSchedule` taint:

```yaml
tolerations:
- key: dedicated
  operator: Equal
  value: neo4j
  effect: NoSchedule
```

Adjust that, plus `nodeSelector` if you use one, to match your pools — in `config/manager/manager.yaml`
for the manifest path, or through chart values for Helm. Keep it consistent with the
`spec.scheduling` you set on `Neo4j` resources, described in
[Operations](../03-neo4j/09-operations.md#placing-pods).

## Verify

```bash
kubectl get crd neo4js.neo4j.com neo4jbackups.neo4j.com \
  neo4jbackupschedules.neo4j.com neo4jrestores.neo4j.com

kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

Then confirm the operator agrees about its scope. The first lines of the log state which
namespaces it watches:

```bash
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager | head -20
```

A controller that starts and immediately exits with `WATCH_NAMESPACE is required` is telling you
the environment variable was lost — the operator refuses to guess a scope. `ImagePullBackOff`
instead means the cluster cannot reach the image: a tag you built but never pushed or loaded, or a
published version that does not exist. `kubectl describe pod` names the reference it tried.

## Run the controller on your machine

While developing you can skip the image altogether and run the controller as a local process
against your kubeconfig, with the CRDs as its only prerequisite. The command lives with the other
from-source workflows, in
[Build the operator image](02-build-image.md#skipping-the-image-entirely).

Do not also run the in-cluster Deployment: two controllers reconciling the same resources will
fight over every object. Either never apply `config/manager`, or scale it down first:

```bash
kubectl scale deployment/neo4j-operator-controller-manager -n neo4j-operator-system --replicas=0
```

## Next

Create your first instance: [Your first Neo4j](../01-getting-started/first-neo4j.md).

To review or narrow what the operator is allowed to touch: [Watch scope and RBAC](04-operator-scope.md).
