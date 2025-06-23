# kubectl-neo4j - Neo4j Operator kubectl Plugin

A comprehensive kubectl plugin for managing Neo4j Enterprise clusters with the Neo4j Operator for Kubernetes.

## Overview

The `kubectl-neo4j` plugin provides a user-friendly command-line interface for managing Neo4j Enterprise clusters, backups, plugins, users, and monitoring operations. It simplifies common tasks and provides powerful troubleshooting capabilities.

## Features

### 🏗️ **Cluster Management**

- Create, scale, and delete Neo4j Enterprise clusters
- Health checks and status monitoring
- Configuration validation
- Log retrieval and analysis

### 💾 **Backup & Restore**

- Create scheduled and one-time backups
- Restore from backups or cloud storage
- Support for S3, GCS, and Azure storage
- Backup status monitoring and management

### 🔌 **Plugin Management**

- Install and manage Neo4j plugins (APOC, GDS, etc.)
- Automatic dependency resolution
- Plugin configuration management
- Version updates and rollbacks

### 👥 **User Management**

- Create and manage Neo4j users
- Role assignment and management
- Password secret integration

### 📊 **Monitoring & Observability**

- Real-time metrics and performance monitoring
- Resource usage tracking
- Event monitoring
- Query performance analysis

### 🔧 **Troubleshooting**

- Comprehensive diagnostics
- Connectivity testing
- Resource allocation analysis
- Configuration validation

## Installation

### Prerequisites

- Kubernetes cluster with kubectl configured
- Neo4j Operator installed in the cluster
- Go 1.21+ (for building from source)

### Install from Release

```bash
# Download the latest release for your platform
curl -LO https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/kubectl-neo4j-$(uname -s)-$(uname -m)

# Make it executable
chmod +x kubectl-neo4j-*

# Move to your PATH
sudo mv kubectl-neo4j-* /usr/local/bin/kubectl-neo4j
```

### Build from Source

```bash
# Clone the repository
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator/cmd/kubectl-neo4j

# Build and install
make install

# Or build for all platforms
make build-all
```

### Verify Installation

```bash
kubectl neo4j version
kubectl neo4j --help
```

## Quick Start

### 1. List Existing Clusters

```bash
kubectl neo4j cluster list
```

### 2. Create a Simple Cluster

```bash
kubectl neo4j cluster create my-cluster \
  --primaries=3 \
  --secondaries=2 \
  --storage-size=10Gi \
  --wait
```

### 3. Check Cluster Health

```bash
kubectl neo4j cluster health my-cluster
```

### 4. Create a Backup

```bash
kubectl neo4j backup create my-backup \
  --cluster=my-cluster \
  --storage=s3 \
  --bucket=my-backups \
  --schedule="0 2 * * *"
```

### 5. Install a Plugin

```bash
kubectl neo4j plugin install apoc \
  --cluster=my-cluster \
  --version=5.26.0 \
  --wait
```

## Command Reference

### Cluster Commands

```bash
# List clusters
kubectl neo4j cluster list [--all-namespaces] [--output=wide|json|yaml]

# Get cluster details
kubectl neo4j cluster get <cluster-name> [--output=json|yaml]

# Create cluster
kubectl neo4j cluster create <cluster-name> [OPTIONS]

# Scale cluster
kubectl neo4j cluster scale <cluster-name> --primaries=N [--secondaries=N]

# Delete cluster
kubectl neo4j cluster delete <cluster-name> [--force]

# Check health
kubectl neo4j cluster health <cluster-name> [--detailed]

# Get status
kubectl neo4j cluster status <cluster-name>

# Get logs
kubectl neo4j cluster logs <cluster-name> [--node=NODE] [--follow] [--tail=N]
```

### Backup Commands

```bash
# List backups
kubectl neo4j backup list [--cluster=CLUSTER] [--all-namespaces]

# Create backup
kubectl neo4j backup create <backup-name> --cluster=CLUSTER [OPTIONS]

# Get backup details
kubectl neo4j backup get <backup-name>

# Delete backup
kubectl neo4j backup delete <backup-name> [--force]

# Show backup status
kubectl neo4j backup status <backup-name>
```

### Restore Commands

```bash
# List restores
kubectl neo4j restore list [--cluster=CLUSTER] [--all-namespaces]

# Create restore
kubectl neo4j restore create <restore-name> --cluster=CLUSTER [OPTIONS]

# Get restore details
kubectl neo4j restore get <restore-name>

# Delete restore
kubectl neo4j restore delete <restore-name> [--force]

# Show restore status
kubectl neo4j restore status <restore-name>
```

### Plugin Commands

```bash
# List plugins
kubectl neo4j plugin list [--cluster=CLUSTER] [--all-namespaces]

# Install plugin
kubectl neo4j plugin install <plugin-name> --cluster=CLUSTER --version=VERSION [OPTIONS]

# Get plugin details
kubectl neo4j plugin get <plugin-name>

# Update plugin
kubectl neo4j plugin update <plugin-name> [--version=VERSION] [--config=KEY=VALUE]

# Uninstall plugin
kubectl neo4j plugin uninstall <plugin-name> [--force]

# Show plugin status
kubectl neo4j plugin status <plugin-name>
```

### User Commands

