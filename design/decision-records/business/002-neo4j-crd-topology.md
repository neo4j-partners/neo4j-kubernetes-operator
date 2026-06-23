# BDR-002 — `Neo4j` CRD topology model and user guidance

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-18 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) |
| **Constraints** | `NEO-001`, `NEO-002`, `NEO-011`; Helm parity — [`helm_neo4j_values.yaml`](../../analysis/helm_neo4j_values.yaml) |

---

## Context

[BDR-001](001-single-neo4j-crd.md) chose **one `Neo4j` CRD**. That resolves the *kind* question. This BDR resolves the **topology composition** question: how users express single-node, HA cluster, and **primary + secondary** layouts (read scaling or analytics) in one API.

### The problem — primary + secondary

Not every deployment is “one server” or “symmetric N-member HA”. Common real-world intents:

| User intent | Example | What the API must express |
|-------------|---------|---------------------------|
| Dev / CI | 1 server | Single writer, no cluster |
| Production HA (writes) | 3 primary members | Quorum, odd primary count, fault tolerance |
| **Primary + analytics** | 1 writer + 1 analytics/read secondary | **Two roles** — not “2 equal cluster members” |
| Read scaling | 3 cores + N secondarys | Primary count **and** replica count |
| Scale out after install | Add member post-deploy | Initial formation size vs current size (`enableServer`) |

The Helm chart today handles this through **several mechanisms at once**, not a single `mode` field:

| Helm mechanism | Purpose |
|----------------|---------|
| `neo4j.minimumClusterSize` | `1` = standalone formation; `≥3` = clustered formation |
| StatefulSet replica count | Current number of pods (may exceed minimum) |
| `neo4j.operations.enableServer` | Enable a server added **outside** initial minimum size |
| `analytics.enabled` + `analytics.type.name: primary \| secondary` | **1 primary + n secondary** multi-instance scenario (ports + config) |
| **One Helm release per cluster member** (chart note L732–735) | Each member = separate release, single-replica StatefulSet |

The operator will **not** replicate “one release per member”. It uses **one `Neo4j` CR → one StatefulSet → N replicas**. Topology fields must therefore express intent clearly in that unified model.

Customers need **guided choices** (use case → valid spec), not ambiguous counters.

---

## Options under review

Five structural options. Each includes a sketch, advantages, and disadvantages.

---

### Option A — Flat server count only (`servers: 1` or `servers: N`)

No `mode`, no roles — only a single integer.

```yaml
spec:
  topology:
    servers: 1          # dev
    # servers: 3        # production HA
    # servers: 2        # primary + secondary? symmetric cluster? unclear
```

| Advantages | Disadvantages |
|------------|---------------|
| Minimal API surface — one field to learn | **`servers: 2` is ambiguous** — invalid HA quorum, or 1+1 analytics? |
| Easy code path — one StatefulSet, `replicas: servers` | Cannot express **core vs secondary** — Neo4j roles collapsed |
| Similar mental model to `spec.replicas` on a Deployment | No validation hook for “odd cores ≥ 3 for production HA” |
| | **Misleading for primary + analytics** — looks like a 2-node HA cluster |
| | Hides `minimumClusterSize` vs current size (scale / enable-server) |
| | Poor fit for Enterprise secondarys (different config and routing) |
| | Docs must overload prose (“when servers=2, you probably meant…”) |

**Helm parity**: partial — maps to replica count only; loses `analytics.type`, `minimumClusterSize`, and enable-server semantics.

---

### Option B — `mode: Standalone | Cluster` + flat `members`

Explicit deployment mode; member count without role split.

```yaml
spec:
  topology:
    mode: Standalone       # single server — members implied or absent
    # --- or ---
    mode: Cluster
    members: 3             # symmetric HA cluster
    # members: 2           # primary + analytics? 2-node quorum? unclear
    minimumMembers: 3      # optional — initial formation size
```

| Advantages | Disadvantages |
|------------|---------------|
| Clear dev path — `Standalone` = no cluster formation | **`members: 2` is ambiguous** — same problem as Option A |
| Separates “cluster vs not” in the API | Conflates **cores** and **secondarys** into one counter |
| Matches BDR-001 initial sketch (`mode` field) | Cannot validate Neo4j quorum rules without overloading `members` |
| Simpler OpenAPI than role blocks | **Primary + analytics** needs undocumented convention or labels |
| | Scale-out (`minimumMembers` vs `members`) adds a second counter anyway |
| | Does not answer “mode **or** role?” — mode alone is insufficient for secondary servers |

