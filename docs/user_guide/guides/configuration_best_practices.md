# Neo4j Configuration Best Practices

This guide provides best practices for configuring Neo4j 5.26+ and 2025.x+ in Kubernetes using the Neo4j Enterprise Operator.

## Important Configuration Changes

### Neo4j 5.26+ Configuration Updates

The operator supports Neo4j 5.26+ which introduced several configuration changes. This guide helps you use the correct, non-deprecated settings.

## Memory Configuration

### ✅ Correct (Neo4j 5.26+)
```yaml
config:
  server.memory.heap.initial_size: "2G"
  server.memory.heap.max_size: "4G"
  server.memory.pagecache.size: "2G"
```

### ❌ Deprecated (Pre-5.26)
```yaml
config:
  dbms.memory.heap.initial_size: "2G"    # Deprecated
  dbms.memory.heap.max_size: "4G"        # Deprecated
  dbms.memory.pagecache.size: "2G"       # Deprecated
```

## TLS/SSL Configuration

### ✅ Correct (Neo4j 5.26+)
```yaml
config:
  # HTTPS configuration
  server.https.enabled: "true"
  server.https.listen_address: "0.0.0.0:7473"

  # Bolt TLS configuration
  server.bolt.enabled: "true"
  server.bolt.tls_level: "REQUIRED"

  # SSL Policies
  dbms.ssl.policy.https.enabled: "true"
  dbms.ssl.policy.bolt.enabled: "true"
```

### ❌ Deprecated
```yaml
config:
  dbms.connector.https.enabled: "true"        # Deprecated
  dbms.connector.bolt.tls_level: "REQUIRED"   # Deprecated
```

## Clustering Configuration

### Discovery Configuration (Operator-Managed)

The operator automatically injects all discovery settings into each pod's startup script. **Do not set discovery keys in `spec.config`** — the validator will reject them.

### ✅ What the operator injects (do not override)

**Neo4j 5.26.x (SemVer)**:
```properties
# Injected into /tmp/neo4j-config/neo4j.conf at pod startup:
dbms.cluster.discovery.resolver_type=LIST
dbms.cluster.discovery.version=V2_ONLY
dbms.cluster.discovery.v2.endpoints=<pod-0-fqdn>:6000,<pod-1-fqdn>:6000,...
```

**Neo4j 2025.x+ / 2026.x+ (CalVer)**:
```properties
# Injected into /tmp/neo4j-config/neo4j.conf at pod startup:
dbms.cluster.discovery.resolver_type=LIST
dbms.cluster.endpoints=<pod-0-fqdn>:6000,<pod-1-fqdn>:6000,...
# No version flag — V2 is the only protocol in CalVer releases
```

**Port used**: **6000** (`tcp-tx`) — V2 cluster traffic + discovery. Port 5000 (`tcp-discovery`) was the V1 discovery port and is **not used** by this operator.

### ❌ Forbidden in `spec.config` (validator will reject)
```yaml
config:
  dbms.cluster.discovery.resolver_type: "..."   # Managed by operator
  dbms.cluster.discovery.v2.endpoints: "..."    # Managed by operator (5.26.x)
  dbms.cluster.endpoints: "..."                  # Managed by operator (2025.x+)
  dbms.kubernetes.label_selector: "..."         # K8S discovery not used

  # Database format (deprecated)
  db.format: "standard"     # Deprecated since 5.23 — use block format
  db.format: "high_limit"   # Deprecated since 5.23 — use block format
  server.groups: "group1"   # Deprecated — use initial.server.tags
```

## Common Configuration Patterns

