# Installation

This guide provides detailed instructions for installing the Neo4j Enterprise Operator.

## Installation Methods

### 1. Using `kubectl`

This is the simplest way to install the operator:

```bash
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-operator.yaml
```

### 2. Using Helm

If you use Helm to manage your Kubernetes applications, you can install the operator with the following commands:

```bash
helm repo add neo4j https://helm.neo4j.com/
helm repo update
helm install neo4j-operator neo4j/neo4j-operator
```

## Verifying the Installation

After installation, you can verify that the operator is running by checking the pods in the `neo4j-operator-system` namespace:

```bash
kubectl get pods -n neo4j-operator-system
```

You should see a pod named `neo4j-operator-controller-manager-...` in the `Running` state.
