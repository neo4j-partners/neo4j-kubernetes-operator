# Neo4jDatabase

This document provides a reference for the `Neo4jDatabase` Custom Resource Definition (CRD). This resource is used to create and manage databases within a Neo4j cluster.

## Overview

The Neo4jDatabase CRD allows you to declaratively manage databases in Neo4j Enterprise clusters. It supports Neo4j 5.26+ features including:
- IF NOT EXISTS clause to prevent reconciliation errors
- WAIT/NOWAIT options for synchronous or asynchronous creation
- Database topology constraints for cluster distribution
- Cypher language version selection (Neo4j 2025.x)
- Initial data import and schema creation
- **Seed URI functionality for creating databases from existing backups** (Neo4j 5.26+)

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required**. The name of the Neo4j cluster to create the database in. |
| `name` | `string` | **Required**. The name of the database to create. |
| `wait` | `boolean` | Whether to wait for database creation to complete. Default: `true` |
| `ifNotExists` | `boolean` | Create database only if it doesn't exist. Prevents errors on reconciliation. Default: `true` |
| `topology` | `DatabaseTopology` | Database distribution topology in the cluster. |
| `defaultCypherLanguage` | `string` | Default Cypher language version (Neo4j 2025.x only). Values: `"5"`, `"25"` |
| `options` | `map[string]string` | Additional database options (e.g., `txLogEnrichment`). |
| `initialData` | `InitialDataSpec` | Initial data to import when creating the database. **Cannot be used with `seedURI`**. |
| `seedURI` | `string` | URI to backup file for database creation. Supports: `s3://`, `gs://`, `azb://`, `https://`, `http://`, `ftp://`. **Cannot be used with `initialData`**. |
| `seedConfig` | `SeedConfiguration` | Advanced configuration for seed URI restoration. |
| `seedCredentials` | `SeedCredentials` | Credentials for accessing seed URI (optional if using system-wide authentication). |
| `state` | `string` | Desired database state. Values: `"online"`, `"offline"` |

### DatabaseTopology

| Field | Type | Description |
|---|---|---|
| `primaries` | `integer` | Number of primary servers to host the database. |
| `secondaries` | `integer` | Number of secondary servers to host the database. |

### InitialDataSpec

| Field | Type | Description |
|---|---|---|
| `source` | `string` | Source type for initial data. Currently supports: `"cypher"` |
| `cypherStatements` | `[]string` | List of Cypher statements to execute on database creation. |

### SeedConfiguration

