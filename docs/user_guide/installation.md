# Installation Guide

This guide provides detailed instructions for installing the Neo4j Enterprise Operator for Kubernetes.

## Quick Installation

### Method 1: Helm Chart Repository (Recommended)

> Available from v1.8.0 onwards. The chart repository is hosted at
> `https://priyolahiri.github.io/neo4j-kubernetes-operator/charts` and is
> updated automatically on every operator release.

```bash
helm repo add neo4j https://priyolahiri.github.io/neo4j-kubernetes-operator/charts
helm repo update

helm install neo4j-operator neo4j/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace
```

**Pin to a specific version**:
```bash
helm install neo4j-operator neo4j/neo4j-operator \
  --version 1.8.0 \
  --namespace neo4j-operator-system \
  --create-namespace
```

**Customise installation values**:
```bash
# View available configuration options
helm show values neo4j/neo4j-operator

helm install neo4j-operator neo4j/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace \
  --set resources.limits.memory=512Mi
```

**Upgrade**:
```bash
helm repo update
helm upgrade neo4j-operator neo4j/neo4j-operator \
  --namespace neo4j-operator-system
```

**Uninstall**:
```bash
helm uninstall neo4j-operator --namespace neo4j-operator-system
```

### Method 2: OCI Registry

Available for all releases (including pre-v1.8.0). Helm 3.8 or later is required for OCI support.

```bash
helm install neo4j-operator oci://ghcr.io/priyolahiri/charts/neo4j-operator \
  --version 1.8.0 \
  --namespace neo4j-operator-system \
  --create-namespace
```

Use the chart version without the `v` prefix (for example, `1.8.0`).

### Method 3: Quick Install from GitHub Release

For environments where running `helm` is inconvenient, every release also publishes a single kubectl-applyable YAML bundle:

```bash
RELEASE_VERSION=v1.8.0  # Replace with desired version

kubectl apply -f https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/neo4j-kubernetes-operator-complete.yaml
```

**What this installs**:
- Custom Resource Definitions (CRDs)
- Operator Deployment (multi-arch images from ghcr.io)
- All required RBAC permissions (ClusterRole, ClusterRoleBinding, ServiceAccount)
- Deployed to the `neo4j-operator-system` namespace

**To find available releases**:
```bash
# Visit: https://github.com/priyolahiri/neo4j-kubernetes-operator/releases
# Or use the GitHub CLI:
gh release list --repo priyolahiri/neo4j-kubernetes-operator
```

**CRDs only** (manage the operator deployment separately):
```bash
kubectl apply -f https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/neo4j-kubernetes-operator.yaml
```

**Supported architectures**: linux/amd64, linux/arm64

### Method 4: Helm from Cloned Repository

Useful for testing an unreleased commit or customising the chart locally.

```bash
git clone https://github.com/priyolahiri/neo4j-kubernetes-operator.git
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
git clone https://github.com/priyolahiri/neo4j-kubernetes-operator.git
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

## Development Installation

For active development and testing with local images:

```bash
# Clone and setup development environment
git clone https://github.com/priyolahiri/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Create development cluster (uses Kind)
make dev-cluster

# Deploy operator (recommended for in-cluster testing)
make operator-setup  # Automated setup (detects cluster and deploys)

# Manual deployment options:
make deploy-dev      # Development namespace with debug features (builds local image)
make deploy-prod     # Production namespace (builds local image)

# Registry-based deployment:
make deploy-dev-registry   # Deploy from registry (requires authentication)
make deploy-prod-registry  # Deploy from ghcr.io registry
```

## Advanced Installation Methods

### Custom Kustomize Configuration

For customized deployments with specific namespace, labels, or resource configurations:

```bash
# After cloning and checking out the latest tag
# Create your own kustomization
cat > kustomization.yaml << EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- config/default

# Customize namespace
namespace: my-neo4j-operator

# Add custom labels
commonLabels:
  environment: production
  team: database

# Customize images (optional)
images:
- name: ghcr.io/priyolahiri/neo4j-kubernetes-operator
  newTag: v1.0.0
