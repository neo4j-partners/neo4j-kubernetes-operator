# Test: backup, restore, schedule & retention on Google Cloud Storage (Workload Identity)

End-to-end steps to exercise the whole backup surface against **GCS** — a `Neo4jBackup` that
**writes** to `gs://…`, a `Neo4jRestore` that **reads** it back, and a `Neo4jBackupSchedule` that owns
the chain **and prunes old chains from the bucket**. Auth is **GKE Workload Identity** (keyless);
GCS's static-key model is a JSON file and does not fit the env-projection path — see the note at the
end.

New to the flow itself (chains, aggregate, the restore round-trip, retention semantics)? The
[PVC runbook](pvc-standalone.md) explains each step conceptually with no cloud in the way; this doc
adds only the GCS-specific identity and verification.

## How the identity works

Two pods touch the bucket, so **two ServiceAccount subjects** are bound to the Google service account
(GSA) — the #1 mistake is binding only one:

| Pod | Kubernetes ServiceAccount | Needs on the bucket |
|-----|---------------------------|---------------------|
| Backup / prune Job | `${NEO4J}-backup` | create objects (write) **and** `storage.objects.delete` (retention prune) |
| Neo4j server | `${NEO4J}` | read objects (restore reads) |

Retention deletes run in an `rclone` prune Job as **`${NEO4J}-backup`**, so the write identity is the
delete identity. Granting the GSA `roles/storage.objectAdmin` on the bucket (create + read + delete)
and binding both subjects to it covers everything.

## Prerequisites

- A GKE cluster with **Workload Identity enabled** on the cluster and node pool
  (`--workload-pool=PROJECT.svc.id.goog`, node pools with `--workload-metadata=GKE_METADATA`), and
  `gcloud` + `kubectl` logged in.
- The operator installed ([install guide](../../docs/user-guide/02-operator-installation/03-install.md)).
- Neo4j **Enterprise**; requires GCS support in neo4j-admin (≥ 5.21 for object-store aggregate).

## 0. Shell variables

```bash
export PROJECT=$(gcloud config get-value project)
export NS=default
export NEO4J=backup-demo                        # → backup SA is "backup-demo-backup"
export BUCKET=neo4j-backups-$RANDOM             # globally unique
export GSA=neo4j-backups                         # Google service account name
export GSA_EMAIL=${GSA}@${PROJECT}.iam.gserviceaccount.com
```

## 1. Create the bucket

```bash
gcloud storage buckets create gs://${BUCKET} --location=US
```

## 2. Google service account + bucket grant (read, write, **and delete**)

`roles/storage.objectAdmin` includes `storage.objects.delete`, which retention needs:

```bash
gcloud iam service-accounts create "$GSA"
gcloud storage buckets add-iam-policy-binding gs://${BUCKET} \
  --member="serviceAccount:${GSA_EMAIL}" --role=roles/storage.objectAdmin
```

## 3. Bind **both** subjects to the GSA (Workload Identity)

```bash
for SA in ${NEO4J}-backup ${NEO4J}; do
  gcloud iam service-accounts add-iam-policy-binding "$GSA_EMAIL" \
    --role=roles/iam.workloadIdentityUser \
    --member="serviceAccount:${PROJECT}.svc.id.goog[${NS}/${SA}]"
done
```

## 4. Deploy Neo4j with the GCP cloud identity

The operator stamps `iam.gke.io/gcp-service-account` onto **both** the `${NEO4J}-backup` and
`${NEO4J}` ServiceAccounts, which GKE maps to the GSA:

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1
kind: Neo4j
metadata:
  name: ${NEO4J}
spec:
  edition: enterprise
  version: "2026.05.0"
  license: { accept: "yes" }
  topology: { mode: Standalone }
  features: { backup: { enabled: true } }
  connectivity: { listeners: { backup: 6362 } }
  security:
    cloudIdentity:
      workloadIdentity:
        provider: gcp
        annotations:
          iam.gke.io/gcp-service-account: ${GSA_EMAIL}
  storage: { volumes: { data: { mode: Dynamic, dynamic: { size: 10Gi } } } }
  auth: { generatePassword: true }
  trust:                          # restore seeds over admin Bolt; opt in to plaintext on a test cluster
    enabled: false
    insecureAdminConnection: true
YAML

