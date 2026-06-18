# BDR-002 — `Neo4j` CRD topology model and user guidance

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-18 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) |
| **Constraints** | `NEO-001`, `NEO-002`, `NEO-011`; Helm `neo4j.minimumClusterSize`, `neo4j.operations.enableServer` |

---

## Context

[BDR-001](001-single-neo4j-crd.md) chose **one `Neo4j` CRD** with `spec.topology.mode: Standalone | Cluster`. That resolves the *kind* question but not the full **topology composition** question.

Real deployments include cases that are neither pure standalone nor a symmetric HA cluster:

| User intent | Example | Why `mode` + `members` alone fails |
|-------------|---------|-------------------------------------|
| Dev / single node | 1 server | Maps to `Standalone` — OK |
| Production HA | 3 core members | Maps to `Cluster`, `members: 3` — OK |
| **Primary + analytics** | 1 core + 1 read/analytics secondary | **Not** standalone; **not** `members: 2` symmetric HA |
| Read scaling | 3 cores + N read replicas | Needs **role** distinction, not one counter |
| Scale out after install | Add member via enable-server | Needs `minimumClusterSize` vs current size semantics (Helm parity) |

The Helm chart expresses this via `neo4j.minimumClusterSize` (default 1 = standalone), StatefulSet replica count, and `neo4j.operations.enableServer` for members added outside the initial size — not via separate CRDs ([`helm_neo4j_values.yaml`](../../analysis/helm_neo4j_values.yaml) lines 26–39).

Customers need **guided choices** (use case → valid spec), not just raw fields.

---

## Analysis

### Axis 1 only — `mode` + flat `members`

```yaml
spec:
  topology:
    mode: Cluster
    members: 2    # primary + analytics?
```

| Advantages | Disadvantages |
|------------|---------------|
| Minimal API | Cannot express **roles** (core vs read replica vs analytics) |
| Easy to document | `members: 2` implies symmetric cluster — **misleading** for 1+1 analytics |
| | Cannot enforce Neo4j quorum rules (cores must be odd, ≥3 for HA) |
| | Hides production vs dev intent |

---

### Axis 1 + role composition — `cores` + `readReplicas` (recommended)

```yaml
spec:
  topology:
    mode: Cluster
    cores:
      members: 1
    readReplicas:
      members: 1    # analytics / read scaling
```

| Advantages | Disadvantages |
|------------|---------------|
| Matches Neo4j causal cluster model (cores + optional read replicas) | Slightly more complex than flat `members` |
| Makes **1+1 analytics** explicit and validatable | Requires Enterprise for read replicas |
| Enables HA warnings (`cores.members < 3`) without blocking dev | Two counters to explain in docs |
| Aligns with scale-out (`NEO-011`) and `enableServer` semantics | Analytics-specific tuning may need `spec.config` in addition |

---

### Named profiles — `topology.profile` or `topologyProfileRef`

```yaml
spec:
  topology:
    profile: primary-plus-analytics
```

| Advantages | Disadvantages |
|------------|---------------|
| Lowest friction for beginners | Indirection — advanced users must read profile definitions |
| Encodes validated combinations | Profile catalog must be maintained |
| Good for docs and `samples/` | Duplicates fields if profile and raw counts both exist |

**Recommendation**: profiles as **optional shortcuts** that expand to `cores` / `readReplicas` — not a replacement for explicit fields.

---

### `spec.intent` enum — `dev` | `production` | `analytics`

| Advantages | Disadvantages |
|------------|---------------|
| User declares goal; operator suggests/fixes topology | Overlaps with `mode` + roles — third mental model |
| Good for admission warnings | Easy to disagree with operator suggestions |

**Recommendation**: use **`status` warnings**, not a required `intent` field. Optional `metadata.labels` for platform teams if needed.

---

### User decision guide (by use case)

```
What do you need?
│
├─ Single server (dev, test, CI)
│    spec.topology.mode: Standalone
│
├─ Production fault tolerance (writes)
│    spec.topology.mode: Cluster
│    spec.topology.cores.members: 3   # odd, ≥3
│    spec.topology.readReplicas.members: 0
│
├─ Primary + analytics / read secondary (NOT production HA)
│    spec.topology.mode: Cluster
│    spec.topology.cores.members: 1
│    spec.topology.readReplicas.members: 1
│    ⚠ status warns: not HA — cores < 3
│
└─ HA writes + read scaling
     spec.topology.mode: Cluster
     spec.topology.cores.members: 3
     spec.topology.readReplicas.members: N
```

---

### Helm chart mapping (reference)

| Helm value | Operator `spec.topology` |
|------------|--------------------------|
| `minimumClusterSize: 1` (default) | `mode: Standalone` **or** `Cluster` with `cores.members: 1`, `readReplicas: 0` |
| `minimumClusterSize: 3` | `mode: Cluster`, `cores.members: 3` |
| StatefulSet replicas > minimumClusterSize | `cores.members` + scale / `enableServer` flow (`NEO-011`) |
| `operations.enableServer: true` | Operator enables servers added outside initial core count |
| Per-member Helm release (chart note L732–735) | **Not** replicated — operator uses **one** `Neo4j` CR + one StatefulSet with N replicas |

