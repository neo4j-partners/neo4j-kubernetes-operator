# Neo4jGrant

This document provides a reference for the `Neo4jGrant` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the grant in. |
| `roleName` | `string` | The name of the role to grant. |
| `username` | `string` | The name of the user to grant the role to. |
