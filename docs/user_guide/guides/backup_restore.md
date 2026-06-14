# Backup and Restore

Back up and restore Neo4j Enterprise clusters and standalone instances via the `Neo4jBackup` and `Neo4jRestore` CRDs. Both deployment kinds are auto-detected by the operator from `target.name` (backup) or `clusterRef` (restore).

## Quick Start

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

kubectl apply -f https://raw.githubusercontent.com/neo4j-partners/neo4j-kubernetes-operator/main/examples/backup-restore/backup-pvc-simple.yaml
kubectl get neo4jbackups simple-backup -w
```

Watch for `status.phase: Completed`. Logs at `kubectl logs job/simple-backup-backup`.

## Prerequisites

- Neo4j Enterprise **5.26.0+** (semver) or **2025.01.0+** (CalVer), enterprise image tag (`neo4j:X-enterprise`)
- Admin credentials Secret
- Storage backend: PVC, S3, GCS, or Azure

## Tutorial: Your First Backup and Restore (~10 minutes)

A complete, copy-paste walk-through: from a running cluster to a verified
backup and a verified restore, using a local PVC (no cloud account needed).
Follow it top to bottom. Everything else in this guide (cloud storage,
scheduling, differential chains, point-in-time recovery) is optional and comes
after.

### Before you start

You need a `Neo4jEnterpriseCluster` (or `Neo4jEnterpriseStandalone`) already in
phase `Ready` — deploy one from the [Getting Started](../getting_started.md)
guide first. Verify, using the cluster name from that guide:

```bash
kubectl get neo4jenterprisecluster minimal-cluster -o jsonpath='{.status.phase}{"\n"}'
# → Ready
```

### 1. Confirm the admin secret

The backup Job authenticates to Neo4j with the cluster's admin credentials —
the **same `neo4j-admin-secret` your cluster already uses** (you created it
when you deployed the cluster). It must live in the same namespace as the
cluster. Confirm it's there:

```bash
kubectl get secret neo4j-admin-secret
```

If it's missing (e.g. your cluster references a differently-named secret),
create it with your cluster's password:

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password='<your-neo4j-password>'
```

### 2. (Optional) Add data, so you can prove the round-trip later

```bash
kubectl exec minimal-cluster-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p '<your-neo4j-password>' -d neo4j \
  "CREATE (:Product {name: 'Widget'}) RETURN 1"
```

### 3. Take a backup

This backs up the `neo4j` database to a PVC the operator creates for you.
`backupType: AUTO` takes a full backup the first time, differentials after.

```yaml
cat <<'EOF' | kubectl apply -f -
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: first-backup
spec:
  target:
    kind: Database          # one database → one restorable artifact
    name: neo4j
    clusterRef: minimal-cluster
  storage:
    type: pvc
    pvc:
      name: first-backup-pvc
      size: 5Gi
  options:
    backupType: AUTO
EOF
```

### 4. Verify the backup completed

```bash
kubectl get neo4jbackup first-backup -o jsonpath='{.status.phase}{"\n"}'
# → Completed
kubectl get neo4jbackup first-backup -o jsonpath='{.status.history[0].artifactFilename}{"\n"}'
# → neo4j-2026-….backup   (the captured artifact)
```

> A **one-shot** backup reaches `Completed`. If you add a `schedule:` field it
> becomes a recurring CronJob that stays in phase `Scheduled` — check
> `status.history[]` for each run instead.
>
> The PVC (`first-backup-pvc`) and its backups **survive even if you delete the
> `Neo4jBackup` CR** — reclaim the space with `kubectl delete pvc first-backup-pvc`.

### 5. Restore into a new database

Restore into a fresh database `neo4jrestored` on the same cluster. The operator
seeds it from the backup over Cypher — online, no downtime, and your original
`neo4j` database is untouched.

```yaml
cat <<'EOF' | kubectl apply -f -
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: first-restore
spec:
  clusterRef: minimal-cluster
  databaseName: neo4jrestored
  source:
    type: backup
    backupRef: first-backup
EOF
```

### 6. Verify the restore — and your data

```bash
kubectl get neo4jrestore first-restore -o jsonpath='{.status.phase}{"\n"}'
# → Completed
kubectl exec minimal-cluster-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p '<your-neo4j-password>' \
  "SHOW DATABASE neo4jrestored YIELD name, currentStatus"
# → neo4jrestored   online
kubectl exec minimal-cluster-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p '<your-neo4j-password>' -d neo4jrestored \
  "MATCH (p:Product) RETURN p.name"
# → "Widget"   (your step-2 data is back)
```

