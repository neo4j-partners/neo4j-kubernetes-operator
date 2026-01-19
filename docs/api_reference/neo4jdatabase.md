# Neo4jDatabase API Reference

The `Neo4jDatabase` Custom Resource Definition (CRD) provides declarative database management for both Neo4j Enterprise clusters and standalone deployments.

## Overview

- **API Version**: `neo4j.neo4j.com/v1alpha1`
- **Kind**: `Neo4jDatabase`
- **Supported Neo4j Versions**: 5.26.0+ (semver) and 2025.01.0+ (calver)
- **Target Deployments**: Both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`
- **Database Creation**: Automated database provisioning with topology control
- **Schema Management**: Initial data import and schema creation
- **Seed URI Support**: Create databases from existing backups (Neo4j 5.26+)

## Key Features

**Universal Compatibility**: Works with both cluster and standalone deployments through automatic resource discovery:

- **Cluster Support**: Create databases across multiple servers with custom topology
- **Standalone Support**: Create databases in single-node deployments
- **Automatic Discovery**: Controller automatically detects target deployment type
- **Unified API**: Same resource definition works for both deployment types
- **Topology Control**: Specify primary/secondary distribution for cluster databases
- **Seed URI**: Create databases from S3, GCS, Azure, HTTP, or FTP backup sources

## Related Resources

- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) - Target cluster deployments
- [`Neo4jEnterpriseStandalone`](neo4jenterprisestandalone.md) - Target standalone deployments
- [`Neo4jBackup`](neo4jbackup.md) - Create backups of databases
- [`Neo4jRestore`](neo4jrestore.md) - Restore databases from backups
- [`Neo4jPlugin`](neo4jplugin.md) - Install plugins for database functionality

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required**. Name of target Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone |
| `name` | `string` | **Required**. Database name to create |
| `wait` | `boolean` | Wait for database creation to complete (default: `true`) |
| `ifNotExists` | `boolean` | Create only if database doesn't exist - prevents reconciliation errors (default: `true`) |
| `topology` | [`DatabaseTopology`](#databasetopology) | Database distribution topology (cluster only) |
| `defaultCypherLanguage` | `string` | Default Cypher version for Neo4j 2025.x: `"5"`, `"25"` |
| `options` | `map[string]string` | Additional database options (e.g., `txLogEnrichment`) |
| `initialData` | [`InitialDataSpec`](#initialdataspec) | Initial data import (**mutually exclusive with `seedURI`**) |
| `seedURI` | `string` | Backup URI for database creation (**mutually exclusive with `initialData`**) |
| `seedConfig` | [`SeedConfiguration`](#seedconfiguration) | Advanced seed URI configuration |
| `seedCredentials` | [`SeedCredentials`](#seedcredentials) | Seed URI access credentials |

### DatabaseTopology

**Cluster-Only Feature**: Database topology is only applicable to `Neo4jEnterpriseCluster` deployments. Ignored for standalone deployments.

| Field | Type | Description |
|---|---|---|
| `primaries` | `int32` | **Required for clusters**. Number of primary servers (minimum: 1) |
| `secondaries` | `int32` | Number of secondary servers (minimum: 0, default: 0) |

**Validation**:
- `primaries + secondaries` must not exceed cluster's `spec.topology.servers`
- Servers are selected based on role constraints (if configured)
- For standalone deployments, topology is automatically managed

### InitialDataSpec

| Field | Type | Description |
|---|---|---|
| `source` | `string` | Source type for initial data: `"cypher"`, `"dump"`, `"csv"` |
| `cypherStatements` | `[]string` | Cypher statements to execute on database creation |
| `configMapRef` | `string` | ConfigMap containing data or statements |
| `secretRef` | `string` | Secret containing data or statements |
| `storage` | [`*StorageLocation`](#storagelocation) | Storage location for data files |

### SeedConfiguration

Advanced configuration for creating databases from seed URIs using Neo4j's CloudSeedProvider.

| Field | Type | Description |
|---|---|---|
| `restoreUntil` | `string` | Point-in-time recovery timestamp (Neo4j 2025.x only) |
| `config` | `map[string]string` | CloudSeedProvider configuration options |

**Point-in-Time Recovery Formats** (Neo4j 2025.x only):
- **RFC3339 Timestamp**: `"2025-01-15T10:30:00Z"`
- **Transaction ID**: `"txId:12345"`

**Configuration Options**:
- `compression`: `"gzip"`, `"lz4"`, `"none"`
- `validation`: `"strict"`, `"lenient"`
- `bufferSize`: Buffer size (e.g., `"64MB"`, `"128MB"`)
- Cloud-specific options for S3, GCS, Azure

#### SeedConfiguration Options

| Option | Values | Description |
|---|---|---|
| `compression` | `"gzip"`, `"lz4"`, `"none"` | Compression format for backup processing. |
| `validation` | `"strict"`, `"lenient"` | Validation mode during restoration. |
| `bufferSize` | size string | Buffer size for processing (e.g., `"64MB"`, `"128MB"`). |

### SeedCredentials

| Field | Type | Description |
|---|---|---|
| `secretRef` | `string` | Name of Kubernetes secret containing credentials for seed URI access |

### StorageLocation

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Storage type: `"s3"`, `"gcs"`, `"azure"`, `"pvc"` |
| `bucket` | `string` | Bucket name (for cloud storage) |
| `path` | `string` | Path within bucket or PVC |
| `pvc` | [`*PVCSpec`](#pvcspec) | PVC configuration (for `pvc` type) |
| `cloud` | [`*CloudBlock`](#cloudblock) | Cloud provider configuration |

### PVCSpec

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of existing PVC to use |
| `storageClassName` | `string` | Storage class name |
| `size` | `string` | Size for new PVC (e.g., `"100Gi"`) |

### CloudBlock

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Cloud provider: `"aws"`, `"gcp"`, `"azure"` |
| `identity` | [`*CloudIdentity`](#cloudidentity) | Cloud identity configuration |

### CloudIdentity

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| `serviceAccount` | `string` | Service account name for cloud identity |
| `autoCreate` | [`*AutoCreateSpec`](#autocreatespec) | Auto-create service account and annotations |

### AutoCreateSpec

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable auto-creation of service account (default: `true`) |
| `annotations` | `map[string]string` | Annotations to apply to auto-created service account |

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
| `conditions` | `[]metav1.Condition` | Current status conditions |
| `phase` | `string` | Current phase of the database |
| `message` | `string` | Human-readable status message |
| `observedGeneration` | `int64` | Generation observed by the controller |
| `dataImported` | `boolean` | Whether initial data has been imported |
| `creationTime` | `*metav1.Time` | When the database was created |
| `size` | `string` | Database size |
| `lastBackupTime` | `*metav1.Time` | Last backup time |
| `state` | `string` | Current database state: `"online"`, `"offline"`, `"starting"`, `"stopping"` |
| `servers` | `[]string` | Servers hosting the database |

## Examples

### Basic Database in Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: cluster-database
spec:
  clusterRef: my-cluster  # References Neo4jEnterpriseCluster
  name: mydb
  wait: true
  ifNotExists: true
  topology:
    primaries: 2    # Distribute across 2 primary servers
    secondaries: 1  # 1 secondary for read scaling
```

