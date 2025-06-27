# Neo4jRestore

This document provides a reference for the `Neo4jRestore` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to restore to. |
| `backupName` | `string` | The name of the backup to restore from. |
