# Backup and Restore

This comprehensive guide explains how to use the Neo4j Kubernetes Operator to back up and restore your Neo4j Enterprise clusters. The operator provides advanced backup and restore capabilities through `Neo4jBackup` and `Neo4jRestore` Custom Resources, supporting multiple storage backends, scheduled backups, point-in-time recovery, and more.

## 🚀 Quick Start (5 minutes)

**New to backup and restore?** Start here for an immediate working backup solution:

### Step 1: Create Your First Backup
```bash
# 1. Create admin credentials (if not already done)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

# 2. Apply a simple backup to local storage
kubectl apply -f https://raw.githubusercontent.com/neo4j-labs/neo4j-kubernetes-operator/main/examples/backup-restore/backup-pvc-simple.yaml
```

### Step 2: Monitor Progress
```bash
# Watch backup status
kubectl get neo4jbackups simple-backup -w

# Check backup job logs
kubectl logs job/simple-backup-backup
```

### Step 3: What You Just Created
- ✅ **Backup Resource**: Backs up your `single-node-cluster` to local PVC storage
- ✅ **Compression**: Automatically compresses backup data
- ✅ **Verification**: Validates backup integrity after creation
- ✅ **Retention**: Keeps the 5 most recent backups

**🎯 Success Indicator**: Status should show `Completed` with `BackupSuccessful` condition.

