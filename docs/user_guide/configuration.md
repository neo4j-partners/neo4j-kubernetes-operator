# Configuration

This guide provides a comprehensive overview of the configuration options available for the `Neo4jEnterpriseCluster` custom resource.

## CRD Specification

The full CRD specification can be found in the [API Reference](../api_reference/neo4jenterprisecluster.md).

## Key Configuration Fields

*   `spec.image`: The Neo4j Docker image to use.
*   `spec.topology`: The number of primary and secondary replicas.
*   `spec.storage`: The storage configuration for the cluster.
*   `spec.auth`: The authentication provider and secret.
*   `spec.license`: The Neo4j Enterprise license secret.
*   `spec.resources`: The CPU and memory resources for the Neo4j pods.
*   `spec.backups`: The backup configuration.
*   `spec.monitoring`: The monitoring configuration.
*   `spec.plugins`: The plugin configuration.
*   `spec.autoScaling`: The autoscaling configuration.
*   `spec.multiCluster`: The multi-cluster configuration.
