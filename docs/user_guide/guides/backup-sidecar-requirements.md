# Backup Sidecar Resource Requirements

## Overview

Standalone deployments use a backup sidecar container. Cluster deployments use a centralized `{cluster}-backup` pod instead of per-pod sidecars. This document focuses on the backup sidecar resources for `Neo4jEnterpriseStandalone`, and notes the cluster backup pod defaults for reference.

## Resource Requirements

### Memory Requirements

The backup sidecar requires sufficient memory to run `neo4j-admin` backup commands, which can be memory-intensive operations.

**Current Configuration (Standalone Sidecar):**
- **Memory Limits**: 1Gi
- **Memory Requests**: 512Mi
- **CPU Limits**: 500m
- **CPU Requests**: 200m

**Cluster Backup Pod Defaults:**
- **Memory Limits**: 1Gi
- **Memory Requests**: 256Mi
- **CPU Limits**: 500m
- **CPU Requests**: 100m

### Why These Memory Limits?

Neo4j admin operations, particularly backups, require significant memory:
- **Neo4j 5.26+ admin tools**: Load JVM with full classpath
- **Backup operations**: Process database metadata and create compressed archives
- **Previous 256Mi limit**: Caused OOMKilled containers and backup failures

### Performance Impact

With the current resource allocation:
- ✅ **Backup operations succeed** without memory constraints
- ✅ **No OOMKilled containers** during backup execution
- ✅ **Stable sidecar operation** with adequate CPU resources
- ✅ **Concurrent backup requests** handled reliably

## Supported Deployment Types

### Neo4jEnterpriseCluster

Clusters use a centralized `{cluster}-backup` pod (container name: `backup`). There is no backup sidecar on server pods.

### Neo4jEnterpriseStandalone

Backup sidecar is automatically added to the standalone pod and handles backup requests via `/backup-requests`.

## Version Support

### Neo4j 5.26+ (SemVer)
- ✅ Full backup support with correct command syntax
- ✅ Uses `--to-path` parameter (not deprecated `--to`)
- ✅ V2_ONLY discovery configuration
- ✅ Proper environment variable sharing
- ✅ Automatic backup path creation (required by Neo4j 5.26+)

### Neo4j 2025.x+ (CalVer)
- ✅ Future-ready version detection
- ✅ Automatic parameter selection (`dbms.kubernetes.discovery.service_port_name`)
- ✅ V2_ONLY discovery (default in 2025.x+)
- ✅ Automatic backup path creation (required by Neo4j 2025.x+)

## Configuration

### Automatic Configuration

The backup sidecar is **automatically configured** for standalone deployments:
- **No manual setup required**
- **Automatic resource allocation**
- **Shared environment variables** with main Neo4j container
- **Proper volume mounts** for data, config, and backup requests

### Environment Variables

The sidecar inherits from the main container and adds:
```yaml
env:
  - name: BACKUP_RETENTION_DAYS
    value: "7"
  - name: BACKUP_RETENTION_COUNT
    value: "10"
  - name: NEO4J_CONF
    value: "/var/lib/neo4j/conf"
  - name: NEO4J_HOME
    value: "/var/lib/neo4j"
```

### Volume Mounts

Required volume mounts:
- `/data` - Data storage and backup destination
- `/backup-requests` - Communication channel for backup requests
- `/var/lib/neo4j/conf` - Neo4j configuration access

## Backup Process

### 1. Request Processing
Backup jobs create JSON requests in `/backup-requests/backup.request`:
```json
{
  "path": "/data/backups/backup-20250721-151209",
  "type": "FULL",
  "database": "neo4j"
}
```

### 2. Path Preparation
The sidecar automatically creates the backup directory before execution:
```bash
mkdir -p $BACKUP_PATH  # Neo4j 5.26+ requires the full path to exist
```

### 3. Command Execution
Sidecar executes Neo4j 5.26+ commands:
```bash
neo4j-admin database backup --include-metadata=all --to-path=$BACKUP_PATH --type=$BACKUP_TYPE --verbose
```

### 4. Cleanup and Retention
- **Automatic cleanup** of old backups based on retention policies
- **Disk space monitoring** before backup execution
- **Status reporting** via `/backup-requests/backup.status`

## Troubleshooting

### Common Issues

**OOMKilled Containers (Pre-1Gi)**
- **Symptom**: Backup sidecar containers restart frequently
- **Solution**: Upgrade to operator version with 1Gi memory limits

**Backup Command Failures**
- **Symptom**: Backup status returns non-zero exit codes
- **Cause**: Insufficient resources, connectivity issues, or path issues (pre-fix)
- **Solution**: Verify resource allocation and Neo4j port 6362 accessibility
- **Note**: Path creation is now handled automatically by the operator

**Environment Variable Issues**
- **Symptom**: Neo4j main container fails to start (standalone only)
- **Cause**: Missing `NEO4J_ACCEPT_LICENSE_AGREEMENT=yes`
- **Solution**: Use updated operator with complete environment configuration

### Verification Commands

Check sidecar resource allocation:
```bash
kubectl get pod <pod-name> -o jsonpath='{.spec.containers[1].resources}'
```

Test backup execution:
```bash
kubectl exec <pod> -c backup-sidecar -- \
  sh -c 'echo "{\"path\":\"/data/backups/test\",\"type\":\"FULL\"}" > /backup-requests/backup.request'
```

Monitor backup status:
```bash
kubectl exec <pod> -c backup-sidecar -- cat /backup-requests/backup.status
```

## Best Practices

1. **Resource Monitoring**: Monitor sidecar memory usage in production
2. **Retention Policies**: Configure appropriate backup retention for your environment
3. **Backup Timing**: Schedule backups during low-traffic periods
4. **Secondary Backups**: Prefer backup from secondary nodes to reduce primary load
5. **Disk Space**: Ensure sufficient disk space for backup operations

## Historical Context

This implementation replaced the previous kubectl-based backup approach with:
- **Improved isolation**: Backup operations in dedicated sidecar
- **Better resource management**: Proper memory allocation prevents failures
- **Enhanced reliability**: No kubectl dependencies in backup jobs
- **Simpler architecture**: Direct communication via shared volumes

## Future Enhancements

- **Progress reporting**: Real-time backup progress updates
- **Metrics export**: Backup operation metrics for monitoring
- **Cloud storage**: Direct cloud storage integration
- **Compression options**: Configurable backup compression levels
