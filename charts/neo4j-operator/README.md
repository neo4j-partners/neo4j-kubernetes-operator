# Neo4j Kubernetes Operator Helm Chart

This Helm chart deploys the Neo4j Enterprise Operator for Kubernetes, which manages Neo4j Enterprise deployments (v5.26+).

## Prerequisites

- Kubernetes 1.32+
- Helm 3.8+
- kubectl configured to access your cluster
- (Optional) cert-manager for TLS certificate management
- (Optional) Prometheus Operator for metrics collection (required only if you set `metrics.serviceMonitor.enabled=true`)

> **cert-manager install order**: The operator runs without cert-manager and only needs it for clusters using cert-manager TLS (`spec.tls.mode: cert-manager`). It picks up the cert-manager CRDs at startup, so if you install cert-manager *after* the operator, restart it (`kubectl rollout restart deployment/neo4j-operator-controller-manager -n <ns>`) to activate the Certificate watch.

## Installation

### From the Helm chart repository (recommended)

```bash
helm repo add neo4j-operator https://priyolahiri.github.io/neo4j-kubernetes-operator/charts
helm repo update

helm install neo4j-operator neo4j-operator/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace
```

### From the OCI registry (all releases)

```bash
helm install neo4j-operator oci://ghcr.io/priyolahiri/charts/neo4j-operator \
  --version 1.8.0 \
  --namespace neo4j-operator-system \
  --create-namespace
```

Use the chart version without the `v` prefix.

### From Source

```bash
git clone https://github.com/priyolahiri/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

helm install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace
```

**Note**: The chart automatically creates ClusterRole and ClusterRoleBinding for cluster-wide operation by default. No manual RBAC setup is required.

### Install with Custom Values

```bash
# Create a values file
cat > my-values.yaml <<EOF
operatorMode: namespace
replicaCount: 1
resources:
  limits:
    cpu: 1000m
    memory: 1Gi
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
EOF

# Install with custom values
helm install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace \
  --values my-values.yaml
```

## Configuration

The following table lists the configurable parameters of the Neo4j Operator chart and their default values.

