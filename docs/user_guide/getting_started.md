# Getting Started

This guide will walk you through the process of deploying your first Neo4j Enterprise cluster on Kubernetes using the Neo4j Enterprise Operator.

## Prerequisites

*   A Kubernetes cluster (v1.19+).
*   `kubectl` installed and configured.
*   A Neo4j Enterprise license.

## Installation

For detailed installation instructions, see the [Installation Guide](installation.md).

For a quick start, you can install the operator with a single command:

```bash
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-operator.yaml
```

## Deploying a Cluster

1.  **Create a secret for your Neo4j password:**

    ```bash
    kubectl create secret generic neo4j-auth --from-literal=password=my-secret-password
    ```

2.  **Create a secret for your Neo4j Enterprise license:**

    ```bash
    kubectl create secret generic neo4j-license --from-file=license.txt
    ```

3.  **Create a `Neo4jEnterpriseCluster` resource:**

    ```yaml
    apiVersion: neo4j.neo4j.com/v1alpha1
    kind: Neo4jEnterpriseCluster
    metadata:
      name: my-neo4j-cluster
    spec:
      image:
        repo: neo4j
        tag: "5.26-enterprise"
      topology:
        primaries: 3
        secondaries: 1
      storage:
        className: "standard"
        size: "10Gi"
      auth:
        secretRef: neo4j-auth
      license:
        secretRef: neo4j-license
    ```

4.  **Apply the resource to your cluster:**

    ```bash
    kubectl apply -f my-neo4j-cluster.yaml
    ```

## Accessing Your Cluster

Once the cluster is running, you can access it using `kubectl port-forward`:

```bash
kubectl port-forward service/my-neo4j-cluster-client 7474:7474 7687:7687
```

You can then access the Neo4j Browser at `http://localhost:7474`.