kubectl -n "$NS" wait --for=condition=Ready neo4j/${NEO4J} --timeout=600s
kubectl -n "$NS" get sa ${NEO4J}-backup -o jsonpath='{.metadata.annotations}'; echo
# expect: {"iam.gke.io/gcp-service-account":"neo4j-backups@<project>.iam.gserviceaccount.com"}
```

> Unlike AWS/Azure, GKE Workload Identity injects **no env vars** — the pod authenticates through the
> node metadata server. So there is nothing to `grep` in the pod env; a Succeeded backup (step 5) is
> the proof the binding works.

Load some data so restore has something to prove:

```bash
PW=$(kubectl -n "$NS" get secret ${NEO4J}-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d | cut -d/ -f2)
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "UNWIND range(1,1000) AS i CREATE (:Item {i:i});"
```

## 5. Backup to GCS

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1
kind: Neo4jBackup
metadata: { name: bk-gcs }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  type: Full
  destination:
    type: gcs
    url: gs://${BUCKET}/neo4j/          # trailing '/' — neo4j-admin needs a directory
YAML

kubectl -n "$NS" get neo4jbackup bk-gcs -w      # wait for PHASE=Succeeded, then Ctrl-C
kubectl -n "$NS" get neo4jbackup bk-gcs -o jsonpath='{.status.phase} {.status.artifacts}{"\n"}'
gcloud storage ls --recursive gs://${BUCKET}/neo4j/
```

If PHASE=Failed, `kubectl -n "$NS" describe neo4jbackup bk-gcs` shows the neo4j-admin cause — usually
a missing `${NEO4J}-backup` binding (step 3) or a missing bucket role (step 2).

## 6. Restore from GCS

Restore reads run on the **server** pod (SA `${NEO4J}`) via the target's `cloudIdentity` — already
wired in step 4. Prove it round-trips by deleting data first:

```bash
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) DELETE n;"

cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1
kind: Neo4jRestore
metadata: { name: rst-gcs }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  overwrite: true
  source: { backupRef: bk-gcs }
YAML

kubectl -n "$NS" get neo4jrestore rst-gcs -o jsonpath='{.status.phase}{"\n"}' -w   # wait for Succeeded
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) RETURN count(n) AS items;"        # expect 1000
```

> `RestoreSeedFailed … does not point to a valid location` almost always means the **operand** binding
> `${PROJECT}.svc.id.goog[${NS}/${NEO4J}]` is missing from step 3.

## 7. Schedule with retention — watch chains prune from the bucket

`keepLast` is the single source of truth — **no GCS lifecycle rule**. Aggregate compaction runs
`neo4j-admin … --keep-old-backup=false`, and an `rclone` prune Job (as `${NEO4J}-backup`, using the
same GSA) purges whole expired chain prefixes. Demo-fast crons let you watch it in minutes:

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1
kind: Neo4jBackupSchedule
metadata: { name: sched-gcs }
spec:
  neo4jRef: { name: ${NEO4J} }
  full:
    schedule: "*/5 * * * *"       # DEMO-FAST
    retention: { keepLast: 3 }
  incremental: { schedule: "* * * * *" }
  aggregate: { enabled: true }
  backupTemplate:
    databases: ["neo4j"]
    destination:
      type: gcs
      url: gs://${BUCKET}/scheduled/   # schedule isolates each chain in its own prefix
YAML
```

After ~20 minutes, confirm only the last 3 chains survive — in the records and in the bucket:

```bash
kubectl -n "$NS" get neo4jbackup -l neo4j.com/schedule=sched-gcs \
  -o jsonpath='{range .items[*]}{.status.chain}{"\n"}{end}' | sort -u        # expect 3 chain ids
gcloud storage ls gs://${BUCKET}/scheduled/                                  # expect 3 chain prefixes
kubectl -n "$NS" get events --field-selector reason=SchedulePruned    --sort-by=.lastTimestamp | tail -3
kubectl -n "$NS" get events --field-selector reason=ScheduleCompacted --sort-by=.lastTimestamp | tail -3
```

> If chains pile up past `keepLast` and you see `SchedulePruneFailed`, the GSA is missing
> `storage.objects.delete` — re-check the `roles/storage.objectAdmin` grant from step 2.

Pause when done:

```bash
kubectl -n "$NS" patch neo4jbackupschedule sched-gcs --type merge -p '{"spec":{"suspend":true}}'
```

## Cleanup

```bash
kubectl -n "$NS" delete neo4jbackupschedule sched-gcs --ignore-not-found
kubectl -n "$NS" delete neo4jbackup -l neo4j.com/schedule=sched-gcs --ignore-not-found
kubectl -n "$NS" delete neo4jrestore rst-gcs --ignore-not-found
kubectl -n "$NS" delete neo4jbackup bk-gcs --ignore-not-found
kubectl -n "$NS" delete neo4j ${NEO4J} --ignore-not-found
gcloud storage rm --recursive gs://${BUCKET}
gcloud iam service-accounts delete "$GSA_EMAIL" --quiet
```

## A note on static keys for GCS

Unlike S3 (`AWS_*`) and Azure (`AZURE_STORAGE_*`), GCS authenticates with a **service-account JSON
key file** referenced by `GOOGLE_APPLICATION_CREDENTIALS` (a *path*). The operator's
`destination.credentials` Secret is projected as **environment variables**, not mounted as a file, so
it does not carry a JSON key the way S3/Azure keys work. For GCS, **use Workload Identity** (above).
If you have a hard requirement for a key file, consult
[Neo4j — back up to cloud storage](https://neo4j.com/docs/operations-manual/current/backup-restore/online-backup/)
for the current file-based mechanism.
