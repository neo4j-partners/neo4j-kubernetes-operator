# Backup & Restore Troubleshooting

Common backup and restore failures and their fixes. For the feature overview and configuration, see the [Backup & Restore guide](../guides/backup_restore.md).

## Common Backup Issues

### Backup Job Failures

#### Symptom: Backup job fails to start
```bash
kubectl get jobs -l app.kubernetes.io/component=backup
# STATUS: Failed or no jobs created
```

**Diagnosis:**
```bash
# Check backup resource status — status.phase tells you which stage it's in:
#   Invalid   → spec failed validation (message has the details)
#   Waiting   → target cluster missing or not Ready yet (transient)
#   Pending   → transient precondition (chain parent missing, chain busy,
#               sharded preflight couldn't connect) — retried automatically
#   Failed    → terminal for one-shot backups
kubectl get neo4jbackup
kubectl describe neo4jbackup production-backup

# Check operator logs for backup controller errors
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -i backup
```

**Common Causes & Solutions:**

1. **Missing ServiceAccount**:
   ```bash
# The operator automatically creates the backup ServiceAccount — check it exists.
# (No Role/RoleBinding is created: the backup Job needs no Kubernetes API access.)
kubectl get serviceaccount neo4j-backup-sa

# If missing, trigger operator reconciliation with a no-op annotation change
kubectl annotate neo4jenterprisecluster production-cluster troubleshooting.neo4j.com/reconcile="$(date +%s)" --overwrite
   ```

2. **Storage Configuration Issues**:
   ```yaml
   # Verify storage configuration in backup spec
   spec:
     storage:
       type: s3
       bucket: "valid-bucket-name"    # Must exist
       path: "backups/"
       cloud:
         provider: aws
         identity:
           provider: aws
       # Credentials must be valid
   ```

3. **Cluster Reference Problems**:
   ```bash
   # Verify cluster exists and is ready
   kubectl get neo4jenterprisecluster production-cluster
   kubectl get pods -l neo4j.com/cluster=production-cluster
   ```

#### Symptom: Backup job starts but fails during execution

**Diagnosis:**
```bash
# Check backup Job's Pod log (one-shot: <neo4jbackup-name>-backup;
# CronJob child: <neo4jbackup-name>-backup-cron-<unix-seconds>).
kubectl logs -n <ns> job/<job-name>

# Check Neo4j server logs for backup-related errors (which server
# was the leader at backup time).
kubectl logs <cluster>-server-0 -c neo4j | grep -i backup
```

**Common Solutions:**

1. **Insufficient Disk Space** (PVC storage):
   ```bash
   # `kubectl exec` into any server pod to inspect the bound PVC.
   kubectl exec <cluster>-server-0 -c neo4j -- df -h /data
   ```
   Increase `Neo4jBackup.spec.storage.pvc.size` (only effective when the operator provisions the PVC — see [bring-your-own PVC](../guides/backup_restore.md#pvc-ownership-auto-provision-vs-bring-your-own) otherwise). Note that `retention.maxCount` / `retention.maxAge` do **not** prune continuously — PVC retention runs only when the Neo4jBackup CR is deleted, so a long-lived scheduled CR accumulates artifacts until then. Prune manually (or via a recurring `neo4j-admin backup aggregate`) if the volume fills.

2. **Database Lock Issues**:
   ```bash
   # Check for long-running transactions
   kubectl exec production-cluster-server-0 -- cypher-shell -u neo4j -p password \
     "CALL db.listTransactions() YIELD transactionId, elapsedTimeMillis WHERE elapsedTimeMillis > 30000"

   # Solution: Wait for transactions to complete or schedule backups off-peak
   ```

3. **Memory Issues in Backup Process**:
   Backup pod resources default to a Burstable profile (request 100m CPU / 512Mi, limit 1 CPU / 2Gi) but **are tunable** via `spec.options.resources` (standard `requests`/`limits`). Raise them for large databases; `spec.options.pageCache` controls the page-cache hint passed to `neo4j-admin`.

### Cloud Storage Issues

#### S3 Backup Failures

**Authentication Issues:**
```bash
# Check AWS credentials using the backup service account (default: neo4j-backup-sa)
kubectl run backup-auth-check --rm -it --image=amazon/aws-cli --serviceaccount=<backup-serviceaccount> -- aws sts get-caller-identity

# Test S3 access
kubectl run backup-auth-check --rm -it --image=amazon/aws-cli --serviceaccount=<backup-serviceaccount> -- aws s3 ls s3://your-backup-bucket/
```

**Solutions:**

1. **IAM Role Issues**:
   ```yaml
   # Use IAM roles for service accounts (IRSA) on the Neo4jBackup CR.
   # The annotations land on the operator-managed neo4j-backup-sa.
   apiVersion: neo4j.neo4j.com/v1beta1
   kind: Neo4jBackup
   spec:
     cloud:
       provider: aws
       identity:
         provider: aws
         autoCreate:
           annotations:
             eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/Neo4jBackupRole"
   ```

2. **Bucket Policy Problems**:
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Principal": {
           "AWS": "arn:aws:iam::123456789:role/Neo4jBackupRole"
         },
         "Action": [
           "s3:GetObject",
           "s3:PutObject",
           "s3:DeleteObject",
           "s3:ListBucket"
         ],
         "Resource": [
           "arn:aws:s3:::your-backup-bucket",
           "arn:aws:s3:::your-backup-bucket/*"
         ]
       }
     ]
   }
   ```

#### Google Cloud Storage Issues

**Service Account Problems:**
```bash
# Check GCP credentials using the backup service account (default: neo4j-backup-sa)
kubectl run backup-auth-check --rm -it --image=google/cloud-sdk:slim --serviceaccount=<backup-serviceaccount> -- gcloud auth list