```bash
# List users
kubectl neo4j user list [--cluster=CLUSTER] [--all-namespaces]

# Create user
kubectl neo4j user create <username> --cluster=CLUSTER --password-secret=SECRET [OPTIONS]

# Get user details
kubectl neo4j user get <user-name>

# Delete user
kubectl neo4j user delete <user-name> [--force]
```

### Monitor Commands

```bash
# Show cluster metrics
kubectl neo4j monitor metrics --cluster=CLUSTER [--follow] [--interval=30s]

# Show performance statistics
kubectl neo4j monitor performance --cluster=CLUSTER [--duration=15m]

# Show cluster events
kubectl neo4j monitor events --cluster=CLUSTER [--follow] [--tail=50]

# Show resource usage
kubectl neo4j monitor top --cluster=CLUSTER [--interval=5s]
```

### Troubleshoot Commands

```bash
# Run comprehensive diagnostics
kubectl neo4j troubleshoot diagnose --cluster=CLUSTER [--detailed]

# Test connectivity
kubectl neo4j troubleshoot connectivity --cluster=CLUSTER [--endpoint=bolt|http|https]

# Check resource allocation
kubectl neo4j troubleshoot resources --cluster=CLUSTER

# Validate configuration
kubectl neo4j troubleshoot config --cluster=CLUSTER
```

## Configuration Examples

### Production Cluster with Auto-scaling

```bash
kubectl neo4j cluster create production \
  --primaries=3 \
  --secondaries=2 \
  --storage-size=100Gi \
  --storage-class=fast-ssd \
  --cpu-request=2 \
  --memory-request=8Gi \
  --cpu-limit=4 \
  --memory-limit=16Gi \
  --enable-autoscaling \
  --max-primaries=5 \
  --max-secondaries=6 \
  --enable-tls \
  --wait
```

### Multi-Zone Deployment

```bash
kubectl neo4j cluster create multi-zone \
  --primaries=3 \
  --secondaries=3 \
  --storage-size=50Gi \
  --zone-distribution \
  --anti-affinity=hard \
  --wait
```

### Backup with Retention Policy

```bash
kubectl neo4j backup create daily-backup \
  --cluster=production \
  --storage=s3 \
  --bucket=neo4j-backups \
  --path=/production/daily \
  --schedule="0 2 * * *" \
  --retention-days=30 \
  --compress \
  --verify
```

### Plugin Installation with Configuration

```bash
kubectl neo4j plugin install apoc \
  --cluster=production \
  --version=5.26.0 \
  --config=apoc.export.file.enabled=true \
  --config=apoc.import.file.enabled=true \
  --config=apoc.uuid.enabled=true \
  --wait
```

## Advanced Usage

### Batch Operations

```bash
# Create multiple clusters
for env in dev staging prod; do
  kubectl neo4j cluster create $env-cluster \
    --primaries=3 \
    --storage-size=20Gi \
    --wait
done

# Install plugins on all clusters
kubectl neo4j cluster list -o json | \
  jq -r '.items[].metadata.name' | \
  xargs -I {} kubectl neo4j plugin install apoc \
    --cluster={} \
    --version=5.26.0
```

### Monitoring Automation

```bash
# Continuous health monitoring
while true; do
  kubectl neo4j cluster list --output=wide
  kubectl neo4j monitor metrics --cluster=production
  sleep 30
done
```

### Backup Automation

```bash
# Create backup before maintenance
kubectl neo4j backup create pre-maintenance-$(date +%Y%m%d) \
  --cluster=production \
  --storage=s3 \
  --bucket=maintenance-backups \
  --wait

# Verify backup completed successfully
kubectl neo4j backup status pre-maintenance-$(date +%Y%m%d)
```

## Troubleshooting

### Common Issues

1. **Plugin Installation Fails**

   ```bash
   kubectl neo4j troubleshoot diagnose --cluster=my-cluster --detailed
   kubectl neo4j plugin status failed-plugin
   ```

2. **Cluster Not Ready**

   ```bash
   kubectl neo4j cluster health my-cluster
   kubectl neo4j troubleshoot resources --cluster=my-cluster
   kubectl neo4j cluster logs my-cluster --tail=100
   ```

3. **Backup Failures**

   ```bash
   kubectl neo4j backup status my-backup
   kubectl neo4j troubleshoot connectivity --cluster=my-cluster
   ```

4. **Performance Issues**

   ```bash
   kubectl neo4j monitor performance --cluster=my-cluster --duration=1h
   kubectl neo4j monitor top --cluster=my-cluster
   ```

### Debug Mode

Enable verbose output for debugging:

```bash
export KUBECTL_NEO4J_DEBUG=true
kubectl neo4j cluster create debug-cluster --dry-run
```

## Contributing

We welcome contributions! Please see our [Contributing Guide](../../CONTRIBUTING.md) for details.

### Development Setup

```bash
# Clone and setup
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator/cmd/kubectl-neo4j

# Install dependencies
make deps

# Run tests
make test

# Build
make build

# Install locally
make install
```

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](../../LICENSE) file for details.

## Support

- **Documentation**: [Neo4j Operator Docs](../../docs/)
- **Issues**: [GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)
- **Community**: [Neo4j Community Forum](https://community.neo4j.com/)
- **Enterprise Support**: Contact Neo4j Support

## Changelog

See [CHANGELOG.md](./CHANGELOG.md) for release notes and version history.
