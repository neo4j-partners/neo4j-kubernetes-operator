# BDR-009 — Scale, enable-server, and pool ordinal semantics

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-26 (accepted 2026-06-22 — Option B) |
| **Depends on** | [BDR-002](002-neo4j-crd-topology.md) — `primaries` + fixed `secondaries.analytics` / `secondaries.read`; [BDR-004](004-neo4j-plugin-topology.md) — per-pool plugins |
| **Helm scope** | `neo4j.minimumClusterSize`, `neo4j.operations.enableServer`, `analytics.enabled`, `analytics.type.name` — group **AGG-TOPO-ROLES** |
| **Constraints** | `NEO-2-011` (scale), `NEO-3-011-SRV-01` (enable server); TOPO-009 / TOPO-010; AC-NEO-SCALE; [Neo4j clustering — adding / removing servers](https://neo4j.com/docs/operations-manual/current/clustering/servers/) |

---

## Context

[BDR-002](002-neo4j-crd-topology.md) decided the **static shape** of a `Neo4j` cluster: one CR → **one StatefulSet → N replicas**, with a **fixed ordinal order** `primaries → secondaries.analytics → secondaries.read`. It did **not** decide day-2 **scale** behaviour. This BDR resolves how members are added and removed, how Neo4j `ENABLE SERVER` / `DEALLOCATE` is automated, and whether a server's pool (role) can change.

### How Helm does it today

| Helm mechanism | Day-2 behaviour |
|----------------|-----------------|
| **One release per member** (chart note) | Each cluster member is an independent single-replica StatefulSet; "scale-up" = `helm install` of another release |
| `neo4j.operations.enableServer: true` | A Job runs `ENABLE SERVER` so a freshly installed release joins the formed cluster (`neo4j-operations.yaml`, gated on `$clusterEnabled`) |
| `neo4j.minimumClusterSize` | Formation gate only — not a live member counter |
| `analytics.type.name: primary \| secondary` | Set **per release** at install; role is fixed for that release's lifetime |

Helm therefore never moves an ordinal: each member is its own release, so adding/removing one never disturbs another. The operator's single-STS model **does not have this property**.

### The ordinal-shift problem (core force)

A StatefulSet owns a **contiguous** ordinal range `0..N-1`, and each ordinal binds a stable identity and PVC (`<name>-<ordinal>`). With the fixed layout from BDR-002:

```
[0 .. p-1]            primaries
[p .. p+a-1]          secondaries.analytics
[p+a .. p+a+r-1]      secondaries.read
```

Only the **last** block (`read`) can grow or shrink without renumbering anything. Changing `primaries.members` or `analytics.members` **shifts the ordinals of every pool after it** — meaning an existing pod (and its PVC and Neo4j server identity) would silently change role. Example: `primaries 1→3` turns the old analytics pod (ordinal 1) into a primary. This is data-unsafe and breaks the BDR-002 invariant that ordinal ⇒ role.

So the scale design must answer three coupled questions:

1. **Scale-up / scale-down** — which pools can change size, and how, without unsafe ordinal reassignment?
2. **Enable server** — how is `ENABLE SERVER` (join) and `DEALLOCATE DATABASE` / drain (leave) automated, replacing the Helm operations Job?
3. **Pool reassignment** — can a server move between pools (e.g. `read` → `analytics`), or is role fixed for the member's life as in Helm?

`status.topology.servers` and the `ServersPendingDrain` condition (status.md) already anticipate a drain workflow; this BDR fixes the spec-side contract they reconcile against.

---

## Cross-cutting rules (all options)

| Rule | Rationale |
|------|-----------|
| Primary scale-down that breaks quorum is rejected (TOPO-010) | Neo4j needs a majority of primaries; uncontrolled removal loses the cluster |
| `primaries.members` stays odd (`1` or `≥3`) | Quorum (BDR-002 validation) |
| New members are not "ready" until Neo4j `ENABLE SERVER` succeeds | A pod that is `Running` but not enabled is not a cluster member |
| Removed members are drained (`DEALLOCATE DATABASE`) before PVC deletion | Avoid data loss / under-replication |
| Plugin/role placement (GDS on `analytics` only) survives any scale op | BDR-004 invariants must hold after scaling |

---

## Options under review

### Option A — Single STS, operator-orchestrated ordinal recompute + migration

Keep one StatefulSet. Any pool's `members` may change declaratively; the reconciler recomputes the ordinal layout and **migrates roles** when middle pools change — draining, re-seeding, and re-enabling affected servers in order. The `scale` subresource maps to the aggregate member count.

```yaml
spec:
  topology:
    mode: Cluster
    primaries: { members: 1 }      # edit to 3 → operator reshuffles ordinals 1..N
    secondaries:
      analytics: { members: 1 }
      read:      { members: 2 }
```

| Advantages | Disadvantages |
|------------|---------------|
| Single workload object — matches BDR-002 as-is, no schema change | **Highest reconciler complexity** — must orchestrate multi-pod role migration safely |
| Any pool freely editable in the spec | Middle-pool change forces drain + re-role of downstream pods → long, risky day-2 op |
| Aggregate `scale` subresource is simple to expose | PVC identity reuse across role changes is data-unsafe unless wiped (data loss) |
| One ordinal namespace, simple Service/headless wiring | Hard to make crash-safe / resumable; large blast radius on a typo |

**Helm parity**: behavioural superset — but Helm never reshuffles, so there is no precedent to validate this path against.

### Option B — One StatefulSet per pool — **accepted**

Amend BDR-002's "one STS": the CR fans out to **three StatefulSets** — `<name>-primary`, `<name>-analytics`, `<name>-read` — each with its own ordinal namespace. Each pool grows/shrinks at **its own tail**; no cross-pool shift is ever possible.

```yaml
spec:
  topology:
    mode: Cluster
    primaries: { members: 3 }      # → STS <name>-primary, ordinals 0..2
    secondaries:
      analytics: { members: 1 }    # → STS <name>-analytics, ordinals 0..0
      read:      { members: 5 }    # → STS <name>-read, ordinals 0..4
```

| Advantages | Disadvantages |
|------------|---------------|
| **No ordinal-shift problem** — every pool scales independently and safely | **Amends BDR-002** "one STS" — bigger change to the static model |
| Each pool can use native `kubectl scale sts/<name>-read` | Three workload objects → more Services, PDBs, reconcile branches |
| Per-pool storage class / resources are natural | Aggregate `scale` subresource less obvious (which STS does it target?) |
| Independent rollout / partition per pool (cleaner upgrades) | Server naming changes (`<name>-read-0` vs `<name>-2`) — status + docs update |
| Closest in spirit to Helm's "one workload per role" intent | Cross-pool ordering/quorum logic spread over multiple objects |

**Helm parity**: strongest conceptual parity — Helm already treats each role as a separate workload (release); this collapses N releases into 3 STSs rather than 1.

### Option C — Single STS, tail-only scaling + immutable middle pools

Keep one StatefulSet and the BDR-002 layout, but make day-2 scaling **only** legal on the **tail** (`secondaries.read`). `primaries.members` and `secondaries.analytics.members` are **immutable after creation**; changing them requires an explicit replace/maintenance procedure (and is only safe when `read.members == 0`). The `scale` subresource targets `secondaries.read.members`.

```yaml
spec:
  topology:
    mode: Cluster
    primaries: { members: 3 }      # immutable post-create
    secondaries:
      analytics: { members: 1 }    # immutable post-create
      read:      { members: 2 }    # ← only this is day-2 scalable
```

| Advantages | Disadvantages |
|------------|---------------|
| **Simplest safe reconciler** — only tail append/trim, ordinals never shift | Cannot grow primaries (HA promotion 1→3) without a maintenance/replace flow |
| Single STS — BDR-002 unchanged | Analytics capacity is fixed at create — re-create needed to change |
| Clear, predictable `scale` subresource (read pool) | Immutability errors may surprise users expecting full editability |
| Smallest blast radius for the common case (read scaling) | Pushes primary/analytics resize into a documented break-glass procedure |

**Helm parity**: partial — Helm can resize any role (new release); this restricts day-2 elasticity to read replicas.

### Option D — Hybrid: fixed "core" STS (primaries + analytics) + elastic "read" STS

Two StatefulSets: a **core** STS holding `primaries` then `analytics` (sized at formation, changed only via controlled maintenance), and a separate **read** STS that is freely elastic. Splits the rarely-changed quorum/analytics members from the frequently-scaled read tier without going all the way to per-pool fan-out.

```yaml
spec:
  topology:
    mode: Cluster
    primaries: { members: 3 }      # core STS <name>-core, ordinals 0..2
    secondaries:
      analytics: { members: 1 }    # core STS <name>-core, ordinal 3
      read:      { members: 5 }    # elastic STS <name>-read, ordinals 0..4
```

| Advantages | Disadvantages |
|------------|---------------|
| Read scaling is fully elastic and isolated from quorum members | Still two workload objects (some BDR-002 amend) |
| `primaries`↔`analytics` keep one ordinal namespace (current core wiring) | `primaries 1→3` still shifts analytics ordinal inside the core STS (same as A, smaller scope) |
| Common case (`kubectl scale sts/<name>-read`) is native and safe | Two-tier model is a third mental model to document |
| Smaller change than full per-pool fan-out (Option B) | Core resize still needs the orchestration/maintenance path |

**Helm parity**: moderate — read tier matches read-replica scaling; core resize still a managed operation.

---

## Enable-server & drain automation (applies to chosen option)

Replaces the Helm operations Job (`neo4j.operations.enableServer`) with reconciler-driven automation, regardless of structural option:

| Event | Operator action | Neo4j command |
|-------|-----------------|---------------|
| New member pod becomes `Running` | Run join against the cluster Service | `ENABLE SERVER '<id>'` |
| Member removed from spec (scale-in) | Drain before deletion; set `ServersPendingDrain` | `DEALLOCATE DATABASE ... FROM '<id>'`, then `DROP SERVER` |
| Primary scale-in below quorum | Reject at admission | — (TOPO-010) |

`NEO-3-011-SRV-01` and the `ServersPendingDrain` status condition (status.md) are the contract this satisfies.

## Pool reassignment

All options treat **role as fixed for a member's lifetime** (matches Helm per-release `analytics.type`). Moving a server between pools is expressed as **scale-in of one pool + scale-out of another**, never an in-place relabel. Rationale: PVC/data and plugin set (BDR-004) differ by role; in-place reassignment would reuse storage across incompatible roles.

---

## Comparison

| Criterion | A — single STS, recompute | B — STS per pool | C — tail-only / immutable | D — hybrid core+read |
|-----------|---------------------------|------------------|---------------------------|----------------------|
| Avoids ordinal-shift hazard | ❌ (handled by migration) | ✅ | ✅ | ⚠️ (only in read tier) |
| Read scaling (common case) | ✅ | ✅ | ✅ | ✅ |
| Primary 1→3 promotion day-2 | ⚠️ risky | ✅ | ❌ (recreate) | ⚠️ (core maintenance) |
| Helm parity | superset (unvalidated) | strongest | partial | moderate |
| API minimalism (schema vs BDR-002) | ✅ unchanged | ⚠️ amends | ✅ unchanged | ⚠️ amends |
| Operator complexity | ❌ highest | ⚠️ moderate | ✅ lowest | ⚠️ moderate |
| Breaking/data-loss risk | ❌ high | ✅ low | ✅ low | ⚠️ medium |
| `kubectl scale` ergonomics | aggregate only | per-pool STS | read pool | read STS |

---

## Decision

**Accepted — Option B** — Charles Boudry, 2026-06-22.

**We will implement Option B** — one StatefulSet per pool in Cluster mode:

| Pool | StatefulSet | Ordinals |
|------|-------------|----------|
| `primaries` | `<name>-primary` | `0 .. primaries.members-1` |
| `secondaries.analytics` | `<name>-analytics` | `0 .. analytics.members-1` (if pool present) |
| `secondaries.read` | `<name>-read` | `0 .. read.members-1` (if pool present) |

**Standalone** remains one StatefulSet (`<name>` or `<name>-standalone`), one replica.

1. **Amends [BDR-002](002-neo4j-crd-topology.md)** — replaces "one CR → one STS" with "one CR → up to three pool StatefulSets" in Cluster mode.
2. **Each pool scales at its own tail** — no cross-pool ordinal shift; native `kubectl scale sts/<name>-read` supported.
3. **Role fixed for member lifetime** — pool change = scale-in + scale-out, never in-place relabel.
4. **Enable / drain** — reconciler replaces Helm `neo4j.operations.enableServer` Job (`ENABLE SERVER` / `DEALLOCATE` / `DROP SERVER`).
5. **`scale` subresource (V1)** — targets **`secondaries.read.members`** when read pool exists; otherwise aggregate or per-pool scale via spec edit (document in `spec.md`).

**Rejected:** Option A (ordinal migration — data-unsafe). **Not adopted:** Option C (tail-only), Option D (hybrid) — superseded by B.

---

## Consequences

### Positive

- A clear, data-safe day-2 scale contract that `status.topology.servers` and `ServersPendingDrain` reconcile against.
- **No ordinal-shift hazard** — each pool has its own ordinal namespace and PVC binding.
- Per-pool `kubectl scale` and independent rollouts.
- `ENABLE SERVER` / `DEALLOCATE` automation replaces the Helm operations Job.
- Pool role immutability keeps BDR-004 plugin/placement invariants intact across scaling.

### Negative

- **Amends BDR-002** — three StatefulSets in Cluster mode → more reconcile branches, Services, PDB wiring.
- Server naming: `<name>-<pool>-<ordinal>` (e.g. `prod-read-2`) — status and docs must align.
- Aggregate `scale` subresource is less obvious; V1 pins read-pool scaling.

### Neutral

- `03-variant_matrix.csv` gains scale-direction variants (scale-out read, scale-in with drain, primary promotion).
- ADR may follow for the reconcile ordering of enable/drain and partition handling.

---

## References

- `design/analysis/helm-fields/_index.csv` — rows `neo4j.minimumClusterSize`, `neo4j.operations.enableServer`, `analytics.enabled`, `analytics.type.name` (AGG-TOPO-ROLES)
- `design/analysis/helm-fields/semantic-concern-report.md` — CONCERN-TOPOLOGY, ordinal order primaries → analytics → read
- `design/09-crd-spec/neo4j/spec.md` — StatefulSet replica count, ordinal→pool, `scale` subresource (L539)
- `design/09-crd-spec/neo4j/status.md` — `status.topology.servers`, `ServersPendingDrain`
- `design/09-crd-spec/neo4j/validation.md` — TOPO-009, TOPO-010 (scale-in)
- [Neo4j clustering — adding / removing servers](https://neo4j.com/docs/operations-manual/current/clustering/servers/)
- [BDR-002](002-neo4j-crd-topology.md) — topology model (amended: per-pool STS — [BDR-009](009-scale-pool-ordinal-semantics.md))
- [BDR-004](004-neo4j-plugin-topology.md) — per-pool plugin model
