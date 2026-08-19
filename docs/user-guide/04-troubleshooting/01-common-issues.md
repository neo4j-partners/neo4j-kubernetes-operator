# Common issues

Symptom-driven fixes for the failures people actually hit, from installing the operator to scaling a
cluster. If you only have a condition reason and want to know what it means, start from the
[error reference](../05-reference/errors.md); if you need to read the operator's log, see
[Operator logs](02-operator-logs.md).

## CRD apply fails: metadata.annotations too long

**Symptom:** `CustomResourceDefinition "neo4js.neo4j.com" is invalid: metadata.annotations: Too long: may not be more than 262144 bytes`

**Cause:** Plain `kubectl apply -f config/crd/bases/neo4j.com_neo4js.yaml` (or `kubectl apply -k config/crd`) uses client-side apply. Kubernetes stores the entire manifest in `kubectl.kubernetes.io/last-applied-configuration`, and the Neo4j CRD OpenAPI schema is ~1.5 MB — above the 256 KiB annotation limit.

**Fix:** Use server-side apply via `make install`:

```bash
make install
# equivalent:
kubectl apply --server-side --force-conflicts -f config/crd/bases/neo4j.com_neo4js.yaml
```

If a previous failed apply left a broken CRD object, delete it first (only when no Neo4j workloads depend on it):

```bash
kubectl delete crd neo4js.neo4j.com --ignore-not-found
make install
```

## CRD not found when applying Neo4j

**Symptom:** `no matches for kind "Neo4j" in version "neo4j.com/v1beta1"`

**Fix:** Install the CRD first:

```bash
make install
kubectl get crd neo4js.neo4j.com
```

## Operator pod not starting

**Symptom:** `neo4j-operator-controller-manager` stays `CrashLoopBackOff` or `Pending`

**Checks:**

```bash
kubectl describe pod -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager
```

Common causes:

- Image `controller:latest` not present on nodes — run `make docker-build` and load into kind, or use `make run` locally.
- RBAC not applied — re-run `kubectl apply -k config/rbac`.
- **Tainted nodes** — if the node pool uses taints (e.g. `dedicated=neo4j:NoSchedule`), the manager must tolerate them. Edit `config/manager/manager.yaml` (`spec.template.spec.tolerations` / optional `nodeSelector`), then `kubectl apply -k config/manager`. Events will show `untolerated taint ...`. Match the same keys as `Neo4j.spec.scheduling.tolerations`.

## Neo4j CR accepted but nothing happens

**Checks:**

```bash
kubectl get neo4j -A
kubectl describe neo4j dev -n default
kubectl get sts,svc,secret,pvc -n default -l app.kubernetes.io/instance=dev
```

- Confirm the operator pod is `Running`.
- Check `status.conditions` for `Error` or `Ready=False` messages.
- For Cluster mode, confirm pool member counts and BYO `spec.trust` Secrets if TLS is enabled.

## PVC stays Pending

**Symptom:** Pod `Pending`, PVC `Pending`

**Check:** `StorageReady=False` on the Neo4j CR — the condition message names the PVC and
`storageClassName` (or that a default StorageClass is missing):

```bash
kubectl get neo4j <name> -o jsonpath='{.status.conditions[?(@.type=="StorageReady")]}{"\n"}'
```

**Fix:**

- Ensure a StorageClass exists and is default, or set `spec.storage.volumes.data.dynamic.storageClassName`.
- On kind, install a local path provisioner or use the default standard StorageClass.
- `kubectl describe pvc <name>` for the provisioner/event detail behind the Pending phase.

## Auth Secret / password

When `spec.auth.generatePassword: true`, the operator creates `{metadata.name}-auth`
(and labels it `neo4j.com/mountable-by-operator=true`):

```bash
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d
```

