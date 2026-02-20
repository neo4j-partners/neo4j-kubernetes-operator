# Backup and Restore

This comprehensive guide explains how to use the Neo4j Kubernetes Operator to back up and restore your Neo4j Enterprise clusters and standalone instances. The operator provides advanced backup and restore capabilities through `Neo4jBackup` and `Neo4jRestore` Custom Resources, supporting multiple storage backends, scheduled backups, point-in-time recovery, and more.

## Quick Start (5 minutes)

New to backup and restore? Start here for an immediate working backup solution.

### Step 1: Create Your First Backup

```bash
# 1. Create admin credentials (if not already done)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

# 2. Apply a simple backup to local PVC storage
kubectl apply -f examples/backup-restore/backup-pvc-simple.yaml
```

### Step 2: Monitor Progress

```bash
# Watch backup status
kubectl get neo4jbackups simple-backup -w

# Check backup job logs
kubectl logs job/simple-backup-backup
```

### Step 3: What You Just Created

- **Backup Resource**: Backs up your `single-node-cluster` to local PVC storage
- **Compression**: Automatically compresses backup data
- **Verification**: Validates backup integrity after creation
- **Retention**: Keeps the 5 most recent backups

**Success Indicator**: Status should show `Completed` with `BackupSuccessful` condition.

### Next Steps by User Type

