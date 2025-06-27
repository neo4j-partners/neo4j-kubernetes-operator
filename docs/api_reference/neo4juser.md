# Neo4jUser

This document provides a reference for the `Neo4jUser` Custom Resource Definition (CRD). This resource is used to create and manage users in a Neo4j cluster.

For a high-level overview of how to manage security, see the [Security Guide](../user_guide/guides/security.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the user in. |
| `username` | `string` | The name of the user. |
| `passwordSecret` | `string` | The name of the secret containing the user's password. |
