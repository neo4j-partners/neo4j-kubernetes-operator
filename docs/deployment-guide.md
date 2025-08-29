# Deployment Guide

This guide provides deployment options for the Neo4j Enterprise Operator, optimized for different environments and use cases.

## Deployment Options Overview

| Target | Make Command | Image Source | Use Case |
|--------|-------------|-------------|----------|
| **Local Development** | `make deploy-dev-local` | Built locally (`neo4j-operator:dev`) | Development with debug features |
| **Local Production-like** | `make deploy-prod-local` | Built locally (`neo4j-operator:latest`) | Local testing with prod settings |
| **Production** | `make deploy-prod` | ghcr.io registry | Production deployment (requires registry access) |
| **Development Overlay** | `make deploy-dev` | Pre-built `neo4j-operator:dev` | Development with custom image |

## Quick Start

### Local Development (Recommended)

For development and testing without external dependencies:

```bash
# Create development cluster
make dev-cluster

# Build and deploy operator with local image
make deploy-dev-local
```

This approach:
- ✅ Builds operator from your current code
- ✅ No registry authentication required
- ✅ Fast iteration cycle
- ✅ Complete control over image content

### Production Deployment

For production environments:

```bash
# Clone and install
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Install CRDs
make install

# Option 1: Local build (recommended for air-gapped or private environments)
make deploy-prod-local

# Option 2: Registry image (requires ghcr.io access)
make deploy-prod
```

## Deployment Details

### Local Image Deployment

The `-local` targets provide the most reliable deployment experience:

**`make deploy-dev-local`**:
- Builds `neo4j-operator:dev` image locally
- Loads image into Kind cluster automatically
- Deploys to `neo4j-operator-dev` namespace
- Enables debug features and development mode
- Includes `DEVELOPMENT_MODE=true` environment variable

**`make deploy-prod-local`**:
- Builds `neo4j-operator:latest` image locally
- Loads image into Kind cluster automatically
- Deploys to `neo4j-operator-system` namespace
- Production resource limits and settings
- No debug overhead

### Registry-Based Deployment

**`make deploy-prod`**:
- Uses `ghcr.io/neo4j-labs/neo4j-kubernetes-operator:latest`
- Requires authenticated access to ghcr.io
- Suitable for production environments with registry access
- Automatic updates when new versions are released

**`make deploy-dev`**:
- Uses local `neo4j-operator:dev` image
- Image must exist locally (build first with `make docker-build IMG=neo4j-operator:dev`)
- Development namespace with debug features

## Verification

After deployment, verify the operator is running:

```bash
# Check operator status
kubectl get deployments -n neo4j-operator-system  # for prod deployments
# or
kubectl get deployments -n neo4j-operator-dev     # for dev deployments

# Check operator logs
kubectl logs -f deployment/neo4j-operator-controller-manager -n <namespace>

# Verify CRDs are installed
kubectl get crd | grep neo4j
```

## Cleanup

To remove the operator and CRDs:

```bash
# Remove operator deployment
make undeploy-prod  # or make undeploy-dev

# Remove CRDs (this will delete all Neo4j instances)
make uninstall
```

## Troubleshooting

### Image Pull Issues

If you see `ImagePullBackOff` errors:

1. **For `-local` deployments**: Ensure Kind cluster is running and image was loaded successfully
2. **For registry deployments**: Check ghcr.io access and authentication
3. **Solution**: Use local deployment options (`make deploy-*-local`)

### Namespace Issues

- Production deployments use `neo4j-operator-system` namespace
- Development deployments use `neo4j-operator-dev` namespace
- Integration tests expect operator in `neo4j-operator-system`

### Best Practices

- ✅ Use `make deploy-*-local` for development and testing
- ✅ Test locally before deploying to production
- ✅ Use production overlays (`make deploy-prod-local`) for staging environments
- ✅ Keep operator and Neo4j versions compatible (5.26+ required)
