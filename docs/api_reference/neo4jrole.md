# Neo4jRole

This document provides a reference for the `Neo4jRole` Custom Resource Definition (CRD). This resource is used to create and manage roles in a Neo4j cluster.

For a high-level overview of how to manage security, see the [Security Guide](../user_guide/guides/security.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the role in. |
| `roleName` | `string` | The name of the role. |
| `permissions` | `[]string` | The permissions for the role. |
