# Installation Guide

This guide provides detailed instructions for installing the Neo4j Enterprise Operator for Kubernetes. The operator is distributed as release tarballs and Kubernetes manifests.

## Quick Installation

### Method 1: Direct Manifest (Recommended)

The fastest way to get started:

```bash
# Install complete operator (CRDs + operator + RBAC)
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator-complete.yaml
```

This single command installs:
- Custom Resource Definitions (CRDs)
- Operator Deployment
- All required RBAC permissions
- ServiceAccount and ClusterRole bindings

### Method 2: CRDs Only

If you want to install just the CRDs (for custom operator deployments):

```bash
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml
```

## Advanced Installation Methods

### Method 3: From Release Tarball

For customization or offline installation:

```bash
# Get the latest release version
LATEST_RELEASE=$(curl -s https://api.github.com/repos/neo4j-labs/neo4j-kubernetes-operator/releases/latest | grep 'tag_name' | cut -d '"' -f 4)
CLEAN_VERSION=${LATEST_RELEASE#v}  # Remove 'v' prefix

# Download the source tarball
wget https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/download/${LATEST_RELEASE}/neo4j-kubernetes-operator-${CLEAN_VERSION}.tar.gz

# Extract and install
tar -xzf neo4j-kubernetes-operator-${CLEAN_VERSION}.tar.gz
cd neo4j-kubernetes-operator-${CLEAN_VERSION}

# Install using kustomize
kubectl apply -k config/default
```

### Method 4: Custom Kustomize Configuration

After extracting the tarball, you can customize the installation:

```bash
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

## Release Assets

Each release provides several assets:

| Asset | Description | Use Case |
|-------|-------------|----------|
| `neo4j-kubernetes-operator-{version}.tar.gz` | Complete source code tarball | Development, customization, offline installation |
| `neo4j-kubernetes-operator-complete.yaml` | Complete operator (CRDs + Operator + RBAC) | Quick production deployment |
| `neo4j-kubernetes-operator.yaml` | Custom Resource Definitions only | Custom operator deployments |
| `examples-{version}.tar.gz` | Example configurations archive | Getting started, reference implementations |
| Individual CRD files | `config/crd/bases/*.yaml` | Selective CRD installation |

## Getting Started with Examples

After installing the operator, download examples:

```bash
# Download examples
LATEST_RELEASE=$(curl -s https://api.github.com/repos/neo4j-labs/neo4j-kubernetes-operator/releases/latest | grep 'tag_name' | cut -d '"' -f 4)
CLEAN_VERSION=${LATEST_RELEASE#v}  # Remove 'v' prefix
wget https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/download/${LATEST_RELEASE}/examples-${CLEAN_VERSION}.tar.gz
tar -xzf examples-${CLEAN_VERSION}.tar.gz

# Create admin secret (required for Neo4j authentication)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password

# Deploy your first Neo4j instance
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Check deployment status
kubectl get neo4jenterprisestandalone
kubectl get pods
```

## Troubleshooting Installation

### Common Issues

#### 1. CRDs Not Installing
```bash
# Check if CRDs exist
kubectl get crd | grep neo4j

# If missing, install CRDs manually
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml
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

#### 4. Webhook Certificate Issues
```bash
# Check if cert-manager is installed (required for TLS features)
kubectl get pods -n cert-manager

# If not installed, install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml
```

### Installation Requirements

- **Kubernetes**: Version 1.21 or higher
- **Neo4j**: Version 5.26+ (supports both SemVer 5.x and CalVer 2025.x formats)
- **cert-manager**: Version 1.5+ (for TLS/SSL features)
- **Permissions**: Cluster-admin access for CRD and RBAC installation

### Next Steps

Once installed, see:
- [Getting Started Guide](getting_started.md) - Deploy your first Neo4j instance
- [Configuration Guide](configuration.md) - Detailed configuration options
- [Examples](../../examples/README.md) - Ready-to-use configurations
