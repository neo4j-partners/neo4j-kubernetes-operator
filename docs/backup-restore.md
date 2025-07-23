# Neo4j Backup and Restore

This guide covers backup and restore operations for Neo4j Enterprise 5.26+ and 2025.x using the Neo4j Kubernetes Operator.

## Overview

The operator provides two CRDs for backup operations:
- `Neo4jBackup` - Manages scheduled and on-demand backups
- `Neo4jRestore` - Handles restore operations from backups

## Backup Operations

### Basic Backup Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-backup
spec:
  clusterRef: my-neo4j-cluster
  target:
    kind: Cluster       # Backup all databases
  schedule:
    cron: "0 2 * * *"   # Daily at 2 AM
  storage:
    type: pvc
    pvc:
      name: backup-pvc
      size: 100Gi
```

### Backup Types

Neo4j 5.26+ supports three backup types:

```yaml
spec:
  options:
    backupType: "FULL"   # FULL, DIFF, or AUTO
    compress: true       # Enable compression (default: true)
    pageCache: "4G"      # Page cache for backup operation
```

- **FULL**: Complete backup of all data
- **DIFF**: Incremental backup (requires previous backup)
- **AUTO**: Automatically choose between FULL and DIFF

### Storage Options

#### PVC Storage
```yaml
spec:
  storage:
    type: pvc
    pvc:
      name: backup-pvc
      storageClassName: fast-ssd
      size: 100Gi
```

#### S3 Storage
```yaml
spec:
  storage:
    type: s3
    bucket: my-neo4j-backups
    path: "backups/production"
    cloud:
      credentialsSecret: aws-backup-credentials
      region: us-east-1
```

#### Google Cloud Storage
```yaml
spec:
  storage:
    type: gcs
    bucket: my-neo4j-backups
    path: "backups/production"
    cloud:
      credentialsSecret: gcs-backup-credentials
```

#### Azure Blob Storage
```yaml
spec:
  storage:
    type: azure
    bucket: mycontainer
    path: "backups/production"
    cloud:
      credentialsSecret: azure-backup-credentials
```

### Backup Targets

#### Cluster Backup (All Databases)
```yaml
spec:
  target:
    kind: Cluster
```

#### Single Database Backup
```yaml
spec:
  target:
    kind: Database
    name: orders    # Specific database name
```

### Advanced Backup Options

#### Backup from Secondary Servers

For clustered deployments, the operator automatically configures backups to run from secondary servers when available:

```yaml
# Automatically handled when cluster has secondaries
spec:
  clusterRef: my-cluster  # If cluster has secondaries, backup uses them
```

#### Retention Policy

```yaml
spec:
  retention:
    maxAge: "7d"        # Keep backups for 7 days
    maxCount: 7         # Keep maximum 7 backups
    deletePolicy: Delete # Delete old backups
```

## Restore Operations

### Basic Restore

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-orders
spec:
  targetCluster: my-neo4j-cluster
  databaseName: orders
  source:
    type: backup
    backupRef: full-backup
```

### Restore with Overwrite

```yaml
spec:
  force: true  # Overwrite existing database
  source:
    type: backup
    backupRef: full-backup
```

### Point-in-Time Recovery

```yaml
spec:
  source:
    type: backup
    backupRef: full-backup
    pointInTime: "2024-01-15T10:30:00Z"
```

### Restore Options

```yaml
spec:
  options:
    verifyBackup: true  # Validate backup before restore
    additionalArgs:
      - "--expand-commands"  # Show detailed progress
```

### Pre/Post Restore Hooks

```yaml
spec:
  preRestoreHooks:
    - type: script
      script: |
        echo "Preparing for restore..."
        # Custom preparation logic

  postRestoreHooks:
    - type: script
      script: |
        echo "Restore completed"
        # Custom post-restore logic
```

## Command Syntax Reference

### Neo4j 5.26.x Commands

**Backup**:
```bash
neo4j-admin database backup [options] [<database>...]
Options:
  --type=<FULL|DIFF|AUTO>
  --compress[=true|false]
  --pagecache=<size>
  --from=<host:port>
```

**Restore**:
```bash
neo4j-admin database restore [options] <database>
Options:
  --from-path=<path>
  --overwrite-destination
  --restore-until=<timestamp>
```

### Neo4j 2025.x Commands

Same as 5.26.x with additional options:
- `--include-metadata=<none|all|users|roles>`
- `--source-database=<name>` (from 2025.02+)

## Best Practices

### Backup Strategy

1. **Use AUTO backup type** for optimal storage efficiency
2. **Schedule backups during low-traffic periods**
3. **Backup from secondary servers** in production clusters
4. **Test restore procedures regularly**
5. **Monitor backup job status and size**

### Storage Recommendations

- **PVC**: Best for on-premise or when cloud storage isn't available
- **S3/GCS/Azure**: Recommended for production with geo-redundancy
- **Compression**: Enable for network efficiency (default: true)
- **Retention**: Balance between storage costs and recovery needs

### Security

1. **Use dedicated service accounts** for cloud storage
2. **Encrypt backups at rest** in cloud storage
3. **Restrict access** to backup storage locations
4. **Rotate credentials** regularly

## Troubleshooting

### Backup Fails

Check backup job logs:
```bash
kubectl logs job/backup-name-backup
```

Common issues:
- Insufficient storage space
- Invalid cloud credentials
- Network connectivity to Neo4j
- Database doesn't exist (for single database backup)

### Restore Fails

Check restore job logs:
```bash
kubectl logs job/restore-name-restore
```

Common issues:
- Backup file not found
- Corrupted backup
- Insufficient permissions
- Database already exists (use `force: true`)

### Slow Backups

- Increase `pageCache` size in backup options
- Check network bandwidth to storage
- Consider using compression
- Backup from secondary servers

## Migration Notes

### From Neo4j 4.x

The operator only supports Neo4j 5.26+. Key differences:
- No `--all-databases` flag (use `--include-metadata=all`)
- No `--check-consistency` flag (separate command)
- Different restore syntax (`--from-path` instead of `--from`)

### Version Detection

The operator automatically detects Neo4j version and uses appropriate command syntax. No manual configuration needed.
