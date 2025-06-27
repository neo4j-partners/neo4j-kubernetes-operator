# Backup and Restore

This guide explains how to back up and restore your Neo4j Enterprise clusters.

## Creating a Backup

To create a backup, you can create a `Neo4jBackup` resource. The operator supports backing up to a variety of storage backends, including S3, GCS, and Azure Blob Storage.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: my-neo4j-backup
spec:
  clusterName: my-neo4j-cluster
  storage:
    s3:
      bucket: my-backup-bucket
      region: us-east-1
```

## Restoring from a Backup

To restore from a backup, you can create a `Neo4jRestore` resource.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: my-neo4j-restore
spec:
  clusterName: my-neo4j-cluster
  backupName: my-neo4j-backup
```
