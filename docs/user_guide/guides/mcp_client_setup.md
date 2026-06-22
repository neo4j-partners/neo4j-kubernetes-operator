# MCP Client Setup

This guide explains how to connect MCP clients (VSCode, Claude Desktop, curl) to a Neo4j MCP server deployed by the operator.

The operator uses the **official `mcp/neo4j` image** — the supported Neo4j product MCP server published by Neo4j, Inc.

| Attribute | Detail |
|---|---|
| **Image** | [`mcp/neo4j`](https://hub.docker.com/r/mcp/neo4j) |
| **Source** | [github.com/neo4j/mcp](https://github.com/neo4j/mcp) |
| **Default port** | `8080` (K8s-friendly; the image's own default is 80/443) |
| **MCP endpoint path** | `/mcp` (fixed, not configurable) |
| **Transports** | `http` (default) or `stdio` |
| **Documentation** | [neo4j.com/docs/mcp](https://neo4j.com/docs/mcp/current/) |

> **Prerequisite**: APOC must be installed in your Neo4j deployment. The `get-schema` tool uses APOC for schema introspection. Install it via the `Neo4jPlugin` CRD.

## Before You Start

1. Deploy MCP using `spec.mcp` on either `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`.
2. Install APOC with the `Neo4jPlugin` CRD (required by the `get-schema` tool).
3. Ensure the MCP Service is reachable from your client.

See [Configuration: MCP Server](../configuration.md#mcp-server) for the full `spec.mcp` reference.

## Minimal example

```yaml
spec:
  mcp:
    enabled: true
    readOnly: true   # disable write-cypher tool
```

The operator injects `NEO4J_URI` automatically. In HTTP mode (the default) no credentials are stored in the pod — they travel with each client request.

## Authentication Model

The `mcp/neo4j` image uses **different authentication for each transport**:

| Transport | How credentials reach Neo4j |
|---|---|
| **HTTP** | Per-request `Authorization` header (Basic Auth `username:password` or Bearer token). The operator does NOT inject `NEO4J_USERNAME`/`NEO4J_PASSWORD`. Configure your MCP client with Neo4j credentials. |
| **STDIO** | `NEO4J_USERNAME` and `NEO4J_PASSWORD` env vars injected by the operator from the admin secret (or a custom secret via `spec.mcp.auth`). |

This means:

- **HTTP mode**: each client request carries its own credentials — ideal for multi-user or shared deployments.
- **STDIO mode**: the operator manages a single set of credentials — ideal for in-cluster automation.

## Available Tools

| Tool | Read-only | Description |
|---|---|---|
| `get-schema` | Yes | Introspect labels, relationship types, and property keys (requires APOC) |
| `read-cypher` | Yes | Execute read-only Cypher queries |
| `write-cypher` | No | Execute write/admin Cypher queries — disabled when `readOnly: true` |
| `list-gds-procedures` | Yes | List available GDS procedures; auto-disabled if GDS is not installed |

## Choose a Transport

### HTTP (default — recommended)

The operator creates a `Deployment` + `ClusterIP Service` (`<name>-mcp:8080`). Expose the service via Ingress, Route, or LoadBalancer for external access. The MCP endpoint is always at `/mcp`.

**In HTTP mode the server starts immediately without connecting to Neo4j at startup.** Connections are made per-request using the credentials from the request's `Authorization` header.

### STDIO (in-cluster only)

No Service is created. The operator injects Neo4j credentials from the admin secret. Use STDIO only when the MCP client runs inside the cluster and can exec into the pod, or for in-cluster automation jobs.

## Test with curl (HTTP mode)

```bash
# From inside the cluster (any pod in the same namespace):
curl -X POST http://<name>-mcp.<namespace>.svc.cluster.local:8080/mcp \
  -H "Authorization: Basic $(echo -n 'neo4j:your-password' | base64)" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

Replace `neo4j:your-password` with your Neo4j username and password. The credentials are passed per-request and used to connect to Neo4j for that request.

```bash
# Port-forward to test locally:
kubectl port-forward svc/<name>-mcp 8080:8080 &
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Basic $(echo -n 'neo4j:your-password' | base64)" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## Expose via Ingress (HTTP mode)

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
          # TLS at the Ingress level (recommended over container TLS):
          tlsSecretName: neo4j-mcp-tls
```

With an Ingress, the endpoint is `https://neo4j-mcp.example.com/mcp`.

## Container-level TLS (HTTP mode)

The `mcp/neo4j` image supports built-in TLS when you provide a certificate. This is an alternative to Ingress TLS termination.

```yaml
spec:
  mcp:
    enabled: true
    http:
      tls:
        secretName: my-mcp-tls-secret   # kubernetes.io/tls secret
        # certKey: tls.crt (default)
        # keyKey: tls.key (default)
```

The secret is mounted read-only. The operator injects `NEO4J_MCP_HTTP_TLS_ENABLED=true` and the cert/key file paths. The default port becomes `8443` when TLS is configured.

> **Recommendation**: For most Kubernetes deployments, terminate TLS at the Ingress layer and leave `spec.mcp.http.tls` unset.

## VSCode

Create or edit `.vscode/mcp.json`:

```json
{
  "servers": {
    "neo4j": {
      "type": "http",
      "url": "http://<mcp-host>:8080/mcp"
    }
  }
}
```

VSCode will prompt for credentials or use the `Authorization` header you configure. Replace `<mcp-host>` with the Ingress hostname or `kubectl port-forward` address. When TLS is terminated at the Ingress or via container TLS (`spec.mcp.http.tls`, port `8443`), use `https://` instead.

## Claude Desktop

Edit `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "neo4j": {
      "type": "http",
      "url": "http://<mcp-host>:8080/mcp"
    }
  }
}
```

Configure Basic Auth with your Neo4j username and password in the MCP client settings or via the `Authorization` header. When TLS is terminated at the Ingress or via container TLS (`spec.mcp.http.tls`, port `8443`), use `https://` instead.

## STDIO Mode: In-Cluster Usage

```yaml
spec:
  mcp:
    enabled: true
    transport: stdio
    # Optional: use a different secret for Neo4j credentials.
    # Defaults to the cluster/standalone admin secret.
    auth:
      secretName: my-readonly-user
      usernameKey: username
      passwordKey: password
```

Because no Service is created, STDIO is for clients running inside the cluster. For desktop clients, use HTTP instead.

## Advanced: custom port

```yaml
spec:
  mcp:
    enabled: true
    http:
      port: 9080
      # Optional: override the HTTP Authorization header name
      # (useful when a proxy rewrites it to a custom header)
      authHeaderName: "X-Neo4j-Authorization"
```

## Advanced: read-only mode

```yaml
spec:
  mcp:
    enabled: true
    readOnly: true   # disables write-cypher tool
```

## Advanced: target a specific database

```yaml
spec:
  mcp:
    enabled: true
    database: movies
    schemaSampleSize: 500
```

## Advanced: logging and telemetry

```yaml
spec:
  mcp:
    enabled: true
    telemetry: false          # disable anonymous usage data
    logLevel: debug           # debug|info|notice|warning|error|critical|alert|emergency
    logFormat: json           # text|json (useful for log aggregators)
```
