# Installation

This guide provides detailed instructions for installing the Neo4j Enterprise Operator.

## Installation Methods

There are two primary ways to install the Neo4j Enterprise Operator:

### 1. Using `kubectl` (Direct Manifests)

This is the simplest and most direct way to install the operator. It's recommended for quick setups and development environments.

```bash
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml
```

This command applies the necessary Custom Resource Definitions (CRDs), the operator `Deployment`, and all the required RBAC permissions (`ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`).

### 2. Using Helm (For Operator Installation)

You can also install the Neo4j Kubernetes Operator using Helm. This method provides greater customization options for the operator deployment itself.

```bash
# Add the Neo4j Helm repository
helm repo add neo4j https://helm.neo4j.com/
helm repo update

# Install the operator into its own namespace
helm install neo4j-operator neo4j/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace
```

This chart handles the installation of the CRDs and the operator deployment, and you can customize the installation using a `values.yaml` file.

## Verifying the Installation

After installation, you can verify that the operator is running by checking the pods in the `neo4j-operator-system` namespace (or the namespace you installed it to):

```bash
kubectl get pods -n neo4j-operator-system
```

You should see a pod named `neo4j-operator-controller-manager-...` in the `Running` state. This indicates the operator is ready to start managing Neo4j clusters.
