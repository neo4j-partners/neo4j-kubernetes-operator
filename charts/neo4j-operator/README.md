# Neo4j Operator Helm chart

Install / upgrade / uninstall the Neo4j **operator controller** (OP-2-001-PKG-02).

This is not the Neo4j *workload* chart (`helm-charts/neo4j`).

## Prerequisites

- Helm 3.8+
- Kubernetes 1.34+ (enforced by the chart's `kubeVersion`)
- Operator image reachable by the cluster (`image.repository` / `tag`)

## Install from the published chart (OCI)

Released charts and images are published to GHCR under `neo4j-partners`. The CRDs are
**not** bundled (the `Neo4j` schema alone is ~1.5 MB — near the etcd object limit — and Helm never
upgrades `crds/`), so apply them once with **server-side apply** from the release asset — one file
containing all four CRDs — then install or upgrade the chart. `VERSION` is the release without the
leading `v`.

```bash
VERSION=1.0.0

# 1. CRDs (once per version; safe to re-run on upgrade)
kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

# 2. Operator (install or upgrade — same command)
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace \
  --set 'watchNamespaces={default}'
```

Every install must declare a scope: set `watchNamespaces` (the examples use `default`) **or**
`clusterWide=true`. The feature-flag examples below omit it for brevity — add your scope flag too.

The chart defaults `image.repository` to `ghcr.io/neo4j-partners/neo4j-kubernetes-operator` and
resolves the tag from `Chart.appVersion`, so no `--set image.*` is needed.

## Install from a local checkout (development)

The `Neo4j` CRD OpenAPI schema is large; install all the CRDs with **server-side apply** first:

```bash
# from repository root
make install

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace \
  --set 'watchNamespaces={default}' \
  --set image.repository=YOUR_REGISTRY/neo4j-kubernetes-operator \
  --set image.tag=YOUR_TAG
```

Or one shot:

```bash
make helm-install IMG=YOUR_REGISTRY/neo4j-kubernetes-operator:YOUR_TAG
```

## Values

| Key | Default | Meaning |
|-----|---------|---------|
| `image.repository` / `tag` / `digest` | ACR repo; tag defaults to `Chart.appVersion`; optional `digest` (`sha256:…`) | Prefer digest or semver — not `latest` |
| `image.pullPolicy` | `IfNotPresent` | Use `Always` only for mutable tags |
| `watchNamespaces` | `[]` | Workload namespaces in `WATCH_NAMESPACE` (+ Role/RoleBinding each). Must not include the operator release namespace (NEO-016). Set this **or** `clusterWide`, not both. |
| `clusterWide` | `false` | Opt-in: watch **all** namespaces via a `ClusterRole` (BDR-003). Grants Secret read cluster-wide; operator namespace still excluded (NEO-016). |
| `serviceAccount.create` / `name` | `true` / `""` | When `create: false`, `name` is **required** (no silent fallback to `default`) |
| `webhook.enabled` | `false` | Validating admission webhook (rejects privileged / hostPath at apply) |
| `webhook.certManager.enabled` | `true` | When webhook on: create self-signed Issuer + Certificate (requires cert-manager) |
| `maxConcurrentReconciles` | `2` | Concurrent Neo4j reconciles (NEO-014). Maximum 16 |
| `platform` | `auto` | Operand UID. `auto` detects OpenShift and leaves `runAsUser`/`fsGroup` unset so `restricted-v2` assigns one. `openshift` always does that. `kubernetes` pins UID/GID/fsGroup `7474` |
| `metrics.enabled` | `false` | Operator HTTPS `/metrics` with Kubernetes auth (NEO-017). Do not use `extraArgs` |
| `metrics.port` | `8443` | Metrics listen port when enabled |
| `metrics.serviceMonitor.enabled` | `false` | Optional Prometheus Operator ServiceMonitor (needs `metrics.enabled` and the CRD) |
| `replicaCount` | `1` | Controller replicas |
| `logging.level` | `info` | stderr verbosity (`debug` / `info` / `error`) |
| `logging.devel` | `false` | Console encoder (local debug) |
| `logging.file.enabled` | `false` | Tee verbose logs to emptyDir file |
| `logging.file.level` | `debug` | File verbosity when enabled |
| `resources` / `tolerations` / `nodeSelector` | see `values.yaml` | Scheduling |

Watch multiple namespaces:

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  -n neo4j-operator-system --create-namespace \
  --set watchNamespaces={default,team-a,team-b}
```

Pin an immutable image (recommended for production):

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  -n neo4j-operator-system \
  --set image.digest=sha256:YOUR_DIGEST
# or: --set image.tag=1.0.0
```

Released images are a manifest index covering `linux/amd64` and `linux/arm64`, so a tag needs no
architecture suffix and a mixed-architecture cluster needs no second value. When pinning by digest,
take the **index** digest — the one `docker buildx imagetools inspect` prints first, or the
`ghcr.io` UI shows for the tag. An architecture-specific digest also resolves, but it pins the
operator to that architecture and will fail to start on any other node:

```bash
docker buildx imagetools inspect ghcr.io/neo4j-partners/neo4j-kubernetes-operator:1.0.0
```

## OpenShift

This applies to every OpenShift cluster: self-managed, ROSA, and Azure Red Hat OpenShift. It is
not specific to IBM Z.

`platform` defaults to `auto`. When the API server exposes `security.openshift.io`, the operator
does not set `runAsUser`, `runAsGroup`, or `fsGroup` on the Neo4j pod. `restricted-v2` assigns a
UID from the project range, so the pod is admitted without an SCC grant. On plain Kubernetes the
same default pins UID, GID, and `fsGroup` to `7474`, which a fresh volume needs in order to be
writable by a non-root process.

Force one or the other when detection is wrong:

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  -n neo4j-operator-system --create-namespace \
  --set platform=openshift \
  --set 'watchNamespaces={default}'
```

`platform: kubernetes` puts `7474` back. `restricted-v2` rejects that UID; the operand ServiceAccount
would need `nonroot-v2`.

Published images are `linux/amd64` and `linux/arm64`, which is what ARO and other x86 or ARM
OpenShift clusters pull. IBM Z is the exception below.

### OpenShift on IBM Z (linux/s390x)

That index has no `linux/s390x` entry. On an IBM Z node the kubelet reports
`no image found in image index for architecture "s390x"` and the pod stays in `ImagePullBackOff`.
Do not set `image.digest` to the ACR or GHCR index, and do not pass that `--set image.digest=…`
on the same command — `--set` overrides the values file and puts the amd64/arm64 index back.
Build on the cluster and install from the internal registry. The steps that were run on this
cluster are in
[`hack/openshift-s390x/README.md`](../../hack/openshift-s390x/README.md):

```bash
oc apply -f hack/openshift-s390x/builds.yaml
oc start-build neo4j-operator-s390x -n neo4j-operator-system --from-dir=. --follow

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  -f hack/openshift-s390x/values-s390x.yaml
```

## Validating webhook (optional)

Rejects privileged containers / `hostPath` at `kubectl apply` (NEO-001). Requires cert-manager:

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  -n neo4j-operator-system --create-namespace \
  --set webhook.enabled=true
```

Without the webhook, the same checks still run at reconcile time.

## Operator metrics (optional)

Off by default. Enabling serves HTTPS `/metrics` with Kubernetes authn/authz (NEO-017).
Do not pass `--metrics-bind-address` through `extraArgs` — that used to be unauthenticated HTTP.

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  -n neo4j-operator-system --create-namespace \
  --set metrics.enabled=true
```

Bind your Prometheus ServiceAccount to ClusterRole `neo4j-operator-metrics-reader` (name is
`<release>-metrics-reader` if the release name is not `neo4j-operator`). Optional
`metrics.serviceMonitor.enabled` needs the Prometheus Operator CRD.

`helm upgrade` rolls the controller only; Neo4j CRs and PVCs are not deleted.

## Uninstall

```bash
helm uninstall neo4j-operator -n neo4j-operator-system
```

Leaves the CRD and any Neo4j workloads/PVCs in place (preserve-data default).
