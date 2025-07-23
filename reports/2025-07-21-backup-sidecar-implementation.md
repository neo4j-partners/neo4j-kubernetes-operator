# Neo4j Backup Sidecar Implementation

**Date**: 2025-07-21
**Status**: Implementation Complete

## Summary

Implemented a sidecar container pattern for Neo4j backups to address the requirement that Neo4j 5.x backups must run locally on the Neo4j pod, not from remote pods.

## Implementation Details

### 1. Sidecar Container

Added a backup sidecar container to each Neo4j pod that:
- Runs alongside the main Neo4j container
- Monitors `/backup-requests/backup.request` file for backup requests
- Executes `neo4j-admin database backup` locally when triggered
- Writes status to `/backup-requests/backup.status`
- Shares the data volume with the main container

### 2. Backup Job Pattern

Modified backup jobs to:
- Use `kubectl exec` to trigger backups via the sidecar
- Write JSON backup requests to the sidecar's request file
- Monitor the status file for completion
- Support both one-time and scheduled (CronJob) backups

### 3. ServiceAccount Requirements

Created ServiceAccount with necessary permissions:
```yaml
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]
```

## Key Components

### Sidecar Container Specification
```go
backupSidecar := corev1.Container{
    Name:  "backup-sidecar",
    Image: neo4jImage,
    Command: []string{"/bin/bash", "-c", backupScript},
    VolumeMounts: []corev1.VolumeMount{
        {Name: DataVolume, MountPath: "/data"},
        {Name: "backup-requests", MountPath: "/backup-requests"},
    },
    Resources: corev1.ResourceRequirements{
        Limits: corev1.ResourceList{
            corev1.ResourceCPU:    resource.MustParse("100m"),
            corev1.ResourceMemory: resource.MustParse("128Mi"),
        },
    },
}
```

### Backup Request Format
```json
{
    "path": "/data/backups/backup-name",
    "type": "FULL",
    "database": "neo4j"  // Optional, omit for cluster backup
}
```

### Backup Triggering Process
1. Backup controller creates a Job/CronJob
2. Job uses `kubectl exec` to write request to sidecar
3. Sidecar detects request and runs backup locally
4. Sidecar writes exit status to status file
5. Job monitors status and reports completion

## Benefits

1. **Neo4j 5.x Compatible**: Backups run locally as required
2. **No Network Issues**: No need for remote connections to port 6362
3. **Resource Efficient**: Minimal resource usage (100m CPU, 128Mi memory)
4. **Flexible**: Supports all backup types and options
5. **Kubernetes Native**: Uses standard patterns (sidecar, kubectl exec)

## Testing

To test the implementation:

1. Deploy the ServiceAccount:
```bash
kubectl apply -f examples/backup/backup-serviceaccount.yaml
```

2. Deploy a cluster with the updated operator

3. Create a backup:
```bash
kubectl apply -f /tmp/test-backup-simple.yaml
```

4. Monitor the backup job:
```bash
kubectl logs -f job/test-backup-simple-backup
```

5. Check sidecar logs:
```bash
kubectl logs test-cluster-primary-0 -c backup-sidecar
```

## Future Enhancements

1. **Cloud Storage**: Add support for uploading to S3/GCS/Azure after backup
2. **Compression**: Add configurable compression options
3. **Progress Monitoring**: Add backup progress reporting
4. **Retention**: Implement automatic cleanup based on retention policies
5. **Metrics**: Export backup metrics for monitoring

## Conclusion

The sidecar pattern successfully addresses Neo4j 5.x backup requirements while maintaining a clean, Kubernetes-native architecture. This approach is more reliable and maintainable than trying to run backups from separate pods.