See [Your first Neo4j](../01-getting-started/first-neo4j.md#5-connect).

## Secret mount / TLS rejected: missing mountable label or items

**Symptom:** Webhook denies the CR, or reconcile fails with `Error=True` /
`reason=SecretNotMountable` (plus a `Warning` Event under the same reason) and nothing is deployed.

**Cause:** NEO-005 — the operator only mounts Secrets the namespace owner opted in, and only
named keys (`items`).

**Fix:**

```bash
kubectl label secret <name> neo4j.com/mountable-by-operator=true
```

Ensure `spec.storage.secretMounts.*.items` (and `trustedCerts.sources[].secret.items`) list
each key. Details: [examples/secrets/README.md](../../../examples/secrets/README.md#mountable-secrets-neo-005).
Why the label exists at all:
[Security](../03-neo4j/05-security.md#why-the-operator-requires-opt-in-labels).

## Scale-in stuck despite setting neo4j.com/drain-ok

**Symptom:** STS does not shrink after you annotate `neo4j.com/drain-ok` on the Neo4j CR.

**Cause:** ADD-02 — drain confirmation is operator-owned `status.drainOK` (+ matching `status.drainOKGeneration`). CR annotations are ignored.

**Check:**

```bash
kubectl get neo4j <name> -o jsonpath='{.status.drainOK}{" gen="}{.status.drainOKGeneration}{" crGen="}{.metadata.generation}{"\n"}'
kubectl get neo4j <name> -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\n"}{end}'
```

Wait for `ServersPendingDrain` to clear after formation finishes `DEALLOCATE`/`DROP`. Do not forge the annotation.

## BYO auth Secret rejected: not delegated (ADD-01)

**Symptom:** `Error=True` / `reason=SecretNotDelegated` (plus a `Warning` Event under the same
reason), mentioning `neo4j.com/allowed-for` or “auth secret … is not delegated”.

**Cause:** `passwordSecretRef` Secrets must be delegated to this CR name (in addition to the
mountable label). Operator-generated `{name}-auth` Secrets are already instance-scoped. An auth
Secret is read by the operator to dial admin Bolt, not just mounted — see
[Security](../03-neo4j/05-security.md#the-second-label) for the reasoning.

**Fix:**

```bash
kubectl label secret <auth-secret> neo4j.com/allowed-for=<neo4j-cr-name>
```

`connectivity.clusterDomain` does not change where the operator dials Bolt; it only affects
Neo4j-advertised DNS.

## Scale-out ENABLE fails: server deallocated or dropped

**Symptom:** Operator log / `Error` condition contains
`can't be enabled because it has been deallocated or dropped`, and `SHOW SERVERS` shows the
new ordinal’s address still in state `Dropped`.

**Cause:** That Neo4j server UUID was `DROP`ped on a prior scale-in. The pod remounted the
**same** data store — typical with **Existing** PVCs, or **Dynamic** under
`whenScaled: Retain` before the operator’s heal recycle runs (or on an older operator
that gated recycle).

**Fix:**

- Redeploy an operator that recycles Dropped Dynamic stores on ENABLE failure (heal path).
  It deletes that ordinal’s pod+PVC so STS recreates an empty volume and a new UUID.
- Immediate unblock without waiting for a build: delete the stale ordinal PVCs and pods
  (e.g. `data-<name>-primary-3`, `data-<name>-primary-4`), then let the STS recreate them.
- For elastic pools that should wipe on scale-in (no retained disks), set
  `storage.volumeClaimRetention.whenScaled: Delete`.
- With **Existing** claims, wipe or replace the volume data (or bind a fresh claim) for that
  ordinal, then delete the pod. The operator will not delete `Existing.claimName` PVCs.

Details: [Scaling members](../03-neo4j/02-clustering.md#scaling-members).

## Scale-in stuck: UnsupportedSinglePrimary / multiple primaries to one primary

**Symptom:** `ServersPendingDrain` reason `UnsupportedSinglePrimary`, or Neo4j error
`Can't go from multiple primaries to one primary`.

**Cause:** Neo4j forbids `ALTER DATABASE SET TOPOLOGY` from a multi-primary topology to
**1** primary. The operator will not drain further in that case.

**Fix:** Set `topology.primaries.members` back to an odd count ≥ 3. Scaling primaries down to 1 is
not supported — recreate if needed.

## Scale-out stuck: UnsupportedSystemScaleUp (1 → N primaries)

**Symptom:** `ClusterFormed` reason `UnsupportedSystemScaleUp` after raising `primaries.members`
from 1.

**Cause:** A single system primary cannot grow via `ENABLE SERVER` alone. The operator does
not automate Neo4j single-to-cluster dump/load. Deploying at 1 primary is fine; changing
primary count is not.

**Fix:** Set `topology.primaries.members` back to 1, or recreate the resource at the target primary
count (typically 3). Scaling analytics/read secondaries only is supported.

## Cluster never forms, `BootstrapGateTooHigh`

**Symptom:** `ClusterFormed=False` with reason `BootstrapGateTooHigh`, all pods `Running` but `0/1`,
nothing answers on Bolt.

**Cause:** `topology.minimumMembers` was created above `primaries.members`. Neo4j waits for primaries
that will never exist, so the `system` database is never created. The validating webhook rejects this
at admission when it is enabled; without it the resource is accepted and the operator reports the
condition instead.

**Fix:** The field is immutable, so recreate the resource with a gate that fits the pool — or simply
omit it and let the operator derive `1` or `3`. A gate left *above* the pool by a later scale-in is a
different, harmless case: the operator caps its quorum check and the cluster stays formed.

## Members roll while scaling

**Symptom:** Primaries restart one after another during a `primaries.members` change, and new members
sit waiting for a Raft snapshot.

**Cause:** Something changed `neo4j.conf`, not the scale itself. A scale alone leaves the rendered
configuration byte-identical — including the
[system bootstrap gate](../03-neo4j/02-clustering.md#the-system-bootstrap-gate), which the operator
keeps at a fixed value for that very reason — so the config checksum does not move and no pod is
recreated. A roll means the patch carried something else, typically `spec.config`, `spec.version` or a
listener change.

**Fix:** Compare the ConfigMap before and after, and split the patch: scale on its own, configuration
changes on their own.

## Cluster with secondaries never Ready (Bolt refused)

**Symptom:** Fresh `primaries.members: 3` plus analytics/read secondaries — all pods `Running`
but `0/1`, Bolt refused, debug.log shows `SELECTED_BOOTSTRAPPER_OTHER` with empty
`raftMemberIdSet` on every primary.

**Cause:** Primaries were discovering secondary internals during system Raft bootstrap.
Operator labels primary internals `neo4j.com/clustering=true` and scopes primary discovery
to that label (Helm parity).

**Fix:** Redeploy an operator that includes that discovery fix, delete the Neo4j CR and
Dynamic PVCs, recreate. Do not reuse poisoned data volumes from a failed bootstrap.

## Ready condition false

Wait for StatefulSet rollout and PVC binding:

```bash
kubectl rollout status statefulset/dev-server -n default
kubectl get neo4j dev -n default -o jsonpath='{.status.conditions[?(@.type=="Ready")]}'
```

Status fields and their meaning: [API reference](../05-reference/api.md#status).

Every condition reason: [Error reference](../05-reference/errors.md).
Operator log levels and the optional log file: [Operator logs](02-operator-logs.md).
