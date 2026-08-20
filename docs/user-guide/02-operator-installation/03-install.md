# Install the operator

Install the `Neo4j` CustomResourceDefinition and the controller into `neo4j-operator-system`. The
procedure is identical on every Kubernetes distribution; only where the image comes from differs,
which is the choice described in [Operator installation](readme.md).

Before starting, make sure the [prerequisites](01-prerequisites.md) are met and, if you built your
own image, that it is reachable from the cluster.

## Install the CRD

The CRD must be applied server-side. Its schema is large enough to exceed the annotation size
limit that a client-side `kubectl apply -f` relies on, so the plain form fails with a metadata
error. From a clone:

```bash
make install
```

That runs `kubectl apply --server-side --force-conflicts` on
`config/crd/bases/neo4j.com_neo4js.yaml`. Without a clone, apply the same file from the release
asset — `VERSION` is the release tag without its leading `v`:

```bash
VERSION=1.0.0-rc1

kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml
```

Every install path below depends on this step, and it is also the only one needed when you run the
controller locally. Re-apply it when you upgrade: the chart never touches the CRD.

## From the published chart

Nothing to build and no image to name. The chart defaults to the controller image published at its
own version, and both artefacts are public:

```bash
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace
```

The same command upgrades an existing release. Add `--set` flags from the table below, or a values
file, exactly as with a local chart.

## From a clone — manifests

```bash
make deploy IMG=neo4j-operator:local
```

This creates the `neo4j-operator-system` namespace, applies the roles and bindings from
`config/rbac`, and applies the controller Deployment from `config/manager`. It also removes the
cluster-scoped role and binding that older installs used, since watch scope is now granted per
namespace.

The Deployment ships with `WATCH_NAMESPACE=default` (workload namespace only). Examples that omit
`metadata.namespace` land in `default` and are reconciled. The operator namespace is never watched
(NEO-016). To reconcile other namespaces, read
[Watch scope and RBAC](04-operator-scope.md) before you go further — the environment variable and
the roles have to be changed together.

## From a clone — the local chart

Same chart as the published one, read from your working tree, with your image:

```bash
make helm-install IMG=myregistry.example.com/neo4j-operator:0.1.0
```

Or drive Helm yourself, which is what you want as soon as you need real values:

```bash
make install    # CRD, server-side

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace \
  --set image.repository=myregistry.example.com/neo4j-operator \
  --set image.tag=0.1.0 \
  --set 'watchNamespaces={default,team-a}'
```

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
kubectl get crd neo4js.neo4j.com

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

Useful while developing, since it skips image builds entirely:

```bash
make install
make run LOG_ARGS="--zap-devel --zap-log-level=debug"
```

`make run` disables leader election and uses your kubeconfig. Do not also run the in-cluster
Deployment: two controllers reconciling the same resources will fight over every object. Either
never apply `config/manager`, or scale it down first:

```bash
kubectl scale deployment/neo4j-operator-controller-manager -n neo4j-operator-system --replicas=0
```

## Next

Create your first instance: [Your first Neo4j](../01-getting-started/first-neo4j.md).

To review or narrow what the operator is allowed to touch: [Watch scope and RBAC](04-operator-scope.md).