### Production Cluster Configuration
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  topology:
    servers: 5   # servers self-organise into primary/secondary roles
  config:
    # Memory settings (using correct server.* prefix)
    server.memory.heap.initial_size: "8G"
    server.memory.heap.max_size: "16G"
    server.memory.pagecache.size: "8G"

    # Query performance
    dbms.logs.query.enabled: "INFO"
    dbms.logs.query.threshold: "1s"
    dbms.logs.query.page_logging_enabled: "true"

    # Transaction settings
    dbms.transaction.timeout: "5m"
    dbms.lock.acquisition.timeout: "2m"

    # Checkpoint tuning
    dbms.checkpoint.interval.time: "15m"
    dbms.checkpoint.interval.tx: "100000"
```

### Development Standalone Configuration
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: dev-instance
spec:
  config:
    # Memory settings (using correct server.* prefix)
    server.memory.heap.initial_size: "1G"
    server.memory.heap.max_size: "2G"
    server.memory.pagecache.size: "512M"

    # Development-friendly settings
    dbms.logs.query.enabled: "true"
    dbms.security.procedures.unrestricted: "gds.*,apoc.*"
    dbms.security.allow_csv_import_from_file_urls: "true"
```

## Configuration Do's and Don'ts

### Do's ✅
- Use `server.memory.*` for memory settings
- Use `server.https.*` and `server.bolt.*` for protocol settings
- Use `db.format: "block"` for new databases
- Let the operator manage all discovery settings (LIST resolver, endpoints, V2_ONLY)

### Don'ts ❌
- Don't use `dbms.mode=SINGLE` (removed in 5.x)
- Don't use `dbms.memory.*` settings (use `server.memory.*`)
- Don't use `dbms.connector.*` settings (use `server.*`)
- Don't use `causal_clustering.*` settings (use `dbms.cluster.*`)
- Don't set `dbms.cluster.discovery.*` or `dbms.cluster.endpoints` in `spec.config` — operator manages these

## Automatic Configuration by Operator

### Cluster Deployments
- LIST discovery with static pod FQDNs via headless service (`{cluster}-server-{n}.{cluster}-headless.{ns}.svc.cluster.local:6000`)
- Version-specific endpoint settings (`dbms.cluster.discovery.v2.endpoints` for 5.26.x, `dbms.cluster.endpoints` for 2025.x+)
- `dbms.cluster.discovery.version=V2_ONLY` for 5.26.x (not needed for CalVer)
- ME/OTHER bootstrap strategy (server-0 = preferred bootstrapper)
- Network advertised addresses and RAFT/routing ports

### Standalone Deployments
- Unified clustering infrastructure (no `dbms.mode=SINGLE`)
- Single-member cluster configuration
- Appropriate network bindings

## Database Configuration Best Practices

### Database Creation Options

The operator supports two main approaches for populating databases with initial data:

#### Standard Database with Initial Data
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-app-database
spec:
  clusterRef: production-cluster
  name: appdb

  # Initial schema and constraints
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
      - "CREATE INDEX user_name IF NOT EXISTS FOR (u:User) ON (u.name)"
      - "CREATE INDEX product_category IF NOT EXISTS FOR (p:Product) ON (p.category)"

  # Database topology
  topology:
    primaries: 2
    secondaries: 1

  wait: true
  ifNotExists: true
```

#### Database from Seed URI (Recommended for Migrations)
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: migrated-database
spec:
  clusterRef: production-cluster
  name: migrated-db

  # Create from existing backup
  seedURI: "s3://my-neo4j-backups/production-backup.backup"

  # Point-in-time recovery (Neo4j 2025.x only)
  seedConfig:
    restoreUntil: "2025-01-15T10:30:00Z"
    config:
      compression: "gzip"
      validation: "strict"
      bufferSize: "128MB"

  # Use system-wide cloud authentication (preferred)
  # seedCredentials: null  # Relies on IAM roles, workload identity, etc.

  # Database distribution
  topology:
    primaries: 2
    secondaries: 2

  wait: true
  ifNotExists: true
  defaultCypherLanguage: "25"  # Neo4j 2025.x only
```

### Database Configuration Best Practices

