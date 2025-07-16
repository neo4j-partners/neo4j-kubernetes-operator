# Getting Started

This guide will walk you through the process of deploying your first Neo4j Enterprise instance on Kubernetes using the Neo4j Enterprise Operator. You can choose between clustered and standalone deployments based on your needs.

## Prerequisites

*   A Kubernetes cluster (v1.21+).
*   `kubectl` installed and configured.
*   A Neo4j Enterprise license.

## Installation

For detailed installation instructions, see the [Installation Guide](installation.md).

For a quick start, you can install the operator with a single command:

```bash
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml
```

## Choosing Your Deployment Type

The Neo4j Enterprise Operator supports two deployment types:

- **Neo4jEnterpriseStandalone**: Single-node deployments for development and testing
- **Neo4jEnterpriseCluster**: Clustered deployments for production with high availability

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
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/standalone/single-node-standalone.yaml
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
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/minimal-cluster.yaml
    ```

3.  **Monitor deployment (2-3 minutes expected):**

    ```bash
    kubectl get pods -l app.kubernetes.io/name=neo4j -w
    ```

### Option 2: Multi-Primary Cluster (Recommended for Production)

1.  **Create admin credentials:**

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password=your-secure-password
    ```

2.  **Deploy the cluster:**

    ```bash
    # For production (with TLS)
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/multi-primary-cluster.yaml

    # For testing (TLS disabled)
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/multi-primary-simple.yaml
    ```

3.  **Monitor deployment (3-5 minutes expected):**

    ```bash
    kubectl get pods -l app.kubernetes.io/name=neo4j -w
    ```

### Option 3: Custom Configuration

If you need a custom configuration, create your own manifest based on our examples:

1. **Browse the examples directory:**
   - [Minimal cluster](../../examples/clusters/minimal-cluster.yaml) - 1 primary + 1 secondary (minimum cluster topology)
   - [Multi-primary cluster](../../examples/clusters/multi-primary-cluster.yaml) - Production HA with TLS
   - [Multi-primary simple](../../examples/clusters/multi-primary-simple.yaml) - Testing HA (TLS disabled)
   - [Kubernetes discovery cluster](../../examples/clusters/k8s-discovery-cluster.yaml) - Production with automatic discovery

2. **Copy and customize an example:**
   ```bash
   curl -o my-cluster.yaml https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/minimal-cluster.yaml
   # Edit my-cluster.yaml to customize settings
   kubectl apply -f my-cluster.yaml
   ```

See the [Examples README](../../examples/README.md) for detailed customization guidance.

### What Happens Next?

The operator will now create several Kubernetes resources to bring your cluster to life:

*   A **StatefulSet** to manage the Neo4j pods.
*   **PersistentVolumeClaims** for storing data and logs.
*   A **headless Service** for internal cluster discovery.
*   A **client-facing Service** for applications to connect to.
*   A **ConfigMap** with your Neo4j configuration.

### Cluster Formation Process

For multi-node clusters, pods start sequentially:

**Minimal Cluster (1 primary + 1 secondary):**
1. **Pod 0** (primary): Forms the initial cluster (0-2 minutes)
2. **Pod 1** (secondary): Joins the cluster (2-3 minutes)

**Multi-Primary Cluster (3 primaries):**
1. **Pod 0** (bootstrap): Forms the initial cluster (0-2 minutes)
2. **Pod 1**: Joins the cluster (2-4 minutes)
3. **Pod 2**: Joins the cluster (4-6 minutes)

**Total deployment time**: 2-3 minutes for minimal clusters, 3-5 minutes for multi-primary clusters.

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

# For multi-primary cluster
kubectl port-forward service/multi-primary-cluster-client 7474:7474 7687:7687

# For your custom cluster (replace with your cluster name)
kubectl port-forward service/YOUR-CLUSTER-NAME-client 7474:7474 7687:7687
```

You can then access the Neo4j Browser at `http://localhost:7474`.

## Next Steps

Now that you have Neo4j running on Kubernetes:

1. **Explore the Neo4j Browser** - Create some sample data and run queries
2. **Connect your applications** - Use the Bolt endpoint (port 7687) for programmatic access
3. **Configure monitoring** - Set up monitoring and alerting for your deployment
4. **Plan backups** - Implement backup strategies for data protection
5. **Scale your deployment** - For clusters, you can scale up/down based on your needs

For more advanced topics, see:
- [Configuration Guide](configuration.md) - Advanced configuration options
- [Security Guide](guides/security.md) - Authentication, TLS, and security best practices
- [Performance Guide](guides/performance.md) - Optimization and scaling strategies
- [Migration Guide](migration_guide.md) - Migrating from previous versions
