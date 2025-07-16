# Neo4j Kubernetes Operator Demo

This directory contains comprehensive demo scripts that showcase the capabilities of the Neo4j Kubernetes Operator.

## Quick Start

### Option 1: Complete Setup + Demo
```bash
# Set up environment and run interactive demo
make demo-setup
make demo

# Or run fast automated demo
make demo-fast
```

### Option 2: Manual Setup
```bash
# If you already have a cluster with the operator
./scripts/demo.sh

# Skip confirmations for presentations
./scripts/demo.sh --skip-confirmations

# Fast mode for quick demonstrations
./scripts/demo.sh --speed fast --skip-confirmations
```

## Demo Features

### 🎯 **Part 1: Single-Node Cluster**
- Perfect for development and testing
- Simple deployment model
- Resource efficient
- Uses unified clustering infrastructure (Neo4j 5.26+)

### 🎯 **Part 2: Multi-Node TLS Cluster**
- Production-ready high availability
- 3-node cluster with Raft consensus
- Automatic TLS certificate management via cert-manager
- End-to-end encryption
- Sequential pod startup demonstration

## Demo Configuration

### Environment Variables
```bash
DEMO_NAMESPACE=default          # Kubernetes namespace
ADMIN_PASSWORD=demo123456       # Neo4j admin password
SKIP_CONFIRMATIONS=false        # Skip interactive prompts
DEMO_SPEED=normal              # fast, normal, slow
```

### Command Line Options
```bash
./scripts/demo.sh [options]

Options:
  --namespace NAMESPACE     Kubernetes namespace (default: default)
  --password PASSWORD       Admin password (default: demo123456)
  --skip-confirmations     Skip interactive confirmations
  --speed SPEED            Demo speed: fast, normal, slow (default: normal)
  --help, -h               Show help
```

## Demo Scenarios

### 🎬 **Interactive Presentation Mode**
Perfect for live audiences with explanations and Q&A:
```bash
make demo
# or
./scripts/demo.sh --speed slow
```

### ⚡ **Fast Automated Mode**
Great for CI/CD validation or quick showcases:
```bash
make demo-fast
# or
./scripts/demo.sh --skip-confirmations --speed fast
```

### 🔧 **Custom Configuration**
Customize for specific environments:
```bash
./scripts/demo.sh \
  --namespace production \
  --password secure123 \
  --speed normal
```

## What the Demo Shows

### 📊 **Operator Capabilities**
- ✅ Automatic resource creation (StatefulSets, Services, ConfigMaps)
- ✅ TLS certificate management with cert-manager integration
- ✅ Sequential cluster formation for data consistency
- ✅ Production-ready configuration templates
- ✅ Health monitoring and readiness checks

### 🔐 **Security Features**
- ✅ TLS encryption for all communication
- ✅ Automatic certificate rotation
- ✅ Secure credential management
- ✅ Network policy ready configuration

### 🚀 **Production Readiness**
- ✅ High availability with multi-node clustering
- ✅ Persistent storage for data durability
- ✅ Resource management and limits
- ✅ Monitoring and metrics endpoints
- ✅ Proper service discovery

## Demo Output

The demo provides rich, colorized output including:

- **Progress indicators** for long-running operations
- **Real-time cluster formation** monitoring
- **Visual status displays** for all resources
- **Connection information** with examples
- **Educational explanations** of each step

### Sample Output
```
╔══════════════════════════════════════════════════════════════════════════════╗
║ DEMO PART 1: Single-Node Neo4j Cluster                                     ║
╚══════════════════════════════════════════════════════════════════════════════╝

[DEMO] We'll start with a simple single-node Neo4j cluster for development...
[INFO] The operator is now creating the following resources:
  • StatefulSet with 1 replica
  • Client and headless services
  • ConfigMap with Neo4j configuration
  • PersistentVolumeClaim for data storage

Waiting for cluster initialization...Done!
[SUCCESS] Single-node cluster is ready!
```

## Prerequisites

The demo requires:

1. **Kubernetes cluster** (Kind cluster recommended)
2. **cert-manager** with ClusterIssuer (auto-installed in dev clusters)
3. **Neo4j Kubernetes Operator** (deployed to dev cluster)
4. **kubectl** configured for cluster access

Use `make demo-setup` to automatically configure all prerequisites.

### 📋 **Cluster Management**

The demo system now uses **intelligent cluster detection**:

- ✅ `make demo-setup` destroys any existing dev/test clusters
- ✅ Creates fresh `neo4j-operator-dev` Kind cluster
- ✅ **Smart operator deployment**: Automatically detects available clusters
- ✅ **Flexible targeting**: Works with dev cluster, test cluster, or both

#### **🎯 Operator Deployment Logic**

The `make operator-setup` target now intelligently handles deployment:

| Scenario | Behavior |
|----------|----------|
| **Only dev cluster** | Deploys to dev cluster |
| **Only test cluster** | Deploys to test cluster |
| **Both clusters exist** | Prefers dev cluster (demo-friendly) |
| **No clusters** | Shows helpful error with setup instructions |

#### **🔧 Manual Operator Control**

```bash
make operator-setup                 # Automated (prefers dev cluster)
make operator-setup-interactive     # Interactive choice for multiple clusters
make operator-status               # Show status across all clusters
make operator-logs                 # Follow logs from any cluster
./scripts/setup-operator.sh cleanup # Remove operator from all clusters
```

This ensures a clean, predictable environment that adapts to your setup.

## Troubleshooting

### Common Issues

**Demo fails with "ca-cluster-issuer not found":**
```bash
# Ensure cert-manager and ClusterIssuer are installed
make dev-cluster
```

**Pods stuck in Pending:**
```bash
# Check storage and resources
kubectl describe pods -l neo4j.com/cluster=neo4j-single
```

**TLS certificate issues:**
```bash
# Check cert-manager status
kubectl get certificates,certificaterequests -A
kubectl describe certificate -A
```

### Debug Mode
```bash
# Run with debug output
./scripts/demo.sh --speed slow 2>&1 | tee demo.log
```

## Cleanup

After the demo:
```bash
# Remove demo clusters
kubectl delete neo4jenterprisecluster neo4j-single neo4j-cluster

# Full environment cleanup
make dev-destroy
```

## Customization

The demo scripts are designed to be easily customized:

- **Modify cluster configurations** in `deploy_single_node()` and `deploy_multi_node_tls()`
- **Adjust timing** by changing `PAUSE_*` variables
- **Add custom explanations** in the `log_demo()` calls
- **Extend with additional scenarios** by adding new functions

## Support

For issues or questions:
- Check the [main documentation](../docs/)
- Review [troubleshooting guides](../docs/user_guide/guides/troubleshooting.md)
- File issues on the project repository