#### ✅ Do's
- **Prefer seed URI for migrations**: Use `seedURI` when migrating from existing Neo4j instances
- **Use system-wide authentication**: Rely on IAM roles, workload identity, managed identities instead of explicit credentials
- **Choose appropriate topology**: Balance primaries and secondaries based on read/write patterns
- **Use .backup format**: Prefer Neo4j backup format over dump format for better performance
- **Set appropriate timeouts**: Use `wait: true` for critical databases to ensure they're ready before proceeding
- **Use IF NOT EXISTS patterns**: Include `ifNotExists: true` and IF NOT EXISTS in Cypher statements
- **Test restoration**: Verify seed URIs are accessible and contain expected data before production use

#### ❌ Don'ts
- **Don't combine data sources**: Never specify both `seedURI` and `initialData` - they conflict
- **Don't use explicit credentials unnecessarily**: Avoid storing cloud credentials in secrets when system-wide auth is available
- **Don't ignore topology validation**: Ensure database topology doesn't exceed cluster capacity
- **Don't use .dump for large datasets**: Use .backup format for better performance with large databases
- **Don't skip point-in-time recovery**: Use `restoreUntil` when precise restoration timing is required (Neo4j 2025.x)

### Seed URI Security Best Practices

#### Authentication Hierarchy (Preferred → Fallback)
1. **System-Wide Authentication** (Most Secure):
   - AWS: IAM roles for service accounts (IRSA), EC2 instance profiles
   - GCP: Workload Identity, default service accounts
   - Azure: Managed identities, service principal environment variables

2. **Explicit Credentials** (When System-Wide Unavailable):
   - Kubernetes secrets with minimal required permissions
   - Temporary credentials with limited lifetime
   - Regular credential rotation

#### Example: System-Wide Authentication Setup
```yaml
# AWS IRSA example
apiVersion: v1
kind: ServiceAccount
metadata:
  name: neo4j-backup-reader
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT:role/Neo4jBackupReader
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  backups:
    cloud:
      provider: aws
      identity:
        provider: aws
        serviceAccount: neo4j-backup-reader  # Uses IAM role
  # ... other cluster configuration
---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: restored-database
spec:
  clusterRef: production-cluster
  seedURI: "s3://my-backups/database.backup"
  # No explicit credentials needed - uses IAM role
```

## Version-Specific Considerations

### Neo4j 5.26.x (SemVer)
- Discovery: LIST resolver — `dbms.cluster.discovery.v2.endpoints=<pod-fqdns>:6000` + `V2_ONLY` (operator-managed)
- Seed URI support with CloudSeedProvider
- No point-in-time recovery for seed URIs

### Neo4j 2025.x+ / 2026.x+ (CalVer)
- Discovery: LIST resolver — `dbms.cluster.endpoints=<pod-fqdns>:6000` (operator-managed, no version flag)
- Same memory and server settings as 5.26.x
- Enhanced seed URI support with point-in-time recovery
- `defaultCypherLanguage` field support
- `restoreUntil` field support in `seedConfig`

## Validation

The operator includes validation to prevent common configuration mistakes:
- Warns about deprecated `db.format` values
- Blocks clustering configurations in standalone deployments
- Validates memory settings against container resources
- Ensures required settings for chosen deployment type

## Migration from Older Versions

If migrating from Neo4j 4.x or earlier 5.x versions:

1. Update all `dbms.memory.*` to `server.memory.*`
2. Update all `dbms.connector.*` to appropriate `server.*` settings
3. Remove any `dbms.mode=SINGLE` configurations
4. Update `causal_clustering.*` to `dbms.cluster.*` (if manually configured)
5. Ensure using `db.format: "block"` for new databases

## References

- [Neo4j 5.26 Configuration Settings](https://neo4j.com/docs/operations-manual/5/configuration/configuration-settings/)
- [Neo4j 2025.x Configuration Settings](https://neo4j.com/docs/operations-manual/2025.06/configuration/configuration-settings/)
- [Neo4j Upgrade Guide](https://neo4j.com/docs/upgrade-migration-guide/current/)