### Basic Database in Standalone

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: standalone-database
spec:
  clusterRef: my-standalone  # References Neo4jEnterpriseStandalone
  name: mydb
  wait: true
  ifNotExists: true
  # Note: topology not needed for standalone (ignored if specified)
```

### Database with Advanced Topology (Cluster Only)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: distributed-database
spec:
  clusterRef: production-cluster  # Must be Neo4jEnterpriseCluster
  name: distributed
  wait: true
  ifNotExists: true
  topology:
    primaries: 3    # Uses 3 servers for primary role
    secondaries: 2  # Uses 2 servers for secondary role
  options:
    txLogEnrichment: "FULL"  # Enhanced transaction logging
  defaultCypherLanguage: "25"  # Neo4j 2025.x only
```

### Multi-Database Setup

```yaml
# User-facing database with high availability
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: user-database
spec:
  clusterRef: production-cluster
  name: users
  topology:
    primaries: 3
    secondaries: 1
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
      - "CREATE INDEX user_name IF NOT EXISTS FOR (u:User) ON (u.name)"

---
# Analytics database optimized for reads
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: analytics-database
spec:
  clusterRef: production-cluster
  name: analytics
  topology:
    primaries: 1     # Minimal write capacity
    secondaries: 4   # Optimized for read scaling
  options:
    txLogEnrichment: "OFF"  # Reduce overhead for analytics
```

