# Neo4jBackup

This document provides a reference for the `Neo4jBackup` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to back up. |
| `storage` | `StorageLocation` | The storage location for the backup. |
| `schedule` | `string` | The cron schedule for the backup. |