That's a full backup and restore. From here:

- **Restoring into a cluster from a `kind: Cluster` (all-databases) backup?** You
  can't seed a cluster from it — back up the specific database with
  `kind: Database` (as above), or restore via `source.type: storage`. See
  [Restore Operations](#restore-operations).
- **Standalone targets** restore offline via a Job (set `stopCluster: true` and
  `force: true`). See [Restore Operations](#restore-operations).
- **Cloud storage** (S3/GCS/Azure/MinIO) → [Cloud Storage Authentication](#cloud-storage-authentication).
- **Scheduled & differential backups** → [Backup Operations](#backup-operations).

## Migrating from legacy `spec.backups`

The legacy `spec.backups` field (and its centralized `{cluster}-backup-0` StatefulSet), and the standalone backup sidecar, have been **removed** — `spec.backups` no longer exists in the CRD schema. Use one or more `Neo4jBackup` CRs instead. The Neo4jBackup CRD covers every legacy capability plus more — one-shot or scheduled (`spec.schedule`) backups, native CronJob retention/suspend, `status.history`, sharded-DB targets, mixed-cadence FULL+DIFF chains via `spec.chainFromBackup`, and per-Job pod resource control (`spec.options.resources`). See the [Migration Guide](../migration_guide.md#6-specbackups-and-the-backup-sidecar-are-removed) for upgrade steps.

## Backup Architecture

The operator spawns a Kubernetes Job that runs `neo4j-admin database backup` from the same Neo4j Enterprise image as the cluster (no sidecar containers). The Job connects to each `{cluster}-server-N` Pod on port 6362 (`server.backup.listen_address=0.0.0.0:6362`, configured automatically). For cloud destinations, `neo4j-admin` streams directly to the bucket — set `tempStorage` for large databases that need a PVC for staging.

Backup Jobs run as the auto-created `neo4j-backup-sa` ServiceAccount in the same namespace. For Workload Identity (IRSA / GKE WI / Azure WI), annotate the SA via `cloud.identity.autoCreate.annotations` — see [Cloud Storage Authentication](#cloud-storage-authentication).

### Backup targets (`target.kind`)

| `target.kind` | `target.name` | `target.clusterRef` | Backs up |
|---|---|---|---|
| `Cluster` | the **instance** name — a `Neo4jEnterpriseCluster` **or** `Neo4jEnterpriseStandalone` (auto-detected) | *(unused — leave unset)* | every database on the instance (`neo4j-admin database backup "*"` — one `.backup` artifact per database) |
| `Database` | the **database** name (e.g. `neo4j`) | the owning instance name | a single database |
| `ShardedDatabase` | the **`Neo4jShardedDatabase` CR (metadata) name** — the operator resolves the logical DB name from that CR's `spec.name` | the owning cluster | all shards in one `neo4j-admin` run |

There is **no `kind: Standalone`** — a standalone is just an instance. To back one up, use `kind: Cluster` with `name: <standalone-name>` (whole instance), or `kind: Database` with `name: <database>` + `clusterRef: <standalone-name>` (one database). Key gotcha: for `kind: Cluster`, `name` is the **instance** name and `clusterRef` is unused; for `kind: Database`, `name` is the **database** and `clusterRef` is the instance that owns it; for `kind: ShardedDatabase`, `name` is the **`Neo4jShardedDatabase` CR name** (the operator resolves the logical DB name from its `spec.name` — the two often differ) and `clusterRef` is the owning cluster.

**Restore is single-database** even when the backup was cluster-wide: a `kind: Cluster` backup leaves one artifact per database in the chain directory, and you create one `Neo4jRestore` per database you want back. **How you reference it depends on the target:**

- **Standalone target** (Job path): referencing a `kind: Cluster` backup works directly — the restore Job's filename glob (`<database>-*.backup | tail -1`) selects the requested database's artifact from the shared directory.
- **Cluster target** (Cypher/seedURI path): reference a **`kind: Database`** backup of the database you're restoring. A `kind: Cluster` backup has no single artifact to seed one database from, so the operator rejects it with guidance to use a `kind: Database` backup or `source.type: storage` with `source.backupPath` set to the exact `<database>-<timestamp>.backup` file.

An all-databases restore mode is tracked in [#222](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/222).

### Storage Layout

All runs of a single `Neo4jBackup` CR write to the **same directory**: `<storage.path>/<cr-name>/`. This is what `neo4j-admin` requires for differential backup chaining — every diff run reads the prior full from the same directory to compute the delta. Per-run identity is preserved by the timestamp `neo4j-admin` embeds in each artifact filename (`<dbname>-YYYY-MM-DDThh-mm-ss.backup`).

Each scheduled run appends to `status.history[]` with a unique `runID` (the Job **name**, e.g. `daily-backup-backup-cron-1764806400` — handy for `kubectl logs job/<runID>`) and the captured artifact filename. To list runs:

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

**Recommendation: use pod identity (IRSA / GKE Workload Identity / Azure Workload Identity) wherever your platform supports it.** Leaving `credentialsSecretRef` empty (or omitting the whole `cloud` credentials block) means the cloud SDK's **default credential chain** is used — which picks up the pod identity automatically. No long-lived keys, nothing to rotate. Explicit credentials work everywhere and are simpler to set up initially, but rotate them regularly and store them only in Kubernetes Secrets.

**Which ServiceAccount needs the IAM binding?** It depends on *where* the cloud access happens:

| Operation | Runs on | Bind the IAM role to |
|---|---|---|
| `Neo4jBackup` / standalone `Neo4jRestore` (Job paths) | a backup/restore **Job pod** | the operator-managed `neo4j-backup-sa` / `neo4j-restore-sa` — via `cloud.identity.autoCreate.annotations` on the CR |
| Cluster `Neo4jRestore` (Cypher path), `Neo4jDatabase` `seedURI`, sharded `seedBackupRef` seeding | the Neo4j **server pods** (the JVM fetches the seed itself) | the **cluster's server pods' ServiceAccount** — the Job SA annotation does nothing here |

**Annotating the server pods' ServiceAccount (Workload Identity):** set
`spec.podServiceAccountAnnotations` on the `Neo4jEnterpriseCluster` — the
operator stamps them onto the ServiceAccount the Neo4j pods run under, so the
JVM assumes the role for the seed fetch:

```yaml
spec:
  podServiceAccountAnnotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-seed   # AWS IRSA
    # iam.gke.io/gcp-service-account: neo4j-seed@my-project.iam.gserviceaccount.com   # GKE WI
```

Set it **before** the pods start (or roll the StatefulSet after adding it) so
the identity webhook injects the credentials at pod admission. (Standalone
pods don't yet support this — their seedURI seeding under Workload Identity is
a known gap.)

With **static credentials** the equivalent split applies: Job paths read the Secret referenced by `credentialsSecretRef`; the seedURI paths need the same Secret projected onto the server pods via the cluster/standalone CR's `spec.extraEnvFrom` (the operator checks this and emits a copy-pasteable fix, or auto-patches under the `neo4j.com/auto-inherit-seed-creds: "true"` annotation).

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

> **One workload identity per namespace.** `neo4j-backup-sa` (and
> `neo4j-restore-sa`) are **shared by every backup/restore CR in the
> namespace** — workload-identity trust policies bind to the ServiceAccount
> name. If two CRs declare *different* `autoCreate.annotations`, the last
> one reconciled wins and the other's cloud access breaks; the operator
> flags this with a `ServiceAccountAnnotationConflict` Warning event on the
> overwriting CR. Use one IAM role/identity with access to **all** backup
> locations in the namespace, or split the CRs across namespaces.

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
  --image=minio/mc --command -- /bin/sh -c "
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

> **Restoring from MinIO / S3-compatible storage.** Set `endpointURL` and `forcePathStyle` on the backup's `cloud` block; the operator injects `AWS_ENDPOINT_URL_S3` and the path-style JVM flag into the **backup and standalone-restore Job pods** automatically. The **cluster Cypher restore** path makes the *server* pods fetch the seed, so they need the endpoint too — for a cluster restore via `backupRef`, the operator now **auto-projects** `AWS_ENDPOINT_URL_S3` (and the path-style JVM opt) onto the cluster's `spec.env` when you opt in with the `neo4j.com/auto-inherit-seed-creds: "true"` annotation (the same gate as seed credentials), waiting for the rolling restart before it seeds. Without the annotation it fails with an actionable error telling you to add it or set the annotation. You can always provide it yourself instead — as a `spec.env` entry or a key in the projected credentials Secret:
>
> ```bash
> kubectl create secret generic minio-backup-credentials \
>   --from-literal=AWS_ACCESS_KEY_ID=minioadmin \
>   --from-literal=AWS_SECRET_ACCESS_KEY=minioadmin \
>   --from-literal=AWS_REGION=us-east-1 \
>   --from-literal=AWS_ENDPOINT_URL_S3=http://minio.minio.svc:9000
> ```
>
> (`Neo4jDatabase`/`Neo4jShardedDatabase` `seedURI` seeding from a custom
> endpoint is not auto-projected — provide the endpoint via the cluster's
> `spec.env` or the projected Secret there.)

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

`storage.pvc.size` is the switch for **who creates the PVC** — but **either way the PVC (and your backups) survive `kubectl delete neo4jbackup`**:

| Pattern | YAML | Behavior |
|---|---|---|
| **Operator auto-provisions** | Set `name` + `size` (+ optional `storageClassName`) | Operator creates the PVC if it doesn't exist. It is **not** owner-ref'd to the CR — backups are durable; reclaim the storage with an explicit `kubectl delete pvc <name>`. |
| **Bring your own PVC** | Set `name` only; omit `size` | Operator just mounts the existing PVC. Pre-create it however you like (kubectl, Helm, Velero, static binding, NFS, etc.). If a `name`-only PVC doesn't exist, the backup Job can't schedule — the operator surfaces the reason in `status` and fails after a short deadline rather than hanging. |

> **Backups are never deleted by deleting the `Neo4jBackup` CR** (this changed in v1.12.1 — earlier versions cascade-deleted an operator-created PVC). Reclaim backup storage explicitly with `kubectl delete pvc`. One PVC can be shared across multiple Neo4jBackup CRs (e.g. a daily + a weekly schedule writing to the same volume).

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

**Concurrent runs across chained CRs are blocked automatically.** Each Job carries an `app.kubernetes.io/part-of: <chain-root>` label; the operator refuses to start a new backup Job while any other Job in the same chain is still active (`status.active>0`) — routes the new run to `Pending` and requeues. On **PVC storage** there's a second, run-time guard: each backup Job takes a lock file (`<chain-root>/.chain.lock`) on the shared directory before running `neo4j-admin`, so two Jobs that slip past the creation-time check (e.g. CronJob children firing in the same instant) serialize instead of corrupting the chain — the second waits up to 1 hour, then fails for its normal retry. Offsetting schedules (different minute on the hour) avoids the wait in practice.

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

**Schedule:** Daily at 2 AM UTC — adjust as needed. The schedule must be plain 5-field UTC cron — `TZ=`/`CRON_TZ=` prefixes are rejected (Kubernetes refuses timezone-embedded CronJob schedules). Scheduled backup names are limited to 40 characters.

> **What `retention` actually does** — read this before relying on it:
>
> - **Cloud storage (S3, GCS, Azure)**: the operator never deletes cloud objects. **Retention cleanup is handled by your bucket's lifecycle rules**, not the operator (which logs a notice to that effect). Configure lifecycle rules directly in your cloud provider:
>   - **S3**: [S3 Lifecycle Rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lifecycle-mgmt.html)
>   - **GCS**: [Object Lifecycle Management](https://cloud.google.com/storage/docs/lifecycle)
>   - **Azure**: [Blob Lifecycle Management](https://learn.microsoft.com/en-us/azure/storage/blobs/lifecycle-management-overview)
> - **PVC storage**: retention is enforced by a cleanup Job that runs **only when the Neo4jBackup CR is deleted** — it is *not* a continuous pruning loop. The Job prunes `*.backup` **files** in this CR's chain directory only, oldest-first by mtime, and **always keeps the newest artifact**. `maxAge` accepts a **single** `d`/`h`/`m`/`s` unit (`"30d"`, `"24h"`; compound values like `"1h30m"` and `"4w"` are rejected). `deletePolicy: Archive` is a reserved no-op.
> - **DIFF caveat**: filenames don't encode FULL vs DIFF, so pruning can orphan diffs whose parent FULL ages out — the operator emits a `BackupRetentionCaveat` warning event when this applies. Prefer `backupType: FULL` on CRs that use retention.

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
```

**Best for:** Enterprise compliance, long-term archival. For cloud storage the `retention` block is advisory — configure GCS Lifecycle Management to expire (or transition to archival storage classes) objects after 90 days. There is no operator-side archive action (`deletePolicy: Archive` is a reserved no-op).

### Suspended Backups

Temporarily pause a scheduled backup without deleting the resource. The operator propagates `suspend: true` to the CronJob's own `spec.suspend`, so Kubernetes stops firing scheduled Jobs entirely; flipping it back to `false` resumes the schedule. `suspend: true` also pauses a one-shot backup that hasn't run yet. (Separately: removing `spec.schedule` from a CR deletes its CronJob — the CR becomes a one-shot backup.)

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

> **You cannot restore into a cluster from a `kind: Cluster` (all-databases) backup.** That backup stores one `.backup` artifact *per database*, so there is no single artifact to seed one database from. The operator rejects it with an actionable error:
> `Neo4jBackup "<name>" is a kind:Cluster (all-databases) backup — … To restore a database to a cluster, either reference a kind:Database backup of that database, or use source.type=storage with source.backupPath set to the exact <database>-<timestamp>.backup file.`
> Back up the specific database with `kind: Database` (one artifact, as in the [tutorial](#tutorial-your-first-backup-and-restore-10-minutes)), or restore via `source.type: storage` pointing at the exact `.backup` file. (Standalone restores from a `kind: Cluster` backup work fine — the Job's filename glob selects the requested database.)

**Cluster path (Cypher)** — works with both cloud and PVC backups:

- **Cloud-backed backup** (S3 / GCS / Azure): the operator passes the exact `.backup` **file** URI of the latest successful run (`s3://bucket/<path>/<backup-cr-name>/<dbname>-<timestamp>.backup`) as `seedURI` — `CloudSeedProvider` seeds a single database from one file, not a directory. When that file is a differential, Neo4j resolves and applies the full + differential chain from the same directory automatically. The cluster's pods must have the cloud credentials Secret projected via `spec.extraEnvFrom` — the operator emits an actionable error if they don't, or auto-patches under annotation `neo4j.com/auto-inherit-seed-creds=true`.
- **PVC-backed backup**: the operator spawns an in-cluster busybox httpd proxy (`backup-seed-proxy-<restore-name>`) mounting the backup PVC RO at `/backup`, then passes the per-run `.backup` file URL as `seedURI` (`http://backup-seed-proxy-<restore-name>:8080/<backup-cr-name>/<filename>`). Neo4j's `URLConnectionSeedProvider` fetches it. The operator also creates a **NetworkPolicy restricting the proxy's ingress to the target cluster's server pods** (effective on enforcing CNIs), and **tears the whole proxy stack down automatically** (Deployment + Service + NetworkPolicy) as soon as the restore reaches `Completed` or `Failed` — it doesn't keep serving the backup PVC for the lifetime of the CR. No credentials required.
- Restoring over an **existing** database requires the explicit opt-in `spec.force: true` (or `spec.options.replaceExisting: true`) — recreate wipes and replaces the live contents, so the operator refuses without it.

> **S3-compatible endpoints (MinIO, on-prem object stores):** the seed
> download runs inside the Neo4j server JVMs, which need the custom endpoint
> configuration in addition to the credentials. Add both to the cluster CR
> before restoring:
>
> ```yaml
> spec:
>   env:
>   - name: AWS_ENDPOINT_URL_S3
>     value: "http://minio.minio.svc:9000"   # your S3-compatible endpoint
>   - name: JAVA_TOOL_OPTIONS
>     value: "-Daws.s3.forcePathStyle=true"
> ```
>
> (Triggers one rolling restart. Real AWS S3 — including IRSA/Workload
> Identity — needs none of this.)

> **After any cluster restore, confirm the database is online:**
>
> ```cypher
> SHOW DATABASE <name> YIELD name, currentStatus, statusMessage;
> ```
>
> `currentStatus: offline` with a populated `statusMessage` means the seed
> failed — the message names the exact cause (e.g. an unreachable seed URI).
- `dbms.recreateDatabase` preserves user/role privileges on the existing database; no `DROP DATABASE` needed.
- For new databases, the operator emits `CREATE DATABASE … OPTIONS { seedURI: '…' } WAIT`.
- Both forms finish with the database online. For the recreate path the operator polls until every allocation reports `online`, bounded by **`spec.timeout`** (default 5m) — raise it for multi-GB stores seeded from object storage.

**Standalone path (Job)**:

- The operator spawns a Kubernetes Job that mounts the backup PVC (or streams from cloud storage), runs `neo4j-admin database restore --from-path=<latest-file>`, then automatically runs `CREATE DATABASE` or `START DATABASE` over Bolt.
- The `<latest-file>` is resolved at Pod startup via shell substitution (`ls <dir>/<dbname>-*.backup | tail -1`), so the most recent run in the chain wins by default.
- Use `stopCluster: true`: the operator scales the instance down for the offline restore and back up afterwards. With `stopCluster: false` it **refuses to start the Job while any server pod is running** (it never writes into a live data volume).

**No manual post-restore Cypher is required** for either path — with one exception: if the backup included users/roles metadata (`includeMetadata`), `neo4j-admin database restore` writes a `restore_metadata.cypher` script under the data directory (`/data/scripts/<dbname>/restore_metadata.cypher`) and the operator does **not** run it. Re-apply users/roles manually:

```bash
kubectl exec <instance>-server-0 -c neo4j -- bash -c \
  'cypher-shell -u neo4j -p "$NEO4J_PASSWORD" -d system \
     -f /data/scripts/<dbname>/restore_metadata.cypher'
```

**Retrying a finished restore**: `Completed`/`Failed` are terminal for the current spec generation. Edit the spec (any change that bumps the generation — typically `source.backupRef`) to issue a fresh attempt; changing `backupRef` re-resolves the pinned `status.resolvedSource` instead of reusing the old snapshot. Or delete and recreate the CR.

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
    replaceExisting: true   # required — "neo4j" already exists on the cluster
  timeout: "30m"            # cluster online-convergence budget (default 5m)
```

On a cluster target this runs entirely over Cypher (no Job, `stopCluster` is ignored) and finishes with the database online. On a standalone target the operator runs the restore Job and then automatically issues `START DATABASE neo4j` (or `CREATE DATABASE neo4j` for a new database). **You do not need to run any Cypher manually.**

**Best for:** Quick recovery from an existing `Neo4jBackup` resource.

#### Restore from a Storage Location

**Finding `backupPath`:** the operator records every run's directory and
artifact filename on the originating `Neo4jBackup`:

```bash
kubectl get neo4jbackup <name> -o jsonpath='{.status.history[0].backupsPath}'        # directory
kubectl get neo4jbackup <name> -o jsonpath='{.status.history[0].artifactFilename}'   # exact .backup file
```

(If the Backup CR still exists, `source.type: backup` with `backupRef`
resolves the path for you — prefer it.)

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
    replaceExisting: true
    tempStorage:
      size: "50Gi"
  force: true
  stopCluster: true   # ignored on cluster targets (Cypher path); required on standalone
```

**Best for:** Cross-cluster recovery, disaster recovery from a known directory in storage (no `Neo4jBackup` CR available in this namespace).

> **Restoring after the `Neo4jBackup` CR is deleted.** A `source.type: backup` restore **pins** the backup's storage location onto its own `status.resolvedSource` the first time it resolves the reference. From that point on the restore reads the snapshot, so deleting the `Neo4jBackup` CR mid-restore (or while it retries) does **not** break it — the operator already knows where the artifacts are.
>
> If you create a **new** restore *after* the Backup CR is gone, there's nothing to resolve, so `source.type: backup` fails with an error pointing you here. Restore directly from the artifacts with **`source.type: storage`** (above): set `source.storage` to the backup's location and point `backupPath` at the exact `.backup` file (cluster) or the directory (standalone). You do **not** need to — and should **not** — re-create the Backup CR: re-creating it triggers a fresh backup Job that writes into the same chain directory, which can shadow the artifact you wanted to restore.
>
> ⚠️ **Restore is destructive and overwrites in place.** With `replaceExisting`/`force` the target database's current data is replaced by the backup. Re-running a restore — including after re-creating a deleted Backup CR — re-seeds and overwrites again. Treat every restore as a destructive operation against the named database.

> **Which run a restore picks**: a cluster restore seeds from the **latest successful artifact of the referenced `Neo4jBackup` CR** (standalone uses `tail -1` of the timestamped glob in that CR's directory). In a FULL+DIFF chain, reference the **DIFF CR** for the latest state or the **FULL CR** to roll back to the last full snapshot — restoring via the FULL CR does *not* apply the newer diffs (and emits a `RestoreFromChainParent` warning). To pin to an arbitrary earlier run, set `source.type: storage` with `backupPath` pointing at the exact `.backup` file, or keep a point-in-time snapshot of the directory (cloud lifecycle rules / versioning).

#### Restore to a Standalone Instance

`clusterRef` can reference a `Neo4jEnterpriseStandalone` as well as a
`Neo4jEnterpriseCluster` — the operator auto-detects the kind and routes to the
standalone path. Unlike the cluster path (Cypher `seedURI`, no downtime), a
standalone restore is **Job-based and takes the instance offline**:

1. The operator scales the standalone StatefulSet to 0 (`stopCluster: true` is
   required — with `false` it refuses while pods are running).
2. A restore Job mounts the data PVC and the backup source and runs
   `neo4j-admin database restore`. From a chain directory it picks the
   **latest** `.backup` file automatically; from a read-only backup PVC it
   extracts via a temp dir (`--temp-path`) since the source mount is RO.
3. The StatefulSet is scaled back up, and once the instance is `Ready` the
   operator brings the database online (`CREATE DATABASE`/`START DATABASE`).

Pre/post restore hooks (`spec.hooks`) run only on this standalone path — see
[Restore with Hooks](#restore-with-hooks).

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
  force: true          # confirms overwriting the existing database
  stopCluster: true   # required: with false the operator refuses while pods are running
```

> On standalone targets, **`spec.force: true`** is the flag that confirms
> overwriting an existing database. (`options.replaceExisting` applies to
> cluster targets.)

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
  clusterRef: recovery-standalone   # Neo4jEnterpriseStandalone (PITR is standalone-only)
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
  clusterRef: dr-standalone   # Neo4jEnterpriseStandalone (PITR is standalone-only)
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
    replaceExisting: true
  force: true
  stopCluster: true
```

---

### Restore with Hooks

Pre and post-restore hooks execute custom operations at key points in the restore lifecycle.

**Hooks run only for `Neo4jEnterpriseStandalone` targets** (the Job-based restore path) — cluster targets restore via Cypher and never invoke hooks. The ordering is:

1. **preRestore** hooks — run **before the instance is stopped**, so Cypher hooks like `CALL db.checkpoint()` hit a live Bolt endpoint.
2. Instance scale-down (`stopCluster: true`), restore Job, scale-up.
3. Automatic `CREATE DATABASE` / `START DATABASE`.
4. **postRestore** hooks — run **after the restored database is registered and started**, so Cypher hooks like `CALL db.awaitIndexes()` target a live database.

#### Restore with Cypher Hooks

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-with-hooks
spec:
  clusterRef: my-standalone   # Neo4jEnterpriseStandalone — hooks are standalone-only
  databaseName: myapp
  source:
    type: backup
    backupRef: production-backup
  force: true   # confirms overwriting the existing database (standalone target)
  options:
    preRestore:
      cypherStatements:
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes()"
        - "CALL dbms.security.clearAuthCache()"
  stopCluster: true
```

#### Restore with Job Hooks

Hook Jobs run in their own pods — anything that talks to Neo4j (e.g. `cypher-shell`) must address the instance's service explicitly (`-a neo4j://<standalone>-client:7687`), or it will try to connect to localhost inside the hook pod. (The legacy `<standalone>-service` name still resolves this release but is deprecated — update saved hook commands to `-client`.)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-with-job-hooks
spec:
  clusterRef: staging-standalone   # Neo4jEnterpriseStandalone — hooks are standalone-only
  databaseName: app-data
  source:
    type: backup
    backupRef: staging-backup
  options:
    preRestore:
      job:
        template:
          container:
            image: my-registry/data-prep:latest
            command: ["/bin/sh"]
            args: ["-c", "/scripts/pre-restore.sh"]
            env:
              - name: INSTANCE_NAME
                value: staging-standalone
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
                value: "neo4j://staging-standalone-client:7687"
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

# View backup history — for SCHEDULED backups this is the authoritative record:
# they stay in phase "Scheduled" (never "Completed") and don't set
# status.lastSuccessTime; each run appends here instead.
kubectl get neo4jbackup daily-backup -o jsonpath='{.status.history}'

# Check backup job logs (one-shot: <name>-backup; scheduled runs: use the
# history entry's runID, which is the Job name)
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