**Helm parity**: partial — `mode` maps loosely to `minimumClusterSize`; loses `analytics.type` and read-replica semantics.

---

### Option C — Stay close to the Helm chart

Mirror Helm field names and semantics on the CR.

```yaml
spec:
  topology:
    minimumClusterSize: 1       # neo4j.minimumClusterSize
    replicas: 2                   # StatefulSet size
  operations:
    enableServer: true            # neo4j.operations.enableServer
  analytics:
    enabled: true
    type: primary                 # analytics.type.name — primary | secondary
```

**Important**: In Helm, **primary + secondary is often two separate releases** (each with `analytics.type` and `minimumClusterSize: 1`). The operator uses **one CR → one StatefulSet → N replicas** — not a literal multi-release copy.

| Advantages | Disadvantages |
|------------|---------------|
| **Lowest migration friction** for Helm users — familiar names | Helm **multi-release** model does not map 1:1 to one CR |
| Direct mapping in `11-helm-mapping.md` | `analytics.type: primary/secondary` ≠ Neo4j **secondary** terminology |
| Preserves `minimumClusterSize` + `enableServer` semantics | `replicas` on CR duplicates StatefulSet spec — drift risk |
| Field-level docs can reference existing chart docs | Does not encode **cores vs secondarys** unless `analytics` is overloaded |
| | Three knobs (`minimumClusterSize`, `replicas`, `analytics`) for one concept |
| | Weak domain language for support and Enterprise causal cluster docs |

**Helm parity**: strongest naming parity; weakest long-term API clarity.

---

### Option D — `mode` + role composition (`cores` + `secondaries` + `analyticsSecondaries`) — **proposed**

Domain-aligned model: deployment mode **and** Neo4j role counts. Helm mapped **into** this shape via `11-helm-mapping.md`, not as CR field names.

- **`cores`** — causal cluster primary members (writers / quorum).
- **`secondaries`** — Enterprise secondarys attached to the core cluster (read scaling).
- **`analyticsSecondaries`** — analytics / GDS secondaries (Helm `analytics.type: secondary`); distinct from causal-cluster read scaling.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: orders
spec:
  edition: enterprise
  topology:
    mode: Cluster
    primaries:      members: 1
    analyticsSecondaries:
      members: 1              # analytics / GDS secondary — Helm analytics secondary
    minimumMembers: 1         # Helm minimumClusterSize — formation gate
```

**Standalone** — mode only; **no `members` fields**:

```yaml
spec:
  topology:
    mode: Standalone
```

**Production HA**:

```yaml
spec:
  topology:
    mode: Cluster
    primaries:      members: 3
    secondaries:
      members: 0
    analyticsSecondaries:
      members: 0
    minimumMembers: 3
```

| Advantages | Disadvantages |
|------------|---------------|
| **Primary + analytics explicit** — `cores: 1`, `analyticsSecondaries: 1` | More fields than Options A and B |
| **`secondaries` vs `analyticsSecondaries`** — separates read scaling from analytics/GDS | Users must learn three role counters (support runbook) |
| **`mode` + roles** — answers both “cluster formation?” and “primary vs secondary?” | `neo4j/spec.md` + validation are the hardest part of V1 |
| Validates Neo4j rules (odd cores, Enterprise for secondarys) | New vocabulary for Helm-only users (migration guide required) |
| HA warnings without blocking dev (`cores < 3` → status warning) | Differs from “one Helm release per member” mental model |
| Matches Neo4j causal cluster + analytics deployment models | |
| Single coherent model for one CR + one StatefulSet | |

**Helm mapping** (translation table in `11-helm-mapping.md`):

| Helm | Operator `spec.topology` |
|------|--------------------------|
| `minimumClusterSize: 1`, no analytics | `mode: Standalone` (no `members` fields) |
| analytics primary + N secondaries (N Helm releases → 1 CR) | `mode: Cluster`, `primaries.members: 1`, `analyticsSecondaries.members: N` |
| `minimumClusterSize: 3` | `mode: Cluster`, `primaries.members: 3`, `minimumMembers: 3` |
| Secondary scaling on HA cluster | `mode: Cluster`, `primaries.members: 3`, `secondaries.members: N` |
| SS replicas > minimumClusterSize | scale / `enableServer` flow (`NEO-011`) |
| `operations.enableServer: true` | Operator enable-server job |

---

### Option E — Named topology profiles (presets only)

User selects a validated preset; operator expands to concrete topology — raw counts optional or absent.

```yaml
spec:
  topology:
    profile: primary-plus-analytics
    # profile: production-ha
    # profile: standalone-dev
    # profile: ha-with-read-replicas
