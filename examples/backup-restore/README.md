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

**🎯 Success:** Status shows `Completed` with `BackupSuccessful` condition.

### Choose Your Path:
- **🟢 New to backups?** Continue with [PVC examples](#basic-backups) below
- **🟡 Production ready?** Jump to [cloud storage examples](#cloud-storage-examples)
- **🔴 Enterprise needs?** Explore [PITR examples](#point-in-time-recovery-pitr)

## Examples Overview

### 🟢 Basic Backups (Start Here)
- [`backup-pvc-simple.yaml`](backup-pvc-simple.yaml) - **Beginner**: Simple one-time backup to PVC
- [`backup-s3-basic.yaml`](backup-s3-basic.yaml) - **Intermediate**: Basic S3 backup

### 🟡 Scheduled Backups (Production Ready)
- [`backup-scheduled-daily.yaml`](backup-scheduled-daily.yaml) - **Intermediate**: Daily scheduled backup

### 🟢 Simple Restores (Start Here)
- [`restore-from-backup.yaml`](restore-from-backup.yaml) - **Beginner**: Restore from backup reference

### 🔴 Point-in-Time Recovery (PITR) - Enterprise
- [`restore-pitr-basic.yaml`](restore-pitr-basic.yaml) - **Advanced**: Basic PITR restore
- [`pitr-setup-complete.yaml`](pitr-setup-complete.yaml) - **Advanced**: Complete PITR setup

## Running Examples

### 1. PVC Backup Example
```bash
# Create storage class if needed
kubectl apply -f storage-class-fast.yaml

# Run PVC backup
kubectl apply -f backup-pvc-simple.yaml

# Monitor progress
kubectl get neo4jbackup simple-backup -w
kubectl describe neo4jbackup simple-backup
```

### 2. S3 Backup Example
```bash
# Create AWS credentials secret
kubectl create secret generic aws-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=your-access-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret-key

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

# Wait for base backup to complete
kubectl wait --for=condition=Ready neo4jbackup/pitr-base-backup --timeout=600s

# Perform PITR restore
kubectl apply -f restore-pitr-basic.yaml

# Monitor restore progress
kubectl get neo4jrestore pitr-restore -w
```

## Storage Configuration

### AWS S3
```bash
# Using IAM roles (recommended)
kubectl annotate serviceaccount neo4j-operator \
  eks.amazonaws.com/role-arn=arn:aws:iam::ACCOUNT:role/neo4j-backup-role

# Using access keys
kubectl create secret generic aws-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=your-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret
```

### Google Cloud Storage
```bash
# Create service account key secret
kubectl create secret generic gcs-credentials \
  --from-file=service-account.json=path/to/service-account.json
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
3. **Verify Backups**: Enable backup verification to ensure data integrity
4. **Secure Credentials**: Use proper secret management for cloud credentials
5. **Plan Retention**: Implement appropriate retention policies for your use case
6. **Document Procedures**: Document your backup and restore procedures
7. **Automate Monitoring**: Set up alerts for backup failures

## Troubleshooting

For detailed troubleshooting information, see:
- [Backup and Restore Troubleshooting Guide](../../docs/user_guide/guides/troubleshooting_backup_restore.md)
- [Backup and Restore User Guide](../../docs/user_guide/guides/backup_restore.md)

## Support

- **Documentation**: [Neo4j Operator Documentation](../../docs/)
- **Issues**: [GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues)
- **Community**: [Neo4j Community Forum](https://community.neo4j.com/)
