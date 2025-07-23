# Neo4j Enterprise Operator for Kubernetes

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/neo4j-labs/neo4j-kubernetes-operator)](https://goreportcard.com/report/github.com/neo4j-labs/neo4j-kubernetes-operator)
[![GitHub Release](https://img.shields.io/github/release/neo4j-labs/neo4j-kubernetes-operator.svg)](https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases)

The Neo4j Enterprise Operator for Kubernetes provides a complete solution for deploying, managing, and scaling Neo4j Enterprise instances (v5.26+) in Kubernetes environments. Built with the Kubebuilder framework, it supports both clustered and standalone deployments for cloud-native graph database operations.

> ⚠️ **ALPHA SOFTWARE WARNING**: This operator is currently in **alpha stage**. There may be breaking changes at any time due to ongoing development. For production use or evaluation, please use the [latest alpha release](https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest) rather than the main branch code.

## 📋 Requirements

- **Neo4j**: Version 5.26 or higher (supports both SemVer 5.x and CalVer 2025.x formats)
- **Kubernetes**: Version 1.21 or higher
- **Go**: Version 1.21+ (for development)
- **cert-manager**: Version 1.5+ (tested with v1.18.2, required for TLS/SSL features)

## 🚀 Quick Start

### For Kubernetes Beginners

If you're new to Kubernetes, start here:

1. **Install the operator** using the latest release:
   ```bash
   # Install the CRDs and operator
   kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml
   ```

2. **Create admin credentials**:
   ```bash
   kubectl create secret generic neo4j-admin-secret \
     --from-literal=username=neo4j \
     --from-literal=password=your-secure-password
   ```

3. **Deploy your first Neo4j instance**:

   **For single-node development** (non-clustered):
   ```bash
   # Download the example from the latest release
   curl -L https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/examples-standalone-single-node.yaml -o standalone.yaml
   kubectl apply -f standalone.yaml
   ```

   **For clustered deployment** (production):
   ```bash
   # Download the example from the latest release
   curl -L https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/examples-cluster-minimal.yaml -o cluster.yaml
   kubectl apply -f cluster.yaml
   ```

4. **Access your Neo4j instance**:
   ```bash
   # For standalone deployment
   kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687

   # For cluster deployment
   kubectl port-forward svc/minimal-cluster-client 7474:7474 7687:7687
   ```
   Open http://localhost:7474 in your browser.

### For Kubernetes Experts

Jump right in with advanced configurations:

```bash
# Install the operator from the latest release
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml

# Deploy production cluster with high availability
# First, clone the release or download specific example files
git clone --branch $(curl -s https://api.github.com/repos/neo4j-labs/neo4j-kubernetes-operator/releases/latest | grep tag_name | cut -d '"' -f 4) https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator
kubectl apply -f examples/clusters/multi-primary-cluster.yaml
```

See the [Complete Installation Guide](docs/user_guide/installation.md) for all deployment options.

## 💡 Examples

Ready-to-use configurations for common deployment scenarios:

### Standalone Deployments (Single-Node, Non-Clustered)
- **[Single-node standalone](examples/standalone/single-node-standalone.yaml)** - Development and testing

### Clustered Deployments (Enterprise Clustering)
- **[Minimal cluster](examples/clusters/minimal-cluster.yaml)** - 1 primary + 1 secondary (minimum cluster topology)
- **[Multi-primary cluster](examples/clusters/multi-primary-cluster.yaml)** - Production with high availability
- **[Kubernetes discovery cluster](examples/clusters/k8s-discovery-cluster.yaml)** - Production with automatic discovery

See the [examples directory](examples/) for complete documentation and additional configurations.

## 📚 Documentation Structure

### 👥 User Guides
- **[Getting Started](docs/user_guide/getting_started.md)** - Installation and first cluster
- **[Installation](docs/user_guide/installation.md)** - All installation methods
- **[Configuration](docs/user_guide/configuration.md)** - Complete configuration reference
- **[Clustering](docs/user_guide/clustering.md)** - High availability setup
- **[Security Guide](docs/user_guide/guides/security.md)** - Authentication, TLS, and RBAC
- **[Backup & Restore](docs/user_guide/guides/backup_restore.md)** - Data protection strategies
- **[Performance Tuning](docs/user_guide/guides/performance.md)** - Optimization techniques
- **[Monitoring](docs/user_guide/guides/monitoring.md)** - Observability and alerting
- **[Upgrades](docs/user_guide/guides/upgrades.md)** - Neo4j version upgrades

### 🔧 Developer & Contributor Guides
- **[Architecture Overview](docs/developer_guide/architecture.md)** - System design and components
- **[Development Setup](docs/developer_guide/development.md)** - Local development environment
- **[Testing Guide](docs/developer_guide/testing.md)** - Test strategy and execution
- **[Contributing](docs/developer_guide/contributing.md)** - How to contribute code

### 📖 API Reference
Complete CRD documentation for all custom resources:
- [Neo4jEnterpriseCluster](docs/api_reference/neo4jenterprisecluster.md) - Clustered deployments
- [Neo4jEnterpriseStandalone](docs/api_reference/neo4jenterprisestandalone.md) - Single-node deployments
- [Neo4jBackup](docs/api_reference/neo4jbackup.md) & [Neo4jRestore](docs/api_reference/neo4jrestore.md)
- [Neo4jDatabase](docs/api_reference/neo4jdatabase.md)
- [Neo4jPlugin](docs/api_reference/neo4jplugin.md)

## ✨ Key Features

### 🏗️ Core Capabilities
- **Dual Deployment Modes**: Choose between clustered (Neo4jEnterpriseCluster) or standalone (Neo4jEnterpriseStandalone) deployments
- **Enterprise Clusters**: Deploy Neo4j Enterprise with clustering support (minimum 1 primary + 1 secondary or multiple primaries)
- **Standalone Deployments**: Single-node Neo4j instances using unified clustering for development and testing
- **High Availability**: Multi-replica clusters with automatic leader election and V2_ONLY discovery
- **Persistent Storage**: Configurable storage classes and volume management
- **Rolling Updates**: Zero-downtime Neo4j version upgrades

### 🔐 Security & Authentication
- **TLS/SSL**: Configurable TLS encryption for client and cluster communications
- **Authentication**: Support for LDAP, OIDC, and native authentication
- **Automatic RBAC**: Operator automatically creates all necessary RBAC resources for backups
- **Network Policies**: Pod-to-pod communication security

### 🚀 Operations & Automation
- **Automated Backups**: Scheduled backups with automatic RBAC management
- **Point-in-Time Recovery**: Restore clusters to specific timestamps with `--restore-until`
- **Database Management**: Create databases with IF NOT EXISTS, WAIT/NOWAIT, and topology constraints
- **Version-Aware Operations**: Automatic detection and adaptation for Neo4j 5.26.x and 2025.x
- **Plugin Management**: Install and configure Neo4j plugins (APOC, GDS, etc.)
- **Query Monitoring**: Performance monitoring and slow query detection

### ⚡ Performance & Efficiency
- **Optimized Reconciliation**: Intelligent rate limiting reduces API calls by 99.8% (18,000+ to ~34 per minute)
- **Smart Status Updates**: Status updates only when cluster state actually changes
- **ConfigMap Debouncing**: 2-minute debounce prevents restart loops from configuration changes
- **Resource Validation**: Automatic validation ensures optimal Neo4j memory settings
- **Built-in Monitoring**: Real-time resource monitoring with operational insights

### 🔧 Deployment Management
Manage your Neo4j deployments using standard kubectl commands:
```bash
# For clustered deployments
kubectl get neo4jenterprisecluster
kubectl describe neo4jenterprisecluster my-cluster

# For standalone deployments
kubectl get neo4jenterprisestandalone
kubectl describe neo4jenterprisestandalone my-standalone

# Operator logs
kubectl logs -l app.kubernetes.io/name=neo4j-operator
```

## 🏃‍♂️ Common Use Cases

### Development & Testing
- **Standalone deployments** for development environments (non-clustered)
- **Minimal clusters** for integration testing (1 primary + 1 secondary)
- **Ephemeral deployments** for CI/CD pipelines
- **Sample data loading** for testing scenarios

### Production Deployments
- **High-availability clusters** across multiple availability zones
- **Automated backup strategies** with off-site storage
- **Performance monitoring** and alerting integration
- **Blue-green deployments** for zero-downtime upgrades

### Enterprise Features
- **LDAP/AD integration** for centralized authentication
- **Plugin ecosystem** (APOC, Graph Data Science, Bloom)
- **Compliance-ready** logging and auditing
- **Resource quotas** and governance controls

## 🎯 Recent Improvements

### Latest Version Enhancements
- **Automatic RBAC for Backups**: The operator now automatically creates all necessary RBAC resources (ServiceAccounts, Roles, RoleBindings) for backup operations - no manual configuration required
- **Enhanced Test Stability**: Improved integration test cleanup with automatic finalizer removal prevents namespace termination issues
- **Better Error Handling**: Fixed nil pointer dereferences and improved error messages for better troubleshooting
- **Improved TLS Cluster Formation**: Enhanced stability for TLS-enabled clusters during initial formation

### Developer Experience
- **Simplified Testing**: New test cleanup patterns and helpers make integration testing more reliable
- **Better Documentation**: Updated troubleshooting guides with common issues and solutions
- **CI/CD Ready**: GitHub workflows automatically handle RBAC generation and deployment

## 🤝 Contributing

We welcome contributions from both Kubernetes beginners and experts!

### Quick Contribution Setup
```bash
# Clone and setup development environment
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator
make dev-cluster  # Creates local Kind cluster
make test-unit    # Run unit tests
```

See the [Contributing Guide](docs/developer_guide/contributing.md) for detailed instructions.

## 📞 Support & Community

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)
- **Discussions**: [GitHub Discussions](https://github.com/neo4j-labs/neo4j-kubernetes-operator/discussions)
- **Neo4j Community**: [Neo4j Community Site](https://community.neo4j.com/)

## 📄 License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
