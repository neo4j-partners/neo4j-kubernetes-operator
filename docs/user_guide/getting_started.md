# Getting Started

This guide will walk you through the process of deploying your first Neo4j Enterprise cluster on Kubernetes using the Neo4j Enterprise Operator.

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

## Deploying a Cluster

We provide ready-to-use examples for common scenarios. Choose the one that fits your needs:

### Option 1: Single-node Cluster (Recommended for Development)

1.  **Create admin credentials:**

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password=your-secure-password
    ```

2.  **Deploy the cluster:**

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/single-node.yaml
    ```

### Option 2: Three-node Cluster (Recommended for Production)

1.  **Create admin credentials:**

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password=your-secure-password
    ```

2.  **Deploy the cluster:**

    ```bash
    # For production (with TLS)
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/three-node-cluster.yaml

    # For testing (TLS disabled)
    kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/three-node-simple.yaml
    ```

3.  **Monitor deployment (3-5 minutes expected):**

    ```bash
    kubectl get pods -l app.kubernetes.io/name=neo4j -w
    ```

### Option 3: Custom Configuration

If you need a custom configuration, create your own manifest based on our examples:

1. **Browse the examples directory:**
   - [Single-node cluster](../../examples/clusters/single-node.yaml) - For development and testing
   - [Three-node cluster](../../examples/clusters/three-node-cluster.yaml) - For production HA (with TLS)
   - [Three-node simple](../../examples/clusters/three-node-simple.yaml) - For testing HA (TLS disabled)
   - [Cluster with read replicas](../../examples/clusters/cluster-with-read-replicas.yaml) - For read scaling

2. **Copy and customize an example:**
   ```bash
   curl -o my-cluster.yaml https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/clusters/single-node.yaml
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

1. **Pod 0** (bootstrap): Forms the initial cluster (0-2 minutes)
2. **Pod 1**: Joins the cluster (2-4 minutes)
3. **Pod 2**: Joins the cluster (4-6 minutes)

**Total deployment time**: 3-5 minutes for a three-node cluster.

You can monitor the progress with `kubectl get pods -w`.

## Accessing Your Cluster

Once the pods are in the `Running` state, you can access the cluster using `kubectl port-forward`:

```bash
# For single-node cluster
kubectl port-forward service/single-node-cluster-client 7474:7474 7687:7687

# For three-node cluster (production)
kubectl port-forward service/three-node-cluster-client 7474:7474 7687:7687

# For three-node simple cluster (testing)
kubectl port-forward service/three-node-simple-client 7474:7474 7687:7687

# For your custom cluster (replace with your cluster name)
kubectl port-forward service/YOUR-CLUSTER-NAME-client 7474:7474 7687:7687
```

You can then access the Neo4j Browser at `http://localhost:7474`.
