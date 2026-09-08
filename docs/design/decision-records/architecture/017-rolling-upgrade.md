# ADR-017 — Rolling upgrade of `spec.version`

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-09-01 |
| **Depends on** | [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — one StatefulSet per pool · [ADR-001](001-crd-validation-process.md) · [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-006](006-apply-and-idempotency.md) · [ADR-007](007-formation-and-bolt.md) |
| **Constraints** | Backlog D-08 · FR `NEO-3-012-UPG-01`, `NEO-3-012-UPG-02` · VER-001..003 ([validation.md](../../crd-spec/neo4j/validation.md)) · `status.upgrade` contract ([status.md](../../crd-spec/neo4j/status.md)) · [Neo4j Upgrade and Migration Guide](https://neo4j.com/docs/upgrade-migration-guide/current/) |

---

## Context

### The capability

A `Neo4j` resource pins its Neo4j version as an image tag:

```yaml
spec:
  version: "2026.05.0"
```

This record is about letting the owner edit that field and have the operator move the running deployment onto the new version safely:

```bash
kubectl patch neo4j prod -n prod --type merge -p '{"spec":{"version":"<newer>"}}'
kubectl get neo4j prod -n prod -w
```

Terms used throughout:

| Term | Meaning |
|------|---------|
| **pool** | A group of members with the same role — `primary`, `analytics`, `read`. Each pool is its own StatefulSet ([BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)). |
| **ordinal** | A pod's index inside its StatefulSet: `prod-primary-0`, `prod-primary-1`, … |
| **partition** | A StatefulSet setting. Set it to `N` and Kubernetes updates only pods whose ordinal is `>= N`. Lowering `N` step by step is how a controller paces a rollout by hand. |
| **reconcile pass** | One run of the operator's loop over one resource. The operator watches the objects it owns, so writing to a StatefulSet triggers another pass. |

Out of scope: upgrading the **operator** itself, and the 4.4 → 5.26 move, which Neo4j documents as a migration requiring downtime rather than an upgrade.

### Who actually performs an upgrade

| Actor | Owns |
|-------|------|
| The person editing the resource | The trigger — one field |
| **The operator** | Turning that into a new container image per pool; refusing changes Neo4j does not permit; ordering the pools; gating between members; reporting progress. **It never touches data.** |
| Kubernetes | The restarts — one pod at a time per pool, highest ordinal first |
| **Neo4j, in the container, on start** | The data. The operator invokes no `neo4j-admin` and sets no migration configuration. |

### What Neo4j requires

These are the rules the design has to satisfy. All are from the official [Upgrade and Migration Guide](https://neo4j.com/docs/upgrade-migration-guide/current/) and apply to both the 5.x and 2025–2026 series unless noted.

| # | Rule | Consequence for the operator |
|---|------|------------------------------|
| **R1** | A rolling upgrade is the supported zero-downtime path for a cluster. Standalone always requires downtime. | The approach is sanctioned. Standalone's outage is inherent, not a defect. |
| **R2** | **A health gate between members, and it is specified.** Before touching the next server, two queries must each return zero rows: `SHOW SERVERS YIELD name, hosting, requestedHosting, serverId WHERE requestedHosting <> hosting` and `SHOW DATABASES YIELD name, currentStatus, requestedStatus WHERE currentStatus <> requestedStatus`. After restart, `SHOW SERVERS` must report that server `Enabled` and `Available`. ([in-place rolling](https://neo4j.com/docs/upgrade-migration-guide/current/version-2025-2026/upgrade/in-place-rolling/)) | **Kubernetes readiness does not satisfy this.** Our readiness probe is a TCP check on the Bolt port: it proves the socket is open, not that the server is hosting its databases. A correct roll needs Bolt, not just pod readiness. |
| **R3** | **Ordering is mandatory for analytics clusters**: system-database secondaries must be upgraded *before* the system primary, or the primary upgrades the system database and the secondaries can no longer join. | This applies to us. `render/serverconfig/operator_defaults.go:71` sets `server.cluster.system_database_mode: SECONDARY` on the read and analytics pools — the exact setting Neo4j's rule names. Any cluster we render with a secondary pool is subject to it. |
| **R4** | **Downgrade is not supported, in any form.** The documented recovery from a failed upgrade is a full rollback restoring a backup taken *before* the upgrade. | VER-002 is a blanket refusal with no carve-out. Recovery is a restore, not a version revert. |
| **R5** | **Upgrade paths are constrained.** LTS releases are mandatory checkpoints and cannot be skipped; anything older than 5.26 must reach 5.26 LTS first. Within 2025–2026, any release may go to any later release in one hop. In 5.x, 5.0–5.6 → 5.10–5.15 requires a stop at 5.9. | A forward jump is not automatically legal. Preflight has to know the path rules, not just "newer than current". |
| **R6** | **Discovery v1 is removed in 2025.01.** A cluster on v1 must complete the v1 → v2 transition before any member reaches 2025.01. | A hard gate on any roll crossing into the CalVer series. |
| **R7** | **Database administration must be frozen during the roll** — Neo4j prescribes DENY grants so databases cannot be stopped, created or dropped for the duration. | The operator must not itself issue topology or database changes mid-roll, and should say what it does about third parties doing so. |
| **R8** | **Store format changes are opt-in.** A *minor* store-format bump is automatic and silent at startup; a *major* bump or a format change requires the offline `neo4j-admin database migrate`, per database, and is not reversible. There is no format change between 4.4 and any 2025–2026 version. | For the in-series bumps in scope here, **no store migration happens**. The long-migration fear does not apply to our scope; it applies to format changes, which cannot ride a rolling upgrade at all. |
| **R9** | `SHOW SERVERS` exposes a per-server **`version`** column. | Per-member version is observable today over the existing routed connection. It does not need a per-pod dial or a `status.members[]` writer. |
| **R10** | On Kubernetes, automated worker-node upgrades can evict every Neo4j pod at once. Neo4j documents a **cluster-wide PodDisruptionBudget** as the mitigation, notes the per-release chart PDB is unsuitable, and states it sits outside the Helm lifecycle. | Nobody owns this today. It is a natural operator job and is tracked here as a separate opportunity. |
| **R11** | Plugin version skew is a hard startup gate — an incompatible GDS or APOC prevents Neo4j from starting at all. | Preflight's plugin check is not a nicety. |

### Where we are today

Editing `spec.version` reaches `render.Context.ImageRef()`, and the operator replaces the pod template of **every** pool in the same pass. Kubernetes then restarts all pools simultaneously.

Within one pool the restart is already correct: one pod at a time, highest ordinal first, waiting for readiness. `tests/actions/assert/cluster-config-restart` proves quorum holds on a 3-primary pool through exactly this. What is missing:

- **Nothing refuses an illegal edit.** `spec.version` has no validation beyond "not empty". Downgrades are accepted (R4). Illegal paths are accepted (R5, R6).
- **Pools move together, in the wrong order.** Raft quorum is not at risk — each StatefulSet still rolls one pod at a time — but R3 makes secondaries-before-primary a *requirement*, not a preference, and today the primary pool may finish first.
- **The gate between members is the wrong gate.** Kubernetes readiness releases the next pod; R2 says the release must wait on two Cypher checks that we never run.
- **Nothing reports or bounds the rollout.** `status.upgrade` is published in the CRD and no code reads or writes it. `UpgradePhase` has no Go constants. `status.version` is copied from `spec.version` once every pod is ready, so it reports intent, not reality.

### What the docs already promise, and why it cannot be built as written

[status.md](../../crd-spec/neo4j/status.md) specifies a staged upgrader driven by the StatefulSet **partition**, resuming from `status.upgrade.currentPartition`. Two defects block it, and this record settles both.

**1 — the partition erases itself.** `domain/workload/reconcile.go` assigns the rendered `updateStrategy` onto the live StatefulSet on every update pass, and `render/workload/statefulset.go` never sets that field — so an empty value is written and the API server defaults it back to `partition: 0`. Because the reconciler watches StatefulSets, writing a partition is itself the event that wakes the operator to overwrite it.

**2 — the resume field cannot express the last step.** `UpgradeStatus.CurrentPartition` is `int32` with `omitempty`, so `0` and *unset* serialise identically — and `0` is the final step of a descending walk. Siblings in the same struct already use `*int32`.

[ADR-006](006-apply-and-idempotency.md) also does not authorise writing `spec.updateStrategy` — its per-kind apply table names only `spec.template`, `spec.replicas` and `spec.volumeClaimTemplates`.

### What is still open

The docs answer far more than this record originally assumed, but not everything:

- **Maximum version skew between simultaneous members.** No N-1 bound is stated. Path rules constrain source → target for one *server*; they do not bound how far apart two live members may be.
- **How long a cluster may run mixed-version.** No deadline is given.
- **Whether writes are safe during the mixed-version window.** The 4.x docs advised disabling writes; the 5.x and CalVer pages say nothing.
- **Abort and resume.** Nothing covers a roll interrupted half-way — which members to revert, what to do with those already upgraded.
- **Whether an older binary can open a store a newer one has already bumped to a higher minor format.** This is what would make any in-place reversion viable, and it is unaddressed.

### Interaction with backup and restore (ADR-015 / BDR-014, in review)

- **Restore is the recovery path, and it must predate the upgrade.** R4 makes a pre-upgrade backup the only documented rollback. `disaster-recovery.md` on that branch adds that a restore target's version must be **≥** the backup's — consistent, and it means a backup taken *during* the roll is useless for going back.
- **ADR-015 does not constrain this record.** Its "the upgrade path is a future guarded maintenance-mode flow" line concerns `system`/whole-cluster DR restore, not a version change.
- **Both need the same health signal.** ADR-015 waits for formation stability before restoring; R2 is the same family of check. One shared helper.
- **Concurrency.** `Preflight` refuses a version change while a `Neo4jRestore` is active. The converse belongs in ADR-015: a restore arriving mid-roll should wait, not fail.

---

## Analysis

### Option A — Pace the roll ourselves and gate on R2

Hold each member until the two Cypher checks pass, then release the next. Mechanically this is either a StatefulSet partition walked down per ordinal, or `OnDelete` with explicit pod deletion.

| Advantages | Disadvantages |
|------------|---------------|
| Implements R2, the gate Neo4j actually prescribes | Requires both defects fixed and an ADR-006 amendment before anything works |
| Implements R3 ordering precisely, per member rather than per pool | One `int32` cursor cannot describe three pools |
| Stops a bad member from releasing the next one | Largest new surface, on the highest-blast-radius path |

### Option B — `OnDelete` and delete pods ourselves

| Advantages | Disadvantages |
|------------|---------------|
| Total control over order and pacing | The operator inherits **every** restart, including the configuration and TLS paths that work today |
| No partition semantics | Adds a second routine pod deletion beside `RecycleMemberStore` |

### Option C — Guard the edit, order the pools, report the rollout (chosen for the first release)

Refuse what Neo4j forbids (R4, R5, R6, R11). Hold the primary pool until the secondary pools have converged (R3). Report progress from the StatefulSets and from `SHOW SERVERS.version` (R9). Leave the intra-pool pacing to Kubernetes.

| Advantages | Disadvantages |
|------------|---------------|
| The refusals and the ordering are the two rules that are *mandatory*; they ship first | **Does not implement R2.** Between members inside a pool, Kubernetes readiness is the gate, and it is weaker than what Neo4j prescribes |
| The rollout under test is the one `cluster-config-restart` already proves | An unhealthy member is discovered only after the next has begun restarting |
| Needs neither the ADR-006 amendment nor a partition cursor | Per-pool ordering satisfies R3 at pool granularity, not per member |

---

## Comparison

| Criterion | A Paced + R2 gate | B OnDelete | C Guard, order, report |
|-----------|-------------------|------------|------------------------|
| Testability | Medium — a new pacing path to assert | Poor — every restart becomes ours to prove | **Best** — reuses the existing restart assertion |
| Complexity | Medium — cursor, per-pool resume, apply-contract change | High | **Low** |
| controller-runtime fit | Fights the current apply until field ownership is settled | Replaces a controller behaviour with our own | **Best** — no apply-contract change |
| Satisfies R2 (health gate) | **Yes** | Yes | No — Kubernetes readiness only |
| Satisfies R3, R4, R5, R6, R11 | Yes | Yes | **Yes** |
| First-release fit | Its prerequisites are C | No | **Yes** |

---

## Decision

We will implement **Option C first, and Option A as the completion of this record** — not as a deferred alternative.

The honest framing: C does not satisfy R2. Kubernetes readiness is a weaker gate than the two Cypher checks Neo4j prescribes. C is chosen first because its parts — the refusals, the ordering, the status writer, a Bolt-side health read — are exactly the prerequisites A needs, and because the refusals and the ordering are the rules that are *mandatory* where the pacing gate is a matter of degree. Shipping C leaves an operator that refuses what Neo4j forbids and orders pools correctly; shipping nothing leaves one that does neither.

**VER-002 is a blanket downgrade refusal with no carve-out** (R4). Recovery from a failed upgrade is restoring a pre-upgrade backup, not reverting the version. It is enforced in the webhook *and* the reconciler, extending the [ADR-001](001-crd-validation-process.md) table: the validating webhook ships disabled by default, so a webhook-only rule protects almost nobody. It is deliberately not CEL — a root-level rule keeps evaluating on the finalizer-removal update, so a rule false for an already-persisted object would make the resource impossible to delete.

**Preflight enforces the path rules** (R5, R6), not merely "newer than current". `spec.version` is CalVer with an optional fourth `LTS` component, so the comparison parses CalVer, not SemVer — VER-001's "semver-compatible tag" wording in `validation.md` needs correcting with it.

### Implementation notes

- **Package `internal/domain/upgrade`** — not the `domain/maintenance` name reserved by [ADR-003](003-neo4j-reconcile-pipeline.md); `maintenance` already means `spec.maintenance.offlineMode` in the user-facing spec.
- **Three placements**, because a refusal running after the `workload` step is useless — by then the pod template has already been replaced:
  - **Refusal** at the top of `runPipeline`, beside the existing `validation.ValidateNeo4j` call.
  - **The pool hold** inside the `workload` pool loop.
  - **The status writer** after `formation`, where the cluster view exists.
- **Condition `Upgrading`**, gate `GateNone` — [status.md](../../crd-spec/neo4j/status.md) requires `status.phase` to stay `Running` during an upgrade, so it narrates without blocking `Ready`.
- **Widen `SHOW SERVERS`** to yield `version`, `health` and `hosting`/`requestedHosting` (R2, R9). One change on the existing routed connection in [ADR-007](007-formation-and-bolt.md)'s client, and it unlocks both the health gate and per-member version reporting.
- **Order of work:** (0) fix both defects and correct `status.md`; (1) the refusals; (2) the status writer, and stop `status.version` echoing the spec; (3) pool ordering per R3; (4) the R2 health gate, which completes Option A.

```go
// internal/domain/upgrade — refusal at pipeline entry, hold in workload, writer after formation.

// Preflight refuses what Neo4j does not permit: a downgrade (R4), a path that skips an LTS or a
// required intermediate (R5), a roll into 2025.01+ from discovery v1 (R6), an incompatible plugin
// (R11), a digest-pinned image where the version would never be pulled, offline mode, an active
// Neo4jRestore, or a cluster already mid-roll.
// It compares spec.version against status.version, parsed as CalVer. No separately pinned field is
// needed: once status.version reports what is running, it IS the anchor, and an empty value means
// "first install", not "downgrade".
// Returns a sentinel error so status.PipelineErrorReason maps it to a catalogued reason.
func Preflight(n *v1beta1.Neo4j) error

// Observe builds status.upgrade from the pool StatefulSets and SHOW SERVERS.version.
func Observe(n *v1beta1.Neo4j, pools map[render.PoolID]*appsv1.StatefulSet, servers []neo4j.Server, now time.Time) *v1beta1.UpgradeStatus

// HoldPoolTemplate keeps the primary pool on its current template until every secondary pool has
// converged (R3). Consulted by the workload pool loop.
func HoldPoolTemplate(n *v1beta1.Neo4j, pool render.PoolID) bool

// ClusterStable is the R2 gate: both documented queries return zero rows. Shared with ADR-015,
// which needs the same signal before a restore.
func ClusterStable(ctx context.Context, admin neo4j.Admin) (bool, string, error)
```

### Separate opportunity — the cluster-wide PodDisruptionBudget

R10 describes a real gap that nobody owns: node auto-upgrades can evict every Neo4j pod at once, the per-release chart PDB does not help, and the mitigation sits outside the Helm lifecycle. Our PDB is opt-in and spans all pools with one `minAvailable`. Reconciling a correct cluster-wide budget — Neo4j suggests `maxUnavailable: 1`, which adapts as the cluster scales — is a natural operator job. It is **not** part of this record: a PDB constrains evictions, not our own rollout, so it protects a different failure. Tracked separately.

---

## Consequences

### Positive

- The operator refuses what Neo4j forbids, instead of attempting it.
- Pool ordering satisfies a mandatory Neo4j rule (R3) that we were previously treating as a preference.
- `status.upgrade` and `status.version` become truthful, and per-member versions are readable from `SHOW SERVERS` without new plumbing.
- Both schema corrections are free while nothing writes those fields.
- Stage 4 completes R2 rather than being an optional refinement, so the record has a defined end state.

### Negative

- Until stage 4, the gate between members inside a pool is weaker than Neo4j prescribes. This is a known, stated shortfall, not an oversight.
- Deviates from the partition design published in `status.md`, which must be corrected.
- Holding a pool freezes its configuration changes too, not only its image.
- VER-002 is checked in two layers, which ADR-001 discourages in general.
- Preflight has to carry Neo4j's version-path rules, which change as new LTS releases land — a maintenance cost with no obvious source of truth in-repo.

### Neutral

- R7 (freezing database administration) is honoured only for the operator's own actions. Whether to issue Neo4j's prescribed DENY grants on the user's behalf is left open.
- Numbered 017 because `015` and `016` are claimed by in-flight branches.
- Stage 0 changes the CRD schema, and the CRD is deliberately not bundled in the operator Helm chart. A release carrying this feature needs the separate `kubectl apply --server-side` step, or the operator writes status fields the API server silently strips.
- Neo4j's own Kubernetes model differs from ours: one Helm release and one single-replica StatefulSet per member, upgraded one release at a time. We use one StatefulSet per pool with N replicas and let Kubernetes pace within it. R2 and R3 apply either way.

---

## References

- [Neo4j Upgrade and Migration Guide](https://neo4j.com/docs/upgrade-migration-guide/current/) — R1, R4, R5
- [In-place rolling upgrade (2025–2026)](https://neo4j.com/docs/upgrade-migration-guide/current/version-2025-2026/upgrade/in-place-rolling/) — R2, R3, R7
- [Upgrade 2025–2026](https://neo4j.com/docs/upgrade-migration-guide/current/version-2025-2026/) — R5, R6
- Contract: [status.md](../../crd-spec/neo4j/status.md) §`status.upgrade` · [validation.md](../../crd-spec/neo4j/validation.md) VER-001..003
- ADRs: [ADR-001](001-crd-validation-process.md) · [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-004](004-status-and-conditions.md) · [ADR-006](006-apply-and-idempotency.md) · [ADR-007](007-formation-and-bolt.md)
- Backlog D-08 "Version upgrade: image bump strategy" · I-07 "Upgrade / migration test fixtures"
- Current gap, user-facing: [09-operations.md](../../../user-guide/03-neo4j/09-operations.md) §Version changes
- Existing restart proof: `tests/actions/assert/cluster-config-restart`
- FR `NEO-3-012-UPG-01`, `NEO-3-012-UPG-02` — traced from [image.tag](../../analysis/helm-fields/fields/image.tag.md); the requirement text is not in this repository.
