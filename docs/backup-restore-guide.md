# Neo4j Enterprise Backup & Restore Guide

This guide covers comprehensive backup and restore operations for Neo4j Enterprise clusters managed by the Neo4j Enterprise Operator.

## Quick Start

### Simple Database Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: my-database-backup
  namespace: neo4j
spec:
  target:
    kind: Database
    name: production
  storage:
    type: pvc
    pvc:
      size: 50Gi
      storageClassName: standard
  options:
    compress: true
    verify: true
```

### Restore from Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-my-database
  namespace: neo4j
spec:
  targetCluster: production-cluster
  source:
    type: backup
    backupRef: my-database-backup
  databaseName: production-restored
  force: false
  stopCluster: true
```

## Backup Operations

### Backup Types

1. **One-time Backup**: Manual backup execution
2. **Scheduled Backup**: Automated backups using cron expressions
3. **Emergency Backup**: High-priority, fast backups

### Storage Options

#### Local Storage (PVC)

```yaml
storage:
  type: pvc
  pvc:
    size: 100Gi
    storageClassName: fast-ssd
```

#### AWS S3

```yaml
storage:
  type: s3
  bucket: my-neo4j-backups
  path: /cluster-backups
  cloud:
    provider: aws
    identity:
      provider: aws
      serviceAccount: neo4j-backup-sa
```

#### Google Cloud Storage

```yaml
storage:
  type: gcs
  bucket: neo4j-backup-bucket
  path: /backups
  cloud:
    provider: gcp
    identity:
      provider: gcp
      serviceAccount: neo4j-backup@project.iam.gserviceaccount.com
```

#### Azure Blob Storage

```yaml
storage:
  type: azure
  bucket: neo4j-backups
  path: /cluster-backups
  cloud:
    provider: azure
    identity:
      provider: azure
      serviceAccount: neo4j-backup-identity
```

### Scheduled Backups

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-backup
spec:
  target:
    kind: Cluster
    name: production-cluster
  storage:
    type: s3
    bucket: daily-backups
  schedule: "0 2 * * *"  # Daily at 2 AM
  options:
    compress: true
    verify: false
  retention:
    maxAge: "7d"
    maxCount: 7
    deletePolicy: Delete
```

### Backup Encryption

```yaml
options:
  encryption:
    enabled: true
    keySecret: backup-encryption-key
    algorithm: AES256
```

## Restore Operations

### Restore Sources

#### From Backup Resource

```yaml
source:
  type: backup
  backupRef: production-backup
```

#### From Direct Storage

```yaml
source:
  type: storage
  storage:
    type: s3
    bucket: backups
  backupPath: /backups/cluster-20240620-143000
```

### Advanced Restore Options

#### Pre/Post Restore Hooks

```yaml
options:
  preRestore:
    cypherStatements:
      - "CALL dbms.components()"
      - "SHOW DATABASES"
  postRestore:
    cypherStatements:
      - "CREATE INDEX IF NOT EXISTS FOR (n:User) ON (n.email)"
    job:
      template:
        container:
          image: custom-script:latest
          command: ["python"]
          args: ["/scripts/post-restore.py"]
```

#### Point-in-Time Restore (PITR)

Point-in-Time Recovery allows you to restore your database to a specific moment in time using a combination of base backups and transaction logs.

##### Basic PITR Configuration

```yaml
source:
  type: pitr
  pointInTime: "2024-06-20T14:30:00Z"
  pitr:
    logStorage:
      type: s3
      bucket: neo4j-transaction-logs
      path: /logs/cluster-name
    logRetention: "7d"
    recoveryPointObjective: "1m"
    baseBackup:
      type: backup
      backupRef: latest-base-backup
    validateLogIntegrity: true
    compression:
      enabled: true
      algorithm: gzip
      level: 6
    encryption:
      enabled: true
      algorithm: AES256
      keySecret: pitr-encryption-key
      keySecretKey: key
```

##### Advanced PITR with Multiple Base Backups

```yaml
source:
  type: pitr
  pointInTime: "2024-06-20T14:30:00Z"
  pitr:
    logStorage:
      type: s3
      bucket: neo4j-logs
      path: /transaction-logs
      cloud:
        provider: aws
        identity:
          provider: aws
          serviceAccount: neo4j-pitr-sa
    logRetention: "30d"
    recoveryPointObjective: "30s"
    baseBackup:
      type: storage
      storage:
        type: s3
        bucket: neo4j-base-backups
        path: /base-backups/cluster-20240620-020000
    validateLogIntegrity: true
    compression:
      enabled: true
      algorithm: zstd
      level: 3
    encryption:
      enabled: true
      algorithm: ChaCha20Poly1305
      keySecret: advanced-encryption-key
```

##### PITR with Local Storage

```yaml
source:
  type: pitr
  pointInTime: "2024-06-20T14:30:00Z"
  pitr:
    logStorage:
      type: pvc
      pvc:
        name: neo4j-transaction-logs
        size: 100Gi
        storageClassName: fast-ssd
    logRetention: "14d"
    recoveryPointObjective: "5m"
    baseBackup:
      type: backup
      backupRef: weekly-base-backup
    validateLogIntegrity: true