### Next Steps by User Type
- **👥 Teams/Production**: Continue to [Cloud Storage Setup](#cloud-storage-examples) → [Scheduled Backups](#scheduled-backup-examples)
- **🔧 Developers**: Try [Database-Specific Backups](#database-backup-to-gcs) → [Restore Testing](#simple-restore-examples)
- **🏢 Enterprise**: Jump to [Point-in-Time Recovery](#point-in-time-recovery-pitr) → [Advanced Configuration](#advanced-configuration)

---

## Prerequisites

- Neo4j Enterprise cluster running version **5.26.0+** (semver) or **2025.01.0+** (calver)
- Kubernetes cluster with the Neo4j Operator installed
- Appropriate storage backend configured (S3, GCS, Azure, or PVC)
- Admin credentials for the Neo4j cluster

## Neo4j Version Requirements

The backup and restore functionality requires Neo4j Enterprise version 5.26.0 or later, or calver versions 2025.01.0 and later. The operator will automatically validate the Neo4j version before performing backup or restore operations.

**Supported Versions:**
- **Semver**: 5.26.0, 5.26.1, 5.27.0, 6.0.0, etc.
- **Calver**: 2025.01.0, 2025.06.1, 2026.01.0, etc.
- **Enterprise Tags**: 5.26.0-enterprise, 2025.01.0-enterprise, etc.

## Backup Architecture

### How Backups Work

The Neo4j Kubernetes Operator uses a **backup sidecar** architecture for reliability:

1. **Backup Sidecar Container**: Every Neo4j pod includes an automatic backup sidecar
   - Handles backup execution directly on the Neo4j node
   - Allocated 1Gi memory to prevent OOM issues
   - Monitors `/backup-requests` volume for backup jobs

2. **Backup Job**: When you create a `Neo4jBackup` resource, the operator:
   - Creates a Kubernetes Job that connects to the sidecar
   - Sends backup request via shared volume
   - Monitors backup progress and status

3. **Path Management**: The backup sidecar automatically:
   - Creates the full backup destination path before execution
   - Handles Neo4j 5.26+ requirement that paths must exist
   - Manages backup retention and cleanup

4. **RBAC Management**: The operator automatically:
   - Creates necessary service accounts in each namespace
   - Sets up roles with pod/exec permissions for backup jobs
   - Manages role bindings for secure backup execution

### Important Notes

- **Path Creation**: Neo4j 5.26+ and 2025.x+ require the backup destination path to exist before running the backup command. The operator handles this automatically.
- **Memory Requirements**: The backup sidecar requires 1Gi memory for reliable operation
- **Direct Execution**: Backups run directly on Neo4j nodes, not through kubectl
- **RBAC**: No manual RBAC setup required - the operator creates all necessary permissions automatically

## Backup Operations

### Basic Backup Concepts

The operator supports two types of backups:

1. **Cluster Backup**: Backs up all databases in the cluster (default)
2. **Database Backup**: Backs up a specific database

Backups can be performed as:
- **One-time backups**: Created immediately
- **Scheduled backups**: Automated using cron expressions

### Storage Backends

The operator supports multiple storage backends:

| Backend | Type | Best For | Difficulty | Cost |
|---------|------|----------|------------|------|
| **PVC** | `pvc` | Development, testing | 🟢 Beginner | Low |
| **S3** | `s3` | Production, AWS environments | 🟡 Intermediate | Medium |
| **GCS** | `gcs` | Production, GCP environments | 🟡 Intermediate | Medium |
| **Azure** | `azure` | Production, Azure environments | 🟡 Intermediate | Medium |

**💡 Choosing a Storage Backend:**
- **Just starting?** Use PVC for simplicity and local testing
- **Production ready?** Choose the cloud provider matching your cluster
- **Multi-cloud?** S3-compatible storage offers the most flexibility

### One-Time Backup Examples

#### 🟢 Beginner: Backup to PVC

The simplest backup option using local Kubernetes storage:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: simple-backup
spec:
  target:
    kind: Cluster
    name: single-node-cluster
  storage:
    type: pvc
    pvc:
      name: backup-storage
      size: 50Gi
      storageClass: standard
  options:
    compress: true
    verify: true
  retention:
    maxCount: 5
```

**✅ Perfect for:** Development, testing, getting started
**⏱️ Setup time:** 2 minutes
**📋 Prerequisites:** None beyond basic cluster

#### 🟡 Intermediate: Cluster Backup to S3

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: cluster-backup-s3
spec:
  target:
    kind: Cluster
    name: my-neo4j-cluster
  storage:
    type: s3
    bucket: my-backup-bucket
    path: neo4j-backups/cluster
    cloud:
      provider: aws
      region: us-east-1
  options:
    compress: true
    verify: true
  retention:
    maxAge: "30d"
    maxCount: 10
```

**✅ Perfect for:** Production AWS environments, long-term retention
**⏱️ Setup time:** 10 minutes
**📋 Prerequisites:** AWS S3 bucket, IAM credentials or roles

#### 🟡 Intermediate: Database Backup to GCS

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: database-backup-gcs
spec:
  target:
    kind: Database
    name: myapp-db
  storage:
    type: gcs
    bucket: my-gcs-backup-bucket
    path: neo4j-backups/myapp
    cloud:
      provider: gcp
      region: us-central1
  options:
    compress: true
    encryption:
      enabled: true
      keySecret: backup-encryption-key
      algorithm: AES256
```

**✅ Perfect for:** Multi-database environments, GCP production
**⏱️ Setup time:** 15 minutes
**📋 Prerequisites:** GCS bucket, service account credentials

#### 🟡 Intermediate: Backup to Azure Blob Storage

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: backup-azure
spec:
  target:
    kind: Cluster
    name: production-cluster
  storage:
    type: azure
    bucket: backups  # Container name
    path: neo4j/production
    cloud:
      provider: azure
      region: eastus
  options:
    compress: true
    verify: true
```

**✅ Perfect for:** Azure production environments
**⏱️ Setup time:** 10 minutes
**📋 Prerequisites:** Azure storage account, access keys

#### 🔄 Alternative: Backup to PVC

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: backup-pvc
spec:
  target:
    kind: Cluster
    name: dev-cluster
  storage:
    type: pvc
    pvc:
      name: backup-storage
      storageClass: fast-ssd
      size: 100Gi
  options:
    compress: true
```

*Note: This is a duplicate of the beginner example above. Already covered in the progression.*

### Scheduled Backup Examples

#### 🟡 Intermediate: Daily Scheduled Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-backup
spec:
  target:
    kind: Cluster
    name: production-cluster
  schedule: "0 2 * * *"  # Daily at 2 AM UTC
  storage:
    type: s3
    bucket: production-backups
    path: daily
    cloud:
      provider: aws
      region: us-west-2
  retention:
    maxAge: "7d"
    maxCount: 7
    deletePolicy: Delete
  options:
    compress: true
    verify: true
```

**✅ Perfect for:** Production environments, consistent backup strategy
**⏱️ Setup time:** 15 minutes
**📋 Prerequisites:** Production cluster, cloud storage
**🔄 Schedule:** Daily at 2 AM UTC - adjust timezone as needed

#### 🔴 Advanced: Weekly Backup with Long Retention

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: weekly-backup
spec:
  target:
    kind: Cluster
    name: production-cluster
  schedule: "0 1 * * 0"  # Weekly on Sunday at 1 AM UTC
  storage:
    type: gcs
    bucket: long-term-backups
    path: weekly
    cloud:
      provider: gcp
      region: us-central1
  retention:
    maxAge: "90d"
    maxCount: 12
    deletePolicy: Archive
  options:
    compress: true
    verify: true
    encryption:
      enabled: true
      keySecret: backup-encryption-key
```

**✅ Perfect for:** Enterprise compliance, long-term archival
**⏱️ Setup time:** 20 minutes
**📋 Prerequisites:** Encryption setup, compliance policies
**🔄 Schedule:** Weekly on Sunday - complements daily backups

### 🔧 Suspended Backups

You can temporarily suspend scheduled backups:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: maintenance-backup
spec:
  target:
    kind: Cluster
    name: my-cluster
  schedule: "0 3 * * *"
  suspend: true  # Suspends the backup schedule
  storage:
    type: s3
    bucket: backups
    path: maintenance
```

## Restore Operations

### Basic Restore Concepts

The operator supports multiple restore sources:

1. **Backup Reference**: Restore from an existing `Neo4jBackup` resource
2. **Storage Location**: Restore directly from storage path
3. **Point-in-Time Recovery (PITR)**: Restore to a specific point in time

### Simple Restore Examples

#### 🟢 Beginner: Restore from Backup Reference

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-from-backup
spec:
  targetCluster: my-neo4j-cluster
  databaseName: neo4j
  source:
    type: backup
    backupRef: daily-backup
  options:
    verifyBackup: true
    replaceExisting: true
  force: false
  stopCluster: true
```

**✅ Perfect for:** Quick recovery, testing restore procedures
**⏱️ Restore time:** 5-15 minutes
**📋 Prerequisites:** Existing backup resource

#### 🟡 Intermediate: Restore from Storage Location

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-from-storage
spec:
  targetCluster: recovery-cluster
  databaseName: myapp-db
  source:
    type: storage
    storage:
      type: s3
      bucket: backup-bucket
      path: neo4j-backups/cluster/backup-20250104-120000
    backupPath: /backup/cluster/backup-20250104-120000
  options:
    verifyBackup: true
    replaceExisting: true
  force: true
  stopCluster: true
```

**✅ Perfect for:** Cross-cluster recovery, disaster scenarios
**⏱️ Restore time:** 10-30 minutes
**📋 Prerequisites:** Direct storage access, backup path knowledge

### 🔴 Advanced: Point-in-Time Recovery (PITR)

PITR allows you to restore your database to a specific point in time using base backups and transaction logs. This is the most sophisticated restore option for precise recovery scenarios.

#### PITR Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: pitr-restore
spec:
  targetCluster: recovery-cluster
  databaseName: production-db
  source:
    type: pitr
    pointInTime: "2025-01-04T12:30:00Z"
    pitr:
      baseBackup:
        type: backup
        backupRef: daily-backup
      logStorage:
        type: s3
        bucket: transaction-logs
        path: neo4j-logs/production
        cloud:
          provider: aws
          region: us-east-1
      logRetention: "7d"
      recoveryPointObjective: "5m"
      validateLogIntegrity: true
      compression:
        enabled: true
        algorithm: gzip
        level: 6
      encryption:
        enabled: true
        keySecret: log-encryption-key
        algorithm: AES256
  options:
    verifyBackup: true
    replaceExisting: true
  force: true
  stopCluster: true
  timeout: "2h"
```

**✅ Perfect for:** Compliance requirements, precise recovery points
**⏱️ Restore time:** 30-120 minutes
**📋 Prerequisites:** Base backup, transaction logs, advanced understanding

#### 🔴 Advanced: PITR with Storage-based Base Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: pitr-storage-restore
spec:
  targetCluster: disaster-recovery
  databaseName: critical-app
  source:
    type: pitr
    pointInTime: "2025-01-04T14:45:30Z"
    pitr:
      baseBackup:
        type: storage
        storage:
          type: gcs
          bucket: base-backups
          path: production/base-backup-20250104
        backupPath: /backup/base-backup-20250104
      logStorage:
        type: gcs
        bucket: transaction-logs
        path: production/logs
      validateLogIntegrity: true
  options:
    verifyBackup: true
  force: true
  stopCluster: true
```

**✅ Perfect for:** Disaster recovery, multi-region scenarios
**⏱️ Restore time:** 45-180 minutes
**📋 Prerequisites:** Complex storage setup, advanced operational knowledge

### 🔴 Advanced: Restore with Hooks

Pre and post-restore hooks allow you to execute custom operations before and after the restore process.

#### Restore with Cypher Hooks

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-with-hooks
spec:
  targetCluster: my-cluster
  databaseName: myapp
  source:
    type: backup
    backupRef: production-backup
  options:
    verifyBackup: true
    replaceExisting: true
    preRestore:
      cypherStatements:
        - "CALL dbms.backup.prepare()"
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes()"
        - "CALL dbms.security.clearAuthCache()"
        - "MATCH (n:User) SET n.lastRestore = datetime()"
  force: false
  stopCluster: true
```

#### Restore with Job Hooks

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-with-job-hooks
spec:
  targetCluster: staging-cluster
  databaseName: app-data
  source:
    type: backup
    backupRef: staging-backup
  options:
    verifyBackup: true
    preRestore:
      job:
        template:
          container:
            image: my-registry/data-prep:latest
            command: ["/bin/sh"]
            args: ["-c", "echo 'Preparing for restore'; /scripts/pre-restore.sh"]
            env:
              - name: CLUSTER_NAME
                value: staging-cluster
              - name: DATABASE_NAME
                value: app-data
        timeout: "10m"
    postRestore:
      job:
        template:
          container:
            image: my-registry/data-validator:latest
            command: ["/bin/sh"]
            args: ["-c", "/scripts/validate-restore.sh"]
            env:
              - name: NEO4J_URI
                value: "neo4j://staging-cluster:7687"
              - name: NEO4J_PASSWORD
                valueFrom:
                  secretKeyRef:
                    name: staging-admin-secret
                    key: password
        timeout: "15m"
  stopCluster: true
```

## 🎯 Decision Guide: Choose Your Backup Strategy

### Quick Decision Tree

```
Are you just getting started?
├─ YES → Start with PVC backup (🟢 Beginner)
└─ NO ↓

Do you need production-grade reliability?
├─ YES → Use cloud storage (S3/GCS/Azure) (🟡 Intermediate)
└─ NO → PVC backup is sufficient

Do you need compliance/audit trails?
├─ YES → Weekly encrypted backups + PITR (🔴 Advanced)
└─ NO → Daily backups with retention

Do you need precise recovery points?
├─ YES → Point-in-Time Recovery (🔴 Advanced)
└─ NO → Regular backup/restore is sufficient
```

### Storage Backend Comparison

| Factor | PVC | S3 | GCS | Azure |
|--------|-----|----|----|-------|
| **Setup Complexity** | 🟢 Simple | 🟡 Medium | 🟡 Medium | 🟡 Medium |
| **Cost** | Low | Medium | Medium | Medium |
| **Durability** | Cluster-dependent | 99.999999999% | 99.999999999% | 99.999999999% |
| **Multi-region** | ❌ No | ✅ Yes | ✅ Yes | ✅ Yes |
| **Encryption** | Optional | ✅ Built-in | ✅ Built-in | ✅ Built-in |
| **Best For** | Dev/Test | AWS prod | GCP prod | Azure prod |

### Backup Frequency Recommendations

| Environment | Frequency | Retention | Storage |
|-------------|-----------|-----------|---------|
| **Development** | Manual | 3-5 backups | PVC |
| **Staging** | Daily | 7 days | Cloud |
| **Production** | Daily + Weekly | 30d + 90d | Cloud + Archive |
| **Critical Systems** | Daily + PITR | 90d + compliance | Multi-region cloud |

## Monitoring Backup and Restore Operations

### Checking Backup Status

```bash
# List all backups
kubectl get neo4jbackups

# Get detailed backup status
kubectl describe neo4jbackup daily-backup

# View backup history
kubectl get neo4jbackup daily-backup -o jsonpath='{.status.history}'

# Check backup job logs
kubectl logs job/daily-backup-backup
```

### Checking Restore Status

```bash
# List all restores
kubectl get neo4jrestores

# Get detailed restore status
kubectl describe neo4jrestore restore-operation

# Check restore job logs
kubectl logs job/restore-operation-restore

# Monitor restore progress
kubectl get neo4jrestore restore-operation -w
```

### Backup and Restore Events

```bash
# View events for backup operations
kubectl get events --field-selector involvedObject.name=daily-backup

# View events for restore operations
kubectl get events --field-selector involvedObject.name=restore-operation
```

## Best Practices

### Backup Best Practices

1. **Regular Testing**: Regularly test your backup and restore procedures
2. **Multiple Retention Policies**: Use different retention policies for different backup frequencies
3. **Encryption**: Always enable encryption for sensitive data
4. **Verification**: Enable backup verification to ensure backup integrity
5. **Cross-Region**: Store backups in different regions for disaster recovery
6. **Monitoring**: Set up monitoring and alerting for backup operations

### Restore Best Practices

1. **Cluster Scaling**: Use `stopCluster: true` for consistent restores
2. **Backup Verification**: Always verify backups before restoring
3. **Test Environment**: Test restores in non-production environments first
4. **Documentation**: Document your restore procedures and test them regularly
5. **Point-in-Time Recovery**: Use PITR for precise recovery requirements
6. **Hooks**: Use pre/post hooks for application-specific requirements

### Security Best Practices

1. **RBAC**: The operator automatically manages RBAC for backup operations. No manual configuration needed.
2. **Secrets Management**: Store encryption keys and credentials in Kubernetes secrets
3. **Network Policies**: Implement network policies to restrict backup traffic
4. **Audit Logging**: Enable audit logging for backup and restore operations
5. **Access Control**: Limit access to backup storage and restoration capabilities

## Advanced Configuration

### Cloud Storage Authentication

#### AWS S3 Authentication

```yaml
# Using IAM roles (recommended)
spec:
  storage:
    type: s3
    bucket: my-bucket
    cloud:
      provider: aws
      region: us-east-1
      # IAM role will be used via service account

---
# Using access keys (less secure)
apiVersion: v1
kind: Secret
metadata:
  name: aws-credentials
type: Opaque
data:
  AWS_ACCESS_KEY_ID: <base64-encoded-key>
  AWS_SECRET_ACCESS_KEY: <base64-encoded-secret>
```

#### Google Cloud Storage Authentication

```yaml
# Using service account key
apiVersion: v1
kind: Secret
metadata:
  name: gcs-credentials
type: Opaque
data:
  service-account.json: <base64-encoded-service-account-json>

---
spec:
  storage:
    type: gcs
    bucket: my-gcs-bucket
    cloud:
      provider: gcp
      region: us-central1
      # Service account will be mounted automatically
```

#### Azure Blob Storage Authentication

```yaml
# Using storage account key
apiVersion: v1
kind: Secret
metadata:
  name: azure-credentials
type: Opaque
data:
  AZURE_STORAGE_ACCOUNT: <base64-encoded-account-name>
  AZURE_STORAGE_KEY: <base64-encoded-storage-key>

---
spec:
  storage:
    type: azure
    bucket: my-container
    cloud:
      provider: azure
      region: eastus
```

### Custom Backup Arguments

```yaml
spec:
  options:
    additionalArgs:
      - "--parallel-recovery"
      - "--temp-path=/tmp/backup"
      - "--verbose"
```

### Cross-Namespace Operations

```yaml
# Backup a cluster in a different namespace
spec:
  target:
    kind: Cluster
    name: production-cluster
    namespace: production  # Different namespace
```

## 🚨 Troubleshooting Quick Reference

**Something not working?** Check these common issues first:

### ⚡ Quick Fixes (30 seconds each)

| Problem | Quick Check | Solution |
|---------|-------------|----------|
| **Backup Failed** | `kubectl describe neo4jbackup <name>` | Check events and conditions |
| **Permission Denied** | `kubectl logs job/<backup-name>-backup` | Verify storage credentials |
| **Version Error** | Check cluster Neo4j version | Ensure 5.26.0+ or 2025.01.0+ |
| **Out of Space** | `kubectl get pvc` | Check storage capacity |
| **Network Issues** | `kubectl get networkpolicies` | Verify connectivity rules |

### 🔍 Detailed Troubleshooting

For comprehensive troubleshooting, diagnostics, and advanced problem-solving:
👉 **[Complete Troubleshooting Guide](../troubleshooting/backup_restore.md)**

The troubleshooting guide includes:
- **Step-by-step diagnostics** for each backup/restore scenario
- **Advanced debugging** techniques and tools
- **Performance tuning** for large datasets
- **Network and security** troubleshooting
- **Storage-specific** problem resolution

## 📚 Additional Resources

### API Documentation
- **[Neo4jBackup API Reference](../../api_reference/neo4jbackup.md)** - Complete field specifications and options
- **[Neo4jRestore API Reference](../../api_reference/neo4jrestore.md)** - Detailed restore configuration reference

### Examples & Templates
- **[Working Examples](../../../examples/backup-restore/)** - Copy-paste ready YAML files
- **[Getting Started Guide](../getting_started.md)** - Deploy your first cluster
- **[Installation Guide](../installation.md)** - Install the operator

### Advanced Topics
- **[Troubleshooting Guide](../troubleshooting/backup_restore.md)** - Comprehensive problem-solving
- **[Security Best Practices](../security.md)** - Secure your backup operations
- **[Performance Tuning](../performance.md)** - Optimize backup/restore performance

### Community & Support
- **[GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)** - Report bugs and request features
- **[Neo4j Community](https://community.neo4j.com/)** - Get help from the community
- **[Neo4j Documentation](https://neo4j.com/docs/)** - Official Neo4j documentation

---

## 🎓 Learning Path Summary

**Just completed backup and restore setup?** Here's what to explore next:

### 🟢 Beginner Path
1. ✅ Complete Quick Start → 2. Set up monitoring → 3. Test restore procedures

### 🟡 Intermediate Path
1. ✅ Cloud storage setup → 2. Implement scheduled backups → 3. Configure alerting

### 🔴 Advanced Path
1. ✅ PITR implementation → 2. Multi-cluster backup strategy → 3. Compliance automation

**Need help?** Start with our [Troubleshooting Guide](../troubleshooting/backup_restore.md) or ask the [community](https://community.neo4j.com/).
