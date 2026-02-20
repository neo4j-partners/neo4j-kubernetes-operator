# Neo4j Database Seed URI Feature Guide

This guide explains how to create Neo4j databases from existing backups or dumps using the seed URI feature in the Neo4j Kubernetes Operator.

## Overview

The seed URI feature allows you to create new Neo4j databases by restoring them from existing backup files stored in cloud storage or accessible via HTTP/FTP. This is useful for:

- **Database Migration**: Moving databases between environments
- **Testing with Production Data**: Creating test databases from production backups
- **Disaster Recovery**: Restoring databases to specific points in time
- **Development Environment Setup**: Seeding development databases with sample data

## Supported URI Schemes

The operator supports the following URI schemes through Neo4j's CloudSeedProvider:

| Scheme | Description | Example |
|--------|-------------|---------|
| `s3://` | Amazon S3 | `s3://my-bucket/backup.backup` |
| `gs://` | Google Cloud Storage | `gs://my-bucket/backup.backup` |
| `azb://` | Azure Blob Storage | `azb://account.blob.core.windows.net/container/backup.backup` |
| `https://` | HTTPS URLs | `https://backup-server.com/backup.backup` |
| `http://` | HTTP URLs | `http://backup-server.com/backup.backup` |
| `ftp://` | FTP servers | `ftp://ftp.server.com/backup.backup` |

## Basic Usage

### Simple Seed URI Database

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-database
spec:
  clusterRef: my-cluster
  name: mydb

  # Seed from S3 backup using system-wide authentication
  seedURI: "s3://my-backups/database.backup"

  # Optional: specify database topology
  topology:
    primaries: 2
    secondaries: 1

  wait: true
  ifNotExists: true
```

## Authentication Methods

### 1. System-Wide Authentication (Recommended)

Use cloud-native authentication mechanisms that don't require explicit credentials:

**AWS S3:**
- IAM roles for service accounts (IRSA)
- EC2 instance profiles
- Environment variables on nodes

**Google Cloud Storage:**
- Workload Identity
- Service account keys via mounted volumes
- Compute Engine default service accounts

**Azure Blob Storage:**
- Managed identities
- Service principal environment variables

### 2. Explicit Credentials via Secrets

For environments where system-wide authentication isn't available:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: backup-credentials
data:
  AWS_ACCESS_KEY_ID: <base64-encoded-key>
  AWS_SECRET_ACCESS_KEY: <base64-encoded-secret>
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-database
spec:
  clusterRef: my-cluster
  name: mydb
  seedURI: "s3://my-backups/database.backup"

  seedCredentials:
    secretRef: backup-credentials
```

## Credential Requirements by Provider

### Amazon S3
**Required:**
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`

**Optional:**
- `AWS_SESSION_TOKEN` (for temporary credentials)
- `AWS_REGION`

### Google Cloud Storage
**Required:**
- `GOOGLE_APPLICATION_CREDENTIALS` (service account JSON key)

**Optional:**
- `GOOGLE_CLOUD_PROJECT`

### Azure Blob Storage
**Required:**
- `AZURE_STORAGE_ACCOUNT`
- Either `AZURE_STORAGE_KEY` OR `AZURE_STORAGE_SAS_TOKEN`

### HTTP/HTTPS/FTP
**Optional:**
- `USERNAME`
- `PASSWORD`
- `AUTH_HEADER` (for custom authentication)

## Advanced Configuration

### Point-in-Time Recovery (Neo4j 2025.x only)

```yaml
seedConfig:
  # Restore to specific timestamp
  restoreUntil: "2025-01-15T10:30:00Z"

  # Or restore to specific transaction ID
  restoreUntil: "txId:12345"
```

### CloudSeedProvider Options

```yaml
seedConfig:
  config:
    # Compression: gzip, lz4, none
    compression: "gzip"

    # Validation: strict, lenient
    validation: "strict"

    # Buffer size for processing
    bufferSize: "128MB"
