# Configuration

This guide provides a comprehensive overview of the configuration options available for both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` custom resources. The operator allows for a declarative approach to managing your Neo4j deployments, where you define the desired state in a YAML file, and the operator works to make it a reality.

## CRD Specification

The full CRD specifications, which detail every possible configuration field, can be found in the API Reference:
- [Neo4jEnterpriseCluster](../api_reference/neo4jenterprisecluster.md) - For clustered deployments
- [Neo4jEnterpriseStandalone](../api_reference/neo4jenterprisestandalone.md) - For single-node deployments

## Key Configuration Fields

Below are some of the most important fields you will use to configure your cluster. For a complete list, please consult the API reference.

*   `spec.image`: The Neo4j Docker image to use. Requires Neo4j Enterprise 5.26+ or 2025.x. You can specify the repository (e.g., `neo4j`), tag (e.g., `5.26-enterprise`), and pull policy.
*   `spec.topology`: (Cluster only) Defines the architecture of your cluster. Specify the total number of servers (minimum 2) that will self-organize into primary and secondary roles based on database requirements. You can optionally configure server role constraints.
*   `spec.storage`: Configures the persistent storage for the cluster, including storage class and size.
*   `spec.auth`: Manages authentication, allowing you to specify the provider (native, LDAP, etc.) and the secret containing credentials.
*   `spec.license`: (Optional) A reference to the Kubernetes secret that holds your Neo4j Enterprise license key. For evaluation, you can use `NEO4J_ACCEPT_LICENSE_AGREEMENT=eval` in environment variables.
*   `spec.resources`: Allows you to set specific CPU and memory requests and limits for the Neo4j pods, which is crucial for performance tuning.
*   `spec.backups`: (Deprecated) Use the separate Neo4jBackup CRD for backup management. The operator now uses a centralized backup StatefulSet for resource efficiency.
*   `spec.monitoring`: Enable and configure monitoring, primarily through the Prometheus exporter.
*   **Plugin management**: Use separate Neo4jPlugin CRDs to install plugins like APOC, GDS, Bloom, GenAI, and N10s. The operator automatically handles Neo4j 5.26+ compatibility requirements (see [Neo4jPlugin API Reference](../api_reference/neo4jplugin.md)).
*   `spec.tls`: Configure TLS/SSL encryption. Set mode to `cert-manager` and provide an issuerRef for automatic certificate management.
*   `spec.config`: Add custom Neo4j configuration settings as key-value pairs. These are added to neo4j.conf.
*   `spec.env`: Add environment variables to Neo4j pods. Note that NEO4J_AUTH and NEO4J_ACCEPT_LICENSE_AGREEMENT are managed by the operator.
*   `spec.service`: Configure service type (ClusterIP, NodePort, LoadBalancer), annotations, and external access settings (Ingress; OpenShift Route).
*   `spec.propertySharding`: (Neo4j 2025.10+, GA in 2025.12) Enable property sharding for horizontal scaling of large datasets. See the [Property Sharding Guide](property_sharding.md) for detailed configuration options.
