# Neo4j Database Seed URI Feature Guide

This guide explains how to create Neo4j databases from existing backups or dumps using the seed URI feature in the Neo4j Kubernetes Operator.

## Overview

The seed URI feature allows you to create new Neo4j databases by restoring them from existing backup files stored in cloud storage or accessible via HTTP/FTP. This is useful for:

- **Database Migration**: Moving databases between environments
- **Testing with Production Data**: Creating test databases from production backups
- **Disaster Recovery**: Restoring databases to specific points in time
- **Development Environment Setup**: Seeding development databases with sample data

## Supported URI Schemes

The operator registers Neo4j's modern seed providers via `dbms.databases.seed_from_uri_providers` (set automatically on both cluster and standalone pods). The registered providers are version-gated:

- `CloudSeedProvider`, `FileSeedProvider`, `URLConnectionSeedProvider` — always registered (all supported Neo4j versions).
- `ServerSeedProvider` — registered only on Neo4j **2026.04+** (the class is not present in earlier releases).
- The deprecated `S3SeedProvider` is **never** registered — `CloudSeedProvider` handles `s3://` via the cloud SDK's default credential chain.

These providers cover the following URI schemes:

| Scheme | Provider | Example |
|--------|----------|---------|
| `s3://` | CloudSeedProvider | `s3://my-bucket/backup.backup` |
| `gs://` | CloudSeedProvider | `gs://my-bucket/backup.backup` |
| `azb://` | CloudSeedProvider | `azb://account.blob.core.windows.net/container/backup.backup` |
| `https://` | URLConnectionSeedProvider | `https://backup-server.com/backup.backup` |
| `http://` | URLConnectionSeedProvider | `http://backup-server.com/backup.backup` |
| `ftp://` | URLConnectionSeedProvider | `ftp://ftp.server.com/backup.backup` |
| `file://` | FileSeedProvider | `file:///path/backup.backup` |

The `seedURI` must point at the **exact `.backup` file**, never a directory — Neo4j seeds a single database from one backup file. (For a DIFF backup, the single DIFF-file URI is sufficient; Neo4j resolves the full parent chain from the same directory.)

## Basic Usage

### Simple Seed URI Database

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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

### Seeding a Sharded Database

`Neo4jShardedDatabase` supports three mutually-exclusive seed sources (`seedURI`, `seedURIs`, and `seedBackupRef` — the validator rejects combining them):

- **`seedURI`** — a single URI; Neo4j expects per-shard backup artifacts named with shard suffixes (e.g. `<db>-g000`, `<db>-p000`).
- **`seedURIs`** — a per-shard map keyed by shard name, for multi-location backups:

  ```yaml
  seedURIs:
    mydb-g000: "s3://my-backups/mydb-g000.backup"
    mydb-p000: "s3://my-backups/mydb-p000.backup"
  ```

- **`seedBackupRef`** — names a `Neo4jBackup` CR (same namespace) whose most-recent Succeeded run is resolved into a concrete seed at reconcile time. Supports both **cloud** backups (resolved to `seedURI`) and **PVC**-backed backups (resolved to per-shard URLs served through an in-cluster HTTP proxy — see [PVC-backed seeds](#pvc-backed-seeds)). If the referenced backup has no Succeeded run yet, the sharded database stays in `Pending` and the reconciler requeues (it does not fail).

`seedBackupRef` is also mutually exclusive with `seedURI` / `seedURIs`.

### PVC-backed seeds

When a seed source resolves to a backup stored on a PVC, the operator spawns a short-lived `backup-seed-proxy-<owner>` Deployment + Service that mounts the backup PVC read-only and serves the `.backup` files over HTTP. Neo4j then fetches each file via `URLConnectionSeedProvider` at the exact `.backup` filename.

Lifecycle and hardening:

- A **NetworkPolicy** restricts the proxy's ingress to the target cluster's server pods (effective on enforcing CNIs; a no-op elsewhere).
- The proxy stack (Deployment + Service + NetworkPolicy) is **torn down automatically as soon as the seed completes**: when a `Neo4jRestore` reaches `Completed`/`Failed`, or when the `Neo4jShardedDatabase` becomes `Ready`. It does not keep serving the backup PVC for the lifetime of the owning CR.
- The proxy is also owner-referenced to the consuming CR, so deleting the CR removes any leftovers.

## Authentication Methods

### 1. System-Wide Authentication (Recommended)

Use cloud-native authentication mechanisms that don't require explicit credentials.

> **Where the identity must be bound:** `CREATE DATABASE … OPTIONS { seedURI }` runs inside the Neo4j JVM **on the server pods** — not in a backup/restore Job. Under pod identity (IRSA / GKE Workload Identity / Azure Workload Identity) the IAM binding therefore goes on the **server pods' ServiceAccount**, not on the `neo4j-backup-sa` used by backup Jobs (`Neo4jBackup`'s `cloud.identity.autoCreate.annotations` only annotates the Job SA and has no effect on seeding).

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

For environments where system-wide authentication isn't available, you can reference a credentials Secret with `spec.seedCredentials.secretRef`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: backup-credentials
data:
  AWS_ACCESS_KEY_ID: <base64-encoded-key>
  AWS_SECRET_ACCESS_KEY: <base64-encoded-secret>
---
apiVersion: neo4j.neo4j.com/v1beta1
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

> **Important — where the credentials must live:** `CREATE DATABASE … OPTIONS { seedURI }` is executed by the Neo4j JVM **on the server pods**, so the cloud SDK's default credential chain resolves against the *server pods'* environment. For credentials to be visible there, the Secret must be projected onto the hosting cluster or standalone CR via `spec.extraEnvFrom`:
>
> ```yaml
> apiVersion: neo4j.neo4j.com/v1beta1
> kind: Neo4jEnterpriseCluster   # or Neo4jEnterpriseStandalone
> metadata:
>   name: my-cluster
> spec:
>   extraEnvFrom:
>   - secretRef:
>       name: backup-credentials
> ```
>
> The operator validates this projection before seeding. If the Secret is missing from `extraEnvFrom`, it emits an actionable error with a copy-pasteable snippet. Add the annotation `neo4j.com/auto-inherit-seed-creds: "true"` on the hosting cluster/standalone CR to let the operator patch `spec.extraEnvFrom` automatically — note this triggers a rolling restart of the server pods. When relying on IRSA / GKE Workload Identity / Azure Workload Identity, no Secret projection is needed.

## Credential Requirements by Provider

### Amazon S3
**Required:**

- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`

**Optional:**

- `AWS_SESSION_TOKEN` (for temporary credentials)
- `AWS_REGION`
- `AWS_ENDPOINT_URL_S3` — required for **MinIO / S3-compatible** stores. The `endpointURL`/`forcePathStyle` fields on `Neo4jBackup`/`Neo4jRestore` only affect their **Job pods**; the server pods doing seedURI fetches read the SDK's standard env vars, so put the endpoint in the Secret you project via `spec.extraEnvFrom`. (Cluster restores that resolve to a custom-endpoint backup emit a `SeedEndpointNotProjected` Warning event when the operator can verify the endpoint is missing from the server pods.)

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

See the [examples/databases/](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples/databases) directory for comprehensive examples:

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
