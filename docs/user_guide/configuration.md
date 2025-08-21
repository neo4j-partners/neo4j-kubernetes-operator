# Configuration

This guide provides a comprehensive overview of the configuration options available for the `Neo4jEnterpriseCluster` custom resource. The operator allows for a declarative approach to managing your Neo4j clusters, where you define the desired state in a YAML file, and the operator works to make it a reality.

## CRD Specification

The full CRD specification, which details every possible configuration field, can be found in the [API Reference](../api_reference/neo4jenterprisecluster.md).

## Key Configuration Fields

Below are some of the most important fields you will use to configure your cluster. For a complete list, please consult the API reference.

*   `spec.image`: The Neo4j Docker image to use. You can specify the repository, tag, and pull policy.
*   `spec.topology`: Defines the architecture of your cluster, including the total number of servers that will self-organize into primary and secondary roles based on database requirements.
*   `spec.storage`: Configures the persistent storage for the cluster, including storage class and size.
*   `spec.auth`: Manages authentication, allowing you to specify the provider (native, LDAP, etc.) and the secret containing credentials.
*   `spec.license`: A reference to the Kubernetes secret that holds your Neo4j Enterprise license key.
*   `spec.resources`: Allows you to set specific CPU and memory requests and limits for the Neo4j pods, which is crucial for performance tuning.
*   `spec.backups`: Configure automated backups, including the schedule and storage location (S3, GCS, etc.).
*   `spec.monitoring`: Enable and configure monitoring, primarily through the Prometheus exporter.
*   Plugin management: Use separate Neo4jPlugin CRDs to install plugins like APOC and GDS (see [Neo4jPlugin API Reference](../api_reference/neo4jplugin.md)).
*   `spec.multiCluster`: For advanced use cases, this allows you to configure deployments across multiple Kubernetes clusters.
