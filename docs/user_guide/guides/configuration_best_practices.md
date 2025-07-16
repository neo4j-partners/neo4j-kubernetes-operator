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

### ✅ Correct (Neo4j 5.26+)
```yaml
config:
  # Discovery configuration (automatically set by operator)
  dbms.cluster.discovery.resolver_type: "K8S"
  dbms.cluster.discovery.version: "V2_ONLY"

  # Database format
  db.format: "block"  # Recommended for all deployments
```

### ❌ Deprecated
```yaml
config:
  dbms.cluster.discovery.type: "K8S"    # Deprecated - use resolver_type
  db.format: "standard"                  # Deprecated since 5.23
  db.format: "high_limit"               # Deprecated since 5.23
  server.groups: "group1"               # Deprecated - use initial.server.tags
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
    primaries: 3
    secondaries: 2
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
- Use `dbms.cluster.discovery.resolver_type` instead of `type`
- Use `db.format: "block"` for new databases
- Let the operator manage Discovery V2 settings

### Don'ts ❌
- Don't use `dbms.mode=SINGLE` (removed in 5.x)
- Don't use `dbms.memory.*` settings (use `server.memory.*`)
- Don't use `dbms.connector.*` settings (use `server.*`)
- Don't use `causal_clustering.*` settings (use `dbms.cluster.*`)
- Don't manually configure Kubernetes discovery endpoints

## Automatic Configuration by Operator

The operator automatically configures many settings for optimal Kubernetes operation:

### Cluster Deployments
- `dbms.cluster.discovery.resolver_type: "K8S"`
- `dbms.cluster.discovery.version: "V2_ONLY"` (for Neo4j 5.26+)
- Kubernetes service discovery endpoints
- Network advertised addresses
- Raft and clustering ports

### Standalone Deployments
- Unified clustering infrastructure (no `dbms.mode=SINGLE`)
- Single-member cluster configuration
- Appropriate network bindings

## Version-Specific Considerations

### Neo4j 5.26+ (Semver)
- Discovery parameter: `dbms.kubernetes.service_port_name`
- Discovery V2 parameter: `dbms.kubernetes.discovery.v2.service_port_name`

### Neo4j 2025.x+ (Calver)
- Discovery parameter: `dbms.kubernetes.discovery.service_port_name`
- Same memory and server settings as 5.26+

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
