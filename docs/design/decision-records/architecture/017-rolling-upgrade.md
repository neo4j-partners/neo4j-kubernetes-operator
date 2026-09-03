# ADR-017 — Rolling upgrade of `spec.version`

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-09-01 |
| **Depends on** | [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — one StatefulSet per pool · [ADR-001](001-crd-validation-process.md) · [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-006](006-apply-and-idempotency.md) |
| **Constraints** | Backlog D-08 · FR `NEO-3-012-UPG-01`, `NEO-3-012-UPG-02` · VER-001..003 ([validation.md](../../crd-spec/neo4j/validation.md)) · `status.upgrade` contract ([status.md](../../crd-spec/neo4j/status.md)) |

---

## Context

### The capability

A `Neo4j` resource pins its Neo4j version as an image tag:

```yaml
spec:
  version: "2026.05.0"
```

Sooner or later the owner of that resource needs a newer Neo4j. The capability this record is about is letting them edit that one field and have the operator move the running deployment onto the new version safely:

```bash
kubectl patch neo4j prod -n prod --type merge -p '{"spec":{"version":"<newer>"}}'
kubectl get neo4j prod -n prod -w
```

What "safely" has to mean for a database:

- **The cluster keeps serving.** A Neo4j cluster elects a leader by Raft, so a majority of primaries must stay up. Restarting two of three primaries at once stops writes across the whole cluster. Members must go down one at a time, and the next one may only start after the previous is back and has rejoined.
- **Clients are not stranded.** Clients connected over `neo4j://` follow the routing table around whichever member is currently down, so a correctly paced upgrade is not an outage for a cluster. It *is* an outage for Standalone — one pod, one restart — and that is inherent, not a defect.
- **A restart is not quick, and how long it takes is not something we know.** The probe defaults are deliberately generous *ceilings*, not expected times: the startup probe allows 1000 failures at 5s (about 83 minutes) before the kubelet gives up, and the shutdown grace period defaults to 3600s because Neo4j checkpoints on the way down. What a member actually takes is store-dependent and is measured nowhere in this repository. The design consequence is what matters: the operator must not assume a member comes back quickly, and any step deadline has to be derived from the *effective* probe configuration rather than a constant — an override replaces a probe wholesale, and the repo's own example ships a 90-second startup budget.
- **Bad changes are refused, not attempted.** Some version edits cannot be performed safely at all — a downgrade, or a change on a resource whose image is pinned by digest so the new version would never actually be pulled. Those must fail at admission rather than half-roll a production cluster.
- **Progress is visible.** The resource must report that an upgrade is in flight, how far it has got, and when it is done, so a human can watch it and automation can wait on it.

Some terms this record uses throughout:

| Term | Meaning |
|------|---------|
| **pool** | A group of members with the same role — `primary`, `analytics`, `read`. Each pool is its own StatefulSet ([BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)). |
| **ordinal** | A pod's index inside its StatefulSet: `prod-primary-0`, `prod-primary-1`, … |
| **partition** | A StatefulSet setting. Set it to `N` and Kubernetes updates only pods whose ordinal is `>= N`, leaving lower ones on the old template. Lowering `N` step by step is how a controller paces a rollout by hand. |
| **reconcile pass** | One run of the operator's loop over one resource. The operator also watches the objects it owns, so writing to a StatefulSet triggers another pass. |

Out of scope: upgrading the **operator** itself, backup and restore, and any data migration that is not performed by Neo4j on start.

### Who actually performs an upgrade

Four actors, and the operator is the smallest of them. Being precise about this is what sizes the work:

| Actor | Owns |
|-------|------|
| The person editing the resource | The trigger — one field |
| **The operator** | Turning that into a new container image per pool; refusing changes it cannot perform; ordering the pools; reporting progress. **It never touches data.** |
| Kubernetes | The restarts themselves — one pod at a time per pool, highest ordinal first, waiting for readiness |
| **Neo4j, in the container, on start** | Everything that happens to the *data*: store format, system database. The operator invokes no `neo4j-admin`, sets no container `Command` on the normal path, and sets no migration configuration. |

So the operator manages upgrading the *deployment*. The database half is delegated wholesale to the Neo4j image — unconfigured by us and unverified afterwards. The only lever we hold over it is accidental: if a store migration outlasts the startup probe budget, the kubelet kills the container mid-migration.

This is also why Option A below collapses. Its advantage was pausing to verify each member — but we cannot verify a migration we neither control nor configure.

### Where we are today

Editing `spec.version` today does something, but nothing that resembles the above. The new version reaches `render.Context.ImageRef()`, which builds the container image reference, and the operator replaces the pod template of **every** pool in the same pass. Kubernetes then restarts all three pools simultaneously.

Within a single pool that restart is already correct: Kubernetes takes one pod at a time, highest ordinal first, and waits for each to become ready. The end-to-end test at `tests/actions/assert/cluster-config-restart` changes configuration on a running 3-primary cluster and asserts that never more than one member is un-ready, that the cluster re-forms afterwards, and that the change reached every member. A version change rides that same machinery.

So the intra-pool restart is not the problem. What is missing is everything around it:

- **Nothing refuses a bad edit.** `spec.version` has no validation beyond "not empty". Downgrades are accepted. VER-002 and VER-003 exist only on paper.
- **All pools move at once.** Note carefully what this does *not* break: each pool's StatefulSet still rolls one pod at a time, so Raft quorum among the primaries is never at risk from the rollout itself. What is lost is the chance to move the members that *cannot* affect quorum first — the read and analytics pools — and so discover that the new image does not come up before any Raft voter has been touched. Cross-pool ordering is a **canary, not a quorum fix**.
- **Nothing reports or bounds the rollout.** `status.upgrade` is published in the CRD and no code reads or writes it. `UpgradePhase` has no Go constants at all. `status.version` is copied from `spec.version` the moment every pod is ready, so it reports intent, not what is running.

### What the docs already promise, and why it cannot be built as written

[status.md](../../crd-spec/neo4j/status.md) specifies a staged upgrader: the operator sets the StatefulSet **partition** and walks it down one ordinal per pass, verifying each member before releasing the next, and resumes from `status.upgrade.currentPartition` if the operator restarts mid-upgrade. Two defects block that design, and this record must settle both before any code is written.

**1 — the partition erases itself.** `domain/workload/reconcile.go` assigns the rendered `updateStrategy` onto the live StatefulSet on every update pass, and `render/workload/statefulset.go` never sets that field, so what gets assigned is an empty value that the API server defaults back to `partition: 0`. Because the reconciler watches StatefulSets, writing a partition is itself the event that wakes the operator up to overwrite it. The mechanism is undone within seconds of being used, not on operator restart.

**2 — the resume field cannot express the last step.** `UpgradeStatus.CurrentPartition` is an `int32` with `omitempty`, so `0` and *unset* serialise identically — and `0` is precisely the final step of a descending walk, the pass that updates ordinal 0. An operator restarting at that point cannot tell "finished" from "never started". Sibling fields in the same status struct already use `*int32` to avoid exactly this.

Separately, [ADR-006](006-apply-and-idempotency.md) does not authorise writing `spec.updateStrategy` at all — its per-kind apply table names only `spec.template`, `spec.replicas` and `spec.volumeClaimTemplates` for StatefulSet. Any partition-based option needs that table amended.

### What we cannot know yet

Nothing in this repository states Neo4j's own upgrade rules: whether a cluster running mixed versions is supported at all and for which jumps, how much version skew is allowed, when a store-format migration is triggered and whether it can be undone, or whether the system-database leader must be restarted last. The operator cannot currently identify the leader in any case — it reads `SHOW SERVERS` with name, address and state only.

This matters for the choice below, because the main thing a paced rollout buys is the chance to *check something* between members, and we cannot yet write down what that check is.

Finally, the blast radius: the operand is a production database. A wrong choice restarts every member of a cluster with no way to stop part-way.

### Interaction with backup and restore (ADR-015 / BDR-014, in review)

Backup and restore is being designed in parallel. Four points of contact, settled here so neither record has to guess:

- **Restore is forward-only, and that bounds recovery.** `docs/design/backup-restore/disaster-recovery.md` (on the in-review branch) states that the target's Neo4j version must be **≥** the backup's. It is the only version-compatibility rule written down anywhere in this repository. Two consequences: it *confirms* backup-and-reload as the path for major versions, since that restores into a newer target; and it *limits* recovery from a failed upgrade, because a backup taken during or after the roll can never be restored into the version we came from. In-place reversion to `status.upgrade.previousVersion` is therefore the only recovery for a partial upgrade — and whether the store is still readable by the old version is one of the unknowns above.
- **ADR-015 does not constrain this record.** Its line about "the upgrade path is a future guarded maintenance-mode flow" concerns `system`/whole-cluster DR restore, not a Neo4j version change; "upgrade path" there is this repo's `ponytail:` idiom for how a deliberate shortcut gets replaced later. Read in context with BDR-014 it carries no requirement that version upgrades run through offline maintenance.
- **Both need the same "formation-stable" signal.** ADR-015 waits for every member enabled, quorum present and `ClusterFormed` before touching a database; this record's `Verifying` step wants exactly that. One shared helper, not two.
- **Concurrency is nobody's decision yet, so it is made here.** `Preflight` refuses a version change while a `Neo4jRestore` is active. The converse belongs in ADR-015: a restore that arrives mid-roll should wait rather than fail, since its formation-stable gate is legitimately false while a member is restarting and would otherwise time out into `RestoreBeforeFormation`.

---

## Analysis

### Option A — Walk a partition down (the design in `status.md`)

Render emits the partition from `status.upgrade.currentPartition`; the operator lowers it one ordinal per pass and verifies the member that just restarted before releasing the next.

| Advantages | Disadvantages |
|------------|---------------|
| Kubernetes still performs the restart — the operator only sets a stopping point | Both defects above must be fixed, plus an ADR-006 amendment, before a single line does anything |
| Gives a real pause and per-member check between ordinals | That check is the whole point, and it cannot be specified from what we know |
| The cursor is a natural resume point across operator restarts | One `int32` cursor cannot describe three pools rolling |

### Option B — Switch to `OnDelete` and delete pods ourselves

Kubernetes stops rolling pods on template change; the operator deletes each pod explicitly, in whatever order it likes.

| Advantages | Disadvantages |
|------------|---------------|
| Total control over order, including leader-last if that turns out to be required | The operator inherits **every** restart, including the configuration and TLS-rotation paths that work correctly today |
| No partition semantics to reason about | Adds a second routine pod deletion beside `RecycleMemberStore`, widening a destructive surface on the hot path |

### Option C — Guard the edit and report the rollout (chosen)

Leave the restart exactly as Kubernetes performs it. Refuse version changes that cannot be carried out safely, hold the primary pool until the secondary pools have converged, and derive `status.upgrade` from what the StatefulSets report.

| Advantages | Disadvantages |
|------------|---------------|
| The rollout under test is the one `cluster-config-restart` already proves keeps quorum | No pause inside a pool — an unhealthy member is noticed only after the next one has begun restarting |
| The part that prevents damage today — the refusal — ships without touching the rollout at all | Deviates from the published design, so `status.md` must be corrected rather than left standing |
| Requires neither the ADR-006 amendment nor a per-pod Bolt connection | Per-member version reporting waits until `status.members[]` has a writer |

---

## Comparison

| Criterion | A Partition | B OnDelete | C Guard and report |
|-----------|-------------|------------|--------------------|
| Testability | Medium — a new pacing path to assert | Poor — every restart in the operator becomes ours to prove | **Best** — reuses the existing restart assertion |
| Complexity | Medium — cursor, per-pool resume, apply-contract change | High | **Low** |
| controller-runtime fit | Fights the current apply until field ownership is settled | Replaces a controller behaviour with our own | **Best** — no apply-contract change |
| Can be specified today | **No** — its verification step needs rules we do not have | No | **Yes** |
| V1 fit | Deferred | No | **Yes** |

---

## Decision

We will implement **Option C**, and keep **Option A** as a later amendment to this record rather than a rejected alternative.

The reasoning is that Kubernetes already performs a correct one-at-a-time restart within a pool, and we have a passing end-to-end test that says so. What is genuinely missing today is a refusal in front of an unsafe edit and a truthful report of what is happening. Option A's advantage over that is the ability to pause and check each member — and the check cannot be written down until Neo4j's own upgrade rules are documented somewhere. Building the pacing before the check exists buys machinery with nothing to put in it.

**VER-002 (downgrade refused) is enforced in the webhook *and* in the reconciler**, extending the [ADR-001](001-crd-validation-process.md) ownership table rather than duplicating it. The validating webhook ships disabled by default in the chart, so a webhook-only rule would protect almost nobody; `imagepolicy` already sets the precedent of checking on both paths. It is deliberately **not** a CEL rule: root-level CEL rules keep evaluating on the update that removes a finalizer, so a rule that is false for an already-persisted object would make the resource impossible to delete.

**VER-002 must permit reverting to `status.upgrade.previousVersion`.** A blanket downgrade refusal would make a half-rolled upgrade unrecoverable, because putting the old version back is the only way out of a `Failed` upgrade. The refusal applies to downgrades *other than* the version the in-flight upgrade started from.

Scope is patch and minor version changes. Major versions stay on the documented backup-and-reload path until the questions in *What we cannot know yet* are answered; that answer also gates cross-pool ordering and any later move to Option A.

### Implementation notes

- **Package `internal/domain/upgrade`** — not the `domain/maintenance` name reserved by [ADR-003](003-neo4j-reconcile-pipeline.md), because `maintenance` already means `spec.maintenance.offlineMode` in the user-facing spec and reusing it would collide in both code and docs.
- **The three parts run in three different places**, because a refusal that runs after the `workload` step is useless — by then the pod template has already been replaced:
  - **Refusal** runs at the top of `runPipeline`, in the block that already re-runs the stateless admission checks (`validation.ValidateNeo4j`) before any domain step. It is a pure function of the resource and needs no cluster access.
  - **The pool hold** is consulted inside the `workload` pool loop, mid-pipeline.
  - **The status writer** runs after `formation`, which is where the cluster view exists.
- **Condition `Upgrading`**, declared in the oracle catalog with a gate of `GateNone` — meaning it narrates progress without blocking `Ready`. [status.md](../../crd-spec/neo4j/status.md) requires `status.phase` to stay `Running` during an upgrade, so the condition must not gate readiness.
- **Order of work.** Fix both defects and correct `status.md` first, while the fields are still dead and changing them is free. Then the refusals. Then the `status.upgrade` writer, which also stops `status.version` echoing the spec. Cross-pool ordering last, since it depends on the open Neo4j questions.

```go
// internal/domain/upgrade — pipeline step after formation.

// Preflight refuses a version change the operator cannot carry out safely (VER-002, VER-003):
// a downgrade, a digest-pinned image where the new version would never be pulled, offline mode,
// or a plugins volume whose JARs can never be refreshed for the new version.
// It compares spec.version against status.version. No separately pinned field is needed: once
// status.version reports what is running rather than echoing the spec, it IS the anchor, and an
// empty status.version means "first install", not "downgrade".
// Returns a sentinel error so status.PipelineErrorReason maps it to a catalogued reason.
func Preflight(n *v1beta1.Neo4j) error

// Observe builds status.upgrade from the pool StatefulSets alone — no Bolt connection needed.
// It reads updatedReplicas, currentRevision and updateRevision, none of which the operator
// reads anywhere today.
func Observe(n *v1beta1.Neo4j, pools map[render.PoolID]*appsv1.StatefulSet, now time.Time) *v1beta1.UpgradeStatus

// HoldPoolTemplate answers "should this pool keep its current pods for now?", and is consulted
// by the workload step's pool loop so the primaries wait for the secondary pools to converge.
// A held pool keeps its live pod template untouched.
func HoldPoolTemplate(n *v1beta1.Neo4j, pool render.PoolID) bool
```

---

## Consequences

### Positive

- The refusal — the only part that prevents damage today — ships without touching the rollout.
- No new pacing path to prove: the rollout under test is the one already shown to keep quorum.
- `status.upgrade` and `status.version` become truthful ahead of the state machine, which is what `kubectl wait` and support runbooks need first.
- Both schema corrections are free while nothing writes those fields, and stop being free the moment something does.

### Negative

- Deviates from the partition design published in `status.md`, which must be corrected rather than left standing as aspirational.
- No pause inside a pool: a member that comes up unhealthy is discovered only after the next one has started restarting.
- Holding a pool freezes its configuration changes too, not only its image. That is the intended reading of "this pool is frozen", but it is a real restriction.
- VER-002 is checked in two layers, which ADR-001 discourages in general.

### Neutral

- Option A is deferred, not rejected — amend this record once the upgrade reporting gives evidence that a pause is needed.
- Numbered 017 because `015` and `016` are claimed by in-flight branches.
- Stage 0 changes the CRD schema, and the CRD is deliberately **not** bundled in the operator Helm chart. A release carrying this feature needs the separate `kubectl apply --server-side` step; without it the operator writes status fields the API server silently strips.

---

## References

- Contract: [status.md](../../crd-spec/neo4j/status.md) §`status.upgrade` · [validation.md](../../crd-spec/neo4j/validation.md) VER-001..003
- ADRs: [ADR-001](001-crd-validation-process.md) · [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-004](004-status-and-conditions.md) · [ADR-006](006-apply-and-idempotency.md) · [ADR-007](007-formation-and-bolt.md)
- Backlog D-08 "Version upgrade: image bump strategy" · I-07 "Upgrade / migration test fixtures"
- Current gap, user-facing: [09-operations.md](../../../user-guide/03-neo4j/09-operations.md) §Version changes
- Existing restart proof: `tests/actions/assert/cluster-config-restart`
- FR `NEO-3-012-UPG-01`, `NEO-3-012-UPG-02` — traced from [image.tag](../../analysis/helm-fields/fields/image.tag.md); the requirement text is not in this repository.
