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
*   `spec.resources`: Allows you to set specific CPU and memory requests and limits for the Neo4j pods, which is crucial for performance tuning.
*   `spec.backups`: (Deprecated) Use the separate Neo4jBackup CRD for backup management. The operator now uses a centralized backup StatefulSet for resource efficiency.
*   `spec.queryMonitoring`: Enable query monitoring and Prometheus metrics exposure.
*   **Plugin management**: Use separate Neo4jPlugin CRDs to install plugins like APOC, GDS, Bloom, GenAI, and N10s. The operator automatically handles Neo4j 5.26+ compatibility requirements (see [Neo4jPlugin API Reference](../api_reference/neo4jplugin.md)).
*   `spec.mcp`: Optional Neo4j MCP server deployment for client integrations (HTTP or STDIO). Requires the APOC plugin via Neo4jPlugin; HTTP uses per-request auth and supports Service/Ingress/Route exposure with optional TLS.
*   `spec.tls`: Configure TLS/SSL encryption. Set mode to `cert-manager` and provide an issuerRef for automatic certificate management.
*   `spec.config`: Add custom Neo4j configuration settings as key-value pairs. These are added to neo4j.conf.
*   `spec.env`: Add environment variables to Neo4j pods. Note that NEO4J_AUTH and NEO4J_ACCEPT_LICENSE_AGREEMENT are managed by the operator.
*   `spec.service`: Configure service type (ClusterIP, NodePort, LoadBalancer), annotations, and external access settings (Ingress; OpenShift Route).
*   `spec.propertySharding`: (Neo4j 2025.10+, GA in 2025.12) Enable property sharding for horizontal scaling of large datasets. See the [Property Sharding Guide](property_sharding.md) for detailed configuration options.

## MCP Server

The operator can deploy an optional Neo4j MCP server alongside a cluster or standalone deployment. The MCP server runs as a separate Deployment and connects to the Neo4j service inside the namespace.
For client configuration and HTTP/STDIO usage, see the [MCP Client Setup Guide](guides/mcp_client_setup.md).

### Requirements

*   **APOC**: MCP relies on APOC. Install APOC using the Neo4jPlugin CRD (see [Neo4jPlugin API Reference](../api_reference/neo4jplugin.md)).
*   **Image**: If `spec.mcp.image` is omitted, the operator uses the default MCP image repo and a tag matching `OPERATOR_VERSION` (when set), otherwise `latest`. You can always pin a specific MCP version via `spec.mcp.image.repo` and `spec.mcp.image.tag`.

### Transport Modes

*   **HTTPS (default, preferred)**: No static credentials in the MCP pod. Clients send Basic Auth or Bearer tokens per request. The operator can create a Service, Ingress, and OpenShift Route for exposure. The endpoint path is `/mcp`.
    *   **Benefits**: standard TLS, per-request auth, works well with desktop clients and external access policies.
*   **STDIO (in-cluster only)**: MCP reads credentials from a Kubernetes Secret. Set `spec.mcp.auth.secretName` (defaults to the Neo4j admin secret) and key names if they differ. No Service/Ingress/Route is created for STDIO.

### TLS for HTTP

Configure TLS via `spec.mcp.http.tls`:
*   `mode: disabled` (default)
*   `mode: secret` with `secretName`
*   `mode: cert-manager` with `issuerRef`

When TLS is enabled, the operator mounts the TLS secret and configures MCP to use it. Default ports are `8080` for HTTP and `8443` when TLS is enabled (override with `spec.mcp.http.port`).

### Example: Cluster MCP (HTTP)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: graph-prod
spec:
  image:
    repo: neo4j
    tag: 2025.01.0-enterprise
  topology:
    servers: 3
  storage:
    className: standard
    size: 50Gi
  mcp:
    enabled: true
    image:
      repo: ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp
      tag: vX.Y.Z
    transport: http
    readOnly: true
    http:
      service:
        type: ClusterIP
```

### Example: Standalone MCP (STDIO)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: graph-dev
spec:
  image:
    repo: neo4j
    tag: 5.26.0-enterprise
  storage:
    className: standard
    size: 10Gi
  auth:
    adminSecret: neo4j-admin-secret
  mcp:
    enabled: true
    image:
      repo: ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp
      tag: vX.Y.Z
    transport: stdio
    auth:
      secretName: neo4j-admin-secret
```
