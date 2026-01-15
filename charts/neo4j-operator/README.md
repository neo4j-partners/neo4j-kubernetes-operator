# Neo4j Kubernetes Operator Helm Chart

This Helm chart deploys the Neo4j Enterprise Operator for Kubernetes, which manages Neo4j Enterprise deployments (v5.26+).

## Prerequisites

- Kubernetes 1.21+
- Helm 3.8+
- kubectl configured to access your cluster
- (Optional) cert-manager for TLS certificate management
- (Optional) Prometheus Operator for metrics collection

## Installation

### Add Helm Repository (when published)

```bash
helm repo add neo4j-operator https://neo4j-partners.github.io/neo4j-kubernetes-operator
helm repo update
```

### Install from Source

```bash
# Clone the repository
git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Install the chart (automatically creates ClusterRole and RBAC permissions)
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
| `image.repository` | Operator image repository | `ghcr.io/neo4j-partners/neo4j-kubernetes-operator` |
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
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

### Metrics Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.service.type` | Service type | `ClusterIP` |
| `metrics.service.port` | Metrics port | `8080` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` |

### Neo4j Defaults

| Parameter | Description | Default |
|-----------|-------------|---------|
| `neo4j.defaultImage` | Default Neo4j image | `neo4j:5.26-enterprise` |
| `neo4j.defaultStorageSize` | Default storage size | `10Gi` |
| `neo4j.defaultStorageClass` | Default storage class | `""` (cluster default) |
| `neo4j.defaultResources.requests.cpu` | Default CPU request | `500m` |
| `neo4j.defaultResources.requests.memory` | Default memory request | `2Gi` |
| `neo4j.defaultResources.limits.cpu` | Default CPU limit | `2` |
| `neo4j.defaultResources.limits.memory` | Default memory limit | `4Gi` |

### Other Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `leaderElection.enabled` | Enable leader election | `true` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Pod tolerations | `[]` |
| `affinity` | Pod affinity | `{}` |
| `priorityClassName` | Priority class name | `""` |

## Deployment Modes

### Cluster-wide Mode (Default)

The operator watches and manages Neo4j resources in all namespaces:

```yaml
operatorMode: cluster
```

### Namespace-scoped Mode

The operator only watches its own namespace (requires only Role, not ClusterRole):

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

### Upgrade the Chart

```bash
helm upgrade neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --reuse-values
```

### Upgrade with New Values

```bash
helm upgrade neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --set image.tag=v0.2.0 \
  --reuse-values
```

## Uninstallation

```bash
# Delete the release
helm delete neo4j-operator --namespace neo4j-operator-system

# Optional: Delete CRDs (this will delete all Neo4j resources!)
kubectl delete crds \
  neo4jbackups.neo4j.neo4j.com \
  neo4jdatabases.neo4j.neo4j.com \
  neo4jenterpriseclusters.neo4j.neo4j.com \
  neo4jenterprisestandalones.neo4j.neo4j.com \
  neo4jplugins.neo4j.neo4j.com \
  neo4jrestores.neo4j.neo4j.com \
  neo4jshardeddatabases.neo4j.neo4j.com

# Delete the namespace
kubectl delete namespace neo4j-operator-system
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
apiVersion: neo4j.neo4j.com/v1alpha1
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

- GitHub Issues: https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues
- Neo4j Community: https://community.neo4j.com/
- Documentation: https://neo4j.com/docs/
