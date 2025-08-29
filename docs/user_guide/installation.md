# Installation Guide

This guide provides detailed instructions for installing the Neo4j Enterprise Operator for Kubernetes. Since this is a private repository, installation requires cloning from source.

## Quick Installation

### Method 1: Git Clone (Recommended)

The primary installation method using git clone:

```bash
# Clone the repository
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Checkout the latest release tag
LATEST_TAG=$(git describe --tags --abbrev=0)
git checkout $LATEST_TAG

# Install CRDs and operator
make install      # Install CRDs
make deploy-prod  # Deploy operator (builds and uses local image)
# or (requires ghcr.io access)
make deploy-prod-registry  # Deploy from ghcr.io registry
```

This installs:
- Custom Resource Definitions (CRDs)
- Operator Deployment
- All required RBAC permissions
- ServiceAccount and ClusterRole bindings

### Method 2: Development Installation

For development and testing with local images:

```bash
# Clone and setup development environment
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Create development cluster
make dev-cluster

# Deploy operator (uses local image by default)
make deploy-dev   # Development namespace with debug features
# or
make deploy-prod  # Production namespace

# Alternative: Use automated setup (detects cluster and deploys)
make operator-setup
```

## Advanced Installation Methods

### Method 3: Custom Kustomize Configuration

For customized deployments:

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
EOF

# Apply custom configuration
kubectl apply -k .
```

### Alternative Deployment Options

After cloning the repository, you can use different deployment configurations:

```bash
# Production deployment (optimized resource limits)
make deploy-prod

# Development deployment (with debugging enabled)
make deploy-dev

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
| `make fmt` | Format code |
| `make lint` | Run linter |
| `make vet` | Run go vet |
| `make test-coverage` | Generate test coverage report |

## Getting Started with Examples

After installing the operator, examples are available in the local `examples/` directory:

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


### Installation Requirements

- **Kubernetes**: Version 1.21 or higher
- **Neo4j**: Version 5.26+ (supports both SemVer 5.x and CalVer 2025.x formats)
- **cert-manager**: Version 1.5+ (optional, only required for TLS-enabled Neo4j deployments)
- **Permissions**: Cluster-admin access for CRD and RBAC installation

### Next Steps

Once installed, see:
- [Getting Started Guide](getting_started.md) - Deploy your first Neo4j instance
- [Configuration Guide](configuration.md) - Detailed configuration options
- [Examples](../../examples/README.md) - Ready-to-use configurations