```

| Preset | Expands to (conceptually) |
|--------|---------------------------|
| `standalone-dev` | 1 server, no cluster |
| `production-ha` | 3 cores, 0 secondarys |
| `primary-plus-analytics` | 1 core, 1 analytics secondary |
| `ha-with-read-replicas` | 3 cores, N secondarys (N in `secondaries.members`) |

| Advantages | Disadvantages |
|------------|---------------|
| **Lowest friction for beginners** — use case → one enum | **Indirection** — GitOps users must read preset definitions |
| Encodes validated combinations (including primary + analytics) | Profile catalog must be **maintained** across Neo4j versions |
| Good for docs, wizards, and `samples/` | Advanced / custom topologies need escape hatch (counts or new profiles) |
| Reduces invalid specs at admission time | Duplication if both `profile` and raw counts exist |
| | Hides actual topology in `kubectl get -o yaml` unless status echoes expansion |
| | “Profile vs counts” — third mental model if combined with Option D fields |

**Helm parity**: indirect — presets documented as expansion rules in `11-helm-mapping.md`.

---

## Comparison matrix

| Criterion | A — `servers: N` | B — mode + members | C — Helm-like | D — mode + roles | E — profiles |
|-----------|------------------|--------------------|-----------------|------------------|--------------|
| Primary + analytics explicit | ❌ | ❌ | ⚠️ via `analytics.type` | ✅ | ✅ (if preset exists) |
| Production HA (3 cores) | ⚠️ | ✅ | ✅ | ✅ | ✅ (if preset exists) |
| Simple dev / standalone | ✅ | ✅ | ✅ | ✅ | ✅ |
| Neo4j quorum validation | ❌ | ⚠️ | ⚠️ | ✅ | ✅ (baked into presets) |
| Helm migration clarity | ❌ | ⚠️ | ✅ | ⚠️ (mapping doc) | ❌ |
| Domain / support clarity | ❌ | ⚠️ | ❌ | ✅ | ⚠️ |
| GitOps / explicit counts | ✅ | ✅ | ✅ | ✅ | ❌ |
| API minimalism | ✅ | ✅ | ❌ | ⚠️ | ✅ |
| One CR + one StatefulSet | ✅ | ✅ | ⚠️ | ✅ | ✅ |

---

## User decision guide (Option D)

```
What do you need?
│
├─ Single server (dev, test, CI)
│    spec.topology.mode: Standalone
│    (no cores / secondaries / analyticsSecondaries / minimumMembers)
│
├─ Production fault tolerance (writes)
│    spec.topology.mode: Cluster
│    spec.topology.primaries.members: 3        # odd, ≥ 3
│    spec.topology.secondaries.members: 0
│    spec.topology.analyticsSecondaries.members: 0
│
├─ Primary + analytics / GDS secondary (NOT production HA for writes)
│    spec.topology.mode: Cluster
│    spec.topology.primaries.members: 1
│    spec.topology.analyticsSecondaries.members: 1
│    ⚠ status warns: NonHA — cores < 3
│
└─ HA writes + read scaling
     spec.topology.mode: Cluster
     spec.topology.primaries.members: 3
     spec.topology.secondaries.members: N
     spec.topology.analyticsSecondaries.members: 0   # optional; GDS secondaries are separate
```

Optional **documentation profiles** (samples / `00-vision.md`) may mirror Option E presets; CRD `profile` field deferred to V1.1+.

---

## Decision

**We will adopt Option D** — `mode: Standalone | Cluster` plus, in cluster mode, **`primaries.members`** and optional **`secondaries[]`**.

> **Amendment 2026-06-22** — fixed counters `secondaries.members` / `analyticsSecondaries.members` replaced by named **`secondaries[]`**. Plugin model → [BDR-004](004-neo4j-plugin-topology.md) (Option E).
>
> **Amendment 2026-06-22 (terminology)** — `cores` → **`primaries`**; `replicaPools[]` → **`secondaries[]`**; removed **`serverRole`**; field name **`secondaries[]`** aligns with Neo4j Primary/Secondary vocabulary.

- **`Standalone`**: `mode` only — no `primaries`, `secondaries`, or `minimumMembers`. Plugins: flat `spec.plugins[]`.
- **`Cluster`**: `primaries.members` **required**; optional `secondaries[]`. Each secondary pool: `name`, `members`, optional `plugins[]`.

```yaml
topology:
  mode: Cluster
  primaries:
    members: 3
  secondaries:
    - name: read-scale
      members: 2
    - name: analytics
      members: 1
  minimumMembers: 3
