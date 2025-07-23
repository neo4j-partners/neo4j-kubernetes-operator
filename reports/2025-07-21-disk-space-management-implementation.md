# Disk Space Management Implementation

**Date**: 2025-07-21
**Status**: Complete

## Summary

Implemented comprehensive disk space management for Neo4j backups to prevent the "No space left on device" errors encountered during testing.

## Implementation Details

### 1. Automatic Cleanup in Backup Sidecar

Enhanced the backup sidecar container with automatic cleanup functionality:

```bash
# Function to clean old backups
cleanup_old_backups() {
    local backup_dir="/data/backups"
    local max_age_days="${BACKUP_RETENTION_DAYS:-7}"
    local max_count="${BACKUP_RETENTION_COUNT:-10}"

    # Remove backups older than max_age_days
    find "$backup_dir" -maxdepth 1 -type d -mtime +$max_age_days -exec rm -rf {} \;

    # Keep only the most recent max_count backups
    # ... (count-based cleanup logic)
}
```

**Features:**
- Runs cleanup before and after each backup
- Configurable via environment variables
- Default: 7 days retention, max 10 backups
- Shows disk usage after cleanup

### 2. Pre-Backup Disk Space Check

Added disk space validation before starting backups:

```bash
# Check disk space before backup
DISK_USAGE=$(kubectl exec -n <namespace> <pod> -c backup-sidecar -- df /data | tail -1 | awk '{print $5}' | sed 's/%//')
if [ $DISK_USAGE -gt 90 ]; then
    echo "ERROR: Insufficient disk space. Usage: $DISK_USAGE%"
    exit 1
fi
```

**Benefits:**
- Prevents backup failures due to insufficient space
- Fails fast with clear error message
- Configurable threshold (default: 90% usage)

### 3. Cleanup Script

Created `hack/cleanup-test-resources.sh` for manual cleanup:

**Features:**
- Removes completed jobs older than 1 hour
- Deletes failed and evicted pods
- Identifies orphaned PVCs
- Shows disk usage by namespace
- Cleans Docker system (for Kind clusters)

**Usage:**
```bash
./hack/cleanup-test-resources.sh
```

### 4. Documentation

Created comprehensive disk management guide:
- `docs/user_guide/guides/backup-disk-management.md`

**Covers:**
- Storage sizing calculations
- Backup strategies for space optimization
- Monitoring and alerting
- Troubleshooting disk full errors
- Best practices

## Configuration Options

### Environment Variables

```yaml
env:
- name: BACKUP_RETENTION_DAYS
  value: "7"    # Days to keep backups
- name: BACKUP_RETENTION_COUNT
  value: "10"   # Maximum number of backups
```

### Backup Options

```yaml
options:
  compress: true        # Reduces backup size by 60-80%
  backupType: DIFF     # Differential backups are smaller
```

## Testing Results

1. **Automatic Cleanup**: Successfully removes old backups
2. **Disk Check**: Prevents backups when disk > 90% full
3. **Resource Usage**: Sidecar uses minimal resources (100m CPU, 256Mi memory)

## Best Practices

1. **Production Environments**:
   - Use external storage (S3, GCS, Azure)
   - Set appropriate retention policies
   - Monitor disk usage with alerts

2. **Test Environments**:
   - Shorter retention (3 days, 5 backups)
   - Run cleanup script regularly
   - Use compressed backups

3. **Storage Sizing**:
   ```
   Required = DB Size × Compression × Backups × Safety Factor
   Example: 100GB × 0.3 × 10 × 1.5 = 450GB
   ```

## Benefits

1. **Prevents Failures**: Disk space checks prevent backup failures
2. **Automatic Management**: Cleanup runs without manual intervention
3. **Configurable**: Retention policies can be adjusted per environment
4. **Monitoring**: Clear visibility into disk usage
5. **Recovery**: Manual cleanup script for emergencies

## Future Enhancements

1. **Metrics Export**: Prometheus metrics for backup sizes/counts
2. **Smart Cleanup**: Keep weekly/monthly backups longer
3. **Compression Options**: Different compression levels
4. **Storage Tiering**: Move old backups to cheaper storage

## Conclusion

The disk space management implementation successfully addresses the root cause of backup failures due to insufficient disk space. The combination of automatic cleanup, pre-checks, and manual tools provides a robust solution for both production and test environments.