```

## File Format Considerations

### Backup Files (.backup) - Recommended
- **Performance**: Much faster restore times
- **Features**: Support for point-in-time recovery, compression
- **Use Cases**: Production workloads, large datasets

### Dump Files (.dump) - Legacy
- **Performance**: Slower restore times for large datasets
- **Compatibility**: Cross-version compatibility, human-readable
- **Use Cases**: Development, testing, cross-version migrations

The operator will warn when using dump files:
```
Warning: Using dump file format. For better performance with large databases,
         consider using Neo4j backup format (.backup) instead.
```

## Database Topology with Seed URIs

You can specify how the restored database should be distributed across your cluster:

```yaml
topology:
  primaries: 2    # Number of primary servers
  secondaries: 3  # Number of secondary servers
```

The operator validates that your topology doesn't exceed cluster capacity and provides warnings for suboptimal configurations.

## Conflict Prevention

The operator prevents conflicting configurations:

```yaml
spec:
  # ERROR: Cannot specify both seedURI and initialData
  seedURI: "s3://my-backups/database.backup"
  initialData:
    cypherStatements:
      - "CREATE (:Person {name: 'Alice'})"
```

When `seedURI` is specified, `initialData` is ignored since the seed provides the initial data.

## Status and Events

The operator provides detailed status and events during seed restoration:

**Events:**
- `DatabaseCreatedFromSeed`: Database successfully created from seed URI
- `DataSeeded`: Database seeded from URI successfully
- `ValidationWarning`: Validation warnings (e.g., suboptimal topology)

**Status Conditions:**
- `Ready`: Database is ready and available
- `ValidationFailed`: Configuration validation failed
- `CreationFailed`: Database creation failed

## Troubleshooting

### Common Issues

1. **Authentication Failures**
   - Verify credentials in referenced secret
   - Check IAM roles/permissions for system-wide auth
   - Ensure workload identity is properly configured

2. **URI Access Failures**
   - Verify the backup file exists at the specified URI
   - Check network connectivity from Neo4j pods
   - Ensure URI format is correct

3. **Validation Errors**
   - Check that referenced cluster exists and is ready
   - Verify topology doesn't exceed cluster capacity
   - Ensure no conflicts between seedURI and initialData

4. **Performance Issues**
   - Consider using .backup format instead of .dump
   - Adjust bufferSize in seedConfig
   - Ensure adequate resources for restoration

### Debugging Commands

```bash
# Check database status
kubectl get neo4jdatabase my-database -o yaml

# View operator logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager

# Check events
kubectl describe neo4jdatabase my-database

# Verify database in Neo4j
kubectl exec -it <neo4j-pod> -- cypher-shell -u neo4j -p <password> "SHOW DATABASES"
```

## Security Best Practices

1. **Use System-Wide Authentication**: Prefer IAM roles, workload identity, and managed identities over explicit credentials
2. **Rotate Credentials**: Regularly rotate any explicit credentials stored in secrets
3. **Least Privilege**: Grant minimal required permissions for backup access
4. **Network Security**: Use private endpoints and VPNs for sensitive backup access
5. **Audit Access**: Monitor and log backup access for compliance

## Examples

See the [examples/databases/](../../../examples/databases/) directory for comprehensive examples:

- `database-from-s3-seed.yaml` - S3 with explicit credentials
- `database-from-gcs-seed.yaml` - Google Cloud Storage with workload identity
- `database-from-azure-seed.yaml` - Azure Blob Storage with both key and SAS token auth
- `database-from-http-seed.yaml` - HTTP/HTTPS/FTP examples
- `database-dump-vs-backup-seed.yaml` - Performance comparison between formats

## Neo4j Version Compatibility

| Feature | Neo4j 5.26+ | Neo4j 2025.x |
|---------|-------------|--------------|
| Basic seed URI | ✅ | ✅ |
| CloudSeedProvider | ✅ | ✅ |
| Point-in-time recovery | ❌ | ✅ |
| All URI schemes | ✅ | ✅ |
| Topology specification | ✅ | ✅ |

## Migration from S3SeedProvider

The operator uses Neo4j's modern CloudSeedProvider instead of the deprecated S3SeedProvider:

- ✅ **Use**: CloudSeedProvider with system-wide authentication
- ❌ **Don't Use**: S3SeedProvider (deprecated in Neo4j 5.x)

This approach provides better security, broader cloud support, and future compatibility.
