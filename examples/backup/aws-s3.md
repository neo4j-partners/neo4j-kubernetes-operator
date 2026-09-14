# Test: backup, restore, schedule & retention on AWS S3 (IRSA or static keys)

End-to-end steps to exercise the whole backup surface against **Amazon S3** — a `Neo4jBackup` that
**writes** to `s3://…`, a `Neo4jRestore` that **reads** it back, and a `Neo4jBackupSchedule` that
owns the chain **and prunes old chains from the bucket**. The recommended auth is **IRSA** (IAM Roles
for Service Accounts — keyless); a **static-key** alternative is at the end.

New to the flow itself (chains, aggregate, the restore round-trip, retention semantics)? The
[PVC runbook](pvc-standalone.md) explains each step conceptually with no cloud in the way; this doc
adds only the S3-specific identity and verification.

## How the identity works

Two pods touch the bucket, so **two ServiceAccount subjects** must be trusted by the IAM role — the
#1 mistake is federating only one:

| Pod | ServiceAccount | Needs |
|-----|----------------|-------|
| Backup / prune Job | `${NEO4J}-backup` | `s3:PutObject` (write) **and** `s3:DeleteObject` (retention prune) |
| Neo4j server | `${NEO4J}` | `s3:GetObject`, `s3:ListBucket` (restore reads) |

Retention deletes are done by an `rclone` prune Job that runs as **`${NEO4J}-backup`** — the same
identity as the backup — so the write role is also the delete role. One IAM role trusted by both
subjects, with all four actions, covers everything.

## Prerequisites

- An EKS cluster with the **OIDC provider enabled** (`eksctl utils associate-iam-oidc-provider …`), and `aws` + `kubectl` logged in.
- The operator installed ([install guide](../../docs/user-guide/02-operator-installation/03-install.md)).
- Neo4j **Enterprise**; requires S3 support in neo4j-admin (≥ 5.19 for object-store aggregate).

## 0. Shell variables

```bash
export NS=default
export NEO4J=backup-demo                     # → backup SA is "backup-demo-backup"
export REGION=us-east-1
export BUCKET=neo4j-backups-$RANDOM          # globally unique
export ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export CLUSTER=my-eks                         # your EKS cluster name
export OIDC=$(aws eks describe-cluster --name "$CLUSTER" \
  --query 'cluster.identity.oidc.issuer' --output text | sed 's#https://##')
export ROLE=neo4j-backups                     # IAM role name
```

## 1. Create the bucket

```bash
aws s3api create-bucket --bucket "$BUCKET" --region "$REGION" \
  $( [ "$REGION" = us-east-1 ] || echo --create-bucket-configuration LocationConstraint="$REGION" )
```

## 2. IAM policy — read, write, **and delete**

Delete is what makes retention able to reclaim bucket storage; without it the schedule keeps piling
up chains and emits `SchedulePruneFailed`.

```bash
cat > /tmp/neo4j-s3-policy.json <<JSON
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": ["s3:ListBucket"], "Resource": "arn:aws:s3:::${BUCKET}" },
    { "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::${BUCKET}/*" }
  ]
}
JSON
aws iam create-policy --policy-name neo4j-s3 --policy-document file:///tmp/neo4j-s3-policy.json
export POLICY_ARN=arn:aws:iam::${ACCOUNT_ID}:policy/neo4j-s3
```

## 3. IAM role trusted by **both** subjects

The trust policy federates the EKS OIDC provider for the two ServiceAccount subjects at once
(`sub` accepts a list):

```bash
cat > /tmp/neo4j-trust.json <<JSON
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "Federated": "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/${OIDC}" },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "${OIDC}:aud": "sts.amazonaws.com",
        "${OIDC}:sub": [
          "system:serviceaccount:${NS}:${NEO4J}-backup",
          "system:serviceaccount:${NS}:${NEO4J}"
        ]
      }
    }
  }]
}
JSON
aws iam create-role --role-name "$ROLE" --assume-role-policy-document file:///tmp/neo4j-trust.json
aws iam attach-role-policy --role-name "$ROLE" --policy-arn "$POLICY_ARN"
export ROLE_ARN=arn:aws:iam::${ACCOUNT_ID}:role/${ROLE}
echo "$ROLE_ARN"
```

## 4. Deploy Neo4j with the AWS cloud identity

The operator stamps `eks.amazonaws.com/role-arn` onto **both** the `${NEO4J}-backup` and `${NEO4J}`
ServiceAccounts, so the two subjects the role trusts both assume it:

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
  topology: { mode: Standalone }
  features: { backup: { enabled: true } }
  connectivity: { listeners: { backup: 6362 } }
  security:
    cloudIdentity:
      workloadIdentity:
        provider: aws
        annotations:
          eks.amazonaws.com/role-arn: ${ROLE_ARN}
  storage: { volumes: { data: { mode: Dynamic, dynamic: { size: 10Gi } } } }
  auth: { generatePassword: true }
  trust:                          # restore seeds over admin Bolt; opt in to plaintext on a test cluster
    enabled: false
    insecureAdminConnection: true
YAML

kubectl -n "$NS" wait --for=condition=Ready neo4j/${NEO4J} --timeout=600s
kubectl -n "$NS" get sa ${NEO4J}-backup -o jsonpath='{.metadata.annotations}'; echo
# expect: {"eks.amazonaws.com/role-arn":"<your role arn>"}
```

Load some data so restore has something to prove:

```bash
PW=$(kubectl -n "$NS" get secret ${NEO4J}-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d | cut -d/ -f2)
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "UNWIND range(1,1000) AS i CREATE (:Item {i:i});"
```

## 5. Backup to S3

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata: { name: bk-s3 }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  type: Full
  destination:
    type: s3
    url: s3://${BUCKET}/neo4j/          # trailing '/' — neo4j-admin needs a directory
YAML

kubectl -n "$NS" get neo4jbackup bk-s3 -w      # wait for PHASE=Succeeded, then Ctrl-C
```

