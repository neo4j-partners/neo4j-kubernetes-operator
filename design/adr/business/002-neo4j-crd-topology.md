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
| Production HA (writes) | 3 core members | Quorum, odd core count, fault tolerance |
| **Primary + analytics** | 1 writer + 1 analytics/read secondary | **Two roles** — not “2 equal cluster members” |
| Read scaling | 3 cores + N read replicas | Core count **and** replica count |
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
| Easy code path — one StatefulSet, `replicas: servers` | Cannot express **core vs read replica** — Neo4j roles collapsed |
| Similar mental model to `spec.replicas` on a Deployment | No validation hook for “odd cores ≥ 3 for production HA” |
| | **Misleading for primary + analytics** — looks like a 2-node HA cluster |
| | Hides `minimumClusterSize` vs current size (scale / enable-server) |
| | Poor fit for Enterprise read replicas (different config and routing) |
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
| Separates “cluster vs not” in the API | Conflates **cores** and **read replicas** into one counter |
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
| Direct mapping in `11-helm-mapping.md` | `analytics.type: primary/secondary` ≠ Neo4j **read replica** terminology |
| Preserves `minimumClusterSize` + `enableServer` semantics | `replicas` on CR duplicates StatefulSet spec — drift risk |
| Field-level docs can reference existing chart docs | Does not encode **cores vs read replicas** unless `analytics` is overloaded |
| | Three knobs (`minimumClusterSize`, `replicas`, `analytics`) for one concept |
| | Weak domain language for support and Enterprise causal cluster docs |

**Helm parity**: strongest naming parity; weakest long-term API clarity.

---

### Option D — `mode` + role composition (`cores` + `readReplicas`) — **proposed**

Domain-aligned model: deployment mode **and** Neo4j role counts. Helm mapped **into** this shape via `11-helm-mapping.md`, not as CR field names.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: orders
spec:
  edition: enterprise
  topology:
    mode: Cluster
    cores:
      members: 1
    readReplicas:
      members: 1              # analytics / read scaling
    minimumMembers: 1         # Helm minimumClusterSize — formation gate
```

**Standalone** shorthand:

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
    cores:
      members: 3
    readReplicas:
      members: 0
    minimumMembers: 3
```

| Advantages | Disadvantages |
|------------|---------------|
| **Primary + analytics explicit** — `cores: 1`, `readReplicas: 1` | More fields than Options A and B |
| **`mode` + roles** — answers both “cluster formation?” and “primary vs secondary?” | Users must learn cores vs read replicas (support runbook) |
| Validates Neo4j rules (odd cores, Enterprise for read replicas) | `Standalone` vs `Cluster` with `cores: 1` — need clear validation rules |
| HA warnings without blocking dev (`cores < 3` → status warning) | New vocabulary for Helm-only users (migration guide required) |
| Matches Neo4j causal cluster model | `neo4j/spec.md` + validation are the hardest part of V1 |
| Single coherent model for one CR + one StatefulSet | Differs from “one Helm release per member” mental model |

**Helm mapping** (translation table in `11-helm-mapping.md`):

| Helm | Operator `spec.topology` |
|------|--------------------------|
| `minimumClusterSize: 1`, no analytics | `mode: Standalone` |
| analytics primary + N secondaries (N Helm releases → 1 CR) | `mode: Cluster`, `cores: 1`, `readReplicas: N` |
| `minimumClusterSize: 3` | `mode: Cluster`, `cores: 3`, `minimumMembers: 3` |
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
| `production-ha` | 3 cores, 0 read replicas |
| `primary-plus-analytics` | 1 core, 1 read replica |
| `ha-with-read-replicas` | 3 cores, N read replicas (N in sub-field?) |

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
│
├─ Production fault tolerance (writes)
│    spec.topology.mode: Cluster
│    spec.topology.cores.members: 3        # odd, ≥ 3
│    spec.topology.readReplicas.members: 0
│
├─ Primary + analytics / read secondary (NOT production HA for writes)
│    spec.topology.mode: Cluster
│    spec.topology.cores.members: 1
│    spec.topology.readReplicas.members: 1
│    ⚠ status warns: NonHA — cores < 3
│
└─ HA writes + read scaling
     spec.topology.mode: Cluster
     spec.topology.cores.members: 3
     spec.topology.readReplicas.members: N
```

Optional **documentation profiles** (samples / `00-vision.md`) may mirror Option E presets; CRD `profile` field deferred to V1.1+.

---

## Decision

**We will adopt Option D** — `mode: Standalone | Cluster` plus **`cores.members`** and **`readReplicas.members`** in cluster mode.

- **Option C** informs **`11-helm-mapping.md`** (translation table), not CR field names.
- **Option E** may appear later as **optional shortcuts** that expand to Option D counts — not a replacement for explicit GitOps specs in V1.

Options **A** and **B** are **rejected** — ambiguous for primary + secondary (`servers: 2` / `members: 2`). Option **E alone** is **rejected** for V1 — too opaque for production GitOps.

### Validation and guidance rules

| Rule | Severity | Message (example) |
|------|----------|-------------------|
| `mode: Standalone` → no `readReplicas` | Error | Read replicas require `mode: Cluster` |
| `cores.members` even and > 0 | Error | Core count must be odd for quorum |
| `cores.members: 1` + `readReplicas ≥ 1` | Warning | Non-HA topology — not for production writes |
| `cores.members < 3` (no production label) | Warning | For HA production use `cores.members ≥ 3` |
| `readReplicas.members > 0` + Community edition | Error | Read replicas require Enterprise |
| Scale-in below formed cluster | Error | Unsupported scale-in — explicit procedure required |

Warnings → **`status.conditions`** (`Type: TopologyWarning`, `Reason: NonHA`).

### V1 scope

| In V1 | Deferred |
|-------|----------|
| `mode: Standalone` | **Option E** — `topology.profile` CRD enum (docs-only presets in samples first) |
| `mode: Cluster`, `cores.members` (1 or 3+) | Separate `analytics` **role** distinct from `readReplica` |
| `readReplicas.members: 0` in P0 tests | Multi-zone `multiCluster` networking variant |
| Validation errors + HA warnings | Auto-correction from guessed user intent |
| Spec design for `cores: 1` + `readReplicas: 1` | Full E2E for analytics topology (prioritise in `13-v1-scope-lock.md`) |

---

## Consequences

### Positive

- **Primary + analytics** is explicit — not a misuse of `members: 2`.
- Actionable guidance via validation and status warnings.
- Helm `minimumClusterSize`, `analytics`, and `enableServer` map through one translation table.
- BDR-001 unchanged — one CRD, one `neo4jRef`.

### Negative

- Topology is the most sensitive section of `neo4j/spec.md` and `neo4j/validation.md`.
- Support must understand cores vs read replicas (runbook + decision tree).
- Migration docs must explain unified CR vs multi-release Helm.

### Neutral

- `03-variant_matrix.csv` — add variants: read replica, primary + analytics, core sizes 1/3/N.
- `domain/workload` branches on `mode`; `domain/formation` handles core vs replica paths.