### General Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `image.repository` | Operator image repository | `ghcr.io/priyolahiri/neo4j-kubernetes-operator` |
| `image.tag` | Operator image tag | `""` (uses chart appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `imagePullSecrets` | Image pull secrets | `[]` |
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full name | `""` |

### Operator Mode

| Parameter | Description | Default |
|-----------|-------------|---------|
| `operatorMode` | Operator mode: `cluster`, `namespace`, or `namespaces` | `cluster` |
| `watchNamespaces` | Namespaces/patterns to watch (when mode is `namespaces`) | `[]` |
| `developmentMode` | Enable development mode | `false` |
| `logLevel` | Log level: `debug`, `info`, `warn`, `error` | `info` |

### RBAC Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` (generated) |
| `rbac.create` | Create RBAC resources | `true` |
| `rbac.externalSecretsIntegration` | Grant the operator RBAC for the external-secrets.io integration (only needed when a CR sets `externalSecrets.enabled`) | `false` |
| `rbac.perNamespaceRoles` | Per-namespace Roles instead of a manager ClusterRole (`operatorMode: namespaces` with a static `watchNamespaces` list only) | `false` |
| `rbac.clusterScopedReads` | Opt-in read-only ClusterRole (`nodes` + `storageclasses`) for installs without a manager ClusterRole — restores zone auto-discovery and storage-expansion validation in namespace-scoped modes | `false` |

### Security Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podSecurityContextEnabled` | Set to `false` to allow platform defaults (e.g., OpenShift SCC) to supply UID/FSGroup | `true` |
| `podSecurityContext.runAsNonRoot` | Run as non-root user | `true` |
| `podSecurityContext.runAsUser` | User ID | `65532` |
| `podSecurityContext.fsGroup` | File system group | `65532` |
| `securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `securityContext.capabilities.drop` | Drop capabilities | `["ALL"]` |
| `securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `true` |

### Resource Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `1000m` |
| `resources.limits.memory` | Memory limit | `1Gi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

### Metrics Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.service.type` | Service type | `ClusterIP` |
| `metrics.service.port` | Metrics port | `8080` |
| `metrics.secure` | Serve `/metrics` over HTTPS with TokenReview authn + SubjectAccessReview authz (scrapers need a Bearer token bound to the `metrics-reader` ClusterRole) | `true` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` |
| `metrics.serviceMonitor.bearerTokenSecret.name` | Secret (type `kubernetes.io/service-account-token`) holding the scrape token. REQUIRED when `metrics.secure=true` and the ServiceMonitor is enabled — the render fails without it | `""` |
| `metrics.serviceMonitor.bearerTokenSecret.key` | Key inside that Secret | `token` |

### Neo4j Defaults

| Parameter | Description | Default |
|-----------|-------------|---------|
| `neo4j.defaultImage` | Default Neo4j image | `neo4j:5.26.0-enterprise` |
| `neo4j.defaultStorageSize` | Default storage size | `10Gi` |
| `neo4j.defaultStorageClass` | Default storage class | `""` (cluster default) |
| `neo4j.defaultResources.requests.cpu` | Default CPU request | `500m` |
| `neo4j.defaultResources.requests.memory` | Default memory request | `2Gi` |
| `neo4j.defaultResources.limits.cpu` | Default CPU limit | `2` |
| `neo4j.defaultResources.limits.memory` | Default memory limit | `4Gi` |

### Network Policy

| Parameter | Description | Default |
|-----------|-------------|---------|
| `networkPolicy.enabled` | Create a NetworkPolicy for the operator pod | `false` |
| `networkPolicy.ingress` | Ingress rules (list of NetworkPolicyIngressRule) | `[]` |
| `networkPolicy.egress` | Egress rules (list of NetworkPolicyEgressRule) | `[]` |

When `networkPolicy.enabled=true` with empty rules, the chart ships defaults:
ingress allows metrics/health scrapes; egress allows DNS (53), the Kubernetes
API on **443/6443**, and Bolt/discovery to workload namespaces. If your
cluster's API server listens on a non-standard port (some managed offerings),
set `networkPolicy.egress` explicitly or the operator cannot reach the API.

### Pod Disruption Budget

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podDisruptionBudget.enabled` | Create a PodDisruptionBudget for the operator | `false` |
| `podDisruptionBudget.minAvailable` | Minimum available replicas | `1` |
| `podDisruptionBudget.maxUnavailable` | Maximum unavailable replicas (mutually exclusive with minAvailable) | `""` |

### Other Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `leaderElection.enabled` | Enable leader election | `true` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Pod tolerations | `[]` |
| `affinity` | Pod affinity | `{}` |
| `priorityClassName` | Priority class name | `""` |
| `podAnnotations` | Extra annotations on the operator pod | `{}` |
| `env` | Extra environment variables for the operator container (list of `{name,value}`) | `[]` |
| `envFrom` | Extra `envFrom` sources (ConfigMap/Secret) for the operator container | `[]` |
| `volumes` | Extra volumes on the operator pod | `[]` |
| `volumeMounts` | Extra volume mounts on the operator container | `[]` |
| `livenessProbe` | Operator liveness probe (httpGet `/healthz:8081`) | see `values.yaml` |
| `readinessProbe` | Operator readiness probe (httpGet `/readyz:8081`) | see `values.yaml` |
| `extraManifests` | Extra Kubernetes manifests to deploy with the release (templated with `tpl`) | `[]` |
| `pluginInitContainer.image` | Image for `Neo4jPlugin` `installMode: VerifiedDownload` init containers; point at an internal mirror for air-gapped clusters | `""` (operator default `curlimages/curl:8.5.0`) |
| `preInstallChecks.enabled` | Run pre-install hook validating prerequisites | `true` |
| `preInstallChecks.image` | Image for the pre-install check Job (needs a shell + kubectl) | `alpine/k8s:1.32.0` |

## Deployment Modes

### Cluster-wide Mode (Default)

The operator watches and manages Neo4j resources in all namespaces:

```yaml
operatorMode: cluster
```

### Namespace-scoped Mode

The operator only watches its own namespace. Manager permissions are granted via a namespaced Role — no cluster-wide *manager* permissions — but the metrics and pre-install-check ClusterRoles still render by default unless you disable those features:

```yaml
operatorMode: namespace
```

### Multi-namespace Mode

The operator watches specific namespaces:

```yaml
operatorMode: namespaces
watchNamespaces:
  - neo4j-dev
  - neo4j-staging
  - neo4j-prod
```

### Multi-namespace Mode with Patterns

Pattern entries are supported when `operatorMode: namespaces`:

```yaml
operatorMode: namespaces
watchNamespaces:
  - team-*
  - regex:^prod-
  - label:{env=prod,tier=backend}
```

Pattern support requires ClusterRole access because the operator must list/watch namespaces.

## Install Without Helm (Kustomize)

```bash
# Install CRDs
kubectl apply -f config/crd/bases/

# Production overlay
kubectl apply -k config/overlays/prod

# Development overlay (Kind only)
kubectl apply -k config/overlays/dev
```

Pattern-based namespace watching (non-Helm):

```yaml
# In your kustomization overlay
patches:
- patch: |-
    - op: add
      path: /spec/template/spec/containers/0/env/-
      value:
        name: WATCH_NAMESPACE
        value: team-*,regex:^prod-,label:{env=prod}
  target:
    kind: Deployment
    name: controller-manager
```

## Upgrading

> **Apply the new release's CRDs before `helm upgrade` — this step is
> mandatory.** Helm installs the chart's `crds/` directory only on first
> install and never upgrades it. Without this refresh, fields added by the
> new release are silently pruned from your manifests by the API server.
>
> **Use `--server-side`.** The largest CRDs (`neo4jenterpriseclusters`,
> `neo4jenterprisestandalones`) exceed the 256 KiB
> `kubectl.kubernetes.io/last-applied-configuration` annotation that
> client-side `kubectl apply` writes, so a plain client-side apply fails
> with `metadata.annotations: Too long`. Server-side apply has no such limit.
> (Helm's own CRD install on first `helm install` is unaffected — it does not
> use that annotation.)

```bash
kubectl apply --server-side -f https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/<version>/neo4j-kubernetes-operator.yaml
```

### Upgrade the Chart

```bash
helm upgrade neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system
```

### Upgrade with New Values

```bash
helm upgrade neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --set image.tag=<version>
```

> Avoid `--reuse-values` as a blanket habit — it pins the previous release's
> values and discards new chart defaults. Re-pass your overrides explicitly
> (`-f my-values.yaml` / `--set ...`).

### Removed values

- **`webhook.*`** — removed. This project does not use admission webhooks (all validation is inline in the controllers); the old block rendered a `--webhook-port` flag the binary doesn't define.
- **`leaderElection.namespace`** — removed. The leader-election lease always lives in the release namespace; a configurable namespace would move the RBAC away from where the lease actually is.
- **`backup.*`** and **`tls.*`** — removed. No template consumed them, so setting e.g. `tls.enabled: true` did nothing. Backups are configured per-CR via `Neo4jBackup`; metrics TLS via `metrics.secure`; Neo4j instance TLS via `spec.tls` on the cluster/standalone CR.

The chart now also fails fast at render time on an invalid `operatorMode` and on `rbac.perNamespaceRoles=true` outside `operatorMode: namespaces`, and `extraManifests` entries are now actually rendered (previously the value was accepted but ignored).

## Uninstallation

The order matters: Neo4j custom resources carry finalizers that only the
**running** operator can remove. Delete the CRs first and wait for them to
disappear — uninstalling the operator (or deleting CRDs) while CRs still
exist wedges them in `Terminating` forever.

```bash
# 1. Delete all Neo4j custom resources and WAIT until this list is empty
kubectl get neo4jenterpriseclusters,neo4jenterprisestandalones,neo4jdatabases,neo4jshardeddatabases,neo4jbackups,neo4jrestores,neo4jusers,neo4jroles,neo4jrolebindings,neo4jauthrules,neo4jplugins -A

kubectl delete neo4jenterpriseclusters,neo4jenterprisestandalones,neo4jdatabases,neo4jshardeddatabases,neo4jbackups,neo4jrestores,neo4jusers,neo4jroles,neo4jrolebindings,neo4jauthrules,neo4jplugins --all -n <namespace>

# 2. Delete the release (only once step 1 shows no remaining CRs)
helm uninstall neo4j-operator --namespace neo4j-operator-system

# 3. Optional: Delete CRDs (this destroys all remaining Neo4j resource definitions!)
kubectl get crd -o name | grep neo4j.neo4j.com | xargs kubectl delete

# 4. Optional: Delete the namespace
kubectl delete namespace neo4j-operator-system
```

If a CR is already stuck in `Terminating` (operator removed too early), strip
its finalizers manually — note the operator's cleanup is skipped, so data may
be orphaned:

```bash
kubectl patch <kind>/<name> -n <namespace> -p '{"metadata":{"finalizers":[]}}' --type=merge
```

## Examples

### Deploy with Prometheus Monitoring

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    labels:
      prometheus: kube-prometheus
```

### Deploy in Restricted Environment

```yaml
imagePullSecrets:
  - name: registry-secret

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 1000
  fsGroup: 1000

resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 128Mi
```

### High Availability Setup

```yaml
replicaCount: 3

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
              - key: app.kubernetes.io/name
                operator: In
                values:
                  - neo4j-operator
          topologyKey: kubernetes.io/hostname
```

### Expose Neo4j via OpenShift Route

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: graph
spec:
  service:
    route:
      enabled: true
      host: graph.apps.example.com
      termination: reencrypt
```

## OpenShift Notes

- Disable the chart pod security context to let the SCC inject UID/FSGroup:
  ```bash
  helm install neo4j-operator ./charts/neo4j-operator \
    --namespace neo4j-operator-system \
    --create-namespace \
    --set podSecurityContextEnabled=false
  ```
- Grant an appropriate SCC to the operator service account (example uses `restricted`; adjust per policy):
  ```bash
  oc adm policy add-scc-to-user restricted -z neo4j-operator -n neo4j-operator-system
  ```
- Neo4j pods will inherit UID/FSGroup from the SCC; keep workload security contexts at defaults or override via CR fields if needed.

## Troubleshooting

### Check Operator Status

```bash
# Check deployment
kubectl get deployment -n neo4j-operator-system

# Check pods
kubectl get pods -n neo4j-operator-system

# Check logs
kubectl logs -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator
```

### Verify CRDs

```bash
kubectl get crds | grep neo4j
```

### Debug Installation

```bash
# Dry run to see what will be installed
helm install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --dry-run \
  --debug

# Get values used in installation
helm get values neo4j-operator -n neo4j-operator-system
```

## Support

- GitHub Issues: https://github.com/priyolahiri/neo4j-kubernetes-operator/issues
- Neo4j Community: https://community.neo4j.com/
- Documentation: https://neo4j.com/docs/
