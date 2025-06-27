# Neo4jGrant

This document provides a reference for the `Neo4jGrant` Custom Resource Definition (CRD). This resource is used to grant roles to users in a Neo4j cluster.

For a high-level overview of how to manage security, see the [Security Guide](../user_guide/guides/security.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the grant in. |
| `roleName` | `string` | The name of the role to grant. |
| `username` | `string` | The name of the user to grant the role to. |