# Test GCS access
kubectl run backup-auth-check --rm -it --image=google/cloud-sdk:slim --serviceaccount=<backup-serviceaccount> -- gsutil ls gs://your-backup-bucket/
```

**Solutions:**
```yaml
# Use Workload Identity on the Neo4jBackup CR
# (annotations land on the operator-managed neo4j-backup-sa)
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
spec:
  cloud:
    provider: gcp
    identity:
      provider: gcp
      autoCreate:
        annotations:
          iam.gke.io/gcp-service-account: "neo4j-backup@project.iam.gserviceaccount.com"
```

#### Azure Blob Storage Issues

**Authentication Problems:**
```bash
# Check Azure credentials using the backup service account (default: neo4j-backup-sa)
kubectl run backup-auth-check --rm -it --image=mcr.microsoft.com/azure-cli --serviceaccount=<backup-serviceaccount> -- az account show

# Test storage access
kubectl run backup-auth-check --rm -it --image=mcr.microsoft.com/azure-cli --serviceaccount=<backup-serviceaccount> -- az storage blob list --account-name storageaccount --container-name backups
```

### Scheduled Backup Issues

#### Symptom: Scheduled backups not running

**Diagnosis:**
```bash
# Check CronJob status
kubectl get cronjob
kubectl describe cronjob production-backup-schedule

# Check backup schedule configuration
kubectl get neo4jbackup production-backup -o yaml | grep -A 10 schedule
```

**Common Solutions:**

1. **Invalid Cron Expression**:
   ```yaml
   # Correct cron syntax
   spec:
     schedule: "0 2 * * *"    # Daily at 2 AM
     # NOT: "0 2 * * * *"     # Invalid - too many fields
   ```

2. **Timezone Issues**:
   Schedules run in **UTC** — there is no `spec.timezone` field, and `TZ=`/`CRON_TZ=` prefixes in the schedule string are **rejected by the validator** (Kubernetes refuses timezone-embedded CronJob schedules). Convert your local time to UTC:
   ```yaml
   spec:
     schedule: "0 2 * * *"    # 02:00 UTC — adjust the hour for your timezone
   ```

3. **Backup Window Conflicts**:
   ```bash
   # Check for overlapping backup jobs
   kubectl get jobs -l app.kubernetes.io/component=backup --sort-by=.metadata.creationTimestamp
   ```

## Common Restore Issues

### Restore Job Failures

#### Symptom: Restore job fails to start

**Diagnosis:**
```bash
# Check restore resource status
kubectl get neo4jrestore
kubectl describe neo4jrestore production-restore

# Check operator logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -i restore
```

**Common Solutions:**

1. **Invalid Backup Reference**:
   ```bash
   # Verify backup exists
   kubectl get neo4jbackup production-backup

   # Check backup completion status. One-shot backups reach phase "Completed";
   # scheduled backups stay in "Scheduled" — check status.history for a
   # Succeeded run instead.
   kubectl get neo4jbackup production-backup -o jsonpath='{.status.phase}'
   kubectl get neo4jbackup production-backup -o jsonpath='{.status.history[*].status}'
   ```
   A restore whose referenced backup has no Succeeded run yet sits in `Pending` ("Waiting for backup to complete") and retries automatically — that is not an error.

2. **Target Cluster Issues**:
   ```bash
   # Ensure target cluster is ready
   kubectl get neo4jenterprisecluster target-cluster
   kubectl get pods -l neo4j.com/cluster=target-cluster
   ```

3. **Storage Access Problems**:
   ```bash
   # Run a transient pod to test S3 access from inside the cluster.
   kubectl run -it --rm s3-test --image=amazon/aws-cli \
     --restart=Never --env-from=secretRef/aws-backup-creds -- \
     s3 ls s3://backup-bucket/path/to/backup/
   ```

#### Symptom: Restore job fails during execution

**Diagnosis:**
```bash
# Check restore job logs (standalone restore Job name: <neo4jrestore-name>-restore)
kubectl logs job/production-restore-restore