---

## Decision

We will model Neo4j topology on **two axes** inside the single `Neo4j` CRD:

### Axis 1 — deployment mode

| `spec.topology.mode` | Meaning |
|----------------------|---------|
| `Standalone` | Single Neo4j server; no cluster formation |
| `Cluster` | Causal cluster; requires `cores` (and optionally `readReplicas`) |

### Axis 2 — role composition (when `mode: Cluster`)

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
      members: 1              # primary / quorum voters
    readReplicas:
      members: 1              # analytics or read scaling
    minimumMembers: 1         # maps Helm minimumClusterSize — initial formation size
  persistence: { ... }
  connectivity: { ... }
  trust: { ... }
  config: { ... }
```

**Standalone** shorthand (no role block):

```yaml
spec:
  topology:
    mode: Standalone
  # cores / readReplicas must be absent or ignored
```

### Validation and guidance rules

| Rule | Severity | Message (example) |
|------|----------|-------------------|
| `mode: Standalone` → no `readReplicas` | Error | Read replicas require `mode: Cluster` |
| `cores.members` even and > 0 | Error | Core count should be odd for quorum |
| `cores.members: 1` + `readReplicas ≥ 1` | Warning | Non-HA topology — not for production writes |
| `cores.members < 3` and production label absent | Warning | For HA production use `cores.members ≥ 3` |
| `readReplicas.members > 0` + `edition: community` | Error | Read replicas require Enterprise |
| `cores.members` decreased below formed cluster | Error | Scale-in must be explicit / supported path |

Warnings surface in **`status.conditions`** (`Type: TopologyWarning`, `Reason: NonHA`) — visible without reading operator logs.

### Optional profiles (V1 docs + samples; CRD field V1.1+)

Document named presets in `samples/` and `00-vision.md`; optional future field:

```yaml
spec:
  topology:
    profile: production-ha          # expands to cores: 3, readReplicas: 0
    # profile: primary-plus-analytics  # cores: 1, readReplicas: 1
```

### V1 scope

| In V1 | Deferred |
|-------|----------|
| `mode: Standalone` | `topology.profile` CRD field (docs-only presets first) |
| `mode: Cluster`, `cores.members` (1 or 3+) | Dedicated `analytics` server role separate from `readReplica` |
| `readReplicas.members: 0` in V1 tests | Multi-zone `multiCluster` networking variant |
| Validation errors + HA warnings | Auto-correction based on `intent` |

Read replicas with `cores: 1` + `readReplicas: 1` are **in scope for spec design** in V1; **test coverage** may land V1.1 depending on `13-dod-v1.md` prioritisation.

---

## Consequences

### Positive

- **Primary + analytics** is a first-class, documented pattern — not a misuse of `members: 2`.
- Customers get **actionable guidance** via validation errors and status warnings.
- Helm `minimumClusterSize` and `enableServer` map cleanly to operator fields.
- BDR-001 unchanged — still one CRD, one `neo4jRef` for day-2 resources.

### Negative

- `neo4j/spec.md` and `neo4j/validation.md` grow — topology section is the most sensitive part.
- Support must understand **cores vs read replicas** — requires runbook and decision tree in docs.
- Differs from Helm's "one release per cluster member" mental model — migration docs must explain unified CR.

### Neutral

- `03-variant_matrix.csv` needs new variants: `Read replica`, `Primary + analytics`, `Core size 1/3/N`.
- `domain/workload` branches on `mode`; `domain/formation` handles core vs replica reconciliation paths.
- [BDR-001](001-single-neo4j-crd.md) Option E (cluster-only) remains rejected — explicit `Standalone` kept for lightweight dev path.

---

## Alternatives considered

| Alternative | Why rejected |
|-------------|--------------|
| Flat `members` only | Cannot guide 1+1 analytics; conflates roles |
| Separate CRD per role (Option D variant) | GitOps and hierarchy complexity — see BDR-001 |
| Required `spec.intent` field | Redundant with mode + roles + warnings |
| Profiles only (no explicit counts) | Too opaque for production GitOps |

---

## References

- [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD
- FR: `NEO-001`, `NEO-002`, `NEO-011` — `01-functional_requirements.csv`
- Helm: [`design/analysis/helm_neo4j_values.yaml`](../../analysis/helm_neo4j_values.yaml) — `minimumClusterSize`, `operations.enableServer`
- CRD spec: [`09-crd-spec/neo4j/`](../../09-crd-spec/neo4j/)
- Validation: [`09-crd-spec/neo4j/validation.md`](../../09-crd-spec/neo4j/validation.md)
- Samples: `samples/standalone.yaml`, `samples/cluster-ha-3.yaml`, `samples/primary-plus-analytics.yaml` *(to create)*