### Database with Schema and Sample Data

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: app-database
spec:
  clusterRef: my-cluster  # Works with cluster or standalone
  name: appdb
  wait: true
  ifNotExists: true
  initialData:
    source: cypher
    cypherStatements:
      # Schema creation
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
      - "CREATE INDEX user_name IF NOT EXISTS FOR (u:User) ON (u.name)"
      - "CREATE INDEX product_category IF NOT EXISTS FOR (p:Product) ON (p.category)"
      # Sample data
      - "CREATE (u:User {name: 'Alice', email: 'alice@example.com'})"
      - "CREATE (p:Product {name: 'Neo4j Enterprise', category: 'Database'})"
      - "MATCH (u:User {name: 'Alice'}), (p:Product {name: 'Neo4j Enterprise'}) CREATE (u)-[:PURCHASED]->(p)"
  topology:  # Only applied if clusterRef is a cluster
    primaries: 2
    secondaries: 1
```

### Neo4j 2025.x Database with Enhanced Features

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: modern-database
spec:
  clusterRef: neo4j-2025-cluster  # Neo4j 2025.x cluster
  name: moderndb
  wait: true
  ifNotExists: true
  defaultCypherLanguage: "25"  # Enable Cypher 25 features
  topology:
    primaries: 2
    secondaries: 1
  options:
    # Neo4j 2025.x specific options
    queryRouting: "ENABLED"
    vectorIndexing: "AUTO"
  initialData:
    source: cypher
    cypherStatements:
      # Use Cypher 25 syntax features
      - "CREATE VECTOR INDEX document_embedding IF NOT EXISTS FOR (d:Document) ON d.embedding OPTIONS {dimension: 1536, similarity: 'cosine'}"
      - "CREATE (d:Document {title: 'Neo4j 2025 Guide', embedding: [0.1, 0.2, 0.3]})"
```

### Database from Seed URI (Production Restore)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: prod-restore-database
spec:
  clusterRef: recovery-cluster  # Target cluster for restore
  name: restored-sales-db

  # Restore from S3 backup (system-wide IAM authentication)
  seedURI: "s3://prod-neo4j-backups/sales-database-2025-01-15.backup"

  # Production topology
  topology:
    primaries: 3    # High availability
    secondaries: 2  # Read scaling

  # Neo4j 2025.x point-in-time recovery
  seedConfig:
    restoreUntil: "2025-01-15T10:30:00Z"  # Specific point in time
    config:
      compression: "lz4"        # Faster decompression
      validation: "strict"      # Ensure data integrity
      bufferSize: "256MB"       # Large buffer for performance
      region: "us-east-1"       # S3 region optimization

  wait: true
  ifNotExists: true
  defaultCypherLanguage: "25"  # Neo4j 2025.x
  options:
    txLogEnrichment: "FULL"    # Enhanced logging for production
```

### Database from Seed URI (Development Copy)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: dev-copy-database
spec:
  clusterRef: dev-standalone  # Can target standalone for development
  name: dev-copy

  # Copy from Google Cloud Storage backup
  seedURI: "gs://dev-backups/prod-snapshot-2025-01-15.backup"

  # Explicit credentials for dev environment
  seedCredentials:
    secretRef: gcs-dev-credentials

  # Simplified configuration for development
  seedConfig:
    config:
      compression: "gzip"
      validation: "lenient"  # Allow minor inconsistencies
      bufferSize: "64MB"     # Smaller buffer for dev

  wait: true
  ifNotExists: true
  # No topology needed for standalone deployment
```

### Multi-Cloud Seed URI Examples

