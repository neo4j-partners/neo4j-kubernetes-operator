# MCP Client Setup

This guide explains how to connect MCP clients (VSCode, Claude Desktop, curl) to an MCP server deployed by the Neo4j Kubernetes Operator. It focuses on the operator-managed MCP deployment and **HTTPS as the preferred transport**.

## Before You Start

1. Deploy MCP using `spec.mcp` on either `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`.
2. Install APOC with the Neo4jPlugin CRD (MCP requires APOC).
3. Ensure the MCP service is reachable (Service, Ingress, or OpenShift Route).

See [Configuration: MCP Server](../configuration.md#mcp-server) for deployment setup details.

## Choose a Transport

### HTTPS (preferred for operator deployments)

HTTPS is the default and preferred transport for operator-managed MCP. It supports per-request authentication and can be exposed via Service, Ingress, or OpenShift Route. The MCP endpoint path is `/mcp`.

Benefits of HTTPS:
- Works cleanly for desktop and external clients.
- Per-request auth (no static credentials stored in the MCP pod).
- TLS encryption and standard ingress/route policies.
- Easier to operate with standard Kubernetes networking and security controls.

### STDIO (in-cluster only, niche use)

STDIO is intended for local/in-cluster usage. The operator does not create a Service/Ingress/Route for STDIO. Use STDIO only when the MCP client runs inside the cluster (for example, in a Job or sidecar) and can access the MCP process directly.

## HTTPS Mode: Get the MCP Endpoint

Pick one of the following exposure options:

- **Service (ClusterIP/NodePort/LoadBalancer)**: Use the service DNS name inside the cluster or the external address (if applicable).
- **Ingress**: Use the configured host and path `/mcp`.
- **OpenShift Route**: Use the route host and path `/mcp`.

Use `https` with the configured port (default `8443`). Only use `http` and port `8080` for local or non-TLS setups.

## HTTPS Mode: Authentication

HTTP requests must include credentials **per request**:

- **Basic Auth**: Standard username/password (for Neo4j native auth).
- **Bearer Token**: SSO/OAuth tokens for supported environments.

Do **not** set `NEO4J_USERNAME` or `NEO4J_PASSWORD` in MCP when using HTTP/S transport.

## HTTPS Mode: Test with curl

```bash
curl -X POST https://<mcp-host>:<port>/mcp \
  -u neo4j:password \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## VSCode (HTTPS)

Create or edit your `mcp.json`:

```json
{
  "servers": {
    "neo4j-mcp": {
      "type": "http",
      "url": "https://<mcp-host>:<port>/mcp",
      "headers": {
        "Authorization": "Basic <base64-username-password>"
      }
    }
  }
}
```

Generate the header value:

```bash
echo -n "neo4j:password" | base64
```

Then restart VSCode and verify with “List Neo4j MCP tools”.

## Claude Desktop (HTTPS)

Edit `claude_desktop_config.json` and add the MCP server:

```json
{
  "mcpServers": {
    "neo4j-mcp": {
      "type": "http",
      "url": "https://<mcp-host>:<port>/mcp",
      "headers": {
        "Authorization": "Basic <base64-username-password>"
      }
    }
  }
}
```

## STDIO Mode: In-Cluster Usage

When using STDIO, the MCP server reads credentials from a Kubernetes Secret (defaulting to the Neo4j admin secret). Because no Service is created, STDIO is best for in-cluster clients that can exec into the MCP pod or run side-by-side.

If you need a desktop client, use HTTPS instead.
