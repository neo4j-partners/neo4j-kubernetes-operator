# Neo4j Enterprise Operator for Kubernetes

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/neo4j-labs/neo4j-kubernetes-operator)](https://goreportcard.com/report/github.com/neo4j-labs/neo4j-kubernetes-operator)
[![GitHub Release](https://img.shields.io/github/release/neo4j-labs/neo4j-kubernetes-operator.svg)](https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases)

The Neo4j Enterprise Operator for Kubernetes provides a complete solution for deploying, managing, and scaling Neo4j Enterprise clusters (v5.26+) in Kubernetes environments. Built with the Kubebuilder framework, it enables cloud-native operations for Neo4j graph databases.

## 📋 Requirements

- **Neo4j**: Version 5.26 or higher (supports both SemVer and CalVer formats)
- **Kubernetes**: Version 1.21 or higher
- **Go**: Version 1.21+ (for development)

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

3. **Deploy your first Neo4j cluster**:
   ```bash
   kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/raw/main/examples/clusters/single-node.yaml
   ```

4. **Access your cluster**:
   ```bash
   kubectl port-forward svc/single-node-cluster-client 7474:7474 7687:7687
   ```
   Open http://localhost:7474 in your browser.

### For Kubernetes Experts

Jump right in with advanced configurations:

```bash
# Install the operator
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml

# Deploy production cluster with high availability
kubectl apply -f examples/clusters/three-node-cluster.yaml
```

See the [Complete Installation Guide](docs/user_guide/installation.md) for all deployment options.

## 💡 Examples

Ready-to-use configurations for common deployment scenarios:

- **[Single-node cluster](examples/clusters/single-node.yaml)** - Development and testing
- **[Three-node cluster](examples/clusters/three-node-cluster.yaml)** - Production with high availability
- **[Cluster with read replicas](examples/clusters/cluster-with-read-replicas.yaml)** - Read scaling

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
- [Neo4jEnterpriseCluster](docs/api_reference/neo4jenterprisecluster.md)
- [Neo4jBackup](docs/api_reference/neo4jbackup.md) & [Neo4jRestore](docs/api_reference/neo4jrestore.md)
- [Neo4jDatabase](docs/api_reference/neo4jdatabase.md)
- [Neo4jPlugin](docs/api_reference/neo4jplugin.md)

## ✨ Key Features

### 🏗️ Core Capabilities
- **Enterprise Clusters**: Deploy Neo4j Enterprise with clustering support
- **High Availability**: Multi-replica clusters with automatic leader election
- **Persistent Storage**: Configurable storage classes and volume management
- **Rolling Updates**: Zero-downtime Neo4j version upgrades

### 🔐 Security & Authentication
- **TLS/SSL**: Configurable TLS encryption for client and cluster communications
- **Authentication**: Support for LDAP, OIDC, and native authentication
- **RBAC**: Kubernetes role-based access control integration
- **Network Policies**: Pod-to-pod communication security

### 🚀 Operations & Automation
- **Automated Backups**: Scheduled backups with configurable retention
- **Point-in-Time Recovery**: Restore clusters to specific timestamps
- **Auto-scaling**: Horizontal Pod Autoscaler (HPA) integration
- **Plugin Management**: Install and configure Neo4j plugins (APOC, GDS, etc.)
- **Query Monitoring**: Performance monitoring and slow query detection

### 🔧 Cluster Management
Manage your Neo4j clusters using standard kubectl commands:
```bash
kubectl get neo4jenterprisecluster
kubectl describe neo4jenterprisecluster my-cluster
kubectl logs -l app.kubernetes.io/name=neo4j-operator
```

## 🏃‍♂️ Common Use Cases

### Development & Testing
- **Single-node clusters** for development environments
- **Ephemeral clusters** for CI/CD pipelines
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
