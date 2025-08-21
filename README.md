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
- **cert-manager**: Version 1.5+ (optional, only required for TLS-enabled Neo4j deployments)

## 🚀 Quick Start

### Installation

Since this is a private repository, installation requires cloning from source:

1. **Clone the repository** and checkout the latest tag:
   ```bash
   # Clone the repository
   git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
   cd neo4j-kubernetes-operator

   # Checkout the latest release tag
   LATEST_TAG=$(git describe --tags --abbrev=0)
   git checkout $LATEST_TAG
   ```

2. **Install the operator** using make targets:
   ```bash
   # Install CRDs into your cluster
   make install

   # Deploy the operator to your cluster
   make deploy
   ```

   **Alternative installation methods**:
   ```bash
   # Deploy with development configuration
   make deploy-dev

   # Deploy with production configuration
   make deploy-prod

   ```

3. **Create admin credentials** (Required for authentication):

   ```bash
   kubectl create secret generic neo4j-admin-secret \
     --from-literal=username=neo4j \
     --from-literal=password=your-secure-password
   ```

   **Important**: The operator manages authentication through Kubernetes secrets. Do not set NEO4J_AUTH directly in environment variables.

4. **Deploy your first Neo4j instance**:

   **For single-node development** (non-clustered):

   ```bash
   kubectl apply -f examples/standalone/single-node-standalone.yaml
   ```

   **For clustered deployment** (production):

   ```bash
   kubectl apply -f examples/clusters/minimal-cluster.yaml
   ```

5. **Access your Neo4j instance**:

   ```bash
   # For standalone deployment
   kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687

   # For cluster deployment
   kubectl port-forward svc/minimal-cluster-client 7474:7474 7687:7687
   ```

   Open <http://localhost:7474> in your browser.

6. **Run tests** to verify installation:

   ```bash
   # Run unit tests
   make test-unit

   # Create test cluster and run integration tests
   make test-cluster
   make test-integration

   # Run end-to-end tests
   make test-e2e
   ```

### Cleanup

To remove the operator from your cluster:

```bash
# Remove operator deployment
make undeploy

# Remove CRDs (this will also remove all Neo4j instances)
make uninstall
```

### Development Installation

For development work:

```bash
# Create development cluster with operator
make dev-cluster
make dev-run  # Run operator locally outside cluster

# Or deploy operator to development cluster
make operator-setup
```

See the [Complete Installation Guide](docs/user_guide/installation.md) for all deployment options.

## 💡 Examples

After cloning the repository, ready-to-use configurations are available in the `examples/` directory:

### Standalone Deployments (Single-Node, Non-Clustered)
- **[Single-node standalone](examples/standalone/single-node-standalone.yaml)** - Development and testing
- **[LoadBalancer standalone](examples/standalone/loadbalancer-standalone.yaml)** - External access with LoadBalancer
- **[NodePort standalone](examples/standalone/nodeport-standalone.yaml)** - External access with NodePort

### Clustered Deployments (Enterprise Server Architecture)
- **[Minimal cluster](examples/clusters/minimal-cluster.yaml)** - 2 servers (minimum cluster topology)
- **[Multi-server cluster](examples/clusters/multi-server-cluster.yaml)** - Production with high availability (5+ servers)
- **[Three-node cluster](examples/clusters/three-node-cluster.yaml)** - 3 servers with TLS for fault tolerance
- **[Topology placement](examples/clusters/topology-placement-cluster.yaml)** - Multi-zone deployment with topology constraints
- **[LoadBalancer cluster](examples/clusters/loadbalancer-cluster.yaml)** - External access with cloud load balancer
- **[Ingress cluster](examples/clusters/ingress-cluster.yaml)** - HTTPS access via Ingress controller

### Plugin Management (NEW!)
- **[Cluster plugin example](examples/plugins/cluster-plugin-example.yaml)** - Install APOC on a Neo4jEnterpriseCluster
- **[Standalone plugin example](examples/plugins/standalone-plugin-example.yaml)** - Install Graph Data Science on standalone
- **[Plugin documentation](examples/plugins/README.md)** - Complete guide to plugin management

### Quick Example Deployment

```bash
# After cloning and installing the operator:
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Check status
kubectl get neo4jenterprisestandalone
kubectl get pods

# Access Neo4j Browser
kubectl port-forward svc/standalone-neo4j-service 7474:7474
```

See the [examples directory](examples/) for complete documentation and additional configurations.

## 🔐 Authentication

The operator manages Neo4j authentication through Kubernetes secrets:

