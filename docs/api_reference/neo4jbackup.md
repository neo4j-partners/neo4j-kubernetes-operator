# Neo4jBackup

This document provides a reference for the `Neo4jBackup` Custom Resource Definition (CRD). This resource is used to define and manage backups of your Neo4j clusters.

For a high-level overview of how to use this resource, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to back up. |
| `storage` | `StorageLocation` | The storage location for the backup. |
| `schedule` | `string` | The cron schedule for the backup. |
