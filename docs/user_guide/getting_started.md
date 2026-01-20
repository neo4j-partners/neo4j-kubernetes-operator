# Getting Started

This guide will walk you through the process of deploying your first Neo4j Enterprise instance on Kubernetes using the Neo4j Enterprise Operator. You can choose between clustered and standalone deployments based on your needs.

## Prerequisites

*   A Kubernetes cluster (v1.21+)
*   `kubectl` installed and configured
*   Neo4j Enterprise Edition (evaluation license acceptable for testing)
*   Go 1.22+ (for building from source)
*   cert-manager 1.18+ (optional, for TLS-enabled deployments)

## Installation

For detailed installation instructions, refer to the main README.

Since this is a private repository, installation requires cloning from source:

```bash
# Clone the repository and checkout latest tag
git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator
LATEST_TAG=$(git describe --tags --abbrev=0)
git checkout $LATEST_TAG

# Install CRDs and operator
make install      # Install CRDs
make deploy-prod  # Deploy operator (builds and uses local image)
# or (requires ghcr.io access)
make deploy-prod-registry  # Deploy from ghcr.io registry
```

## Operator Modes

The Neo4j Operator supports two operational modes:

- **Production Mode** (default): Optimized for stability, security, and monitoring in production environments
- **Development Mode**: Optimized for rapid development, debugging, and local testing

For detailed information about modes, configuration options, and caching strategies, see the [Operator Modes Guide](operator-modes.md).

### Quick Mode Selection

```bash
# Production deployment (uses local image by default)
make deploy-prod

# Development deployment (uses local image by default)
make deploy-dev

# Alternative: Registry-based deployment
make deploy-prod-registry  # Requires ghcr.io access
make deploy-dev-registry   # Requires registry access for dev image
```

**⚠️ Important:** The operator must always run in-cluster, even for development. This ensures proper DNS resolution and cluster connectivity required for Neo4j cluster formation.

## Choosing Your Deployment Type

The Neo4j Enterprise Operator supports two deployment types:

- **Neo4jEnterpriseStandalone**: Single-node deployments for development, testing, and simple production workloads
- **Neo4jEnterpriseCluster**: Clustered deployments (minimum 2 servers) for production with high availability and automatic failover

## Deploying a Standalone Instance (Development)

For development, testing, or simple workloads that don't require high availability:

1.  **Create admin credentials:**

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password=your-secure-password
    ```

2.  **Deploy the standalone instance:**

    ```bash
    kubectl apply -f examples/standalone/single-node-standalone.yaml
    ```

3.  **Check deployment status:**

    ```bash
    kubectl get neo4jenterprisestandalone
    kubectl get pods
    ```

4.  **Access Neo4j Browser:**

    ```bash
    kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687
    ```

    Open http://localhost:7474 in your browser.

## Deploying a Cluster (Production)

For production workloads requiring high availability and clustering:

### Option 1: Minimal Cluster (Recommended for Testing)

1.  **Create admin credentials:**

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password=your-secure-password
    ```

2.  **Deploy the minimal cluster:**

    ```bash
    kubectl apply -f examples/clusters/minimal-cluster.yaml
    ```

3.  **Monitor deployment (2-3 minutes expected):**

    ```bash
    kubectl get pods -l app.kubernetes.io/name=neo4j -w
    ```

### Option 2: Multi-Server Cluster (Recommended for Production)

