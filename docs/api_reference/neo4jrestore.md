# Neo4jRestore

This document provides a reference for the `Neo4jRestore` Custom Resource Definition (CRD). This resource is used to restore a Neo4j cluster from a backup.

For a high-level overview of how to use this resource, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to restore to. |
| `backupName` | `string` | The name of the backup to restore from. |
