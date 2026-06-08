# Installation Guide

This guide provides detailed instructions for installing the Neo4j Enterprise Operator for Kubernetes.

## Quick Installation

### Method 1: Helm Chart Repository (Recommended)

> Available from v1.8.0 onwards. The chart repository is hosted at
> `https://neo4j-partners.github.io/neo4j-kubernetes-operator/charts` and is
> updated automatically on every operator release.

```bash
helm repo add neo4j-operator https://neo4j-partners.github.io/neo4j-kubernetes-operator/charts
helm repo update

helm install neo4j-operator neo4j-operator/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace
```

**Pin to a specific version**:
```bash
helm install neo4j-operator neo4j-operator/neo4j-operator \
  --version 1.8.0 \
  --namespace neo4j-operator-system \
  --create-namespace
```

**Customise installation values**:
```bash
# View available configuration options
helm show values neo4j-operator/neo4j-operator

helm install neo4j-operator neo4j-operator/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace \
  --set resources.limits.memory=512Mi
```

**Upgrade**:
```bash
helm repo update
helm upgrade neo4j-operator neo4j-operator/neo4j-operator \
  --namespace neo4j-operator-system
```

**Uninstall**:
```bash
helm uninstall neo4j-operator --namespace neo4j-operator-system
```

### Method 2: OCI Registry

Available for all releases (including pre-v1.8.0). Helm 3.8 or later is required for OCI support.

```bash
helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --version 1.8.0 \
  --namespace neo4j-operator-system \
  --create-namespace
```

Use the chart version without the `v` prefix (for example, `1.8.0`).

### Method 3: Quick Install from GitHub Release

For environments where running `helm` is inconvenient, every release also publishes a single kubectl-applyable YAML bundle:

```bash
RELEASE_VERSION=v1.8.0  # Replace with desired version

kubectl apply -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/neo4j-kubernetes-operator-complete.yaml
```

**What this installs**:
- Custom Resource Definitions (CRDs)
- Operator Deployment (multi-arch images from ghcr.io)
- All required RBAC permissions (ClusterRole, ClusterRoleBinding, ServiceAccount)
- Deployed to the `neo4j-operator-system` namespace

**To find available releases**:
```bash
# Visit: https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases
# Or use the GitHub CLI:
gh release list --repo neo4j-partners/neo4j-kubernetes-operator
```

**CRDs only** (manage the operator deployment separately):
```bash
kubectl apply -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/neo4j-kubernetes-operator.yaml
```

**Supported architectures**: linux/amd64, linux/arm64

### Method 4: Helm from Cloned Repository

Useful for testing an unreleased commit or customising the chart locally.

```bash
git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

LATEST_TAG=$(git describe --tags --abbrev=0)
git checkout $LATEST_TAG    # or omit to test main

helm install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace
```

### Method 5: Git Clone with Make Targets

For development, customization, or when you need to build from source:

```bash
# Clone the repository
git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Checkout the latest release tag
LATEST_TAG=$(git describe --tags --abbrev=0)
git checkout $LATEST_TAG

# Install CRDs and operator
make install      # Install CRDs into your cluster
make deploy-prod  # Deploy operator (builds and uses local image)

# Alternative deployment options:
make deploy-prod-local     # Explicit local build and deploy to Kind cluster
make deploy-prod-registry  # Deploy from ghcr.io registry (requires authentication)
```

**What this installs**:
- Custom Resource Definitions (CRDs)
- Operator Deployment
- All required RBAC permissions
- ServiceAccount and ClusterRole bindings

## Custom Kustomize Configuration

For deployments that need a custom namespace, labels, or image tag, layer your own kustomization on top of `config/default`:

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- config/default
namespace: my-neo4j-operator
commonLabels:
  team: database
images:
- name: ghcr.io/neo4j-partners/neo4j-kubernetes-operator
  newTag: v1.0.0