1.  **Create admin credentials:**

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password=your-secure-password
    ```

2.  **Deploy the cluster:**

    ```bash
    # For production (with TLS)
    kubectl apply -f examples/clusters/multi-server-cluster.yaml

    # For testing (TLS disabled)
    kubectl apply -f examples/clusters/three-node-cluster.yaml
    ```

3.  **Monitor deployment (3-5 minutes expected):**

    ```bash
    kubectl get pods -l app.kubernetes.io/name=neo4j -w
    ```

### Option 3: Custom Configuration

If you need a custom configuration, create your own manifest based on our examples:

1. **Browse the examples directory:**
   - [Minimal cluster](../../examples/clusters/minimal-cluster.yaml) - 2 servers (minimum cluster topology)
   - [Multi-server cluster](../../examples/clusters/multi-server-cluster.yaml) - Production HA with TLS
   - [Three-node cluster](../../examples/clusters/three-node-cluster.yaml) - Three servers with TLS
   - [Production optimized cluster](../../examples/clusters/production-optimized-cluster.yaml) - Production with advanced features

2. **Copy and customize an example:**
   ```bash
   cp examples/clusters/minimal-cluster.yaml my-cluster.yaml
   # Edit my-cluster.yaml to customize settings
   kubectl apply -f my-cluster.yaml
   ```

See the [Examples README](../../examples/README.md) for detailed customization guidance.

### What Happens Next?

The operator will now create several Kubernetes resources to bring your cluster to life:

*   A **StatefulSet** to manage the Neo4j pods.
*   **PersistentVolumeClaims** for storing data and logs.
*   A **headless Service** for StatefulSet pod identity.
*   A **discovery Service** for Kubernetes-based cluster formation.
*   A **client-facing Service** for applications to connect to.
*   A **ConfigMap** with your Neo4j configuration.

### Cluster Formation Process

For multi-server clusters, all pods start simultaneously using ParallelPodManagement:

**Minimal Cluster (2 servers):**
1. **All server pods**: Start simultaneously (ParallelPodManagement)
2. **Cluster formation**: Servers discover each other and self-organize (1-2 minutes)
3. **Ready state**: All servers join the cluster automatically

**Multi-Server Cluster (3+ servers):**
1. **All server pods**: Start simultaneously for optimal formation
2. **Self-organization**: Servers automatically assign roles based on database topology requirements
3. **Database hosting**: Servers can host databases as primaries or secondaries as needed

**Total deployment time**: 2-3 minutes for minimal clusters, 3-5 minutes for multi-server clusters.

You can monitor the progress with `kubectl get pods -w`.

## Accessing Your Deployment

Once the pods are in the `Running` state, you can access your deployment using `kubectl port-forward`:

### For Standalone Deployments
```bash
# For standalone deployment
kubectl port-forward service/standalone-neo4j-service 7474:7474 7687:7687
```

### For Cluster Deployments
```bash
# For minimal cluster
kubectl port-forward service/minimal-cluster-client 7474:7474 7687:7687

# For multi-server cluster
kubectl port-forward service/multi-server-cluster-client 7474:7474 7687:7687

# For your custom cluster (replace with your cluster name)
kubectl port-forward service/YOUR-CLUSTER-NAME-client 7474:7474 7687:7687
```

You can then access the Neo4j Browser at `http://localhost:7474`.

## Creating Databases

Once your cluster is running, you can create and manage databases using the Neo4jDatabase CRD:

### Basic Database Creation

```bash
# Create a simple database
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-database
spec:
  clusterRef: minimal-cluster  # or your cluster name
  name: mydb
  wait: true
  ifNotExists: true
EOF
```

### Database from Existing Backup (Seed URI)

If you have existing Neo4j backups in cloud storage, you can create databases directly from them:

```bash
# Create database from S3 backup
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: restored-database
spec:
  clusterRef: minimal-cluster
  name: restored-db
  seedURI: "s3://my-backups/database.backup"
  topology:
    primaries: 1
    secondaries: 1
  wait: true
  ifNotExists: true
EOF
```

### Property Sharding for Large Datasets

For large datasets that require horizontal scaling, you can enable property sharding (Neo4j 2025.10+, GA as of 2025.12):

```bash
# Create a property sharding enabled cluster
kubectl apply -f examples/property_sharding/basic-property-sharding.yaml

# Create a sharded database with property distribution
kubectl apply -f - <<EOF
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jShardedDatabase
metadata:
  name: large-dataset-db
spec:
  clusterRef: basic-sharding-cluster
  name: largedata
  defaultCypherLanguage: "25"
  propertySharding:
    propertyShards: 4
    graphShard:
      primaries: 3
      secondaries: 0
    propertyShardTopology:
      replicas: 1
  wait: true
EOF
```

For detailed database management, see:
- [Neo4jDatabase API Reference](../api_reference/neo4jdatabase.md)
- [Property Sharding Guide](property_sharding.md)
- [Database Seed URI Guide](../seed-uri-feature-guide.md)
- [Database Examples](../../examples/databases/)
- [Property Sharding Examples](../../examples/property_sharding/)

## Next Steps

Now that you have Neo4j running on Kubernetes:

1. **Explore the Neo4j Browser** - Create some sample data and run queries
2. **Create databases** - Use the Neo4jDatabase CRD to create and manage databases
3. **Connect your applications** - Use the Bolt endpoint (port 7687) for programmatic access
4. **Configure monitoring** - Set up monitoring and alerting for your deployment
5. **Plan backups** - Implement backup strategies for data protection
6. **Scale your deployment** - For clusters, you can scale up/down based on your needs

For more advanced topics, see:
- [Configuration Guide](configuration.md) - Advanced configuration options
- [Security Guide](guides/security.md) - Authentication, TLS, and security best practices
- [Performance Guide](guides/performance.md) - Optimization and scaling strategies
- [Migration Guide](migration_guide.md) - Migrating from previous versions