- **Teams/Production**: Continue to [Cloud Storage Setup](#cloud-storage-examples) → [Scheduled Backups](#scheduled-backup-examples)
- **Developers**: Try [Database-Specific Backups](#database-backup-examples) → [Restore Testing](#simple-restore-examples)
- **Enterprise**: Jump to [Point-in-Time Recovery](#point-in-time-recovery-pitr) → [Advanced Configuration](#advanced-configuration)

---

## Prerequisites

- Neo4j Enterprise cluster or standalone running version **5.26.0+** (semver) or **2025.01.0+** (CalVer)
- Kubernetes cluster with the Neo4j Operator installed
- Appropriate storage backend configured (S3, GCS, Azure, or PVC)
- Admin credentials for the Neo4j instance

## Supported Deployment Types

Both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` are fully supported as backup and restore targets. The `targetCluster` field (on restore) and `target.name` / `target.clusterRef` fields (on backup) can reference either type — the operator detects the deployment type automatically.

## Neo4j Version Requirements

Backup and restore require Neo4j Enterprise 5.26.0 or later, or CalVer 2025.01.0+.

**Supported Versions:**
- **Semver**: 5.26.0, 5.26.1 (5.26.x is the last semver LTS — no 5.27+ exists)
- **CalVer**: 2025.01.0, 2025.04.0, 2026.01.0, etc.
- **Enterprise tags required**: `neo4j:5.26.0-enterprise`, `neo4j:2025.01.0-enterprise`

---

## Backup Architecture

### How Backups Work

The operator runs a Kubernetes **Job** that executes `neo4j-admin database backup` directly inside the **same Neo4j Enterprise image** as your cluster. No sidecar containers, no separate tooling images.

**End-to-end flow:**

1. You create a `Neo4jBackup` resource.
2. The operator creates a Kubernetes Job using the same Neo4j Enterprise image as your cluster.
3. The Job runs `neo4j-admin database backup --from=<server-pod-fqdn>:6362 --to-path=<destination>` against each server pod.
4. For cloud storage destinations (`s3://`, `gs://`, `azb://`), `neo4j-admin` streams data directly to the cloud — no intermediate local copy is required (though `tempPath` is strongly recommended; see below).
5. The operator updates the `Neo4jBackup` status as the Job progresses.

**Backup listen address**: The operator automatically configures `server.backup.listen_address=0.0.0.0:6362` in each server pod's `neo4j.conf`, so backup Jobs can reach any server pod without additional manual configuration.

**Pod naming**: Backup Jobs connect to `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc., using their full Kubernetes DNS FQDNs on port 6362.

### RBAC

The operator automatically creates a `neo4j-backup-sa` ServiceAccount in the same namespace as your backup resource. Backup Jobs run as this service account.

**No Role or RoleBinding is created** — backup Jobs invoke `neo4j-admin` directly against the backup port; no Kubernetes API access is needed by the backup process itself.

If you are using **Workload Identity** (AWS IRSA, GKE Workload Identity, or Azure Workload Identity), you attach annotations to the `neo4j-backup-sa` ServiceAccount via the `cloud.identity.autoCreate.annotations` field (see [Cloud Storage Authentication](#cloud-storage-authentication)).

### Backup Types

| Type | Description | `backupType` value |
|------|-------------|-------------------|
| **Full** | Complete snapshot of all database files | `FULL` |
| **Differential** | Only pages changed since the last full backup | `DIFF` |

Differential backups are significantly smaller and faster for large databases. The operator selects the correct parent backup automatically (most recent full by default; or most recent differential if `preferDiffAsParent: true` is set — requires CalVer 2025.04+).

### Storage Backends

| Backend | Type | Best For |
|---------|------|----------|
| **PVC** | `pvc` | Development, testing, air-gapped environments |
| **AWS S3** | `s3` | Production on AWS |
| **Google Cloud Storage** | `gcs` | Production on GCP |
| **Azure Blob Storage** | `azure` | Production on Azure |

---

## Cloud Storage Authentication

Cloud backup Jobs need permission to write to your bucket. The operator supports two authentication paths.

### Choosing Your Authentication Method

| Scenario | Recommended Method |
|----------|--------------------|
| AWS EKS with IRSA configured | Workload Identity (IRSA) |
| GKE with Workload Identity enabled | Workload Identity (GKE WI) |
| AKS with Workload Identity enabled | Workload Identity (Azure WI) |
| Any cloud — quick setup, non-production | Explicit credentials via `credentialsSecretRef` |
| On-prem Kubernetes (no cloud IAM) | Explicit credentials via `credentialsSecretRef` |
| Compliance requires no long-lived keys | Workload Identity |

**Security recommendation**: Prefer Workload Identity in production. Explicit credentials work everywhere and are simpler to set up initially, but rotate them regularly and store them only in Kubernetes Secrets.

---

### AWS S3 Authentication

#### Path 1: Explicit Credentials

Create a Kubernetes Secret with your AWS credentials:

```bash
kubectl create secret generic aws-backup-creds \
  --from-literal=AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --from-literal=AWS_DEFAULT_REGION=us-east-1
```

Reference the Secret in your `Neo4jBackup`:

```yaml
storage:
  type: s3
  bucket: my-neo4j-backups
  path: cluster-backups
  cloud:
    provider: aws
    credentialsSecretRef: aws-backup-creds
```

The operator mounts all keys from the Secret as environment variables in the backup Job pod, which `neo4j-admin` picks up automatically.

#### Path 2: AWS IRSA (IAM Roles for Service Accounts)

Annotate the automatically-created `neo4j-backup-sa` ServiceAccount with your IAM role ARN:

```yaml
storage:
  type: s3
  bucket: my-neo4j-backups
  path: cluster-backups
  cloud:
    provider: aws
    identity:
      autoCreate:
        annotations:
          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role
```

The IAM role must allow these actions on your bucket:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:ListBucket",
        "s3:DeleteObject"
      ],
      "Resource": [
        "arn:aws:s3:::my-neo4j-backups",
        "arn:aws:s3:::my-neo4j-backups/*"
      ]
    }
  ]
}
```

The IAM role must also have a trust relationship with the EKS OIDC provider for the `neo4j-backup-sa` ServiceAccount in your namespace.

---

### Google Cloud Storage Authentication

#### Path 1: Explicit Credentials (Service Account Key)

The key inside the Secret **must** be named `GOOGLE_APPLICATION_CREDENTIALS_JSON`:

```bash
kubectl create secret generic gcs-backup-creds \
  --from-literal=GOOGLE_APPLICATION_CREDENTIALS_JSON="$(cat /path/to/service-account-key.json)"
```

Reference the Secret in your `Neo4jBackup`:

```yaml
storage:
  type: gcs
  bucket: my-neo4j-backups
  path: cluster-backups
  cloud:
    provider: gcp
    credentialsSecretRef: gcs-backup-creds
```

The operator writes the JSON value to a file inside the backup Job pod and sets `GOOGLE_APPLICATION_CREDENTIALS` to point to it. `neo4j-admin` authenticates using the Application Default Credentials chain.

#### Path 2: GKE Workload Identity

Annotate the `neo4j-backup-sa` ServiceAccount with the GCP service account to impersonate:

```yaml
storage:
  type: gcs
  bucket: my-neo4j-backups
  path: cluster-backups
  cloud:
    provider: gcp
    identity:
      autoCreate:
        annotations:
          iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com
```

You must also bind the Kubernetes ServiceAccount to the GCP service account and grant the GCP SA storage access:

```bash
# Allow the Kubernetes SA to impersonate the GCP SA
gcloud iam service-accounts add-iam-policy-binding \
  neo4j-backup@my-project.iam.gserviceaccount.com \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:my-project.svc.id.goog[my-namespace/neo4j-backup-sa]"

# Grant GCS objectAdmin to the GCP SA
gsutil iam ch \
  serviceAccount:neo4j-backup@my-project.iam.gserviceaccount.com:objectAdmin \
  gs://my-neo4j-backups
```

Replace `my-namespace` with the Kubernetes namespace where your `Neo4jBackup` resource lives.

---

### Azure Blob Storage Authentication

#### Path 1: Explicit Credentials (Storage Account Key)

```bash
kubectl create secret generic azure-backup-creds \
  --from-literal=AZURE_STORAGE_ACCOUNT=mystorageaccount \
  --from-literal=AZURE_STORAGE_KEY=<your-storage-account-key>
```

Reference the Secret in your `Neo4jBackup`:

```yaml
storage:
  type: azure
  bucket: neo4j-backups   # This is the Azure container name
  path: cluster-backups
  cloud:
    provider: azure
    credentialsSecretRef: azure-backup-creds
```

#### Path 2: Azure Workload Identity

Annotate the `neo4j-backup-sa` ServiceAccount with your Azure client ID:

```yaml
storage:
  type: azure
  bucket: neo4j-backups
  path: cluster-backups
  cloud:
    provider: azure
    identity:
      autoCreate:
        annotations:
          azure.workload.identity/client-id: <AZURE_CLIENT_ID>
```

The Azure AD application / managed identity identified by `AZURE_CLIENT_ID` must have the `Storage Blob Data Contributor` role on the storage container (or account). Your AKS cluster must have the Azure Workload Identity webhook installed and the federated credential configured for the `neo4j-backup-sa` ServiceAccount.

---

## Backup Operations

### One-Time Backup Examples

#### Backup to PVC (Local Storage)

The simplest backup option — no cloud credentials needed:

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
      storageClassName: standard
  options:
    compress: true
    verify: true
  retention:
    maxCount: 5
```

**Best for:** Development, testing, getting started, air-gapped environments.

#### Cluster Backup to S3 with Explicit Credentials

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
      credentialsSecretRef: aws-backup-creds
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
  retention:
    maxAge: "30d"
    maxCount: 10
```

**Best for:** Production AWS environments with static credentials. Swap `credentialsSecretRef` for `identity.autoCreate.annotations` to use IRSA instead.

#### Cluster Backup to S3 with IRSA

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: cluster-backup-s3-irsa
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
      identity:
        autoCreate:
          annotations:
            eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
  retention:
    maxAge: "30d"
    maxCount: 10
```

**Best for:** Production EKS environments — no long-lived credentials stored in Secrets.

#### Cluster Backup to GCS with Explicit Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: cluster-backup-gcs
spec:
  target:
    kind: Cluster
    name: my-neo4j-cluster
  storage:
    type: gcs
    bucket: my-gcs-backup-bucket
    path: neo4j-backups/cluster
    cloud:
      provider: gcp
      credentialsSecretRef: gcs-backup-creds
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
  retention:
    maxAge: "30d"
    maxCount: 10
```

#### Cluster Backup to Azure with Explicit Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: cluster-backup-azure
spec:
  target:
    kind: Cluster
    name: production-cluster
  storage:
    type: azure
    bucket: neo4j-backups   # Azure container name
    path: cluster/production
    cloud:
      provider: azure
      credentialsSecretRef: azure-backup-creds
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
```

### Database Backup Examples

To back up a specific database rather than the whole cluster, use `kind: Database`. **`clusterRef` is required** when targeting a specific database — it identifies which cluster or standalone instance owns the database.

#### Database Backup to GCS

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: database-backup-gcs
spec:
  target:
    kind: Database
    name: myapp-db          # The database name
    clusterRef: my-cluster  # Required: the cluster that owns this database
  storage:
    type: gcs
    bucket: my-gcs-backup-bucket
    path: neo4j-backups/myapp
    cloud:
      provider: gcp
      credentialsSecretRef: gcs-backup-creds
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
    encryption:
      enabled: true
      keySecret: backup-encryption-key
      algorithm: AES256
```

#### Database Backup to S3

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: database-backup-s3
spec:
  target:
    kind: Database
    name: production-db
    clusterRef: production-cluster
  storage:
    type: s3
    bucket: my-backup-bucket
    path: databases/production-db
    cloud:
      provider: aws
      credentialsSecretRef: aws-backup-creds
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
```

### Differential Backups

Differential backups capture only the pages changed since the last full backup, making them faster and smaller:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: differential-backup
spec:
  target:
    kind: Cluster
    name: my-neo4j-cluster
  storage:
    type: s3
    bucket: my-backup-bucket
    path: neo4j-backups/differential
    cloud:
      provider: aws
      credentialsSecretRef: aws-backup-creds
  options:
    backupType: DIFF
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
```

#### `preferDiffAsParent` (CalVer 2025.04+ only)

By default, differential backups use the most recent **full** backup as their parent. On CalVer 2025.04 and later, you can instruct the operator to use the most recent **differential** backup as the parent instead, creating a chain of incrementally smaller backups:

```yaml
options:
  backupType: DIFF
  preferDiffAsParent: true  # CalVer 2025.04+ only
  tempPath: /tmp/neo4j-backup-temp
```

This option is ignored on Neo4j 5.26.x (semver) and CalVer versions before 2025.04.

### The `tempPath` Option

For cloud storage destinations, `neo4j-admin` may use local disk space during streaming. Set `tempPath` to a dedicated temporary directory to avoid filling the pod's working filesystem:

```yaml
options:
  tempPath: /tmp/neo4j-backup-temp
```

**Strongly recommended for all cloud storage backups.** The directory is created automatically if it does not exist.

---

### Scheduled Backup Examples

#### Daily Scheduled Backup to S3

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
      credentialsSecretRef: aws-backup-creds
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
  retention:
    maxAge: "7d"
    maxCount: 7
    deletePolicy: Delete
```

**Schedule:** Daily at 2 AM UTC — adjust as needed. Retention keeps 7 days' worth of backups.

> **Cloud storage retention note**: The `retention` settings above control how many backup records the operator tracks and attempts to clean up in PVC storage. For cloud storage (S3, GCS, Azure), **retention cleanup is handled by your bucket's lifecycle rules**, not the operator. The operator logs a notice when PVC retention cleanup is skipped for cloud-backed backups. Configure lifecycle rules directly in your cloud provider:
>
> - **S3**: [S3 Lifecycle Rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lifecycle-mgmt.html)
> - **GCS**: [Object Lifecycle Management](https://cloud.google.com/storage/docs/lifecycle)
> - **Azure**: [Blob Lifecycle Management](https://learn.microsoft.com/en-us/azure/storage/blobs/lifecycle-management-overview)

#### Weekly Backup with Long Retention

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
      identity:
        autoCreate:
          annotations:
            iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com
  options:
    compress: true
    verify: true
    tempPath: /tmp/neo4j-backup-temp
    encryption:
      enabled: true
      keySecret: backup-encryption-key
  retention:
    maxAge: "90d"
    maxCount: 12
    deletePolicy: Archive
```

**Best for:** Enterprise compliance, long-term archival. Configure GCS Lifecycle Management to expire objects after 90 days.

### Suspended Backups

Temporarily pause a scheduled backup without deleting the resource:

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
    cloud:
      provider: aws
      credentialsSecretRef: aws-backup-creds
```

---

## Restore Operations

### How Restore Works

When you create a `Neo4jRestore` resource:

1. The operator creates a Kubernetes Job that runs `neo4j-admin database restore` using the same Neo4j Enterprise image.
2. The Job restores the database files from the specified source location.
3. After the restore Job completes successfully, the operator **automatically creates or starts the database** via Bolt:
   - If the database does not exist: runs `CREATE DATABASE <dbname>` automatically.
   - If the database exists but is stopped: runs `START DATABASE <dbname>` automatically.
4. **No manual post-restore Cypher is required** to bring the database online.

### Simple Restore Examples

#### Restore from a Backup Reference

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

After the Job completes, the operator automatically runs `START DATABASE neo4j` (or `CREATE DATABASE neo4j` if it was a new database). **You do not need to run any Cypher manually.**

**Best for:** Quick recovery from an existing `Neo4jBackup` resource.

#### Restore from a Storage Location

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-from-s3
spec:
  targetCluster: recovery-cluster
  databaseName: myapp-db
  source:
    type: storage
    storage:
      type: s3
      bucket: backup-bucket
      path: neo4j-backups/cluster/backup-20250104-120000
      cloud:
        provider: aws
        credentialsSecretRef: aws-backup-creds
    backupPath: /backup/cluster/backup-20250104-120000
  options:
    verifyBackup: true
    replaceExisting: true
  force: true
  stopCluster: true
```

**Best for:** Cross-cluster recovery, disaster recovery from a known backup path.

#### Restore to a Standalone Instance

`targetCluster` can reference a `Neo4jEnterpriseStandalone` as well as a `Neo4jEnterpriseCluster`:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-to-standalone
spec:
  targetCluster: my-standalone   # References Neo4jEnterpriseStandalone
  databaseName: neo4j
  source:
    type: backup
    backupRef: standalone-backup
  options:
    verifyBackup: true
    replaceExisting: true
  stopCluster: true
```

---

### Point-in-Time Recovery (PITR)

PITR restores your database to a specific point in time using a base backup combined with transaction logs.

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
          credentialsSecretRef: aws-backup-creds
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

**Best for:** Compliance requirements, precise recovery to a moment before a bad event.

#### PITR with Storage-Based Base Backup

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
          cloud:
            provider: gcp
            credentialsSecretRef: gcs-backup-creds
        backupPath: /backup/base-backup-20250104
      logStorage:
        type: gcs
        bucket: transaction-logs
        path: production/logs
        cloud:
          provider: gcp
          credentialsSecretRef: gcs-backup-creds
      validateLogIntegrity: true
  options:
    verifyBackup: true
  force: true
  stopCluster: true
```

---

### Restore with Hooks

Pre and post-restore hooks execute custom operations at key points in the restore lifecycle.

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
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes()"
        - "CALL dbms.security.clearAuthCache()"
  force: false
  stopCluster: true
```

Note: The operator still automatically runs `CREATE DATABASE` or `START DATABASE` after the restore Job and post-restore hooks complete.

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
            args: ["-c", "/scripts/pre-restore.sh"]
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

---

## Decision Guide: Choose Your Backup Strategy

### Quick Decision Tree

```
Are you just getting started?
├── YES → PVC backup (beginner)
└── NO ↓

Do you need production-grade cloud durability?
├── YES → Cloud storage (S3 / GCS / Azure)
└── NO → PVC backup is sufficient

Are you on a managed Kubernetes service (EKS, GKE, AKS)?
├── YES → Use Workload Identity (no long-lived credentials)
└── NO → Use explicit credentialsSecretRef

Do you need compliance / precise recovery points?
├── YES → Point-in-Time Recovery (PITR)
└── NO → Regular backup / restore is sufficient

Do you need smaller, faster incremental backups?
├── YES → Differential backups (DIFF)
└── NO → Full backups suffice
```

### Storage Backend Comparison

| Factor | PVC | S3 | GCS | Azure |
|--------|-----|----|----|-------|
| **Setup Complexity** | Simple | Medium | Medium | Medium |
| **Cost** | Low | Medium | Medium | Medium |
| **Durability** | Cluster-dependent | 99.999999999% | 99.999999999% | 99.999999999% |
| **Multi-region** | No | Yes | Yes | Yes |
| **Encryption at rest** | Optional | Built-in | Built-in | Built-in |
| **Best For** | Dev/Test | AWS prod | GCP prod | Azure prod |

### Backup Frequency Recommendations

| Environment | Frequency | Retention | Storage |
|-------------|-----------|-----------|---------|
| **Development** | Manual | 3–5 backups | PVC |
| **Staging** | Daily | 7 days | Cloud |
| **Production** | Daily + Weekly | 30d + 90d | Cloud |
| **Critical Systems** | Daily + PITR | 90d + compliance | Multi-region cloud |

---

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

---

## Best Practices

### Backup Best Practices

1. **Regular Testing**: Regularly run restore procedures in a test environment to verify your backups are usable.
2. **Multiple Retention Tiers**: Use different retention policies for daily vs. weekly backups.
3. **Encryption**: Enable encryption for sensitive data, especially in multi-tenant environments.
4. **Verification**: Always enable `verify: true` to catch corrupted backups early.
5. **Cross-Region**: Store backups in a different region from your cluster for disaster recovery.
6. **tempPath for cloud**: Always set `tempPath` for cloud-destination backups to avoid filling the pod filesystem.
7. **Lifecycle Rules**: Configure bucket lifecycle rules for cloud storage — the operator does not manage cloud object expiry directly.
8. **Monitoring**: Set up alerting on `Neo4jBackup` status conditions.

### Restore Best Practices

1. **Stop the cluster**: Use `stopCluster: true` to ensure consistency during restore.
2. **Verify before restoring**: Enable `verifyBackup: true`.
3. **Test in non-production first**: Always validate the restore procedure on a non-production cluster before relying on it in an incident.
4. **Document procedures**: Keep a written runbook for common recovery scenarios.
5. **Automatic database creation**: The operator handles `CREATE DATABASE` and `START DATABASE` after restore — no manual Cypher needed.

### Security Best Practices

1. **Prefer Workload Identity**: Use IAM roles / Workload Identity in managed Kubernetes environments instead of long-lived static credentials.
2. **Rotate credentials**: Rotate `credentialsSecretRef` Secrets regularly if you use explicit credentials.
3. **Least-privilege IAM**: Grant only the bucket-level permissions actually needed (`PutObject`, `GetObject`, `ListBucket`, `DeleteObject`).
4. **Network Policies**: Restrict egress from backup Job pods to the backup port (6362) and your cloud storage endpoints.
5. **Encrypt backups**: Use backup encryption for sensitive databases, especially in regulated industries.

---

## Advanced Configuration

### Cloud Storage Authentication (Full Reference)

The `cloud` block inside `storage` supports the following fields:

```yaml
storage:
  type: s3 | gcs | azure
  bucket: <bucket-or-container-name>
  path: <prefix-within-bucket>
  cloud:
    provider: aws | gcp | azure

    # Path 1: Explicit credentials
    credentialsSecretRef: <secret-name>
    # Secret keys by provider:
    #   AWS: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION
    #   GCP: GOOGLE_APPLICATION_CREDENTIALS_JSON (must be this exact key)
    #   Azure: AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY

    # Path 2: Workload Identity annotations on neo4j-backup-sa
    identity:
      autoCreate:
        annotations:
          <annotation-key>: <annotation-value>
        # Examples:
        #   AWS IRSA:  eks.amazonaws.com/role-arn: arn:aws:iam::...
        #   GKE WI:    iam.gke.io/gcp-service-account: sa@project.iam.gserviceaccount.com
        #   Azure WI:  azure.workload.identity/client-id: <client-id>
```

Only one of `credentialsSecretRef` or `identity` should be specified at a time.

### Backup Options Reference

```yaml
options:
  # Backup type: FULL (default) or DIFF
  backupType: FULL

  # For DIFF backups on CalVer 2025.04+: use latest diff instead of latest full as parent
  preferDiffAsParent: false

  # Compress backup data (recommended)
  compress: true

  # Verify backup integrity after creation
  verify: true

  # Temporary directory for cloud streaming (strongly recommended for cloud storage)
  tempPath: /tmp/neo4j-backup-temp

  # Encryption at rest
  encryption:
    enabled: true
    keySecret: backup-encryption-key
    algorithm: AES256

  # Pass additional flags directly to neo4j-admin database backup
  additionalArgs:
    - "--verbose"
```

### Custom Backup Arguments

Pass flags directly to `neo4j-admin database backup`:

```yaml
spec:
  options:
    additionalArgs:
      - "--verbose"
      - "--parallel-recovery"
```

### Cross-Namespace Operations

```yaml
# Backup a cluster in a different namespace
spec:
  target:
    kind: Cluster
    name: production-cluster
    namespace: production
```

---

## Troubleshooting Quick Reference

### Quick Fixes

| Problem | Quick Check | Solution |
|---------|-------------|----------|
| **Backup Failed** | `kubectl describe neo4jbackup <name>` | Check events and conditions |
| **Permission Denied on cloud storage** | `kubectl logs job/<backup-name>-backup` | Verify `credentialsSecretRef` or Workload Identity setup |
| **Version Error** | Check cluster Neo4j version | Ensure 5.26.0+ or 2025.01.0+ |
| **Pod filesystem full** | Check `df -h` in backup pod | Set `tempPath` to a larger volume or use a PVC |
| **Backup job fails with `path does not exist`** | Check `tempPath` | Set a valid `tempPath` or ensure the path is auto-created |
| **`preferDiffAsParent` has no effect** | Check Neo4j version | Requires CalVer 2025.04+ |
| **Database not online after restore** | Check restore status | Should be automatic — check operator logs for Bolt errors |

### Detailed Troubleshooting

For comprehensive troubleshooting, diagnostics, and advanced problem-solving, see the [Complete Troubleshooting Guide](../troubleshooting/backup_restore.md).

---

## Additional Resources

### API Documentation

- **[Neo4jBackup API Reference](../../api_reference/neo4jbackup.md)** — Complete field specifications and options
- **[Neo4jRestore API Reference](../../api_reference/neo4jrestore.md)** — Detailed restore configuration reference

### Examples and Templates

- **[Working Examples](../../../examples/backup-restore/)** — Copy-paste ready YAML files
- **[Getting Started Guide](../getting_started.md)** — Deploy your first cluster
- **[Installation Guide](../installation.md)** — Install the operator

### Advanced Topics

- **[Troubleshooting Guide](../troubleshooting/backup_restore.md)** — Comprehensive problem-solving
- **[Security Best Practices](../security.md)** — Secure your backup operations
- **[Performance Tuning](../performance.md)** — Optimize backup/restore performance

### Community and Support

- **[GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues)** — Report bugs and request features
- **[Neo4j Community](https://community.neo4j.com/)** — Get help from the community
- **[Neo4j Documentation](https://neo4j.com/docs/)** — Official Neo4j documentation