Verify the artifact landed and the pod assumed the role:

```bash
kubectl -n "$NS" get neo4jbackup bk-s3 -o jsonpath='{.status.phase} {.status.artifacts}{"\n"}'
aws s3 ls s3://${BUCKET}/neo4j/ --recursive
POD=$(kubectl -n "$NS" get pod -l job-name=bk-s3-backup -o name | head -1)
kubectl -n "$NS" get "$POD" -o jsonpath='{.spec.serviceAccountName}{"\n"}'    # backup-demo-backup
kubectl -n "$NS" get "$POD" -o jsonpath='{range .spec.containers[0].env[*]}{.name}{"\n"}{end}' | grep AWS
#   expect AWS_ROLE_ARN, AWS_WEB_IDENTITY_TOKEN_FILE (injected by the EKS pod-identity webhook)
```

If PHASE=Failed, `kubectl -n "$NS" describe neo4jbackup bk-s3` shows the neo4j-admin cause — usually a
missing subject in the trust policy (step 3) or a missing action in the IAM policy (step 2).

## 6. Restore from S3

Restore reads run on the **server** pod (SA `${NEO4J}`) via the target's `cloudIdentity` — already
wired in step 4. Prove it round-trips by deleting data first:

```bash
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) DELETE n;"

cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata: { name: rst-s3 }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  overwrite: true
  source: { backupRef: bk-s3 }
YAML

kubectl -n "$NS" get neo4jrestore rst-s3 -o jsonpath='{.status.phase}{"\n"}' -w   # wait for Succeeded
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "MATCH (n:Item) RETURN count(n) AS items;"       # expect 1000
```

> `RestoreSeedFailed … does not point to a valid location` almost always means the **operand**
> subject `system:serviceaccount:${NS}:${NEO4J}` is missing from the role's trust policy (step 3), or
> the server pod lacks `AWS_*` env (older operator). Check `kubectl -n "$NS" exec ${NEO4J}-server-0 -c
> neo4j -- env | grep AWS`.

## 7. Schedule with retention — watch chains prune from the bucket

`keepLast` is the single source of truth for retention — **no S3 lifecycle rule**. The operator
reclaims bucket storage two ways: aggregate compaction runs `neo4j-admin … --keep-old-backup=false`,
and an `rclone` prune Job (running as `${NEO4J}-backup`, using the same role) purges whole expired
chain prefixes. Demo-fast crons let you watch it in minutes:

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackupSchedule
metadata: { name: sched-s3 }
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
      type: s3
      url: s3://${BUCKET}/scheduled/   # schedule isolates each chain in its own prefix
YAML
```

After ~20 minutes, confirm only the last 3 chains survive — in the records and in the bucket:

```bash
kubectl -n "$NS" get neo4jbackup -l neo4j.com/schedule=sched-s3 \
  -o jsonpath='{range .items[*]}{.status.chain}{"\n"}{end}' | sort -u      # expect 3 chain ids
aws s3 ls s3://${BUCKET}/scheduled/                                        # expect 3 chain prefixes
kubectl -n "$NS" get events --field-selector reason=SchedulePruned    --sort-by=.lastTimestamp | tail -3
kubectl -n "$NS" get events --field-selector reason=ScheduleCompacted --sort-by=.lastTimestamp | tail -3
```

Pause when done:

```bash
kubectl -n "$NS" patch neo4jbackupschedule sched-s3 --type merge -p '{"spec":{"suspend":true}}'
```

## Cleanup

```bash
kubectl -n "$NS" delete neo4jbackupschedule sched-s3 --ignore-not-found
kubectl -n "$NS" delete neo4jbackup -l neo4j.com/schedule=sched-s3 --ignore-not-found
kubectl -n "$NS" delete neo4jrestore rst-s3 --ignore-not-found
kubectl -n "$NS" delete neo4jbackup bk-s3 --ignore-not-found
kubectl -n "$NS" delete neo4j ${NEO4J} --ignore-not-found
aws s3 rb s3://${BUCKET} --force
aws iam detach-role-policy --role-name "$ROLE" --policy-arn "$POLICY_ARN"
aws iam delete-role --role-name "$ROLE"
aws iam delete-policy --policy-arn "$POLICY_ARN"
```

## Static-key alternative (portable, no IRSA)

Skip steps 3–4's IAM role. Create an IAM user with the step-2 policy, then a Secret with its keys and
reference it from each backup's `destination.credentials`. The operator projects the keys as env into
the backup **and** prune Jobs, so the user needs the same `s3:DeleteObject` for retention.

```bash
kubectl -n "$NS" create secret generic aws-backup-creds \
  --from-literal=AWS_ACCESS_KEY_ID=AKIA... \
  --from-literal=AWS_SECRET_ACCESS_KEY=... \
  --from-literal=AWS_DEFAULT_REGION="$REGION"
# For MinIO or another S3-compatible endpoint, also add:
#   --from-literal=AWS_ENDPOINT_URL_S3=http://minio.minio.svc:9000
```

Deploy the `Neo4j` **without** `security.cloudIdentity` (a static-key backup needs nothing on the
instance beyond the backup listener — restore reads, however, still need the target's identity, so
static keys suit backup-only or MinIO testing). Reference the Secret per backup and in the schedule:

```yaml
spec:
  destination:
    type: s3
    url: s3://my-bucket/neo4j/
    credentials: { secretName: aws-backup-creds }
```