1. **Secret-based Authentication**: Create a secret with `username` and `password` keys
2. **Automatic Configuration**: The operator automatically configures NEO4J_AUTH from the secret
3. **Managed Variables**: NEO4J_AUTH and NEO4J_ACCEPT_LICENSE_AGREEMENT are managed by the operator

### Authentication Configuration

```yaml
# Create the secret
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password

# Reference in your cluster specification
spec:
  auth:
    provider: native  # Options: native, ldap, kerberos, jwt
    adminSecret: neo4j-admin-secret
```

**Important Notes**:
- Do not set NEO4J_AUTH in the `env` section - it will be ignored
- NEO4J_ACCEPT_LICENSE_AGREEMENT is automatically set for Enterprise edition
- For production, consider using external secret management solutions

See [authentication example](examples/clusters/auth-example.yaml) for complete configuration.

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
- **Server-Based Architecture**: Enterprise clusters use unified server StatefulSets where servers self-organize into database primary/secondary roles
- **Flexible Topology**: Specify total server count and let Neo4j automatically assign database hosting roles based on requirements
- **High Availability**: Multi-server clusters with automatic leader election and V2_ONLY discovery
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
- **Plugin Management**: Install and configure Neo4j plugins using NEO4J_PLUGINS environment variable (APOC, GDS, etc.)
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
- **Standalone deployments** for development environments (single-node, non-clustered)
- **Minimal clusters** for integration testing (2 servers self-organizing)
- **Ephemeral deployments** for CI/CD pipelines
- **Sample data loading** for testing scenarios with seed URIs

### Production Deployments
- **High-availability clusters** across multiple availability zones with topology placement
- **Server pools** that automatically assign database hosting based on requirements
- **Automated backup strategies** with off-site storage and automatic RBAC
- **Performance monitoring** and alerting integration
- **Blue-green deployments** for zero-downtime upgrades

### Enterprise Features
- **LDAP/AD integration** for centralized authentication
- **Plugin ecosystem** with Neo4j 5.26+ compatibility:
  - **APOC & APOC Extended**: Environment variable configuration (Neo4j 5.26+ compatible)
  - **Graph Data Science (GDS)**: Automatic security configuration and license support
  - **Bloom**: Complete setup with web interface and security settings
  - **GenAI**: AI provider integrations (OpenAI, Vertex AI, Azure OpenAI, Bedrock)
  - **Neo Semantics (N10s)**: RDF and semantic web support
- **Smart Plugin Configuration**: Automatic detection of plugin type and appropriate configuration method
- **Compliance-ready** logging and auditing
- **Resource quotas** and governance controls

## 🎯 Recent Improvements

### Latest Version Enhancements
- **Neo4j 5.26+ Plugin Compatibility**: Complete rework of plugin system for Neo4j 5.26+ compatibility
  - **APOC Environment Variables**: APOC configuration now uses environment variables (no longer supported in neo4j.conf)
  - **Automatic Security Settings**: Plugin-specific procedure security applied automatically
  - **Plugin Type Detection**: Smart configuration based on plugin requirements
  - **Dependency Management**: Automatic resolution and installation of plugin dependencies
- **Enhanced External Access**: Full support for LoadBalancer, NodePort services and Ingress resources with automatic connection string generation in status
- **Cloud Provider Integration**: Automatic detection of AWS, GCP, and Azure with optimal LoadBalancer configurations
- **Improved Service Configuration**: Support for static IPs, source ranges, external traffic policies, and custom annotations
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

# Create local Kind cluster for development
make dev-cluster

# Run tests to verify setup
make test-unit     # Unit tests
make test-cluster  # Create test cluster
make test-integration  # Integration tests

# Deploy operator for development
make operator-setup
```

### Available Make Targets

**Development & Testing**:

- `make dev-cluster` - Create development Kind cluster
- `make dev-run` - Run operator locally (outside cluster)
- `make test-unit` - Run unit tests
- `make test-integration` - Run integration tests
- `make test-e2e` - Run end-to-end tests

**Operator Installation**:

- `make install` - Install CRDs
- `make deploy` - Deploy operator
- `make deploy-dev` - Deploy with dev config
- `make deploy-prod` - Deploy with production config
- `make undeploy` - Remove operator
- `make uninstall` - Remove CRDs

**Code Quality**:

- `make fmt` - Format code
- `make lint` - Run linter
- `make vet` - Run go vet
- `make test-coverage` - Generate coverage report

See the [Contributing Guide](docs/developer_guide/contributing.md) for detailed instructions.

## 📞 Support & Community

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)
- **Discussions**: [GitHub Discussions](https://github.com/neo4j-labs/neo4j-kubernetes-operator/discussions)
- **Neo4j Community**: [Neo4j Community Site](https://community.neo4j.com/)

## 📄 License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
