# Neo4j Database Examples

This directory contains examples for creating and managing Neo4j databases using the Neo4j Kubernetes Operator.

## Standard Database Creation

### Basic Database Examples
- **Simple Database**: See `../database/database-with-topology.yaml` - Database with specified topology
- **Neo4j 2025.x Database**: See `../database/database-2025x.yaml` - Database using Neo4j 2025.x features
- **Note**: Basic database creation examples are shown in the main user guide

## Seed URI Database Creation

The seed URI feature allows creating databases from existing backups stored in cloud storage or accessible via HTTP/FTP.

### Cloud Storage Examples
- `database-from-s3-seed.yaml` - Amazon S3 with explicit credentials
- `database-from-gcs-seed.yaml` - Google Cloud Storage with workload identity
- `database-from-azure-seed.yaml` - Azure Blob Storage (key + SAS token methods)

### HTTP/FTP Examples
- `database-from-http-seed.yaml` - HTTP/HTTPS/FTP with authentication

### Format Comparison
- `database-dump-vs-backup-seed.yaml` - Performance comparison between .dump and .backup formats

## Key Features Demonstrated

### Authentication Methods
1. **System-Wide Authentication (Recommended)**
   - AWS: IAM roles, instance profiles
   - GCP: Workload identity, service accounts
   - Azure: Managed identities

2. **Explicit Credentials**
   - Kubernetes secrets with cloud credentials
   - HTTP basic authentication

### Advanced Seed Configuration
- Point-in-time recovery (Neo4j 2025.x)
- Compression options (gzip, lz4, none)
- Validation modes (strict, lenient)
- Custom buffer sizes

### Database Topology
- Primary/secondary server distribution
- Capacity validation against cluster topology
- Performance optimization warnings

## Quick Start

### 1. Create a Neo4j Enterprise Cluster
```bash
kubectl apply -f ../clusters/minimal-cluster.yaml
```

### 2. Create Database from S3 Backup
```bash
# Create credentials secret (replace with your values)
kubectl create secret generic s3-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=your-access-key \
  --from-literal=AWS_SECRET_ACCESS_KEY=your-secret-key

# Create database from seed
kubectl apply -f database-from-s3-seed.yaml
```

### 3. Verify Database Creation
```bash
# Check database status
kubectl get neo4jdatabase sales-database-from-s3

# View detailed status
kubectl describe neo4jdatabase sales-database-from-s3

# Connect to Neo4j and verify
kubectl port-forward svc/production-cluster-client 7474:7474 &
# Open http://localhost:7474 and run: SHOW DATABASES
```

## File Format Guidelines

### Use .backup Format When:
- Restoring large datasets (>1GB)
- Need point-in-time recovery
- Performance is critical
- Using Neo4j Enterprise exclusively

### Use .dump Format When:
- Cross-version compatibility needed
- Human-readable format preferred
- Small datasets (<100MB)
- Migrating from Community Edition

## Security Best Practices

1. **Prefer System-Wide Authentication**
   ```yaml
   # Good: No explicit credentials
   seedURI: "s3://my-backups/database.backup"
   # seedCredentials: null (uses IAM roles)
   ```

2. **Use Least Privilege Permissions**
   - Grant minimal S3/GCS/Azure permissions
   - Use read-only access for backup restoration
   - Implement bucket/container policies

3. **Rotate Credentials Regularly**
   - Update Kubernetes secrets periodically
   - Use temporary credentials when possible
   - Monitor credential usage

## Performance Tips

1. **Optimize Seed Configuration**
   ```yaml
   seedConfig:
     config:
       compression: "lz4"        # Fast compression
       bufferSize: "256MB"       # Large buffer for big files
       validation: "lenient"     # Skip intensive validation
   ```

2. **Choose Appropriate Topology**
   ```yaml
   topology:
     primaries: 2      # Multiple primaries for write scale
     secondaries: 2    # Read replicas for query scale
   ```

3. **Monitor Resource Usage**
   ```yaml
   options:
     "dbms.memory.heap.max_size": "4g"
     "dbms.memory.pagecache.size": "2g"
   ```

## Troubleshooting

### Common Issues
- **Credential errors**: Check secret contents and IAM permissions
- **URI access failures**: Verify backup file exists and is accessible
- **Topology validation**: Ensure database topology fits cluster capacity
- **Performance issues**: Consider .backup format and larger buffer sizes

### Debugging Commands
```bash
# View operator logs
kubectl logs -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator

# Check database events
kubectl get events --field-selector involvedObject.name=my-database

# Test backup access
kubectl run test-pod --rm -it --image=amazon/aws-cli \
  -- aws s3 ls s3://my-bucket/backup.backup
```

## Related Documentation

- [Seed URI Feature Guide](../docs/seed-uri-feature-guide.md) - Comprehensive feature documentation
- [Neo4j CloudSeedProvider Documentation](https://neo4j.com/docs/operations-manual/current/database-administration/standard-databases/seed-from-uri/) - Official Neo4j documentation
- [Cluster Examples](../clusters/README.md) - Neo4j cluster configuration examples
