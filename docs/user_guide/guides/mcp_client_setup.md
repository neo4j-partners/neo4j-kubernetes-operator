# MCP Client Setup

This guide explains how to connect MCP clients (VSCode, Claude Desktop, curl) to a Neo4j MCP server deployed by the operator.

The operator uses the **official `mcp/neo4j-cypher` image** from Docker Hub (Verified Publisher by Neo4j). No custom image build is required.

- **Image**: [`mcp/neo4j-cypher`](https://hub.docker.com/r/mcp/neo4j-cypher)
- **Default port**: `8000`
- **Default endpoint path**: `/mcp/`
- **Transports**: `http` (default) or `stdio`

## Before You Start

1. Deploy MCP using `spec.mcp` on either `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`.
2. Install APOC with the `Neo4jPlugin` CRD (`get_neo4j_schema` requires APOC's schema inspection procedures).
3. Ensure the MCP Service is reachable from your client.

See [Configuration: MCP Server](../configuration.md#mcp-server) for deployment setup details.

## Minimal cluster example

```yaml
spec:
  mcp:
    enabled: true
    # image defaults to mcp/neo4j-cypher:latest — no need to specify
    readOnly: true   # disable write tools
```

The operator automatically injects `NEO4J_URL`, `NEO4J_USERNAME`, and `NEO4J_PASSWORD` from the cluster admin secret.

## Choose a Transport

### HTTP (default)

Deploys a `Deployment` + `ClusterIP Service` (`<name>-mcp:8000`). Expose the service via Ingress, Route, or LoadBalancer. The MCP endpoint path is `/mcp/` (trailing slash required by the official image).

### STDIO (in-cluster only)

No Service is created. Use STDIO only when the MCP client runs inside the cluster (for example, a sidecar or Kubernetes Job) and can exec into the pod.

## HTTP Mode: Test with curl

```bash
# Inside-cluster test (works from any pod in the same namespace)
curl -X POST http://<name>-mcp.<namespace>.svc.cluster.local:8000/mcp/ \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

The official image does not enforce HTTP-level client authentication (credentials are used by the MCP server to connect to Neo4j, not to authenticate API callers). Apply authentication at the Ingress or network policy layer if needed.

## HTTP Mode: Expose via Ingress

```yaml
spec:
  mcp:
    enabled: true
    http:
      service:
        type: ClusterIP
        ingress:
          enabled: true
          host: neo4j-mcp.example.com
          className: nginx
          # TLS at the ingress level (official image does not terminate TLS):
          tlsSecretName: neo4j-mcp-tls
```

With an Ingress, reach the endpoint at `https://neo4j-mcp.example.com/mcp/`.

## VSCode

Create or edit `.vscode/mcp.json`:

```json
{
  "servers": {
    "neo4j": {
      "type": "http",
      "url": "http://<mcp-host>:8000/mcp/"
    }
  }
}
```

Replace `<mcp-host>` with the Ingress host or `kubectl port-forward` address.

## Claude Desktop

Edit `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "neo4j": {
      "type": "http",
      "url": "http://<mcp-host>:8000/mcp/"
    }
  }
}
```

## Advanced: custom port and path

```yaml
spec:
  mcp:
    enabled: true
    http:
      port: 9000
      path: /neo4j-mcp/
      allowedOrigins: "https://myapp.example.com"
      allowedHosts: "myapp.example.com,localhost"
      readTimeout: 60    # seconds
```

## Advanced: restrict to read-only queries

```yaml
spec:
  mcp:
    enabled: true
    readOnly: true   # disables write_neo4j_cypher tool
```

## Advanced: target a specific database

```yaml
spec:
  mcp:
    enabled: true
    database: movies    # default database for all queries
    schemaSampleSize: 500
    responseTokenLimit: 20000
```

## STDIO Mode: In-Cluster Usage

Configure STDIO and provide a secret with Neo4j credentials (defaults to the admin secret if `auth` is omitted):

```yaml
spec:
  mcp:
    enabled: true
    transport: stdio
    # Optional: override credentials (defaults to cluster admin secret)
    auth:
      secretName: my-readonly-user
      usernameKey: username
      passwordKey: password
```

Because no Service is created, STDIO is best for clients running inside the cluster. For desktop clients, use HTTP instead.

## Available Tools

| Tool | Description | Read-Only Safe |
|---|---|---|
| `get_neo4j_schema` | Returns nodes, properties, and relationships (uses APOC) | Yes |
| `read_neo4j_cypher` | Executes a read Cypher query | Yes |
| `write_neo4j_cypher` | Executes a write Cypher query | No (disabled when `readOnly: true`) |