```

##### PITR Configuration Options

| Option | Description | Default | Example |
|--------|-------------|---------|---------|
| `logStorage` | Location for transaction logs | Required | S3, GCS, Azure, PVC |
| `logRetention` | How long to keep transaction logs | `7d` | `30d`, `168h`, `1w` |
| `recoveryPointObjective` | Maximum data loss tolerance | `1m` | `30s`, `5m`, `1h` |
| `baseBackup` | Reference to base backup | Required | Backup ref or storage location |
| `validateLogIntegrity` | Verify log files before restore | `true` | `true`, `false` |
| `compression.algorithm` | Compression method for logs | `gzip` | `gzip`, `lz4`, `zstd` |
| `encryption.algorithm` | Encryption method for logs | `AES256` | `AES256`, `ChaCha20Poly1305` |

## Best Practices

### Backup Strategy

1. **Regular Scheduled Backups**: Implement daily or weekly automated backups
2. **Retention Policies**: Configure appropriate retention to balance cost and recovery needs
3. **Verification**: Enable backup verification for critical data
4. **Encryption**: Use encryption for sensitive data backups
5. **Cross-Region Storage**: Store backups in different regions for disaster recovery

### Restore Planning

1. **Test Restores**: Regularly test restore procedures in non-production environments
2. **Downtime Planning**: Plan for cluster downtime during large restores
3. **Staging Environment**: Use staging clusters for restore validation
4. **Data Consistency**: Verify data consistency after restore operations

### Performance Optimization

#### Fast Backups

```yaml
options:
  compress: false  # Skip compression for speed
  verify: false    # Skip verification for speed
  additionalArgs:
    - "--parallel-recovery"
    - "--force"
```

#### Efficient Storage

```yaml
options:
  compress: true   # Enable compression for storage efficiency
  encryption:
    enabled: true
    algorithm: ChaCha20  # Fast encryption algorithm
```

## Monitoring and Troubleshooting

### Backup Status Monitoring

```bash
# Check backup status
kubectl get neo4jbackups -n neo4j

# Get detailed backup information
kubectl describe neo4jbackup production-backup -n neo4j

# View backup logs
kubectl logs -l app.kubernetes.io/component=backup -n neo4j
```

### Restore Status Monitoring

```bash
# Check restore progress
kubectl get neo4jrestores -n neo4j

# Monitor restore job
kubectl get jobs -l app.kubernetes.io/component=restore -n neo4j

# View restore logs
kubectl logs -l app.kubernetes.io/component=restore -n neo4j -f
```

### Common Issues

#### Backup Failures

1. **Storage Access**: Verify storage credentials and permissions
2. **Network Connectivity**: Check network policies and firewall rules
3. **Neo4j Health**: Ensure cluster is healthy before backup
4. **Resource Limits**: Verify sufficient CPU/memory for backup jobs

#### Restore Failures

1. **Cluster State**: Ensure target cluster is ready
2. **Database Conflicts**: Check for existing databases when not using force
3. **Resource Requirements**: Verify sufficient storage and compute resources
4. **Network Access**: Ensure restore jobs can access storage and Neo4j cluster

### Debugging Commands

```bash
# Check backup job details
kubectl describe job backup-job-name -n neo4j

# View backup pod logs
kubectl logs backup-pod-name -n neo4j

# Check restore job status
kubectl get job restore-job-name -n neo4j -o yaml

# View cluster events
kubectl get events -n neo4j --sort-by='.lastTimestamp'
```

## Security Considerations

### IAM/RBAC Configuration

#### AWS IAM Policy Example

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::neo4j-backups",
        "arn:aws:s3:::neo4j-backups/*"
      ]
    }
  ]
}
```

#### Kubernetes RBAC

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: neo4j-backup-sa
  namespace: neo4j
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: neo4j-backup-role
  namespace: neo4j
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["persistentvolumeclaims"]
  verbs: ["get", "list", "create"]
```

### Secret Management

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: backup-encryption-key
  namespace: neo4j
type: Opaque
data:
  key: <base64-encoded-encryption-key>
```

## Production Examples

See [config/samples/backup-restore-examples.yaml](../config/samples/backup-restore-examples.yaml) for comprehensive production-ready examples including:

- Enterprise-grade scheduled backups
- Multi-cloud storage configurations
- Advanced restore scenarios
- Emergency backup procedures
- Cross-namespace restore operations

## Support

For issues related to backup and restore operations:

1. Check the [troubleshooting section](#monitoring-and-troubleshooting) above
2. Review operator logs: `kubectl logs -l app.kubernetes.io/name=neo4j-operator -n neo4j-system`
3. Verify Neo4j cluster health before backup/restore operations
4. Ensure proper storage access and network connectivity
