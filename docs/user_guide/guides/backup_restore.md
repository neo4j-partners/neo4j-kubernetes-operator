# Backup and Restore

Back up and restore Neo4j Enterprise clusters and standalone instances via the `Neo4jBackup` and `Neo4jRestore` CRDs. Both deployment kinds are auto-detected by the operator from `target.name` (backup) or `clusterRef` (restore).

## Quick Start

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

kubectl apply -f examples/backup-restore/backup-pvc-simple.yaml
kubectl get neo4jbackups simple-backup -w
```

Watch for `status.phase: Completed`. Logs at `kubectl logs job/simple-backup-backup`.

## Prerequisites

- Neo4j Enterprise **5.26.0+** (semver) or **2025.01.0+** (CalVer), enterprise image tag (`neo4j:X-enterprise`)
- Admin credentials Secret
- Storage backend: PVC, S3, GCS, or Azure

## Migrating from legacy `spec.backups`

The legacy `spec.backups` field (and its centralized `{cluster}-backup-0` StatefulSet), and the standalone backup sidecar, have been **removed** — `spec.backups` no longer exists in the CRD schema. Use one or more `Neo4jBackup` CRs instead. The Neo4jBackup CRD covers every legacy capability plus more — one-shot or scheduled (`spec.schedule`) backups, native CronJob retention/suspend, `status.history`, sharded-DB targets, mixed-cadence FULL+DIFF chains via `spec.chainFromBackup`, and per-Job pod resource control (`spec.options.resources`). See the [Migration Guide](../migration_guide.md#6-specbackups-and-the-backup-sidecar-are-removed) for upgrade steps.

## Backup Architecture

The operator spawns a Kubernetes Job that runs `neo4j-admin database backup` from the same Neo4j Enterprise image as the cluster (no sidecar containers). The Job connects to each `{cluster}-server-N` Pod on port 6362 (`server.backup.listen_address=0.0.0.0:6362`, configured automatically). For cloud destinations, `neo4j-admin` streams directly to the bucket — set `tempStorage` for large databases that need a PVC for staging.

Backup Jobs run as the auto-created `neo4j-backup-sa` ServiceAccount in the same namespace. For Workload Identity (IRSA / GKE WI / Azure WI), annotate the SA via `cloud.identity.autoCreate.annotations` — see [Cloud Storage Authentication](#cloud-storage-authentication).

### Backup targets (`target.kind`)

| `target.kind` | `target.name` | `target.clusterRef` | Backs up |
|---|---|---|---|
| `Cluster` | the **instance** name — a `Neo4jEnterpriseCluster` **or** `Neo4jEnterpriseStandalone` (auto-detected) | *(unused — leave unset)* | every database on the instance |
| `Database` | the **database** name (e.g. `neo4j`) | the owning instance name | a single database |
| `ShardedDatabase` | the logical sharded-DB name (e.g. `products`) | the owning cluster | all shards in one `neo4j-admin` run |

There is **no `kind: Standalone`** — a standalone is just an instance. To back one up, use `kind: Cluster` with `name: <standalone-name>` (whole instance), or `kind: Database` with `name: <database>` + `clusterRef: <standalone-name>` (one database). Key gotcha: for `kind: Cluster`, `name` is the **instance** name and `clusterRef` is unused; for `kind: Database`/`ShardedDatabase`, `name` is the **database** and `clusterRef` is the instance that owns it.

### Storage Layout

All runs of a single `Neo4jBackup` CR write to the **same directory**: `<storage.path>/<cr-name>/`. This is what `neo4j-admin` requires for differential backup chaining — every diff run reads the prior full from the same directory to compute the delta. Per-run identity is preserved by the timestamp `neo4j-admin` embeds in each artifact filename (`<dbname>-YYYY-MM-DDThh-mm-ss.backup`).

Each scheduled run appends to `status.history[]` with a unique `runID` (Job UID) and the captured artifact filename. To list runs:

```bash
kubectl get neo4jbackup daily-backup -o jsonpath='{.status.history[*].runID}'
```

Two different `Neo4jBackup` CRs pointing at the same bucket+path stay isolated by the per-CR segment. To chain runs for diff support, you must reuse the same CR (typically via `spec.schedule`).

### Backup Types

| Type | Description | `backupType` value |
|------|-------------|-------------------|
| **Auto** (default) | FULL on the first run, DIFF on subsequent runs | `AUTO` |
| **Full** | Complete snapshot of all database files | `FULL` |
| **Differential** | Only pages changed since the last full backup | `DIFF` |

`backupType` defaults to `AUTO` when omitted. Differential backups are significantly smaller and faster for large databases. Because all runs share a directory, `neo4j-admin` auto-detects the chain — set `backupType: DIFF` and the operator points `--to-path` at the directory containing the prior full. Set `preferDiffAsParent: true` (CalVer 2025.04+) to chain diffs off the latest diff instead of the latest full.

### Storage Backends

| Backend | Type | Best For |
|---------|------|----------|
| **PVC** | `pvc` | Development, testing, air-gapped environments |
| **AWS S3** | `s3` | Production on AWS |
| **MinIO / S3-compatible** | `s3` + `endpointURL` | On-premises, air-gapped, or self-hosted object storage |
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
  --from-literal=AWS_REGION=us-east-1
```

