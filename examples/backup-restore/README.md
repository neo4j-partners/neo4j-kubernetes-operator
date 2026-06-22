# Backup and Restore Examples

This directory contains practical examples for Neo4j backup and restore operations using the Kubernetes operator.

## Prerequisites

Before running these examples:

1. **Neo4j Operator** is installed and running
2. **Neo4j Enterprise cluster** (version 5.26.0+ or 2025.01.0+) is deployed
3. **Admin credentials** are configured in a secret
4. **Storage backend** is properly configured

## 🚀 5-Minute Quick Start

**New to backup and restore?** Get a working backup in 5 minutes:

```bash
# 1. Create admin credentials (if not already done)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

# 2. Deploy a simple backup
kubectl apply -f backup-pvc-simple.yaml

# 3. Watch it work!
kubectl get neo4jbackup simple-backup -w
```

**🎯 Success:** Status shows phase `Completed`, with the `Ready` condition `True` (reason `BackupSucceeded`). Note this applies to **one-shot** backups only — scheduled backups stay in phase `Scheduled` and record their runs in `status.history[]` instead.

### Choose Your Path:
- **🟢 New to backups?** Continue with [PVC examples](#basic-backups) below
- **🟡 Production ready?** Jump to [cloud storage examples](#cloud-storage-examples)
- **🔴 Enterprise needs?** Explore [PITR examples](#point-in-time-recovery-pitr)

## Examples Overview

### 🟢 Basic Backups (Start Here)
- [`backup-pvc-simple.yaml`](backup-pvc-simple.yaml) - **Beginner**: Simple one-time backup to PVC
- [`backup-s3-basic.yaml`](backup-s3-basic.yaml) - **Intermediate**: Basic S3 backup
- [`backup-with-type.yaml`](backup-with-type.yaml) - **Intermediate**: Cluster backup to PVC with explicit `backupType`/`pageCache`
- [`backup-minio.yaml`](backup-minio.yaml) - **Intermediate**: Backup to a self-hosted S3-compatible store (MinIO)

### 🟡 Scheduled Backups (Production Ready)
- [`backup-scheduled-daily.yaml`](backup-scheduled-daily.yaml) - **Intermediate**: Daily scheduled backup
- [`backup-incremental.yaml`](backup-incremental.yaml) - **Intermediate**: Scheduled `AUTO` (full-then-diff) backup of a single database to S3

### 🟢 Simple Restores (Start Here)
- [`restore-from-backup.yaml`](restore-from-backup.yaml) - **Beginner**: Restore from backup reference
- [`restore-overwrite.yaml`](restore-overwrite.yaml) - **Intermediate**: Destructive overwrite of an existing database (`force` + `replaceExisting`)

### 🔴 Point-in-Time Recovery (PITR) - Enterprise
- [`restore-pitr-basic.yaml`](restore-pitr-basic.yaml) - **Advanced**: Basic PITR restore
- [`pitr-setup-complete.yaml`](pitr-setup-complete.yaml) - **Advanced**: Complete PITR setup

## Running Examples

### 1. PVC Backup Example
```bash
# Run PVC backup (the operator provisions the PVC using your cluster's
# default StorageClass — see the comments in the example to pin one)
kubectl apply -f backup-pvc-simple.yaml

# Monitor progress
kubectl get neo4jbackup simple-backup -w
kubectl describe neo4jbackup simple-backup
```

### 2. S3 Backup Example
```bash
# Create AWS credentials secret — all three keys are required (the backup Job
# reads AWS_REGION as a non-optional env var)
kubectl create secret generic aws-backup-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=your-access-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret-key \
  --from-literal=AWS_REGION=us-east-1

# Run S3 backup
kubectl apply -f backup-s3-basic.yaml

# Check backup status
kubectl get neo4jbackup s3-backup -o wide
```

### 3. Scheduled Backup Example
```bash
# Deploy daily backup schedule
kubectl apply -f backup-scheduled-daily.yaml

# Check CronJob creation
kubectl get cronjobs

# View backup history
kubectl get neo4jbackup daily-backup -o jsonpath='{.status.history}'
```

### 4. PITR Example
```bash
# Set up complete PITR environment
kubectl apply -f pitr-setup-complete.yaml

# Wait for the first scheduled run to succeed. Do NOT use
# `kubectl wait --for=condition=Ready` here — scheduled backups never leave
# phase Scheduled (only one-shot backups reach Completed/Ready), so that wait
# hangs forever. Poll status.history instead:
until kubectl get neo4jbackup pitr-base-backup \
    -o jsonpath='{.status.history[*].status}' | grep -q Succeeded; do
  sleep 10
done

# Perform PITR restore
kubectl apply -f restore-pitr-basic.yaml

# Monitor restore progress
kubectl get neo4jrestore pitr-restore -w
```

## Storage Configuration

### AWS S3

**Using IAM roles / IRSA (recommended).** Backup and restore Jobs run as the
operator-managed `neo4j-backup-sa` / `neo4j-restore-sa` ServiceAccounts — *not*
the operator's own SA. Don't `kubectl annotate` anything manually; declare the
role binding in the backup spec and the operator applies it to `neo4j-backup-sa`
on every reconcile:

```yaml
spec:
  storage:
    type: s3
    bucket: my-neo4j-backups
    cloud:
      provider: aws
      identity:
        provider: aws
        autoCreate:
          annotations:
            eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT:role/neo4j-backup-role
```

```bash
# Using access keys (all three keys required)
kubectl create secret generic aws-backup-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=your-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret \
  --from-literal=AWS_REGION=us-east-1
```

### Google Cloud Storage
```bash
# Create service account key secret — the key MUST be named
# GOOGLE_APPLICATION_CREDENTIALS_JSON and contain the JSON as a string value
# (not a file path):
kubectl create secret generic gcs-credentials \
  --from-literal=GOOGLE_APPLICATION_CREDENTIALS_JSON="$(cat path/to/service-account.json)"
```

### Azure Blob Storage
```bash
# Create storage account secret
kubectl create secret generic azure-credentials \
  --from-literal=AZURE_STORAGE_ACCOUNT=your-account \
  --from-literal=AZURE_STORAGE_KEY=your-key
```

## Monitoring and Debugging

### Check Status
```bash
# List all backups and restores
kubectl get neo4jbackups
kubectl get neo4jrestores

# View detailed status
kubectl describe neo4jbackup <backup-name>
kubectl describe neo4jrestore <restore-name>
```

### View Logs
```bash
# Backup job logs
kubectl logs job/<backup-name>-backup

# Restore job logs
kubectl logs job/<restore-name>-restore

# Operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

### Debug Issues
```bash
# Check events
kubectl get events --sort-by=.metadata.creationTimestamp

# Check resource usage
kubectl top pods

# Validate storage access
kubectl exec -it <backup-pod> -- ls -la /backup/
```

## Cleanup

### Remove Backup Resources
```bash
# Delete specific backup
kubectl delete neo4jbackup <backup-name>

# Delete all backups
kubectl delete neo4jbackups --all

# Delete all restores
kubectl delete neo4jrestores --all
```

### Cleanup Storage
```bash
# Clean up PVC backups
kubectl delete pvc backup-storage

# Clean up cloud storage (manual)
aws s3 rm s3://your-bucket/neo4j-backups/ --recursive
gsutil rm -r gs://your-bucket/neo4j-backups/
az storage blob delete-batch --source your-container --pattern "neo4j-backups/*"
```

## Best Practices

1. **Test Regularly**: Test backup and restore procedures in non-production environments
2. **Monitor Storage**: Set up monitoring for storage usage and backup completion
3. **Validate Backups**: Set `spec.options.validate: true` to run `neo4j-admin backup validate` after each backup (result recorded on `status.history[].validation`)
4. **Secure Credentials**: Use proper secret management for cloud credentials
5. **Plan Retention**: Implement appropriate retention policies for your use case
6. **Document Procedures**: Document your backup and restore procedures
7. **Automate Monitoring**: Set up alerts for backup failures

## Troubleshooting

For detailed troubleshooting information, see:
- [Backup and Restore Troubleshooting Guide](../../docs/user_guide/troubleshooting/backup_restore.md)
- [Backup and Restore User Guide](../../docs/user_guide/guides/backup_restore.md)

## Support

- **Documentation**: [Neo4j Operator Documentation](../../docs/)
- **Issues**: [GitHub Issues](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues)
- **Community**: [Neo4j Community Forum](https://community.neo4j.com/)