# Check target cluster logs during restore
kubectl logs target-cluster-server-0 | grep -i restore
```

**Common Solutions:**

1. **Insufficient Storage Space**:
   ```bash
   # Check available space on target cluster
   kubectl exec target-cluster-server-0 -- df -h /data

   # Solution: Increase PVC size before restore
   ```

2. **Database Already Exists**:
   ```yaml
   # Overwrite the existing same-named database. Use either the
   # option-level replaceExisting flag or the top-level force flag.
   spec:
     force: true              # top-level; adds --overwrite-destination=true
     options:
       replaceExisting: true  # equivalent option-level flag
   ```

3. **Version Incompatibility**:
   ```bash
   # Check Neo4j versions
   kubectl exec source-cluster-server-0 -- neo4j version
   kubectl exec target-cluster-server-0 -- neo4j version
   ```

#### Symptom: Cluster restore reports `Failed` with "Cluster missing seed credentials projection"

**Cause:** the cluster pods need the cloud credentials Secret projected via `spec.extraEnvFrom` so the JVM's AWS/GCP/Azure SDK can authenticate the `seedURI` fetch from `CloudSeedProvider`.

**Fix:**

```yaml
# On the Neo4jEnterpriseCluster CR:
spec:
  extraEnvFrom:
    - secretRef:
        name: <your-backup-creds-secret>
```

Or, set the annotation `neo4j.com/auto-inherit-seed-creds=true` on the cluster CR — the operator will patch `extraEnvFrom` automatically (triggers a rolling restart so Neo4j picks up the env vars).

#### Symptom: Cluster restore stuck in `Running`, no Job created

**Expected.** Cluster Neo4jRestore targets use the Cypher path (`dbms.recreateDatabase` or `CREATE DATABASE OPTIONS{seedURI}`) — no Job is spawned. Check the operator log:

```bash
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager \
  | grep -E "Cluster Cypher restore|recreateDatabase|CREATE DATABASE"
```

If you see `No seed providers found to satisfy the provided uri 's3://...'`, the cluster doesn't have the cloud creds projected — see the section above.

#### Symptom: Sharded DB restore rejected with "use Neo4jShardedDatabase.spec.replaceExisting"

**Cause:** `Neo4jRestore` doesn't support sharded databases — the Cypher shape (`SET GRAPH SHARD` / `SET PROPERTY SHARDS`) only fits `CREATE DATABASE`, not `dbms.recreateDatabase`.

**Fix:** restore via the `Neo4jShardedDatabase` CR's destructive-restore flow:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jShardedDatabase
metadata:
  name: products
spec:
  # … existing sharding fields …
  seedBackupRef: products-backup
  replaceExisting: true
  force: true
```

See [Property Sharding](../property_sharding.md) for details.

### Point-in-Time Recovery (PITR) Issues

PITR via `Neo4jRestore` (`source.type: pitr`) runs the `neo4j-admin database restore --restore-until=…` Job and is supported **only for `Neo4jEnterpriseStandalone` targets**. For cluster point-in-time recovery, create a `Neo4jDatabase` with `spec.seedConfig.restoreUntil` instead — a `Neo4jRestore` with `source.type: pitr` pointing at a cluster `clusterRef` is rejected by the validator.

#### Symptom: PITR restore rejected for a cluster target

```
source.type=pitr is not supported for cluster targets … For cluster
point-in-time recovery, create a Neo4jDatabase with spec.seedConfig.restoreUntil instead
```

**Fix:** use the `Neo4jDatabase` seed-config path for clusters; reserve `Neo4jRestore` PITR for standalone.

#### Symptom: PITR restore fails with timestamp errors

**Solutions:**

1. **Missing PITR source config**: `source.type: pitr` requires `source.pitr.baseBackup` or `source.pointInTime` (or both):
   ```yaml
   spec:
     source:
       type: pitr
       pointInTime: "2025-01-15T14:30:00Z"   # ISO 8601 (metav1.Time)
       pitr:
         baseBackup:
           type: backup
           backupRef: production-backup
   ```
   The operator renders `pointInTime` into `neo4j-admin database restore --restore-until="2025-01-15 14:30:00"`.

2. **Timestamp Outside Backup Range**:
   ```bash
   # Check backup time range
   kubectl logs job/production-backup-20250115 | grep -E "(start|end).*time"
   ```

3. **Neo4j Version Compatibility**:
   ```yaml
   # PITR only available in Neo4j 2025.x
   spec:
     image:
       repo: "neo4j"
       tag: "2025.01.0-enterprise"
   ```

## Performance Issues

### Slow Backup Performance

**Diagnosis:**
```bash
# Monitor backup progress
kubectl logs job/production-backup-latest -f

# Check resource utilization during backup
kubectl top pod production-cluster-server-0
```