# plugins: see BDR-004 (Option E)
#   pluginDefinitions: { gds: { licenseSecretRef: ... }, bloom: { ... } }
#   primaries.plugins: [apoc]
#   secondaries[].plugins: [apoc] | [gds, bloom]
```

**StatefulSet ordinals** (declaration order):

```
[0 .. primaries-1]                              → primary
[pool₀ start .. pool₀ start + members - 1]      → secondaries[0]
[pool₁ start ..]                                → secondaries[1]
…
```

- **Option C** informs **`11-helm-mapping.md`** (translation table), not CR field names.
- **Option E** may appear later as **optional shortcuts** that expand to Option D counts — not a replacement for explicit GitOps specs in V1.

Options **A** and **B** are **rejected** — ambiguous for primary + secondary (`servers: 2` / `members: 2`). Option **E alone** is **rejected** for V1 — too opaque for production GitOps.

**Rejected (superseded by amendment):** fixed fields `secondaries.members` / `analyticsSecondaries.members` — conflate topology with plugin intent (GDS implied by field name).

### Validation and guidance rules

| Rule | Severity | Message (example) |
|------|----------|-------------------|
| `mode: Standalone` + `primaries` / `secondaries` / `minimumMembers` | Error | `members` fields are not allowed when `mode` is `Standalone` |
| `mode: Cluster` without `primaries.members` | Error | `primaries.members` is required when `mode` is `Cluster` |
| `secondaries` without `primaries` | Error | `primaries.members` must be set before secondary pools |
| `secondaries[].members > 0` when `mode: Standalone` | Error | Replica pools require `mode: Cluster` |
| `secondaries[].name` not unique | Error | duplicate secondary pool name |
| `gds` / `bloom` in `primaries.plugins` | Error | GDS/Bloom forbidden on primaries |
| `primaries.members` even and > 0 | Error | Primary count must be odd for quorum |
| `primaries.members: 1` + any secondary pool | Warning | Non-HA topology — not for production writes |
| `primaries.members < 3` (no production label) | Warning | For HA production use `primaries.members ≥ 3` |
| `secondaries` on Community edition | Error | Secondary pools require Enterprise edition |
| `secondaries[]` with `gds` in `plugins` without analytics-capable config | Error | GDS on secondary pool requires analytics server configuration |
| Scale-in below formed cluster | Error | Unsupported scale-in — explicit procedure required |

Warnings → **`status.conditions`** (`Type: TopologyWarning`, `Reason: NonHA`).

### V1 scope

| In V1 | Deferred |
|-------|----------|
| `mode: Standalone` — no member fields | **Option E** — `topology.profile` presets |
| `mode: Cluster`, `primaries.members` (1 or 3+) | Multi-zone `multiCluster` networking variant |
| `secondaries[]` — named secondary pools + per-pool plugins (BDR-004) | Auto-correction from guessed user intent |
| Validation errors + HA warnings | |
| Named pools + per-pool plugins (BDR-004) | Full E2E for all pool × plugin combos |

---

## Consequences

### Positive

- **Primary + analytics** — explicit pool (e.g. `gds-bloom`) with inline `plugins: [gds, bloom]`.
- **Named pools** — topology sizing and plugins colocated; no separate plugin map.
- Actionable guidance via validation and status warnings.
- Helm `minimumClusterSize`, `analytics`, and `enableServer` map through one translation table.
- BDR-001 unchanged — one CRD, one `neo4jRef`.

### Negative

- Topology is the most sensitive section of `neo4j/spec.md` and `neo4j/validation.md`.
- Support must understand primaries vs secondaries vs GDS placement (runbook + decision tree).
- Migration docs must explain unified CR vs multi-release Helm.

### Neutral

- `03-variant_matrix.csv` — add variants: secondary, primary + analytics (analyticsSecondaries), core sizes 1/3/N.
- `domain/workload` branches on `mode`; `domain/formation` handles core vs replica paths.
