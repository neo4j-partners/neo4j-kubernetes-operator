# Neo4jUser

This document provides a reference for the `Neo4jUser` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the user in. |
| `username` | `string` | The name of the user. |
| `passwordSecret` | `string` | The name of the secret containing the user's password. |
