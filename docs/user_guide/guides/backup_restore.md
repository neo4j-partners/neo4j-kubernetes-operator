# Backup and Restore

This guide explains how to use the operator to back up and restore your Neo4j Enterprise clusters. The operator uses `Neo4jBackup` and `Neo4jRestore` Custom Resources to manage these operations declaratively.

## Creating a Backup

To create a backup, you define a `Neo4jBackup` resource. The operator will then create a Kubernetes `Job` or `CronJob` to perform the backup using Neo4j's official backup tools.

The operator supports backing up to a variety of storage backends, including S3, GCS, and Azure Blob Storage, as well as any Kubernetes-supported `PersistentVolumeClaim`.

### Example: Scheduled S3 Backup

This example creates a backup that runs every day at 2 AM and stores the data in an S3 bucket.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: my-daily-s3-backup
spec:
  clusterName: my-neo4j-cluster
  schedule: "0 2 * * *" # Cron schedule
  storage:
    s3:
      bucket: my-backup-bucket
      region: us-east-1
```

## Restoring from a Backup

To restore from a backup, you create a `Neo4jRestore` resource. The operator will create a Kubernetes `Job` to perform the restore. The restore process will temporarily scale down the Neo4j cluster, perform the restore, and then scale the cluster back up.

### Example: Restore from a `Neo4jBackup` resource

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: my-neo4j-restore
spec:
  clusterName: my-neo4j-cluster
  backupName: my-daily-s3-backup # Restore from the backup defined above
```

For more detailed information on all the available options, see the [Neo4jBackup API Reference](../../api_reference/neo4jbackup.md) and [Neo4jRestore API Reference](../../api_reference/neo4jrestore.md).
