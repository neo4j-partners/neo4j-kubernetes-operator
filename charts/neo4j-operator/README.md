# Neo4j Operator Helm chart

Install / upgrade / uninstall the Neo4j **operator controller** (OP-2-001-PKG-02).

This is not the Neo4j *workload* chart (`helm-charts/neo4j`).

## Prerequisites

- Helm 3.8+
- Kubernetes 1.28+
- Operator image reachable by the cluster (`image.repository` / `tag`)

## Install from the published chart (OCI)

Released charts and images are published to GHCR under `neo4j-partners`. The CRD is
**not** bundled (it is ~1.5 MB — near the etcd object limit — and Helm never upgrades
`crds/`), so apply it once with **server-side apply** from the release asset, then
install or upgrade the chart. `VERSION` is the release without the leading `v`.

```bash
VERSION=0.1.0

# 1. CRD (once per version; safe to re-run on upgrade)
kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

# 2. Operator (install or upgrade — same command)
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace
```

The chart defaults `image.repository` to `ghcr.io/neo4j-partners/neo4j-kubernetes-operator` and
resolves the tag from `Chart.appVersion`, so no `--set image.*` is needed.

## Install from a local checkout (development)

The Neo4j CRD OpenAPI schema is large; install it with **server-side apply** first:

```bash
# from repository root
make install

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace \
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
| `watchNamespaces` | `[default]` | Workload namespaces in `WATCH_NAMESPACE` (+ Role/RoleBinding each). Must not include the operator release namespace (NEO-016). |
| `serviceAccount.create` / `name` | `true` / `""` | When `create: false`, `name` is **required** (no silent fallback to `default`) |
| `webhook.enabled` | `false` | Validating admission webhook (rejects privileged / hostPath at apply) |
| `webhook.certManager.enabled` | `true` | When webhook on: create self-signed Issuer + Certificate (requires cert-manager) |
| `maxConcurrentReconciles` | `2` | Concurrent Neo4j reconciles (NEO-014). Maximum 16 |
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
# or: --set image.tag=0.1.0
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
