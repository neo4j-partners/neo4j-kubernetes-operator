# Test: backup & restore a 3-primary Cluster to an object store

The backup/restore/schedule objects are **identical** for a Cluster and a Standalone — you point
`neo4jRef` at a Cluster instead. This runbook covers only what actually differs on a cluster, on top
of the per-cloud identity setup you already have from one of:
[AWS S3](aws-s3.md) · [Azure](azure-blob.md) · [GCS](gcs.md).

**Use an object store, not a PVC.** A cluster restore seeds **every member independently**, so a PVC
destination would need a `ReadWriteMany`, POSIX-compliant volume shared by all pods. An object store
sidesteps that — each primary reads the artifact over the same `cloudIdentity`. The examples below
use S3; swap the `destination` block and identity for Azure/GCS as in their runbooks.

## What's different on a cluster

| | Standalone | Cluster |
|-|-----------|---------|
| Admin Bolt path | not needed | **required** — `trust.certificates.bolt` (verified TLS) or `trust.insecureAdminConnection: true` (NEO-004) |
| Server pods | `${NEO4J}-server-0` | `${NEO4J}-primary-0`, `-1`, `-2` (one StatefulSet per pool) |
| Writing test data | direct `bolt://` | **routed** `neo4j://` session (a direct write to a non-leader is refused) |
| Restore | seeds the one node | `CREATE OR REPLACE DATABASE … TOPOLOGY 3 PRIMARIES` — **every primary seeds from the store independently** |
| Identity | backup SA + operand SA | **same two SAs** (`${NEO4J}-backup`, `${NEO4J}`) — cluster changes nothing here |

## Prerequisites

- Finish the identity setup (bucket, IAM/role/GSA, both subjects trusted) from your chosen cloud
  runbook — the two ServiceAccount subjects (`${NEO4J}-backup`, `${NEO4J}`) are derived from the CR
  name exactly as for Standalone, so nothing changes there.
- `export NEO4J=cluster-demo NS=default BUCKET=... ROLE_ARN=...` (as in that runbook).

## 1. Deploy the cluster as a backup target

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: ${NEO4J}
spec:
  edition: enterprise
  version: "2026.05.0"
  license: { accept: "yes" }
  topology:
    mode: Cluster
    primaries: { members: 3 }
  features: { backup: { enabled: true } }
  connectivity: { listeners: { backup: 6362 } }
  security:
    cloudIdentity:
      workloadIdentity:
        provider: aws                       # or gcp / azure — match your runbook
        annotations:
          eks.amazonaws.com/role-arn: ${ROLE_ARN}
  storage: { volumes: { data: { mode: Dynamic, dynamic: { size: 10Gi } } } }
  auth: { generatePassword: true }
  trust:
    # Cluster admission requires an admin Bolt path for the operator (NEO-004): this plaintext
    # opt-in (Warning event), or trust.certificates.bolt for verified TLS.
    enabled: false
    insecureAdminConnection: true
YAML

kubectl -n "$NS" wait --for=condition=Ready neo4j/${NEO4J} --timeout=900s   # cluster formation is slower
```

Write test data over a **routed** session (so it lands on the leader from whichever pod you exec):

```bash
PW=$(kubectl -n "$NS" get secret ${NEO4J}-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d | cut -d/ -f2)
kubectl -n "$NS" exec ${NEO4J}-primary-0 -c neo4j -- \
  cypher-shell -a neo4j://localhost:7687 -u neo4j -p "$PW" \
  "UNWIND range(1,1000) AS i CREATE (:Item {i:i});"
```

## 2. Backup — a member streams it to the store

The `Neo4jBackup` is exactly the Standalone one; the operator runs it against a cluster member:

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata: { name: bk-cluster }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  type: Full
  destination:
    type: s3
    url: s3://${BUCKET}/cluster/
YAML

kubectl -n "$NS" get neo4jbackup bk-cluster -w      # wait for PHASE=Succeeded, then Ctrl-C
```

## 3. Restore — every primary reseeds from the store

Delete data on the leader (routed), then restore. The operator issues `CREATE OR REPLACE DATABASE
neo4j … TOPOLOGY 3 PRIMARIES`, so each primary pulls the seed from the object store on its own:

```bash
kubectl -n "$NS" exec ${NEO4J}-primary-0 -c neo4j -- \
  cypher-shell -a neo4j://localhost:7687 -u neo4j -p "$PW" "MATCH (n:Item) DELETE n;"

cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata: { name: rst-cluster }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  overwrite: true
  source: { backupRef: bk-cluster }
YAML

kubectl -n "$NS" get neo4jrestore rst-cluster -o jsonpath='{.status.phase}{"\n"}' -w   # wait for Succeeded
```

Confirm the seed landed on **all three** primaries — query each member directly (`bolt://`, `-d
neo4j`) so you're reading that member's local store, not routing to the leader:

```bash
for i in 0 1 2; do
  echo -n "primary-$i: "
  kubectl -n "$NS" exec ${NEO4J}-primary-$i -c neo4j -- \
    cypher-shell -a bolt://localhost:7687 -d neo4j -u neo4j -p "$PW" --format plain \
    "MATCH (n:Item) RETURN count(n);"
done
# expect: 1000 on primary-0, primary-1, and primary-2
```

## 4. Schedule with retention

Identical to the Standalone schedule — point `neo4jRef` at the cluster. Retention and compaction
prune the bucket the same way (the `rclone` prune Job runs as `${NEO4J}-backup`). Follow the
"Schedule with retention" section of your cloud runbook, substituting `neo4jRef: { name: ${NEO4J} }`.

## Cleanup

```bash
kubectl -n "$NS" delete neo4jbackupschedule --all -n "$NS" --ignore-not-found
kubectl -n "$NS" delete neo4jrestore rst-cluster --ignore-not-found
kubectl -n "$NS" delete neo4jbackup bk-cluster --ignore-not-found
kubectl -n "$NS" delete neo4j ${NEO4J} --ignore-not-found
# then delete the bucket + IAM/GSA as in your cloud runbook's cleanup
```