| Field | Type | Description |
|---|---|---|
| `restoreUntil` | `string` | Point-in-time recovery timestamp (Neo4j 2025.x only). Format: RFC3339 (e.g., `"2025-01-15T10:30:00Z"`) or transaction ID (e.g., `"txId:12345"`). |
| `config` | `map[string]string` | CloudSeedProvider configuration options. See [supported options](#seedconfiguration-options). |

#### SeedConfiguration Options

| Option | Values | Description |
|---|---|---|
| `compression` | `"gzip"`, `"lz4"`, `"none"` | Compression format for backup processing. |
| `validation` | `"strict"`, `"lenient"` | Validation mode during restoration. |
| `bufferSize` | size string | Buffer size for processing (e.g., `"64MB"`, `"128MB"`). |

### SeedCredentials

| Field | Type | Description |
|---|---|---|
| `secretRef` | `string` | **Required**. Name of Kubernetes secret containing credentials for seed URI access. |

#### Required Secret Keys by URI Scheme

**Amazon S3 (`s3://`)**:
- `AWS_ACCESS_KEY_ID` (required)
- `AWS_SECRET_ACCESS_KEY` (required)
- `AWS_SESSION_TOKEN` (optional, for temporary credentials)
- `AWS_REGION` (optional)

**Google Cloud Storage (`gs://`)**:
- `GOOGLE_APPLICATION_CREDENTIALS` (required, service account JSON key)
- `GOOGLE_CLOUD_PROJECT` (optional)

**Azure Blob Storage (`azb://`)**:
- `AZURE_STORAGE_ACCOUNT` (required)
- Either `AZURE_STORAGE_KEY` or `AZURE_STORAGE_SAS_TOKEN` (required)

**HTTP/HTTPS/FTP**:
- `USERNAME` (optional)
- `PASSWORD` (optional)
- `AUTH_HEADER` (optional, for custom authentication)

## Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | Current phase of the database. Values: `"Pending"`, `"Creating"`, `"Ready"`, `"Failed"` |
| `state` | `string` | Current database state. Values: `"online"`, `"offline"`, `"starting"`, `"stopping"` |
| `servers` | `[]string` | List of servers hosting the database. |
| `dataImported` | `boolean` | Whether initial data has been imported. |
| `message` | `string` | Human-readable status message. |
| `lastUpdated` | `Time` | Timestamp of last status update. |

## Examples

### Basic Database Creation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-database
spec:
  clusterRef: my-cluster
  name: mydb
  wait: true
  ifNotExists: true
```

### Database with Topology Constraints

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: distributed-database
spec:
  clusterRef: production-cluster
  name: distributed
  wait: true
  ifNotExists: true
  topology:
    primaries: 3
    secondaries: 2
  options:
    txLogEnrichment: "FULL"
```

### Database with Initial Schema (Neo4j 5.26.x)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: app-database
spec:
  clusterRef: my-cluster
  name: appdb
  wait: true
  ifNotExists: true
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
      - "CREATE INDEX user_name IF NOT EXISTS FOR (u:User) ON (u.name)"
      - "CREATE INDEX product_category IF NOT EXISTS FOR (p:Product) ON (p.category)"
```

### Neo4j 2025.x Database with Cypher 25

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: modern-database
spec:
  clusterRef: neo4j-2025-cluster
  name: moderndb
  wait: true
  ifNotExists: true
  defaultCypherLanguage: "25"  # Use Cypher 25 features
  topology:
    primaries: 2
    secondaries: 1
```

### Database from Seed URI (S3 Backup)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: seeded-database
spec:
  clusterRef: production-cluster
  name: restored-sales-db

  # Create from S3 backup
  seedURI: "s3://my-neo4j-backups/sales-database-2025-01-15.backup"

  # Use system-wide authentication (IAM roles)
  # seedCredentials: null

  # Database topology
  topology:
    primaries: 2
    secondaries: 1

  # Point-in-time recovery (Neo4j 2025.x)
  seedConfig:
    restoreUntil: "2025-01-15T10:30:00Z"
    config:
      compression: "gzip"
      validation: "strict"
      bufferSize: "128MB"

  wait: true
  ifNotExists: true
  defaultCypherLanguage: "25"
```

### Database from Seed URI with Explicit Credentials

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: backup-credentials
data:
  AWS_ACCESS_KEY_ID: dGVzdC1hY2Nlc3Mta2V5          # EXAMPLE: Replace with your actual base64-encoded access key
  AWS_SECRET_ACCESS_KEY: dGVzdC1zZWNyZXQta2V5      # EXAMPLE: Replace with your actual base64-encoded secret key
  AWS_REGION: dXMtd2VzdC0y
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: seeded-database-with-creds
spec:
  clusterRef: test-cluster
  name: test-db

  # Create from Google Cloud Storage backup
  seedURI: "gs://my-gcs-backups/test-database.backup"

  # Use explicit credentials
  seedCredentials:
    secretRef: backup-credentials

  topology:
    primaries: 1
    secondaries: 1

  wait: true
  ifNotExists: true
```

### Asynchronous Database Creation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: async-database
spec:
  clusterRef: my-cluster
  name: asyncdb
  wait: false  # NOWAIT - returns immediately
  ifNotExists: true
```

## Behavior

### Creation Process

**Standard Database Creation:**
1. The operator checks if the database already exists (if `ifNotExists: true`)
2. Constructs the CREATE DATABASE command with appropriate options
3. Executes the command on the cluster
4. If `wait: true`, waits for database to be fully online
5. If initial data is specified, imports it after database is online
6. Updates the status with current state and hosting servers

**Seed URI Database Creation:**
1. The operator checks if the database already exists (if `ifNotExists: true`)
2. If `seedCredentials` are specified, prepares cloud authentication
3. Constructs the CREATE DATABASE FROM URI command with seed configuration
4. Executes the command on the cluster with CloudSeedProvider
5. If `wait: true`, waits for database restoration to complete
6. Updates the status with current state and hosting servers
7. **Note**: Initial data import is skipped when using seed URI (data comes from the seed)

### Version-Specific Behavior

**Neo4j 5.26.x**:
- Standard CREATE DATABASE syntax
- Seed URI support with CloudSeedProvider
- No Cypher language version support
- No point-in-time recovery for seed URIs
- Supports all topology and option features

**Neo4j 2025.x**:
- Supports `defaultCypherLanguage` field
- Enhanced seed URI support with point-in-time recovery (`restoreUntil`)
- Enhanced topology management
- Additional database options available

### Reconciliation

The operator continuously reconciles the database state:
- If database doesn't exist and `ifNotExists: true`, creates it
- If database exists and state differs, updates it (start/stop)
- If topology changes, redistributes database (Neo4j 5.20+)
- Updates status with current database information

## Best Practices

### General Best Practices
1. **Always use `ifNotExists: true`** in production to prevent reconciliation errors
2. **Set appropriate topology** based on your availability requirements
3. **Use `wait: true`** for critical databases to ensure they're ready
4. **Include IF NOT EXISTS** in schema creation statements
5. **Test database creation** in staging before production deployment

### Seed URI Best Practices
6. **Prefer system-wide authentication** (IAM roles, workload identity) over explicit credentials
7. **Use .backup format** for better performance with large datasets compared to .dump format
8. **Don't combine `seedURI` and `initialData`** - they conflict with each other
9. **Use point-in-time recovery** (`restoreUntil`) when available for precise restoration
10. **Test seed URI access** from Neo4j pods before creating databases
11. **Monitor restoration progress** - large backups may take significant time
12. **Use appropriate compression** (`gzip` or `lz4`) for faster transfer and processing

## Troubleshooting

### Database Creation Fails

Check the operator logs:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

Common issues:
- Insufficient cluster resources (primaries/secondaries)
- Name conflicts with existing databases
- Invalid Cypher statements in initial data
- Network connectivity to cluster
- **Seed URI issues**: Invalid URI format, inaccessible backup file, credential problems

### Database Stuck in Pending

Verify cluster is ready:
```bash
kubectl get neo4jenterprisecluster <cluster-name>
```

Check database status:
```bash
kubectl describe neo4jdatabase <database-name>
```

### Seed URI Troubleshooting

**Authentication Issues:**
```bash
# Check secret exists and has correct keys
kubectl get secret backup-credentials -o yaml

# Test access from a pod
kubectl run test-pod --rm -it --image=amazon/aws-cli \
  -- aws s3 ls s3://my-bucket/backup.backup
```

**URI Access Issues:**
- Verify the backup file exists at the specified URI
- Check network connectivity from Neo4j pods to the URI
- Ensure firewall rules allow outbound access
- Test URI format: `scheme://host/path/file.backup`

**Performance Issues:**
- Use `.backup` format instead of `.dump` for large datasets
- Increase `bufferSize` in `seedConfig.config`
- Use `compression: "lz4"` for faster processing
- Monitor pod resources during restoration

**Validation Errors:**
```bash
# Check for configuration conflicts
kubectl describe neo4jdatabase <database-name>

# Common validation errors:
# - seedURI and initialData cannot be used together
# - Database topology exceeds cluster capacity
# - Invalid URI scheme or format
# - Missing required credential keys in secret
```

### Initial Data Not Imported

- Ensure Cypher statements are valid
- Check for constraint/index conflicts
- Verify database is online before import
- Review operator logs for import errors
- **Note**: Initial data is automatically skipped when using `seedURI`

### Seed URI Events and Status

Monitor database creation progress:
```bash
# Watch for seed-specific events
kubectl get events --field-selector involvedObject.name=<database-name>

# Key events to look for:
# - DatabaseCreatedFromSeed: Success
# - DataSeeded: Restoration complete
# - ValidationWarning: Configuration warnings
# - CreationFailed: Seed restoration failed
```