```

```bash
kubectl apply -k .
```

For local development (Kind cluster + local image build) use `make dev-cluster` followed by `make operator-setup`. See the [developer guide](../developer_guide/development.md) for the full inner-loop workflow.

## Verifying the Installation

After installation, verify that the operator is running:

```bash
# Check operator pod status (default namespace: neo4j-operator-system)
kubectl get pods -n neo4j-operator-system

# Check CRDs are installed
kubectl get crd | grep neo4j

# View operator logs
kubectl logs -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator
```

Expected output:
```bash
# Pod should be Running
NAME                                        READY   STATUS    RESTARTS   AGE
neo4j-operator-controller-manager-xxx       2/2     Running   0          1m

# CRDs should be present
neo4jbackups.neo4j.neo4j.com
neo4jdatabases.neo4j.neo4j.com
neo4jenterpriseclusters.neo4j.neo4j.com
neo4jenterprisestandalones.neo4j.neo4j.com
neo4jplugins.neo4j.neo4j.com
neo4jrestores.neo4j.neo4j.com
```

## Available Make Targets

After cloning the repository, you have access to these make targets:

### Installation & Deployment
| Target | Description |
|--------|-------------|
| `make install` | Install CRDs into your cluster |
| `make deploy-prod` | Deploy operator with production configuration |
| `make deploy-dev` | Deploy with development configuration |
| `make deploy-prod` | Deploy with production configuration |
| `make undeploy-prod/undeploy-dev` | Remove operator deployment |
| `make uninstall` | Remove CRDs (also removes all Neo4j instances) |

### Development & Testing
| Target | Description |
|--------|-------------|
| `make dev-cluster` | Create Kind development cluster |
| `make operator-setup` | Deploy operator in-cluster (required for proper DNS) |
| `make operator-setup` | Deploy operator to existing cluster |
| `make test-unit` | Run unit tests |
| `make test-integration` | Run integration tests |

### Code Quality
| Target | Description |
|--------|-------------|
| `make fmt` | Format code with gofmt |
| `make lint` | Run golangci-lint (strict mode) |
| `make lint-lenient` | Run golangci-lint with relaxed rules |
| `make vet` | Run go vet |
| `make security` | Run gosec security scan |
| `make tidy` | Tidy and verify go modules |
| `make clean` | Clean build artifacts |
| `make test-coverage` | Generate test coverage report |

## Getting Started with Examples

After installing the operator, you can deploy your first Neo4j instance using examples.

### Option 1: Using Examples from GitHub Release

Download example configurations from the release:

```bash
# Download examples archive
RELEASE_VERSION=v1.0.0  # Replace with your installed version
curl -LO https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/examples-${RELEASE_VERSION#v}.tar.gz

# Extract examples
tar -xzf examples-${RELEASE_VERSION#v}.tar.gz

# Create admin secret (required for Neo4j authentication)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password

# Deploy your first Neo4j instance
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Check deployment status
kubectl get neo4jenterprisestandalone
kubectl get pods

# Access Neo4j Browser
kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687
```

### Option 2: Using Examples from Cloned Repository

If you cloned the repository, examples are available in the local `examples/` directory:

```bash
# Create admin secret (required for Neo4j authentication)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password

# Deploy your first Neo4j instance
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Check deployment status
kubectl get neo4jenterprisestandalone
kubectl get pods

# Access Neo4j Browser
kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687
```

## Uninstalling the Operator

> **Warning**: Deleting CRDs cascades to every Neo4j instance the operator manages. Back up first.

```bash
# Helm install — Methods 1, 2, 4
helm uninstall neo4j-operator --namespace neo4j-operator-system

# kubectl-apply install — Method 3
kubectl delete -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v1.10.0/neo4j-kubernetes-operator-complete.yaml

# git-clone + make install — Method 5
make undeploy-prod   # or undeploy-dev

# CRDs (any install method) — only after all Neo4j instances are deleted
kubectl get crd -o name | grep neo4j.neo4j.com | xargs kubectl delete

# Namespace (optional)
kubectl delete namespace neo4j-operator-system
```

## Troubleshooting Installation

### Common Issues

#### 1. CRDs Not Installing
```bash
# Check if CRDs exist
kubectl get crd | grep neo4j

# If missing, install CRDs manually
make install
```

#### 2. Operator Pod Not Starting
```bash
# Check operator logs
kubectl logs -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator

# Check operator pod events
kubectl describe pod -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator
```

#### 3. RBAC Permission Issues
```bash
# Check if ServiceAccount exists
kubectl get sa -n neo4j-operator-system

# Check ClusterRole and ClusterRoleBinding
kubectl get clusterrole | grep neo4j-operator
kubectl get clusterrolebinding | grep neo4j-operator
```

#### 4. Image Pull Errors

If the operator pod shows `ImagePullBackOff` or `ErrImagePull`:

```bash
# Check pod events for image pull errors
kubectl describe pod -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator

# Verify image exists and is accessible
docker pull ghcr.io/neo4j-partners/neo4j-kubernetes-operator:latest

# For private registries, create image pull secret
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<github-token> \
  --namespace=neo4j-operator-system

# Add imagePullSecrets to deployment
kubectl patch deployment neo4j-operator-controller-manager \
  -n neo4j-operator-system \
  --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/imagePullSecrets", "value": [{"name": "ghcr-secret"}]}]'
```

**Note**: The ghcr.io images are public for this operator, so authentication should not be required for pulling images.

#### 5. Manifest URL Not Found (404 Error)

If you get a 404 error when applying manifests from GitHub releases:

```bash
# Verify the release exists
gh release list --repo neo4j-partners/neo4j-kubernetes-operator

# Or visit: https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases

# Ensure you're using the correct version format (with 'v' prefix)
# Correct: v1.0.0
# Incorrect: 1.0.0
```

### Installation Requirements

- **Kubernetes**: Version 1.32 or higher
- **Neo4j**: Version 5.26 LTS (the final SemVer release) or any CalVer release (2025.x, 2026.x, and onward)
- **cert-manager**: Version 1.20+ (optional, only required for Neo4j deployments that use cert-manager TLS, i.e. `spec.tls.mode: cert-manager`)
- **Permissions**: Cluster-admin access for CRD and RBAC installation

> **cert-manager install order**: The operator installs and runs fine without cert-manager. It only watches cert-manager `Certificate` resources when the cert-manager CRDs are present *at operator startup*. If you install cert-manager **after** the operator, restart the operator so the watch becomes active:
> ```
> kubectl rollout restart deployment/neo4j-operator-controller-manager -n neo4j-operator-system
> ```
> If you know you'll use cert-manager TLS, install cert-manager **before** the operator to avoid the restart.

> **OpenShift note**: Clusters enforcing SCCs with allocated UID/FSGroup ranges should disable the chart’s pod security context so SCC can inject IDs, then bind an appropriate SCC (e.g., `restricted`) to the operator service account:
> ```
> helm install neo4j-operator ./charts/neo4j-operator \
>   --namespace neo4j-operator-system \
>   --create-namespace \
>   --set podSecurityContextEnabled=false
>
> oc adm policy add-scc-to-user restricted -z neo4j-operator -n neo4j-operator-system
> ```

### Installing via OLM on OpenShift

- Build/push a bundle and catalog (see `docs/developer_guide/openshift_olm.md`).
- Apply the sample `CatalogSource` and `Subscription` under `config/samples/olm/` after updating the catalog image and channel to match your bundle.

### Next Steps

Once installed, see:
- [Getting Started Guide](getting_started.md) - Deploy your first Neo4j instance
- [Configuration Guide](configuration.md) - Detailed configuration options
- [Examples](https://github.com/neo4j-partners/neo4j-kubernetes-operator/tree/main/examples) - Ready-to-use configurations