EOF

# Apply custom configuration
kubectl apply -k .
```

### Alternative Deployment Configurations

After cloning the repository, you can use different deployment configurations based on your environment:

```bash
# Production deployment (optimized resource limits, no debug logging)
make deploy-prod           # Builds and deploys local image
# or
make deploy-prod-registry  # Deploy from ghcr.io registry

# Development deployment (with debug logging enabled, relaxed resource limits)
make deploy-dev            # Builds and deploys local dev image
# or
make deploy-dev-registry   # Deploy from registry
```

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
curl -LO https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/examples-${RELEASE_VERSION#v}.tar.gz

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
kubectl port-forward svc/standalone-neo4j-service 7474:7474
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
kubectl port-forward svc/standalone-neo4j-service 7474:7474
```

## Uninstalling the Operator

### Helm Uninstallation

If you installed via Helm (Methods 1, 2, or 4):

```bash
# Uninstall the Helm release
helm uninstall neo4j-operator --namespace neo4j-operator-system

# Optionally delete the namespace
kubectl delete namespace neo4j-operator-system

# Note: CRDs are not automatically deleted by Helm uninstall
# To remove CRDs manually (WARNING: This deletes all Neo4j instances):
kubectl delete crd neo4jbackups.neo4j.neo4j.com
kubectl delete crd neo4jdatabases.neo4j.neo4j.com
kubectl delete crd neo4jenterpriseclusters.neo4j.neo4j.com
kubectl delete crd neo4jenterprisestandalones.neo4j.neo4j.com
kubectl delete crd neo4jplugins.neo4j.neo4j.com
kubectl delete crd neo4jrestores.neo4j.neo4j.com
kubectl delete crd neo4jroles.neo4j.neo4j.com
kubectl delete crd neo4jrolebindings.neo4j.neo4j.com
kubectl delete crd neo4jusers.neo4j.neo4j.com
kubectl delete crd neo4jshardeddatabases.neo4j.neo4j.com
```

### Quick Install Uninstallation

If you installed using Method 3 (kubectl apply from a GitHub release):

```bash
# Get the release version you installed
RELEASE_VERSION=v1.8.0  # Replace with your installed version

# Delete the operator and CRDs
kubectl delete -f https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/${RELEASE_VERSION}/neo4j-kubernetes-operator-complete.yaml
```

**Warning**: Deleting CRDs will also delete all Neo4j instances managed by the operator.

### Make Targets Uninstallation

If you installed using Method 5 (Git Clone with Make):

```bash
# From the cloned repository directory
cd neo4j-kubernetes-operator

# Undeploy operator (keeps CRDs and Neo4j instances)
make undeploy-prod  # If you used deploy-prod
# or
make undeploy-dev   # If you used deploy-dev

# Uninstall CRDs (WARNING: This deletes all Neo4j instances)
make uninstall
```

**Complete Cleanup** (removes everything):
```bash
# Remove all Neo4j instances first (if you want to preserve data, backup first)
kubectl delete neo4jenterpriseclusters --all -A
kubectl delete neo4jenterprisestandalones --all -A

# Undeploy operator
make undeploy-prod  # or make undeploy-dev

# Uninstall CRDs
make uninstall

# Delete namespace (optional)
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
docker pull ghcr.io/priyolahiri/neo4j-kubernetes-operator:latest

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
gh release list --repo priyolahiri/neo4j-kubernetes-operator

# Or visit: https://github.com/priyolahiri/neo4j-kubernetes-operator/releases

# Ensure you're using the correct version format (with 'v' prefix)
# Correct: v1.0.0
# Incorrect: 1.0.0
```

### Installation Requirements

- **Kubernetes**: Version 1.30 or higher
- **Neo4j**: Version 5.26 LTS (the final SemVer release) or any CalVer release (2025.x, 2026.x, and onward)
- **cert-manager**: Version 1.20+ (optional, only required for TLS-enabled Neo4j deployments)
- **Permissions**: Cluster-admin access for CRD and RBAC installation

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
- [Examples](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples) - Ready-to-use configurations