**Optimization Strategies:**

1. **Reduce primary load**: Use database-specific backups and schedule during low-traffic windows.

2. **Avoid overlapping backups**: Stagger `Neo4jBackup` schedules so only one job runs per cluster at a time.

3. **Storage Performance Tuning**: Back the `Neo4jBackup` Job's destination PVC with a high-performance storage class (e.g. `fast-ssd`) for backup staging.

4. **Network Optimization**:
   ```yaml
   spec:
     config:
       # Increase buffer sizes for backup operations
       server.memory.off_heap.max_size: "2g"
       server.memory.pagecache.size: "4g"
   ```

### Slow Restore Performance

**Optimization:**

1. **Target Cluster Resources**:
   ```yaml
   spec:
     resources:
       requests:
         memory: "8Gi"
         cpu: "4"
       limits:
         memory: "16Gi"
         cpu: "8"
   ```

2. **Storage Configuration**:
   ```yaml
   spec:
     storage:
       className: "fast-ssd"
       size: "1Ti"
   ```

## Monitoring and Alerting

### Backup Health Monitoring

**Prometheus Metrics** (exported by the operator):
```yaml
# Backup attempts, labelled by result ("success"/"failure")
neo4j_operator_backup_total{cluster_name, namespace, result}
# Backup duration histogram
neo4j_operator_backup_duration_seconds{cluster_name, namespace}
# Size of the latest backup in bytes
neo4j_operator_backup_size_bytes{cluster_name, namespace}

# Alert rules
groups:
- name: neo4j-backup
  rules:
  - alert: BackupFailure
    expr: increase(neo4j_operator_backup_total{result="failure"}[24h]) > 0
    labels:
      severity: critical
    annotations:
      summary: "Neo4j backup failed"
      description: "Backup for cluster {{ $labels.cluster_name }} failed"
```

**Log Monitoring:**
```bash
# Monitor backup logs
kubectl logs -f job/production-backup-latest | grep -E "(ERROR|WARN|SUCCESS)"

# Set up log alerts
kubectl logs -f -n neo4j-operator-system deployment/neo4j-operator-controller-manager | \
  grep -i "backup.*failed" --line-buffered | \
  while read line; do
    echo "BACKUP ALERT: $line"
    # Send to alerting system
  done
```

### Backup Validation

**Automated Validation Script:**
```bash
#!/bin/bash
# Validate backup completeness

BACKUP_NAME="production-backup"
NAMESPACE="default"

validate_backup() {
  # One-shot backups reach phase "Completed". Scheduled backups stay in
  # "Scheduled" forever — check status.history for a Succeeded run instead.
  local backup_status=$(kubectl get neo4jbackup $BACKUP_NAME -n $NAMESPACE -o jsonpath='{.status.phase}')

  if [ "$backup_status" = "Scheduled" ]; then
    local last_run=$(kubectl get neo4jbackup $BACKUP_NAME -n $NAMESPACE \
      -o jsonpath='{.status.history[-1].status}')
    if [ "$last_run" != "Succeeded" ]; then
      echo "❌ Last scheduled run not successful: ${last_run:-no runs yet}"
      return 1
    fi
  elif [ "$backup_status" != "Completed" ]; then
    echo "❌ Backup failed or incomplete: $backup_status"
    return 1
  fi

  # Check backup size (human-readable string such as "2.5GB", parsed by
  # neo4j-admin output; empty if the Pod log couldn't be read)
  local backup_size=$(kubectl get neo4jbackup $BACKUP_NAME -n $NAMESPACE -o jsonpath='{.status.stats.size}')
  echo "ℹ️  Latest backup size: ${backup_size:-unknown}"

  echo "✅ Backup validation passed"
  return 0
}

# Run validation
validate_backup
```

## Emergency Recovery

For full disaster recovery (corrupted primary, restore to a new cluster from latest backup), follow the standard restore flow in the [Backup & Restore guide § Restore Operations](../guides/backup_restore.md#restore-operations). The normal `Neo4jRestore` CR with `source.type: backup` + `clusterRef` pointing at a fresh cluster IS the emergency procedure — there's no separate path. Use `spec.force: true` (top-level) to overwrite existing data. To roll back to a specific timestamp before the corruption: on a standalone target use `Neo4jRestore` with `source.type: pitr` and `source.pointInTime`; on a cluster target use a `Neo4jDatabase` with `spec.seedConfig.restoreUntil` (cluster `Neo4jRestore` PITR is rejected — see [PITR Issues](#point-in-time-recovery-pitr-issues)).

## See Also

- [Backup & Restore Guide](../guides/backup_restore.md)
- [Performance Tuning](../performance.md)
- [Security Guide](../security.md)
- [Split-Brain Recovery](split-brain-recovery.md)