```yaml
# AWS S3 with explicit credentials
apiVersion: v1
kind: Secret
metadata:
  name: s3-credentials
type: Opaque
data:
  AWS_ACCESS_KEY_ID: <base64-encoded-access-key>
  AWS_SECRET_ACCESS_KEY: <base64-encoded-secret-key>
  AWS_REGION: dXMtZWFzdC0x  # us-east-1
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: s3-restore-db
spec:
  clusterRef: prod-cluster
  name: s3-restored
  seedURI: "s3://prod-backups/full-backup-2025-01-15.backup"
  seedCredentials:
    secretRef: s3-credentials
  topology:
    primaries: 2
    secondaries: 1

---
# Google Cloud Storage with service account
apiVersion: v1
kind: Secret
metadata:
  name: gcs-credentials
type: Opaque
data:
  GOOGLE_APPLICATION_CREDENTIALS: <base64-encoded-service-account-json>
  GOOGLE_CLOUD_PROJECT: <base64-encoded-project-id>
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: gcs-restore-db
spec:
  clusterRef: test-cluster
  name: gcs-restored
  seedURI: "gs://test-backups/snapshot-2025-01-15.backup"
  seedCredentials:
    secretRef: gcs-credentials

---
# Azure Blob Storage with SAS token
apiVersion: v1
kind: Secret
metadata:
  name: azure-credentials
type: Opaque
data:
  AZURE_STORAGE_ACCOUNT: <base64-encoded-account-name>
  AZURE_STORAGE_SAS_TOKEN: <base64-encoded-sas-token>
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: azure-restore-db
spec:
  clusterRef: azure-cluster
  name: azure-restored
  seedURI: "azb://backups/neo4j-backup-2025-01-15.backup"
  seedCredentials:
    secretRef: azure-credentials

---
# HTTP/HTTPS with basic authentication
apiVersion: v1
kind: Secret
metadata:
  name: http-credentials
type: Opaque
data:
  USERNAME: <base64-encoded-username>
  PASSWORD: <base64-encoded-password>
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: http-restore-db
spec:
  clusterRef: local-cluster
  name: http-restored
  seedURI: "https://backup-server.example.com/backups/neo4j-2025-01-15.backup"
  seedCredentials:
    secretRef: http-credentials
```

### Advanced Database Management

```yaml
# Asynchronous database creation
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: async-database
spec:
  clusterRef: my-cluster
  name: asyncdb
  wait: false  # Returns immediately (NOWAIT mode)
  ifNotExists: true
  topology:
    primaries: 1
    secondaries: 0

---
# Database with complex initial data from ConfigMap
apiVersion: v1
kind: ConfigMap
metadata:
  name: complex-schema
data:
  schema.cypher: |
    // Create constraints
    CREATE CONSTRAINT user_id IF NOT EXISTS ON (u:User) ASSERT u.id IS UNIQUE;
    CREATE CONSTRAINT product_sku IF NOT EXISTS ON (p:Product) ASSERT p.sku IS UNIQUE;

    // Create indexes
    CREATE INDEX user_email IF NOT EXISTS FOR (u:User) ON (u.email);
    CREATE INDEX product_name IF NOT EXISTS FOR (p:Product) ON (p.name);

    // Create sample data
    CREATE (u1:User {id: 1, name: 'Alice', email: 'alice@example.com'});
    CREATE (u2:User {id: 2, name: 'Bob', email: 'bob@example.com'});
    CREATE (p1:Product {sku: 'NEO4J-ENT', name: 'Neo4j Enterprise'});

    // Create relationships
    MATCH (u:User {id: 1}), (p:Product {sku: 'NEO4J-ENT'})
    CREATE (u)-[:PURCHASED {date: date('2025-01-15')}]->(p);
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: complex-database
spec:
  clusterRef: my-cluster
  name: complex-app
  initialData:
    source: cypher
    configMapRef: complex-schema  # Reference to ConfigMap
  topology:
    primaries: 2
    secondaries: 1
```

## Behavior

### Target Discovery

**Automatic Resource Discovery**: The controller automatically determines the target deployment type:

1. **Cluster Lookup**: First attempts to find `Neo4jEnterpriseCluster` with matching name
2. **Standalone Fallback**: If cluster not found, looks for `Neo4jEnterpriseStandalone`
3. **Validation**: Applies appropriate validation rules based on target type
4. **Client Creation**: Uses correct Neo4j client (cluster vs standalone connection)

### Database Creation Process

**Standard Database Creation**:
1. Discover target deployment (cluster or standalone)
2. Check if database exists (if `ifNotExists: true`)
3. Validate topology constraints (cluster only)
4. Construct CREATE DATABASE command with Neo4j 5.26+ syntax
5. Execute command via appropriate client connection
6. Wait for completion (if `wait: true`)
7. Import initial data (if specified)
8. Update status with current state

**Seed URI Database Creation**:
1. Discover target deployment and validate seed URI format
2. Prepare cloud authentication (if `seedCredentials` specified)
3. Construct CREATE DATABASE FROM URI command with CloudSeedProvider
4. Execute command with seed configuration options
5. Wait for restoration completion (if `wait: true`)
6. Update status (**Note**: initial data import skipped - data comes from seed)

