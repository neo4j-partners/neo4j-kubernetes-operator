# Neo4j Enterprise Operator for Kubernetes

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/neo4j-partners/neo4j-kubernetes-operator)](https://goreportcard.com/report/github.com/neo4j-partners/neo4j-kubernetes-operator)
[![GitHub Release](https://img.shields.io/github/release/neo4j-partners/neo4j-kubernetes-operator.svg)](https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases)

The Neo4j Kubernetes Operator automates the deployment and management of Neo4j Enterprise Edition.

The Operator deploys Neo4j EE v5.26+.  It supports both clustered and standalone deployments for cloud-native graph database operations.

> [!WARNING]
> **Alpha Software — Please Read Before Using**
>
> This project is in **alpha stage** and is maintained by a **single maintainer** in a personal capacity. Development is assisted by LLM-based tooling, which means the codebase may contain subtle bugs, incomplete features, or unexpected behavior despite best efforts.
>
> - **No production guarantees**: This operator is not recommended for production workloads without thorough independent validation. Use at your own risk.
 > - **No official Neo4j support**: This project is not an official Neo4j product and is not supported by Neo4j, Inc. in any capacity. The maintainer is a Product Manager at Neo4j, but maintains this project in a personal capacity. Support is provided solely by the maintainer on a best-effort basis through [GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues).
> - **Breaking changes**: As an alpha project, APIs and behavior may change between releases without notice.

## 📑 Table of Contents

- [Requirements](#-requirements)
- [Quick Start](#-quick-start)
  - [Installation](#installation)
  - [Cleanup](#cleanup)
  - [Development Installation](#development-installation)
- [Database Management](#-database-management)
- [User & Role Management](#-user--role-management)
- [Property Sharding](#-property-sharding-infinigraph-ga)
- [Backup and Restore](#-backup-and-restore)
- [Examples](#-examples)
- [Authentication](#-authentication)
- [Documentation Structure](#-documentation-structure)
- [Key Features](#-key-features)
- [Common Use Cases](#️-common-use-cases)
- [Recent Improvements](#-recent-improvements)
- [Contributing](#-contributing)
- [Support](#-support)

## 📋 Requirements

- **Neo4j**: Version 5.26.x (last semver LTS) or 2025.x.x+ (CalVer — the successor versioning scheme)
- **Kubernetes**: Version 1.21 or higher
- **Go**: Version 1.24+ (for development)
- **cert-manager**: Version 1.18+ (optional, only required for TLS-enabled Neo4j deployments)

## 🚀 Quick Start

### Installation

Installation requires cloning from source:

1. **Clone the repository** and checkout the latest tag:

   ```bash
   # Clone the repository
   git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
   cd neo4j-kubernetes-operator

   # Checkout the latest release tag
   LATEST_TAG=$(git describe --tags --abbrev=0)
   git checkout $LATEST_TAG
   ```

2. **Install the operator** using Helm (recommended) or make targets:

   **Helm Installation (Recommended)**:
   ```bash
   # Install using Helm chart (automatically handles RBAC)
   helm install neo4j-operator ./charts/neo4j-operator \
     --namespace neo4j-operator-system \
     --create-namespace
   ```
   ```bash
   # Install from GHCR OCI registry
   helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
     --version <release-version> \
     --namespace neo4j-operator-system \
     --create-namespace
   ```
   Note: use the chart version without the `v` prefix (for example, `0.2.0`).

   **Make Targets**:
   ```bash
   # Install CRDs into your cluster
   make install

   # Deploy the operator (choose based on your environment)
   make deploy-prod        # Production deployment (uses local neo4j-operator:latest image)
   make deploy-dev         # Development deployment (uses local neo4j-operator:dev image)

   # Registry-based deployment (requires cluster-admin permissions)
   make deploy-prod-registry  # Deploy from Docker Hub registry (auto-checks RBAC)
   make deploy-dev-registry   # Deploy dev overlay with registry image (auto-checks RBAC)

   # For users without cluster-admin permissions
   make deploy-namespace-scoped  # Deploy with namespace-only permissions (limited functionality)
   ```

   **Note**:
   - **Helm chart** automatically creates all necessary RBAC permissions
   - **Registry deployments** automatically check and help set up RBAC permissions
   - If you encounter permission errors, the operator will guide you through the setup process
   - **RBAC scope**: `operatorMode=cluster` and `operatorMode=namespaces` install ClusterRole/ClusterRoleBinding; `operatorMode=namespace` installs Role/RoleBinding in a single namespace (details: docs/user_guide/operator-modes.md)

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

   # Or run tests step by step against an existing cluster
   make test-cluster             # Create test cluster
   make test-integration-ci      # Run essential tests (assumes cluster exists)
   make test-cluster-delete      # Clean up test cluster
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
apiVersion: neo4j.neo4j.com/v1beta1
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
apiVersion: neo4j.neo4j.com/v1beta1
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

## 👥 User & Role Management

Once a cluster or standalone is `Ready`, manage Neo4j users, roles, and privileges declaratively via three CRDs. Privileges live on roles; users are bound to roles by name.

> **Prerequisites**: A `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in `Ready` phase. Enterprise edition only (RBAC roles and `accountStatus: suspended` are Enterprise-only Neo4j features).

### Step 1: Create a custom role with privileges (`Neo4jRole`)

The role's privileges are reconciled against `SHOW ROLE PRIVILEGES AS COMMANDS` on every loop. Any out-of-band `REVOKE` is reverted unless `enforcePrivileges: false`.

```bash
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: analytics-reader
spec:
  clusterRef: minimal-cluster   # or your standalone's name
  privileges:
    - "GRANT ACCESS ON DATABASE neo4j TO analytics-reader"
    - "GRANT MATCH {*} ON GRAPH neo4j NODES * TO analytics-reader"
    - "DENY WRITE ON GRAPH neo4j TO analytics-reader"
EOF
```

Built-in roles (`reader`, `editor`, `publisher`, `architect`, `admin`) don't need a `Neo4jRole` — bind to them directly.

### Step 2: Create a user (`Neo4jUser`) — password sourced from a Secret

```bash
# Password lives in the Secret, never in the CR. Rotation = update the Secret;
# the controller detects the change via SHA-256 hash and applies ALTER USER.
kubectl create secret generic analytics-reader-creds \
  --from-literal=password='ChangeMe123!'

kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: analytics-reader
spec:
  clusterRef: minimal-cluster
  username: analytics_reader
  passwordSecretRef:
    name: analytics-reader-creds
  roles:
    - analytics-reader      # custom role from Step 1
EOF
```

If the role doesn't exist yet, the user enters `PendingDependencies` and reconciles automatically when it lands.

### Step 3: Inspect status

```bash
kubectl get neo4juser analytics-reader -o jsonpath='{.status.currentRoles}'
kubectl get neo4jrole analytics-reader -o jsonpath='{.status.appliedPrivileges}'

# Cluster-level rollup (when monitoring.enabled=true, default):
kubectl get neo4jenterprisecluster minimal-cluster -o jsonpath='{.status.diagnostics.users}'
kubectl get neo4jenterprisecluster minimal-cluster -o jsonpath='{.status.diagnostics.roles}'
```

### Bonus: Bind an SSO/LDAP user the operator does NOT own

When users are provisioned externally (typically by Neo4j on first OIDC/LDAP login), use `Neo4jRoleBinding` — it manages role grants without ever creating or dropping the user.

```bash
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRoleBinding
metadata:
  name: alice-binding
spec:
  clusterRef: minimal-cluster
  username: alice@example.com   # SSO-provisioned, no Neo4jUser CR
  roles: [editor, analytics-reader]
EOF
```

If the user doesn't exist yet, the binding sits in `UserNotFound` and reconciles when it appears.

**Documentation and more examples:**

- [User & Role Management Guide](docs/user_guide/user_role_management.md) — end-to-end walkthrough with troubleshooting
- [Neo4jUser API reference](docs/api_reference/neo4juser.md)
- [Neo4jRole API reference](docs/api_reference/neo4jrole.md)
- [Neo4jRoleBinding API reference](docs/api_reference/neo4jrolebinding.md)
- [`examples/users-roles/`](examples/users-roles/) — six end-to-end YAMLs covering native users, custom roles, SSO bindings, suspension, external auth, and built-in adoption

## 🔄 Property Sharding (Infinigraph GA)

Property sharding (Infinigraph) was introduced and is GA as of Neo4j 2025.12. It keeps the graph shard (nodes/relationships) in a Raft group while distributing properties across property shards for horizontal scale.

### Requirements

- **Neo4j Version**: 2025.12+ Enterprise (not available on Aura)
- **Minimum Servers**: 2 servers minimum (3+ recommended for HA graph shard primaries)
- **Memory**: 4Gi minimum, 8Gi+ recommended per server
- **CPU**: 2+ cores per server for cross-shard queries
- **Authentication**: Admin secret required
- **Storage**: Storage class must be specified
- **Network**: Low-latency networking for transaction log shipping
- **Cypher**: `db.query.default_language=CYPHER_25` is required for sharded databases

### Quick Start

1. **Create admin secret (required):**

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password
```

2. **Create a property sharding enabled cluster:**

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: sharding-cluster
spec:
  image:
    repo: neo4j
    tag: 2025.12-enterprise  # Requires 2025.12+ for property sharding
  auth:
    adminSecret: neo4j-admin-secret
  topology:
    servers: 3  # 3+ recommended for HA graph shard primaries
  storage:
    size: 10Gi
    className: standard
  resources:
    requests:
      memory: 8Gi
      cpu: 2000m
    limits:
      memory: 16Gi
      cpu: 4000m
  propertySharding:
    enabled: true
    config:
      internal.dbms.sharded_property_database.enabled: "true"
      internal.dbms.sharded_property_database.allow_external_shard_access: "false"
      db.query.default_language: "CYPHER_25"
```

3. **Create a sharded database:**

```yaml
apiVersion: neo4j.com/v1beta1
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
      replicas: 1
```

### Examples

- **[Basic setup](examples/property_sharding/basic-property-sharding.yaml)** - Simple property sharded database
- **[Advanced configuration](examples/property_sharding/advanced-property-sharding.yaml)** - Production setup with multiple shards
- **[Development setup](examples/property_sharding/development-property-sharding.yaml)** - Minimal resources for testing
- **[With backup](examples/property_sharding/property-sharding-with-backup.yaml)** - Backup configuration for sharded databases

For complete documentation, see [Property Sharding Guide](examples/property_sharding/README.md).
Note: `backupConfig` on `Neo4jShardedDatabase` is not orchestrated yet; use `Neo4jBackup` resources for shard backups.

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

### Plugin Management
- **[Cluster plugin example](examples/plugins/cluster-plugin-example.yaml)** - Install APOC on a Neo4jEnterpriseCluster
- **[Standalone plugin example](examples/plugins/standalone-plugin-example.yaml)** - Install Graph Data Science on standalone
- **[Plugin documentation](examples/plugins/README.md)** - Complete guide to plugin management

### Users & Roles (NEW!)
- **[Read-only user](examples/users-roles/01-readonly-user.yaml)** - Bind to the built-in `reader` role
- **[Custom role with user](examples/users-roles/02-custom-role-with-user.yaml)** - The canonical pattern: a Neo4jRole carrying privileges plus a Neo4jUser bound to it
- **[Suspended user](examples/users-roles/03-suspended-user.yaml)** - Disable an account without dropping it
- **[External authentication](examples/users-roles/04-external-auth.yaml)** - OIDC + LDAP only (no native password)
- **[Adopt a built-in role](examples/users-roles/05-adopt-builtin.yaml)** - Tighten privileges on `editor` with `adoptBuiltin: true`
- **[Bind an SSO user to roles](examples/users-roles/06-rolebinding-sso-user.yaml)** - `Neo4jRoleBinding` for users provisioned externally (LDAP/OIDC first-login)
- **[Users & Roles documentation](examples/users-roles/README.md)** - End-to-end declarative RBAC

### Property Sharding (Infinigraph GA - Neo4j 2025.12+)
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
    authenticationProviders: ["native"]
    adminSecret: neo4j-admin-secret
```

**Important Notes**:
- Do not set NEO4J_AUTH in the `env` section - it will be ignored
- NEO4J_ACCEPT_LICENSE_AGREEMENT is automatically set for Enterprise edition
- For production, consider using external secret management solutions

See [authentication example](examples/clusters/auth-example.yaml) for complete configuration.

### Declarative Users, Roles & Privileges

Beyond the bootstrap admin secret, you can manage application-level users, roles, and privileges declaratively via the [`Neo4jUser`](docs/api_reference/neo4juser.md) and [`Neo4jRole`](docs/api_reference/neo4jrole.md) CRDs. Passwords come from `Secret`s, role bindings are reconciled like any other resource, and privilege drift is auto-corrected. See the [User & Role Management Guide](docs/user_guide/user_role_management.md).

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
- [Neo4jShardedDatabase](docs/api_reference/neo4jshardeddatabase.md) - Property sharded databases (Infinigraph, GA in 2025.12+)
- [Neo4jPlugin](docs/api_reference/neo4jplugin.md)
- [Neo4jUser](docs/api_reference/neo4juser.md) - Declarative user management (passwords, roles, status, external auth)
- [Neo4jRole](docs/api_reference/neo4jrole.md) - Declarative role management with privilege-drift reconciliation
- [Neo4jRoleBinding](docs/api_reference/neo4jrolebinding.md) - Role grants for externally-provisioned users (SSO/LDAP/OIDC)

See the [User & Role Management Guide](docs/user_guide/user_role_management.md) for an end-to-end walkthrough.

## ✨ Key Features

### 🏗️ Core Capabilities
- **Dual Deployment Modes**: Choose between clustered (Neo4jEnterpriseCluster) or standalone (Neo4jEnterpriseStandalone) deployments
- **Server-Based Architecture**: Enterprise clusters use unified server StatefulSets where servers self-organize into database primary/secondary roles
- **Flexible Topology**: Specify total server count and let Neo4j automatically assign database hosting roles based on requirements
- **Property Sharding**: Neo4j property sharding (Infinigraph, GA in 2025.12+) for massive scale graph databases
- **High Availability**: Multi-server clusters with automatic leader election and V2_ONLY discovery
- **Persistent Storage**: Configurable storage classes and volume management
- **Rolling Updates**: Zero-downtime Neo4j version upgrades
- **OpenShift Route Support**: Optional OpenShift Routes via `spec.service.route` for cluster and standalone services

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
- **Prometheus Metrics**: Neo4j built-in metrics endpoint exposed via `spec.monitoring`

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

Note: "Compliance-ready logging and auditing" means the operator exposes Neo4j logging/audit controls via `spec.config` and emits Kubernetes Events for key actions; you still need to enable the desired Neo4j log settings and ship/retain logs per your compliance requirements.

## 🎯 Recent Improvements

### Declarative User & Role Management (April 2026)

Three new CRDs — **`Neo4jUser`**, **`Neo4jRole`**, and **`Neo4jRoleBinding`** — close the last imperative gap in the operator. Users, roles, and privileges are now expressible as Kubernetes resources alongside infrastructure, with full GitOps support and automatic drift reconciliation.

- **`Neo4jUser`**: identity, password (sourced from `Secret`), `accountStatus`, home database, role bindings, and external auth providers (OIDC/LDAP). Password rotation triggered automatically when the Secret value changes.
- **`Neo4jRole`**: role existence and privilege management with full `SHOW ROLE PRIVILEGES AS COMMANDS`-based drift reconciliation. Manual `GRANT/REVOKE` outside the operator is reverted on the next loop unless `enforcePrivileges: false`. Built-in roles (`reader`, `editor`, etc.) protected by default; opt in via `adoptBuiltin: true`.
- **`Neo4jRoleBinding`**: role grants for users provisioned externally (SSO/LDAP first-login users that the operator does not own). Never creates or drops the user; only manages role grants. Optional `enforceExclusive: true` for strict role-set control.
- **Privileges live on the role, not the user.** Bind users to roles via `Neo4jUser.spec.roles` or `Neo4jRoleBinding.spec.roles`. Roles can be referenced before they exist — the user/binding enters `PendingDependencies` and reconciles automatically when the role lands.
- **Live diagnostics**: when `spec.monitoring.enabled=true` (default), the cluster/standalone status now surfaces `SHOW USERS` / `SHOW ROLES` summaries in `status.diagnostics`, so you can observe the effect of declarative RBAC without `kubectl exec`.

See the [User & Role Management Guide](docs/user_guide/user_role_management.md) and the [users-roles examples](examples/users-roles/).

### v1.7.0-alpha: API Version Bump to v1beta1 (Breaking Changes)

> **⚠️ Upgrading from v1.6.0-alpha or earlier requires updating all manifests.** See the [Migration Guide](docs/user_guide/migration_guide.md#upgrading-to-v170-alpha-api-version-bump-to-v1beta1) for details.

**Breaking changes:**
- **API version**: All CRDs changed from `neo4j.neo4j.com/v1alpha1` to `neo4j.neo4j.com/v1beta1`. Every manifest must be updated.
- **Bolt TLS enforcement**: When TLS is enabled, `server.bolt.tls_level` is now `REQUIRED` (was `OPTIONAL`). Plain `bolt://` connections are rejected on TLS-enabled clusters and standalones — clients must use `bolt+s://` or `bolt+ssc://`.
- **Deprecated config key**: `dbms.logs.query.enabled` is deprecated and will produce a validation warning. Use `db.logs.query.enabled` instead.

**Other improvements in this release:**
- Standalone deployments now have readiness, liveness, and startup probes (previously had none — pods were marked Ready before Neo4j was actually accepting connections)
- Standalone status endpoints correctly report `bolt+s://` when TLS is enabled (was always `bolt://`)
- Fixed duplicate `server.bolt.*` config entries in standalone ConfigMap when TLS enabled (caused CrashLoopBackOff)
- Demo script overhauled: TLS on both standalone and cluster, cleanup flags, confirmation for destructive steps

### v1.6.0-alpha: API Stabilization (Breaking Changes)

> **⚠️ Upgrading from v1.5.0-alpha or earlier requires manifest changes.** See the [Migration Guide](docs/user_guide/migration_guide.md#upgrading-to-v160-alpha-api-stabilization) for details.

Key changes: `targetCluster` renamed to `clusterRef` in Neo4jRestore, deprecated `auth.provider`/`auth.secretRef` removed in favor of `authenticationProviders`/`authorizationProviders` lists, standalone `spec.route` and `spec.persistence` consolidated into existing fields, secret reference types unified into `SecretKeyRef`.

### Latest Version Enhancements
- **Property Sharding Support (GA)**: Neo4j property sharding (Infinigraph, introduced in 2025.12)
  - **Automatic Configuration**: Applies required sharding settings (CYPHER_25 default language, sharded database enablement)
  - **Version Validation**: Ensures Neo4j 2025.12+ for property sharding compatibility
  - **Topology Requirements**: Validates minimum 2 servers for property sharding clusters (3+ recommended for HA)
  - **Neo4jShardedDatabase CRD**: CRD for creating and managing property-sharded databases
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

### Observability & GitOps (February 2026)
- **Live Cluster Diagnostics**: `SHOW SERVERS` and `SHOW DATABASES` results surfaced in `status.diagnostics` — no more `kubectl exec` for cluster health
- **Structured Kubernetes Events**: All state transitions emit typed events (`ClusterFormationStarted`, `BackupCompleted`, `SplitBrainDetected`, etc.) consumable by monitoring pipelines — see the [Events Reference](docs/user_guide/guides/kubernetes-events.md)
- **Custom Prometheus Metrics**: Per-server health gauge, cluster phase gauge, backup counters, reconcile histograms — all with cluster/namespace labels
- **ArgoCD/Flux Health Checks**: Native ArgoCD Lua scripts for all 7 CRDs; Flux detects readiness via standard `Ready` condition automatically
- **Standardized Status Conditions**: All CRDs now emit `Ready`, `ServersHealthy`, and `DatabasesHealthy` conditions with consistent `Reason` and `Message` fields
- **Multi-Registry Support**: `spec.image.pullSecrets` wired into StatefulSet `imagePullSecrets` for ECR/GCR/ACR/private registry support

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
- **Go 1.25+**
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
git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
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

## 📞 Support

- **Documentation**: [docs/](docs/)
- **GitHub Issues**: https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues
