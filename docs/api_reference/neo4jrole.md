# Neo4jRole

This document provides a reference for the `Neo4jRole` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the role in. |
| `roleName` | `string` | The name of the role. |
| `permissions` | `[]string` | The permissions for the role. |