> The region lives in this Secret as `AWS_REGION` — there is no `spec.cloud.region` field. For `credentialsSecretRef` backups all three keys (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`) are required; a missing key fails the backup Job pod. With IRSA/Workload Identity (no Secret) the SDK resolves the region ambiently.

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
      provider: aws
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

### MinIO and S3-Compatible Storage

MinIO is a high-performance, S3-compatible object store popular for on-premises and air-gapped Kubernetes environments. The operator supports MinIO (and other S3-compatible stores such as Ceph RGW and Cloudflare R2) using two additional fields on `CloudBlock`:

| Field | Purpose |
|-------|---------|
| `endpointURL` | Custom S3 API endpoint (injected as `AWS_ENDPOINT_URL_S3`) |
| `forcePathStyle` | Path-style addressing required by MinIO (injects `-Daws.s3.forcePathStyle=true` into the JVM) |

#### Step 1: Create the credentials Secret

MinIO uses the same secret key names as AWS. The `AWS_REGION` value is required by the SDK but ignored by MinIO — any value works.

```bash
kubectl create secret generic minio-backup-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=minioadmin \
  --from-literal=AWS_SECRET_ACCESS_KEY=minioadmin \
  --from-literal=AWS_REGION=us-east-1
```

#### Step 2: Create the backup resource

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: minio-backup
spec:
  target:
    kind: Cluster
    name: my-cluster
  storage:
    type: s3
    bucket: neo4j-backups        # bucket must already exist in MinIO
    path: cluster/full
    cloud:
      provider: aws
      credentialsSecretRef: minio-backup-credentials
      endpointURL: http://minio.minio.svc:9000  # adjust namespace/port
      forcePathStyle: true                       # required for MinIO
  options:
    backupType: FULL
    compress: true
    tempStorage:
      size: "20Gi"
```

> **External MinIO over TLS**: Change `endpointURL` to `https://minio.example.com`. Ensure your MinIO TLS certificate is trusted by the container (or use a properly signed cert). Self-signed certs require mounting the CA into the pod — use `additionalArgs` to pass `--ssl-certificate-authorities` if needed.

#### Verify the backup reached MinIO

```bash
kubectl run minio-client --rm -it --restart=Never \
  --image=minio/mc -- /bin/sh -c "
    mc alias set local http://minio.minio.svc:9000 minioadmin minioadmin
    mc ls local/neo4j-backups/cluster/"
```

#### Troubleshooting MinIO

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `NoSuchBucket` | Bucket not created | `mc mb local/neo4j-backups` |
| `connection refused` | Wrong endpoint URL | Verify `endpointURL` and MinIO pod readiness |
| `SignatureDoesNotMatch` | Wrong credentials | Check secret key values |
| Path-style not working | `forcePathStyle` missing | Confirm `forcePathStyle: true` in spec |
| `SSL handshake failed` | TLS mismatch | Use `http://` for in-cluster; mount CA for self-signed certs |

> **Restoring from MinIO / S3-compatible storage uses the same two fields.** Set `endpointURL` and `forcePathStyle` on the `Neo4jRestore` `spec.source.storage.cloud` block (and they apply equally to `Neo4jDatabase` `seedURI` seeding) — the operator injects `AWS_ENDPOINT_URL_S3` and the path-style JVM flag into the restore Job and the seeding pods exactly as it does for backup.

Full examples with scheduled incremental backups: [`examples/backup-restore/backup-minio.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-minio.yaml).

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
      provider: gcp
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
      provider: azure
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
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
  retention:
    maxCount: 5
```

**Best for:** Development, testing, getting started, air-gapped environments.

#### PVC ownership: auto-provision vs bring-your-own

`storage.pvc.size` is the switch:

| Pattern | YAML | Lifecycle |
|---|---|---|
| **Operator auto-provisions** | Set `name` + `size` (+ optional `storageClassName`) | PVC is owner-ref'd to the Neo4jBackup CR. Deleted when the CR is deleted (backups go with it). |
| **Bring your own PVC** | Set `name` only; omit `size` | Operator just mounts the existing PVC. Survives CR deletion. Pre-create it however you like (kubectl, Helm, Velero, static binding, NFS, etc.). |

Use bring-your-own when you want backups to survive `kubectl delete neo4jbackup`, or to share one PVC across multiple Neo4jBackup CRs (e.g. one daily + one weekly schedule writing to the same volume).

#### Cluster Backup to S3 with Explicit Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
  retention:
    maxAge: "30d"
    maxCount: 10
```

**Best for:** Production AWS environments with static credentials. Swap `credentialsSecretRef` for `identity.autoCreate.annotations` to use IRSA instead.

#### Cluster Backup to S3 with IRSA

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
        provider: aws
        autoCreate:
          annotations:
            eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role
  options:
    compress: true
    validate: true
    tempStorage:
      size: "50Gi"
  retention:
    maxAge: "30d"
    maxCount: 10
```

**Best for:** Production EKS environments — no long-lived credentials stored in Secrets.

#### Cluster Backup to GCS with Explicit Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
  retention:
    maxAge: "30d"
    maxCount: 10
```

#### Cluster Backup to Azure with Explicit Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
```

### Database Backup Examples

To back up a specific database rather than the whole cluster, use `kind: Database`. **`clusterRef` is required** when targeting a specific database — it identifies which cluster or standalone instance owns the database.

#### Database Backup to GCS

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
```

#### Database Backup to S3

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
```

### Differential Backups

Differential backups capture only the pages changed since the last full backup, making them faster and smaller:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
```

#### Mixed-cadence backups: daily FULL + hourly DIFF

`spec.chainFromBackup` composes two CRs into one backup chain — typically a daily FULL CR plus an hourly (or per-minute) DIFF CR that chains off it. Both CRs write to the **same directory** (`<base>/<daily-cr-name>/`); `neo4j-admin --type=DIFF` discovers the prior FULL and chains the diff off it.

```yaml
# Daily FULL (Sundays at 02:00 — or any cadence)
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata: { name: inventory-daily }
spec:
  target:    { kind: Database, name: inventory, clusterRef: my-cluster }
  schedule:  "0 2 * * *"
  storage:   { type: s3, bucket: backups, path: prod, cloud: {…} }
  options:   { backupType: FULL }
  retention: { maxCount: 30 }
---
# Hourly DIFF — chains into the daily's directory
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata: { name: inventory-hourly }
spec:
  target:          { kind: Database, name: inventory, clusterRef: my-cluster }
  schedule:        "30 * * * *"      # offset 30 min to avoid racing the daily
  chainFromBackup: inventory-daily   # ← shared directory
  storage:         { type: s3, bucket: backups, path: prod, cloud: {…} }
  options:         { backupType: DIFF }
  retention:       { maxCount: 168 }
```

**Constraints** (validator-enforced; mismatches → `status.phase=Failed`):

- The parent CR (`inventory-daily`) must exist in the same namespace.
- Both CRs must have the same `target` (kind + name + clusterRef).
- Both CRs must use the same storage backend (type + bucket + path).
- `chainFromBackup` cannot point to self.

**Concurrent runs across chained CRs are blocked automatically.** Each Job carries an `app.kubernetes.io/part-of: <chain-root>` label; the operator refuses to start a new backup Job while any other Job in the same chain is still active (`status.active>0`) — routes the new run to `Pending` and requeues. This prevents the hourly DIFF from firing while the daily FULL is still writing, which would corrupt the chain. Offsetting schedules (different minute on the hour) avoids the wait in practice.

**Restore** seeds from the **latest successful artifact of the CR you reference** — *not* the latest file in the shared directory. This selects your recovery point:

- `Neo4jRestore.spec.source.backupRef: inventory-hourly` → the newest **differential**. Neo4j applies the full + differential chain backward to it → you get the **latest state**.
- `backupRef: inventory-daily` → the newest **full** → you roll back to the **last full snapshot**; the hourly diffs are **not** applied.

So reference the CR whose latest backup matches the recovery point you want — the DIFF CR for "latest", the FULL CR for "last full snapshot". Restoring via the parent FULL CR emits a `RestoreFromChainParent` Warning event naming the DIFF children, so a restore that intends "latest" but references the FULL CR isn't a silent surprise.

#### `preferDiffAsParent` (CalVer 2025.04+ only)

By default, differential backups use the most recent **full** backup as their parent. On CalVer 2025.04 and later, you can instruct the operator to use the most recent **differential** backup as the parent instead, creating a chain of incrementally smaller backups:

```yaml
options:
  backupType: DIFF
  preferDiffAsParent: true  # CalVer 2025.04+ only
  tempStorage:
    size: "50Gi"
```

This option is ignored on Neo4j 5.26.x (semver) and CalVer versions before 2025.04.

### The `tempPath` Option

For cloud storage destinations, `neo4j-admin` may use local disk space during streaming. Set `tempPath` to a dedicated temporary directory to avoid filling the pod's working filesystem:

```yaml
options:
  tempStorage:
    size: "50Gi"
```

**Strongly recommended for all cloud storage backups.** The directory is created automatically if it does not exist.

---

### Scheduled Backup Examples

#### Daily Scheduled Backup to S3

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true
    tempStorage:
      size: "50Gi"
  retention:
    maxAge: "168h"
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
apiVersion: neo4j.neo4j.com/v1beta1
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
        provider: gcp
        autoCreate:
          annotations:
            iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com
  options:
    compress: true
    validate: true
    tempStorage:
      size: "50Gi"
  retention:
    maxAge: "90d"
    maxCount: 12
    deletePolicy: Archive
```

**Best for:** Enterprise compliance, long-term archival. Configure GCS Lifecycle Management to expire objects after 90 days.

### Suspended Backups

Temporarily pause a scheduled backup without deleting the resource:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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

The operator picks the right restore method based on the target kind. The Neo4j docs flag `neo4j-admin database restore` as **unsafe on clusters** ("not safe on a cluster since clusters have additional state that would be inconsistent with the restored database"), so the operator uses different paths:

| Target | Restore method | Backed by |
|---|---|---|
| `Neo4jEnterpriseCluster` (standard DB) | Cypher over Bolt — no Job | `dbms.recreateDatabase(name, {seedURI})` if the DB exists, otherwise `CREATE DATABASE name OPTIONS { seedURI } WAIT` |
| `Neo4jEnterpriseStandalone` | Kubernetes Job | `neo4j-admin database restore --from-path=<latest-file-in-chain>` followed by `CREATE/START DATABASE` |
| `Neo4jShardedDatabase` (sharded) | Rejected with actionable error | Use `Neo4jShardedDatabase.spec.replaceExisting: true` + `force: true` instead — see [Property Sharding](../property_sharding.md) |

**Cluster path (Cypher)** — works with both cloud and PVC backups:

- **Cloud-backed backup** (S3 / GCS / Azure): the operator passes the exact `.backup` **file** URI of the latest successful run (`s3://bucket/<path>/<backup-cr-name>/<dbname>-<timestamp>.backup`) as `seedURI` — `CloudSeedProvider` seeds a single database from one file, not a directory. When that file is a differential, Neo4j resolves and applies the full + differential chain from the same directory automatically. The cluster's pods must have the cloud credentials Secret projected via `spec.extraEnvFrom` — the operator emits an actionable error if they don't, or auto-patches under annotation `neo4j.com/auto-inherit-seed-creds=true`.
- **PVC-backed backup**: the operator spawns an in-cluster busybox httpd proxy (`backup-seed-proxy-<restore-name>`) mounting the backup PVC RO at `/backup`, then passes the per-run `.backup` file URL as `seedURI` (`http://backup-seed-proxy-<restore-name>:8080/<backup-cr-name>/<filename>`). Neo4j's `URLConnectionSeedProvider` fetches it. The proxy Deployment + Service are owned by the `Neo4jRestore` CR and GC'd when it's deleted. No credentials required.
- `dbms.recreateDatabase` preserves user/role privileges on the existing database; no `DROP DATABASE` needed.
- For new databases, the operator emits `CREATE DATABASE … OPTIONS { seedURI: '…' } WAIT`.
- Both forms block until the new state is online — when the restore returns, the database is ready.

**Standalone path (Job)**:

- The operator spawns a Kubernetes Job that mounts the backup PVC (or streams from cloud storage), runs `neo4j-admin database restore --from-path=<latest-file>`, then automatically runs `CREATE DATABASE` or `START DATABASE` over Bolt.
- The `<latest-file>` is resolved at Pod startup via shell substitution (`ls <dir>/<dbname>-*.backup | tail -1`), so the most recent run in the chain wins by default.

**No manual post-restore Cypher is required** for either path.

### Simple Restore Examples

#### Restore from a Backup Reference

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-from-backup
spec:
  clusterRef: my-neo4j-cluster
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
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-from-s3
spec:
  clusterRef: recovery-cluster
  databaseName: myapp-db
  source:
    type: storage
    storage:
      type: s3
      bucket: backup-bucket
      path: neo4j-backups/cluster
      cloud:
        provider: aws
        credentialsSecretRef: aws-backup-creds
    # For a CLUSTER target, backupPath must be the exact .backup FILE
    # (CloudSeedProvider seeds a single DB from one file, not a directory).
    # The chain root + filename are recorded on
    # Neo4jBackup.status.history[*].{backupsPath,artifactFilename}; if it's a
    # differential, Neo4j applies the full+diff chain from the same directory.
    # Standalone targets may pass just the directory — the Job picks the
    # latest file via `ls … | tail -1`.
    backupPath: daily-backup/myapp-db-2025-06-01T02-00-00.backup
  options:
    verifyBackup: true
    replaceExisting: true
    tempStorage:
      size: "50Gi"
  force: true
  stopCluster: true   # ignored on cluster targets (Cypher path); honored on standalone
```

**Best for:** Cross-cluster recovery, disaster recovery from a known directory in storage (no `Neo4jBackup` CR available in this namespace).

> **Restoring after the `Neo4jBackup` CR is deleted.** A `source.type: backup` restore **pins** the backup's storage location onto its own `status.resolvedSource` the first time it resolves the reference. From that point on the restore reads the snapshot, so deleting the `Neo4jBackup` CR mid-restore (or while it retries) does **not** break it — the operator already knows where the artifacts are.
>
> If you create a **new** restore *after* the Backup CR is gone, there's nothing to resolve, so `source.type: backup` fails with an error pointing you here. Restore directly from the artifacts with **`source.type: storage`** (above): set `source.storage` to the backup's location and point `backupPath` at the exact `.backup` file (cluster) or the directory (standalone). You do **not** need to — and should **not** — re-create the Backup CR: re-creating it triggers a fresh backup Job that writes into the same chain directory, which can shadow the artifact you wanted to restore.
>
> ⚠️ **Restore is destructive and overwrites in place.** With `replaceExisting`/`force` the target database's current data is replaced by the backup. Re-running a restore — including after re-creating a deleted Backup CR — re-seeds and overwrites again. Treat every restore as a destructive operation against the named database.

> **Which run a restore picks**: a cluster restore seeds from the **latest successful artifact of the referenced `Neo4jBackup` CR** (standalone uses `tail -1` of the timestamped glob in that CR's directory). In a FULL+DIFF chain, reference the **DIFF CR** for the latest state or the **FULL CR** to roll back to the last full snapshot — restoring via the FULL CR does *not* apply the newer diffs (and emits a `RestoreFromChainParent` warning). To pin to an arbitrary earlier run, set `source.type: storage` with `backupPath` pointing at the exact `.backup` file, or keep a point-in-time snapshot of the directory (cloud lifecycle rules / versioning).

#### Restore to a Standalone Instance

`clusterRef` can reference a `Neo4jEnterpriseStandalone` as well as a `Neo4jEnterpriseCluster`:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-to-standalone
spec:
  clusterRef: my-standalone   # References Neo4jEnterpriseStandalone
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

> **Note:** `source.type: pitr` (the `--restore-until` path) applies only to a `Neo4jEnterpriseStandalone` target. For cluster point-in-time recovery, create a `Neo4jDatabase` with `spec.seedConfig.restoreUntil`. The operator rejects `source.type: pitr` against a cluster target with an actionable error.

#### PITR Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: pitr-restore
spec:
  clusterRef: recovery-cluster
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
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: pitr-storage-restore
spec:
  clusterRef: disaster-recovery
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
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-with-hooks
spec:
  clusterRef: my-cluster
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
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-with-job-hooks
spec:
  clusterRef: staging-cluster
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

## Operational Notes

- Enable `validate: true` to run `neo4j-admin backup validate` after each backup, so recoverability issues are surfaced at backup time (on `status.history[].validation`) rather than discovered at restore time.
- For cloud destinations, set `tempStorage` (PVC for staging) on large databases — without it `neo4j-admin` buffers in the Pod's filesystem.
- The operator doesn't manage cloud object expiry — configure bucket lifecycle rules to delete old backups.
- Restore is non-trivial post-incident; rehearse it against a staging cluster at least once.
- Prefer Workload Identity (IRSA / GKE WI / Azure WI) over static credentials. Rotate `credentialsSecretRef` Secrets if you do use them.
- IAM permissions needed: `PutObject`, `GetObject`, `ListBucket`, `DeleteObject` on the backup bucket prefix.

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
    #   AWS: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION
    #   GCP: GOOGLE_APPLICATION_CREDENTIALS_JSON (must be this exact key)
    #   Azure: AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY

    # Path 2: Workload Identity annotations on neo4j-backup-sa
    identity:
      provider: aws | gcp | azure   # required; match the storage provider
      autoCreate:
        annotations:
          <annotation-key>: <annotation-value>
        # Examples:
        #   AWS IRSA:  eks.amazonaws.com/role-arn: arn:aws:iam::...
        #   GKE WI:    iam.gke.io/gcp-service-account: sa@project.iam.gserviceaccount.com
        #   Azure WI:  azure.workload.identity/client-id: <client-id>
```

Only one of `credentialsSecretRef` or `identity` should be specified at a time.

### Temporary Storage for Cloud Operations

Cloud backups and restores require local staging space for `neo4j-admin`. Without explicit temp storage, staging uses the container's ephemeral disk, which may be too small for large databases.

The `tempStorage` field tells the operator to create a PVC for staging automatically:

```yaml
spec:
  options:
    tempStorage:
      size: "50Gi"              # should be >= expected backup size
      storageClassName: gp3     # optional, uses cluster default if omitted
```

The operator:

1. Creates a PVC named `{backup-name}-temp-staging` (or `{restore-name}-temp-staging`)
2. Sets the CR as owner — the PVC is garbage-collected when the backup/restore CR is deleted
3. Mounts the PVC at `/tmp/neo4j-staging` in the Job pod
4. Passes `--temp-path=/tmp/neo4j-staging` to `neo4j-admin`

If you prefer to manage the PVC yourself, use `tempPath` instead (points to any path you've mounted via `additionalArgs` or other means).

### Backup Options Reference

```yaml
options:
  # Backup type: AUTO (default — FULL first run, DIFF after), FULL, or DIFF
  backupType: AUTO

  # For DIFF backups on CalVer 2025.04+: use latest diff instead of latest full as parent
  preferDiffAsParent: false

  # Compress backup data (recommended)
  compress: true

  # Validate backup recoverability after creation by running
  # `neo4j-admin backup validate`. Failures are recorded on
  # status.history[].validation but do NOT fail the Job.
  validate: true

  # Operator-managed staging PVC for cloud operations (recommended for large databases)
  tempStorage:
    size: "50Gi"
    storageClassName: gp3  # optional

  # Manual temp path (alternative to tempStorage — you must mount the volume yourself)
  # tempPath: /my/mounted/volume

  # Include users/roles metadata in backup (Neo4j 5.26+)
  # Values: all (default), none, users, roles
  includeMetadata: all

  # Multi-threaded transaction application during backup
  parallelRecovery: false

  # Preserve failed backup artifacts for debugging
  keepFailed: false

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

- **[Working Examples](https://github.com/neo4j-partners/neo4j-kubernetes-operator/tree/main/examples/backup-restore)** — Copy-paste ready YAML files
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