### Version-Specific Behavior

**Neo4j 5.26.x**:
- Standard `CREATE DATABASE` syntax with `TOPOLOGY` clause
- Seed URI support via CloudSeedProvider
- No Cypher language version support
- Compatible with both cluster and standalone deployments

**Neo4j 2025.x**:
- Enhanced `CREATE DATABASE` with `DEFAULT LANGUAGE CYPHER` support
- Point-in-time recovery for seed URIs (`restoreUntil`)
- Advanced seed configuration options
- Same compatibility with cluster and standalone deployments

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

**Check operator logs**:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

**Common Issues**:
- **Target Not Found**: `clusterRef` doesn't match any cluster or standalone
- **Topology Validation**: Insufficient servers for requested topology (cluster only)
- **Name Conflicts**: Database name already exists
- **Authentication**: Connection issues to Neo4j instance
- **Seed URI Issues**: Invalid format, inaccessible backup, credential problems
- **Version Compatibility**: Using 2025.x features with 5.26.x Neo4j

**Cluster-Specific Issues**:
- Server capacity exceeded (primaries + secondaries > cluster servers)
- Role constraint conflicts (e.g., requesting primaries from SECONDARY-only servers)

**Standalone-Specific Issues**:
- Topology specified for standalone deployment (will be ignored)
- Authentication configuration missing (`adminSecret` not configured)

### Database Stuck in Pending

**Verify target deployment**:
```bash
# For cluster targets
kubectl get neo4jenterprisecluster <cluster-name>
kubectl describe neo4jenterprisecluster <cluster-name>

# For standalone targets
kubectl get neo4jenterprisestandalone <standalone-name>
kubectl describe neo4jenterprisestandalone <standalone-name>

# Check database status
kubectl describe neo4jdatabase <database-name>
kubectl get events --field-selector involvedObject.name=<database-name>
```

**Common Causes**:
- Target deployment not ready or in failed state
- Neo4j authentication issues
- Network connectivity problems
- Resource constraints (memory, CPU)
- Database name validation failures

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

**Troubleshooting Steps**:
```bash
# Check database is online
kubectl exec <target-pod> -c neo4j -- \
  cypher-shell -u neo4j -p <password> "SHOW DATABASES YIELD name, currentStatus WHERE name = '<db-name>'"

# Verify Cypher statements manually
kubectl exec <target-pod> -c neo4j -- \
  cypher-shell -u neo4j -p <password> -d <db-name> "<test-statement>"

# Check operator logs for import errors
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager | grep -i "initial.*data"
```

**Common Issues**:
- Invalid Cypher syntax in statements
- Constraint/index name conflicts
- Database not fully online before import
- Insufficient privileges for data import
- **Note**: Initial data automatically skipped when using `seedURI`
- **Standalone**: Ensure `adminSecret` properly configures authentication

### Seed URI Troubleshooting

**Monitor Progress**:
```bash
# Watch database creation events
kubectl get events --field-selector involvedObject.name=<database-name> --watch

# Check Neo4j logs for CloudSeedProvider activity
kubectl logs <target-pod> -c neo4j | grep -i "cloud.*seed\|restore"

# Verify seed URI accessibility
kubectl run test-uri --rm -it --image=curlimages/curl -- \
  curl -I "<seed-uri>"  # Test HTTP/HTTPS accessibility
```

**Key Events**:
- `DatabaseCreatedFromSeed`: Successful seed URI restoration
- `DataSeeded`: Database seeding completed
- `ValidationWarning`: Configuration or URI format warnings
- `CreationFailed`: Seed restoration failed
- `AuthenticationError`: Credential issues with seed URI

**Authentication Debugging**:
```bash
# Check secret exists and has correct format
kubectl get secret <seed-credentials-secret> -o yaml

# Test cloud credentials from pod
kubectl run aws-test --rm -it --image=amazon/aws-cli -- \
  aws s3 ls <s3-bucket>  # For S3 URIs
```

**Performance Issues**:
- Use `.backup` format instead of `.dump` for large datasets
- Increase `bufferSize` in seed configuration
- Use faster compression (`lz4` instead of `gzip`)
- Monitor pod resource usage during restoration
