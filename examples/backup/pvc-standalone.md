# Test: backup, chain, aggregate, restore, schedule & retention on a PVC (no cloud)

A self-contained walkthrough of the **whole backup surface** using an in-cluster
`PersistentVolumeClaim` as the destination — **no cloud account, no credentials**. It is the fastest
way to see every feature end-to-end before you wire an object store (see the per-cloud runbooks:
[Azure](azure-blob.md), [AWS](aws-s3.md), [GCS](gcs.md)).

What you will exercise:

- A **Full → Incremental** backup **chain**, and an **Aggregate** that collapses it into one recovered full.
- A **Restore** that provably brings data back (we delete rows, then restore).
- A **Schedule** with **`aggregate.enabled`** and **operator-owned retention** (`keepLast`) — watch
  chains form and older ones get pruned end-to-end (files **and** records).

> PVC destinations are a dev/local path, not a disaster-recovery path (the artifacts live in the
> same cluster as the database). For DR, use an object store — the flow below is identical, only the
> `destination` block changes.

## Prerequisites

- A running cluster and `kubectl` pointed at it (a local [kind](../../docs/user-guide/01-getting-started/local-kind.md) cluster is fine).
- The operator installed ([install guide](../../docs/user-guide/02-operator-installation/03-install.md)).
- A default `StorageClass` that provisions `ReadWriteOnce` volumes (kind's `standard` works).
- Neo4j **Enterprise** (backup is an Enterprise feature). All manifests set `edition: enterprise`.

All commands target your current namespace; add `-n <ns>` throughout to use another.

## 1. Deploy the target and load some data

Apply the target — a Standalone Neo4j with the backup listener, plus the destination claim mounted
as its `backups` volume (so restore can read the artifact back):

```bash
kubectl apply -f examples/backup/01-neo4j-pvc-destination.yaml
kubectl wait --for=condition=Ready neo4j/backup-demo --timeout=600s
```

Load 1,000 rows so restore has something to prove later:

```bash
PW=$(kubectl get secret backup-demo-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d | cut -d/ -f2)
kubectl exec backup-demo-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "UNWIND range(1,1000) AS i CREATE (:Item {i:i});"
kubectl exec backup-demo-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) RETURN count(n) AS items;"
# expect:
# items
# 1000
```

## 2. Full backup — anchor a chain

```bash
kubectl apply -f examples/backup/02-backup-full.yaml
kubectl get neo4jbackup bk-full -w      # wait for PHASE=Succeeded, then Ctrl-C
```

Verify the artifact was cataloged:

```bash
kubectl get neo4jbackup bk-full \
  -o jsonpath='{.status.phase}{"\n"}{range .status.artifacts[*]}{"  "}{.database}{" "}{.type}{" "}{.path}{"\n"}{end}'
# expect:
# Succeeded
#   neo4j Full neo4j-2026-...backup
```

## 3. Incremental — extend the chain

Add more data, then take a differential that attaches to the full above:

```bash
kubectl exec backup-demo-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "UNWIND range(1001,1500) AS i CREATE (:Item {i:i});"

kubectl apply -f examples/backup/03-backup-incremental.yaml
kubectl get neo4jbackup bk-incr -w      # wait for PHASE=Succeeded, then Ctrl-C
```

`bk-incr` is a differential on top of `bk-full`: written to the same claim, `neo4j-admin` extends
the full already on disk rather than starting a new chain, so restoring the increment replays
full + increment together (see step 5).

```bash
kubectl get neo4jbackup -o custom-columns=NAME:.metadata.name,TYPE:.spec.type,PHASE:.status.phase
# expect bk-full and bk-incr both Succeeded
```

> `status.chain` (and the `neo4j.com/chain` label) are populated only for **schedule-managed**
> backups (step 6), where the operator generates and stamps the chain id. Ad-hoc backups leave it
> empty — the on-disk chain is still real, it just isn't labelled.

## 4. Aggregate — collapse the chain into one recovered full

```bash
kubectl apply -f examples/backup/04-backup-aggregate.yaml
kubectl get neo4jbackup bk-aggregate -w  # wait for PHASE=Succeeded, then Ctrl-C
```

`bk-aggregate` produces a single recovered full that encodes full+increment, so a restore from it
seeds one artifact instead of replaying the chain. The original `bk-full`/`bk-incr` links are kept
(an ad-hoc Aggregate never deletes your data):

```bash
kubectl get neo4jbackup bk-aggregate \
  -o jsonpath='{.status.phase}{"\n"}{range .status.artifacts[*]}{"  "}{.database}{" "}{.type}{" "}{.path}{"\n"}{end}'
# expect: Succeeded, one Full artifact
```

## 5. Restore — prove data comes back

Simulate data loss, then restore from the full and confirm the row count returns:

```bash
# Destroy half the data (now 1000, was 1500):
kubectl exec backup-demo-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) WHERE n.i > 1000 DELETE n;"
kubectl exec backup-demo-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) RETURN count(n) AS items;"   # 1000

# Restore from the Full backup (bk-full → 1000 rows):
kubectl apply -f examples/backup/05-restore.yaml
kubectl get neo4jrestore rst-demo -o jsonpath='{.status.phase}{"\n"}' -w      # wait for Succeeded, then Ctrl-C

kubectl exec backup-demo-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) RETURN count(n) AS items;"
# expect: items = 1000  (the store as of the Full backup)
```

> To restore the **1,500-row** state instead, edit `05-restore.yaml` to `source.backupRef:
> bk-aggregate` (the recovered full that encodes the increment) and re-apply — the count returns 1500.
> If a restore fails with `DatabaseExists`, you omitted `overwrite: true`.

## 6. Schedule with retention — watch chains form and prune

Everything above was manual. A `Neo4jBackupSchedule` automates it and — this is the new part —
**owns retention**: `keepLast` keeps only the last N whole chains and prunes older ones entirely.
The example uses **demo-fast crons** (full every 5 min, incremental every min) so you can watch it in
minutes:

```bash
kubectl apply -f examples/backup/06-schedule.yaml
kubectl get neo4jbackupschedule sched-demo \
  -o custom-columns=NAME:.metadata.name,FULL:.spec.full.schedule,CHAIN:.status.currentChain
```

Watch backups accumulate (a new chain every 5 min, increments every min in between):

```bash
kubectl get neo4jbackup -l neo4j.com/schedule=sched-demo \
  -o custom-columns=NAME:.metadata.name,TYPE:.spec.type,PHASE:.status.phase,CHAIN:.status.chain -w
```

After ~20 minutes there are 4+ chains, and retention kicks in. Confirm only the **last 3 chains**
survive — older chains are pruned end-to-end:

```bash
# Distinct chains remaining (should be 3):
kubectl get neo4jbackup -l neo4j.com/schedule=sched-demo \
  -o jsonpath='{range .items[*]}{.status.chain}{"\n"}{end}' | sort -u
# expect: exactly 3 chain ids

# The prune is real, not just record bookkeeping — retention runs an owned Job that deletes the
# expired chain's files, then deletes its records. Watch the events:
kubectl get events --field-selector reason=SchedulePruned --sort-by=.lastTimestamp | tail -5
# expect: "Retention removed a whole expired backup chain ... N backups were pruned"
```

Aggregate compaction also runs at each chain boundary — when a full opens a new chain, the one that
just closed is compacted into a recovered full (`ScheduleCompacted` events):

```bash
kubectl get events --field-selector reason=ScheduleCompacted --sort-by=.lastTimestamp | tail -5
```

Pause the schedule when you're done watching (keeps the records, stops new backups):

```bash
kubectl patch neo4jbackupschedule sched-demo --type merge -p '{"spec":{"suspend":true}}'
```

## Cleanup

```bash
kubectl delete neo4jbackupschedule sched-demo --ignore-not-found
kubectl delete neo4jrestore rst-demo --ignore-not-found
kubectl delete neo4jbackup -l neo4j.com/schedule=sched-demo --ignore-not-found
kubectl delete neo4jbackup bk-full bk-incr bk-aggregate --ignore-not-found
kubectl delete neo4j backup-demo --ignore-not-found
kubectl delete pvc neo4j-backups --ignore-not-found      # deletes the stored artifacts
```

## What changes for an object store

The `Neo4jBackup` / `Neo4jBackupSchedule` / `Neo4jRestore` objects are **identical** except the
`destination` block: replace

```yaml
    destination:
      type: pvc
      pvc:
        claimName: neo4j-backups
```

with an object-store URL and (for static keys) a credentials Secret:

```yaml
    destination:
      type: s3                       # or gcs / azure
      url: s3://my-bucket/neo4j/     # gs://…  |  azb://account/container/neo4j/
      credentials:                   # omit to use workload identity instead
        secretName: neo4j-cloud-creds
```

Object-store retention deletes whole expired chain prefixes with an `rclone` prune Job (the identity
needs delete permission), and compaction uses `neo4j-admin --keep-old-backup=false`. The per-cloud
runbooks cover identity setup and verification:
[Azure](azure-blob.md) · [AWS](aws-s3.md) · [GCS](gcs.md).
