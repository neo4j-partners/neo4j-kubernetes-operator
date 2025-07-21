# Backup Disk Space Management Guide

This guide covers disk space management for Neo4j backups in Kubernetes environments.

## Overview

Neo4j backups can consume significant disk space, especially in production environments with:
- Large databases
- Frequent backup schedules
- Multiple backup types (FULL, DIFF, AUTO)
- Long retention policies

## Automatic Cleanup

### Backup Sidecar Retention

The backup sidecar container automatically manages disk space with configurable retention policies:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  # ... other configuration ...
  podTemplate:
    spec:
      containers:
      - name: backup-sidecar
        env:
        - name: BACKUP_RETENTION_DAYS
          value: "14"  # Keep backups for 14 days
        - name: BACKUP_RETENTION_COUNT
          value: "20"  # Keep maximum 20 backups
```

Default retention settings:
- `BACKUP_RETENTION_DAYS`: 7 days
- `BACKUP_RETENTION_COUNT`: 10 backups

The sidecar automatically:
1. Removes backups older than retention days
2. Keeps only the most recent N backups
3. Runs cleanup before and after each backup

## Manual Cleanup

### Using the Cleanup Script

For test environments or emergency cleanup:

```bash
# Run the cleanup script
./hack/cleanup-test-resources.sh

# What it does:
# - Removes completed jobs older than 1 hour
# - Deletes failed and evicted pods
# - Identifies orphaned PVCs
# - Shows disk usage by namespace
# - Cleans Docker system (for Kind clusters)
```

### Manual Commands

Check disk usage:
```bash
# Check PV usage
kubectl get pv -o custom-columns=NAME:.metadata.name,CAPACITY:.spec.capacity.storage,CLAIM:.spec.claimRef.name

# Check node disk usage
kubectl describe nodes | grep -A5 "Allocated resources:"

# Check specific PVC usage
kubectl exec <neo4j-pod> -- df -h /data
```

Clean up old backups manually:
```bash
# Delete backups older than 7 days
kubectl exec <neo4j-pod> -c backup-sidecar -- \
  find /data/backups -maxdepth 1 -type d -mtime +7 -exec rm -rf {} \;

# Keep only 5 most recent backups
kubectl exec <neo4j-pod> -c backup-sidecar -- bash -c \
  'cd /data/backups && ls -t | tail -n +6 | xargs -r rm -rf'
```

## Best Practices

### 1. Storage Sizing

Calculate required storage:
```
Required Storage = Database Size × Backup Compression Ratio × Number of Retained Backups × Safety Factor

Example:
- Database Size: 100GB
- Compression Ratio: 0.3 (70% compression)
- Retained Backups: 10
- Safety Factor: 1.5
- Required: 100GB × 0.3 × 10 × 1.5 = 450GB
```

### 2. Backup Strategy

Optimize backup types:
```yaml
# Daily full backups with short retention
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-full
spec:
  schedule: "0 2 * * *"  # 2 AM daily
  options:
    backupType: FULL
    compress: true
  retention:
    maxAge: "3d"
    maxCount: 3

# Hourly differential backups
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: hourly-diff
spec:
  schedule: "0 * * * *"  # Every hour
  options:
    backupType: DIFF
    compress: true
  retention:
    maxAge: "1d"
    maxCount: 24
```

### 3. Monitoring

Set up alerts for disk usage:
```yaml
# Prometheus alert example
groups:
- name: neo4j-backups
  rules:
  - alert: BackupDiskSpaceHigh
    expr: |
      (1 - (node_filesystem_avail_bytes{mountpoint="/data"} /
      node_filesystem_size_bytes{mountpoint="/data"})) > 0.8
    for: 10m
    annotations:
      summary: "Backup disk usage above 80%"
```

### 4. External Storage

For production, consider external storage:

#### S3 Storage
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: s3-backup
spec:
  storage:
    type: s3
    bucket: my-neo4j-backups
    path: production/cluster-1
  retention:
    maxAge: "30d"  # S3 lifecycle policies handle cleanup
```

#### PVC with StorageClass
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: backup-pvc
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: fast-ssd  # Use appropriate storage class
  resources:
    requests:
      storage: 500Gi
```

## Troubleshooting

### Disk Full Errors

Symptoms:
```
java.io.IOException: No space left on device
```

Quick fixes:
1. Run cleanup script: `./hack/cleanup-test-resources.sh`
2. Delete old backups: `kubectl exec <pod> -c backup-sidecar -- rm -rf /data/backups/old-*`
3. Increase PVC size (if storage class supports expansion)

### Prevention

1. **Set appropriate retention policies**
   ```yaml
   env:
   - name: BACKUP_RETENTION_DAYS
     value: "3"  # Shorter for test environments
   - name: BACKUP_RETENTION_COUNT
     value: "5"  # Fewer backups for test
   ```

2. **Use compressed backups**
   ```yaml
   options:
     compress: true  # Reduces backup size by 60-80%
   ```

3. **Monitor disk usage proactively**
   ```bash
   # Add to monitoring scripts
   kubectl exec <pod> -- df -h /data | awk '$5+0 > 80 {print "WARNING: " $0}'
   ```

## Summary

Effective disk space management requires:
- Automatic cleanup via sidecar retention policies
- Regular monitoring of disk usage
- Appropriate backup strategies (FULL vs DIFF)
- External storage for production environments
- Proactive cleanup in test environments

The backup sidecar's built-in cleanup functionality handles most scenarios automatically, but manual intervention may be needed for test environments or exceptional situations.
