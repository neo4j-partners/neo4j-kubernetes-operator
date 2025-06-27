# Neo4jDatabase

This document provides a reference for the `Neo4jDatabase` Custom Resource Definition (CRD). This resource is used to create and manage databases within a Neo4j cluster.

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to create the database in. |
| `databaseName` | `string` | The name of the database. |
