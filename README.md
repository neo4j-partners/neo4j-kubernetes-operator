# Neo4j Enterprise Operator for Kubernetes

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/neo4j-labs/neo4j-kubernetes-operator)](https://goreportcard.com/report/github.com/neo4j-labs/neo4j-kubernetes-operator)
[![GitHub Release](https://img.shields.io/github/release/neo4j-labs/neo4j-kubernetes-operator.svg)](https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases)

The Neo4j Enterprise Operator for Kubernetes provides a complete solution for deploying, managing, and scaling Neo4j Enterprise instances (v5.26+) in Kubernetes environments. Built with the Kubebuilder framework, it supports both clustered and standalone deployments for cloud-native graph database operations.

> ⚠️ **ALPHA SOFTWARE WARNING**: This operator is currently in **alpha stage**. There may be breaking changes at any time due to ongoing development. For production use or evaluation, please use the [latest alpha release](https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest) rather than the main branch code.

## 📑 Table of Contents

- [Requirements](#-requirements)
- [Quick Start](#-quick-start)
  - [Installation](#installation)
  - [Cleanup](#cleanup)
  - [Development Installation](#development-installation)
- [Database Management](#-database-management)
- [Property Sharding](#-property-sharding-preview)
- [Backup and Restore](#-backup-and-restore)
- [Examples](#-examples)
- [Authentication](#-authentication)
- [Documentation Structure](#-documentation-structure)
- [Key Features](#-key-features)
- [Common Use Cases](#️-common-use-cases)
- [Recent Improvements](#-recent-improvements)
- [Contributing](#-contributing)
- [Support & Community](#-support--community)

## 📋 Requirements

- **Neo4j**: Version 5.26 or higher (supports both SemVer 5.x and CalVer 2025.x formats)
- **Kubernetes**: Version 1.21 or higher
- **Go**: Version 1.22+ (for development)
- **cert-manager**: Version 1.18+ (optional, only required for TLS-enabled Neo4j deployments)

## 🚀 Quick Start

### Installation

Installation requires cloning from source:

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

   # Deploy the operator (choose based on your environment)
   make deploy-prod        # Production deployment (uses local neo4j-operator:latest image)
   make deploy-dev         # Development deployment (uses local neo4j-operator:dev image)
   # or (for registry-based deployment)
   make deploy-prod-registry  # Deploy from ghcr.io registry (requires authentication)
   make deploy-dev-registry   # Deploy dev overlay with registry image
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

6. **Verify your installation** (Optional):

   ```bash
   # Run unit tests (fast, no cluster required)
   make test-unit

   # Run integration tests (automatically creates cluster and deploys operator)
   make test-integration

   # Or run tests step by step
   make test-cluster        # Create test cluster
   make test-integration    # Run integration tests
   make test-cluster-delete # Clean up test cluster
   ```

## 📊 Database Management

After deploying a Neo4j instance (standalone or cluster), you can create and manage databases using the Neo4jDatabase CRD.

> **Prerequisites**: You must first deploy either a `Neo4jEnterpriseStandalone` or `Neo4jEnterpriseCluster` before creating databases.

### Step 1: Deploy a Neo4j Instance

Choose one of the following deployment types:

**Option A: Standalone Instance (Development/Testing)**

```bash
kubectl apply -f examples/standalone/single-node-standalone.yaml

# Wait for deployment
kubectl get neo4jenterprisestandalone
kubectl wait --for=condition=Ready neo4jenterprisestandalone/standalone --timeout=300s
```

**Option B: Cluster Instance (Production)**

```bash
kubectl apply -f examples/clusters/minimal-cluster.yaml

# Wait for cluster formation
kubectl get neo4jenterprisecluster
kubectl wait --for=condition=Ready neo4jenterprisecluster/minimal-cluster --timeout=300s
```

### Step 2: Create Databases

**For cluster deployments:**

```bash
# Create a database on your cluster
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-cluster-database
spec:
  clusterRef: minimal-cluster  # Reference to your Neo4jEnterpriseCluster
  name: appdb
  topology:
    primaries: 1
    secondaries: 1
  wait: true
  ifNotExists: true
EOF
```

**For standalone deployments:**

```bash
# Create a database on your standalone instance
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-standalone-database
spec:
  clusterRef: standalone  # Reference to your Neo4jEnterpriseStandalone
  name: devdb
  wait: true
  ifNotExists: true
EOF
```

**More database examples:**

- [Database with custom topology](examples/database/database-with-topology.yaml)
- [Database for standalone instance](examples/database/database-standalone.yaml)
- [Database from S3 backup](examples/databases/database-from-s3-seed.yaml)
- [Database from existing backup](examples/databases/database-dump-vs-backup-seed.yaml)

## 🔄 Property Sharding (PREVIEW)

> ⚠️ **PREVIEW FEATURE**: Property Sharding is a preview feature available in Neo4j 2025.06+. It enables massive scale graph databases by separating graph structure and properties into specialized shards.

Property sharding separates your graph data into:
- **Graph shards**: Store nodes and relationships (no properties)
- **Property shards**: Store node and relationship properties

### Requirements

- **Neo4j Version**: 2025.06+ (CalVer format)
- **Minimum Servers**: 3+ servers required
- **Memory**: At least 4GB per server (recommended 8GB+)

### Quick Start

1. **Create a property sharding enabled cluster:**

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: sharding-cluster
spec:
  image:
    repo: neo4j
    tag: 2025.06-enterprise
  topology:
    servers: 3
  resources:
    requests:
      memory: 4Gi
    limits:
      memory: 8Gi
  propertySharding:
    enabled: true
```

2. **Create a sharded database:**

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jShardedDatabase
metadata:
  name: my-sharded-db
spec:
  clusterRef: sharding-cluster
  name: products
  defaultCypherLanguage: "25"  # Required for property sharding
  propertySharding:
    propertyShards: 2
    graphShard:
      primaries: 2
      secondaries: 1
    propertyShardTopology:
      primaries: 1
      secondaries: 1
```

### Examples

- **[Basic setup](examples/property_sharding/basic-property-sharding.yaml)** - Simple property sharded database
- **[Advanced configuration](examples/property_sharding/advanced-property-sharding.yaml)** - Production setup with multiple shards
- **[Development setup](examples/property_sharding/development-property-sharding.yaml)** - Minimal resources for testing
- **[With backup](examples/property_sharding/property-sharding-with-backup.yaml)** - Backup configuration for sharded databases

For complete documentation, see [Property Sharding Guide](examples/property_sharding/README.md).

## 💾 Backup and Restore

### Setting up Backups

**Simple PVC-based backup:**

```bash
kubectl apply -f examples/backup-restore/backup-pvc-simple.yaml
```

**S3-based backup with scheduling:**

```bash
kubectl apply -f examples/backup-restore/backup-s3-basic.yaml
```

**Scheduled daily backups:**

```bash
kubectl apply -f examples/backup-restore/backup-scheduled-daily.yaml
```

### Restoring from Backup

```bash
# Restore from a previous backup
kubectl apply -f examples/backup-restore/restore-from-backup.yaml
```

**Advanced backup features:**

- [Point-in-time recovery setup](examples/backup-restore/pitr-setup-complete.yaml)
- [Incremental backups](examples/backup/backup-incremental.yaml)
- [Backup with specific types](examples/backup/backup-with-type.yaml)

### Cleanup

To remove the operator from your cluster:

```bash
# Remove operator deployment (choose your deployment mode)
make undeploy-prod  # or undeploy-dev

# Remove CRDs (this will also remove all Neo4j instances)
make uninstall
```

### Development Installation

For development work with locally built images:

```bash
# Create development cluster
make dev-cluster

# Deploy operator (uses local images by default)
make deploy-dev   # Deploy to dev namespace with local neo4j-operator:dev image
# or
make deploy-prod  # Deploy to prod namespace with local neo4j-operator:latest image

# Alternative: Use automated setup (detects available clusters)
make operator-setup

# Note: Production deployment now uses local images by default
```

For additional deployment options, see the Installation section above.

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

### Property Sharding (PREVIEW - Neo4j 2025.06+)
- **[Basic property sharding](examples/property_sharding/basic-property-sharding.yaml)** - Simple property sharded database setup
- **[Advanced property sharding](examples/property_sharding/advanced-property-sharding.yaml)** - Production configuration with multiple shards
- **[Development property sharding](examples/property_sharding/development-property-sharding.yaml)** - Development setup with minimal resources
- **[Property sharding with backup](examples/property_sharding/property-sharding-with-backup.yaml)** - Backup configuration for sharded databases
- **[Property sharding documentation](examples/property_sharding/README.md)** - Complete guide to property sharding

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
- **[Contributing](CONTRIBUTING.md)** - How to contribute code

### 📖 API Reference
Complete CRD documentation for all custom resources:
- [Neo4jEnterpriseCluster](docs/api_reference/neo4jenterprisecluster.md) - Clustered deployments
- [Neo4jEnterpriseStandalone](docs/api_reference/neo4jenterprisestandalone.md) - Single-node deployments
- [Neo4jBackup](docs/api_reference/neo4jbackup.md) & [Neo4jRestore](docs/api_reference/neo4jrestore.md)
- [Neo4jDatabase](docs/api_reference/neo4jdatabase.md)
- [Neo4jShardedDatabase](docs/api_reference/neo4jshardeddatabase.md) - Property sharded databases (PREVIEW)
- [Neo4jPlugin](docs/api_reference/neo4jplugin.md)

## ✨ Key Features

### 🏗️ Core Capabilities
- **Dual Deployment Modes**: Choose between clustered (Neo4jEnterpriseCluster) or standalone (Neo4jEnterpriseStandalone) deployments
- **Server-Based Architecture**: Enterprise clusters use unified server StatefulSets where servers self-organize into database primary/secondary roles
- **Flexible Topology**: Specify total server count and let Neo4j automatically assign database hosting roles based on requirements
- **Property Sharding**: Neo4j 2025.06+ property sharding support for massive scale graph databases (PREVIEW)
- **High Availability**: Multi-server clusters with automatic leader election and V2_ONLY discovery
- **Persistent Storage**: Configurable storage classes and volume management
- **Rolling Updates**: Zero-downtime Neo4j version upgrades

### 🔐 Security & Authentication
- **TLS/SSL**: Configurable TLS encryption for client and cluster communications
- **Authentication**: Support for native, LDAP, Kerberos, and JWT authentication
- **Automatic RBAC**: Operator automatically creates all necessary RBAC resources for backups
- **Network Policies**: Pod-to-pod communication security

### 🚀 Operations & Automation
- **Automated Backups**: Scheduled backups with centralized backup StatefulSet (resource-efficient)
- **Point-in-Time Recovery**: Restore clusters to specific timestamps with `--restore-until`
- **Database Management**: Create databases with topology constraints, seed URIs, and point-in-time recovery
- **Version-Aware Operations**: Automatic detection and adaptation for Neo4j 5.26.x and 2025.x
- **Plugin Management**: Smart plugin installation with automatic configuration (APOC, GDS, Bloom, GenAI, N10s, GraphQL)
- **Split-Brain Detection**: Automatic detection and repair of split-brain scenarios in clusters

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
- **Property Sharding Support (PREVIEW)**: Neo4j 2025.06+ property sharding integration for massive scale deployments
  - **Automatic Configuration**: Property sharding clusters configured with required settings automatically
  - **Version Validation**: Ensures Neo4j 2025.06+ versions for property sharding compatibility
  - **Topology Requirements**: Validates minimum 3 servers for property sharding clusters
  - **Neo4jShardedDatabase CRD**: New CRD for creating and managing property-sharded databases
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

> **⚠️ IMPORTANT: Kind Required for Development**
> This project **exclusively uses Kind (Kubernetes in Docker)** for all development workflows, testing, and CI emulation. You must install Kind before contributing.

### Prerequisites for Contributors

**Required Tools:**
- **Go 1.21+**
- **Docker**
- **kubectl**
- **Kind** (Kubernetes in Docker) - **MANDATORY**
- **make**

**Quick Kind Installation:**
```bash
# macOS (Homebrew)
brew install kind

# Linux (binary download)
curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind

# Verify installation
kind version
```

For detailed installation instructions, see our [Contributing Guide](CONTRIBUTING.md).

### Quick Contribution Setup
```bash
# Clone and setup development environment
git clone https://github.com/neo4j-labs/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Create local Kind cluster for development
make dev-cluster

# Run tests to verify setup
make test-unit          # Unit tests (fast, no cluster required)
make test-integration   # Integration tests (auto-creates cluster, deploys operator)
make test-ci-local      # Emulate CI workflow with debug logging (Added 2025-08-22)

# Deploy operator for development
make operator-setup
```

### Available Make Targets

**Development & Testing**:

- `make dev-cluster` - Create development Kind cluster
- `make operator-setup` - Deploy operator in-cluster (recommended)
- `make test-unit` - Run unit tests (fast, no cluster required)
- `make test-integration` - Run integration tests (auto-creates cluster, deploys operator)
- `make test-ci-local` - Emulate CI workflow with debug logging

**Operator Installation**:

- `make install` - Install CRDs
- `make deploy-prod` - Deploy with production config
- `make deploy-dev` - Deploy with development config
- `make undeploy-prod/undeploy-dev` - Remove operator deployment
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
