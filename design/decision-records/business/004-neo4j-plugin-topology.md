# BDR-004 — Plugin model for `Neo4j` CRD

| | |
|---|---|
| **Status** | proposed — **no decision yet** |
| **Date** | 2026-06-22 |
| **Depends on** | [BDR-002](002-neo4j-crd-topology.md) — `primaries` + fixed `secondaries.analytics` / `secondaries.read` |
| **Constraints** | Neo4j GDS deployment docs; `NEO-2-003`; Helm `NEO4J_PLUGINS` |

---

## Context

Neo4j plugins (APOC Core, GDS, Bloom, APOC Extended, …) are installed **per server instance**. Operational and **licensing** rules constrain where they may run:

| Plugin | License | Topology constraint (Neo4j docs) |
|--------|---------|----------------------------------|
| **APOC Core** | Included with Enterprise | Any server |
| **GDS** | GDS Enterprise license (shared Secret — see invariants below) | **Forbidden on primaries** — secondary or secondary / analytics server only |
| **Bloom** | Separate license per instance | Typically non-primary |
| **APOC Extended** | Community / optional | Per-node choice |

In a cluster:

- **All primary members** must share the same plugin set (homogeneous quorum members).
- **Non-primary pools** may differ — e.g. read scaling with APOC only, another pool with **GDS + Bloom**.
- A **per-pod** plugin matrix (every replica × every combination) is unmaintainable.

[BDR-002](002-neo4j-crd-topology.md) chose **fixed secondary pools** (`analytics`, `read`) for topology sizing. **This BDR decides how plugins attach to those pools** — and whether plugin **configuration** (license Secret, JAR version, `apoc.conf`) is colocated with assignment or factored out.

**Open design tension** (raised in review):

- It is useful to see **which plugins** a pool runs **next to** the pool definition.
- It is awkward to repeat **license Secret, version, JAR overrides** in every pool that references `gds`.

---

## Plugin invariants (all options)

These rules apply **regardless of which structural option is chosen** (C, D, E, or F).

### 1. GDS must not run on primaries

In **Cluster** mode, a cluster **cannot** have GDS installed on its **primary** members. The operator MUST reject or refuse to reconcile any spec that assigns `gds` (or equivalent analytics-only plugins governed by the same Neo4j constraint) to `topology.primaries`.

- GDS runs only on **`secondaries.analytics`** ([Neo4j GDS cluster deployment](https://neo4j.com/docs/graph-data-science/current/production-deployment/neo4j-cluster/)).
- Primary pods may run plugins allowed on transactional members (e.g. APOC Core) but **not** GDS.

### 2. Licensed plugins use one shared Secret per plugin

When a plugin requires a license (GDS Enterprise, Bloom, …), the license is configured **once** in the CRD via a Kubernetes `Secret` reference (`licenseSecretRef` in `pluginDefinitions` or the chosen equivalent). The **same Secret** is mounted on **every pod** that runs that plugin — one pool or several pools, N members each.

- **API / operator model:** one `licenseSecretRef` per plugin id, not one Secret per pod ordinal.
- **Deployment:** each GDS-enabled pod still needs the license file locally (`gds.enterprise.license_file`); the operator materialises it from the shared Secret on each instance.
- **Commercial entitlement** (how many GDS instances the contract covers) is out of scope for the CRD — validate with Neo4j licensing / PS.

---

## Options under review

Six structural options. Each includes a sketch, advantages, and disadvantages.

---

### Option A — Same plugin set on every server

One global `spec.plugins[]` applied to all pods (Standalone and Cluster).

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 3
    secondaries:
      - name: read-scale
        members: 2
  plugins:
    - name: apoc
    - name: gds
      licenseSecretRef: gds-license
```

| Advantages | Disadvantages |
|------------|---------------|
| Simplest API — one list to learn | **Invalid for GDS on primaries** — Neo4j forbids / unsupported |
| No per-pool coordination | Cannot run APOC-only on read-scale and GDS on analytics pool |
| Easy Helm migration (`NEO4J_PLUGINS` on all pods) | Wastes resources installing unused JARs on wrong roles |
| Minimal operator logic | **License compliance risk** — GDS license on primary pods |
| | Does not match production Neo4j cluster guidance |

**Verdict:** **Rejected** for Cluster mode — fails GDS topology and license rules. May remain valid for **Standalone** only.

---

### Option B — Fixed counters: `secondaries` + `analyticsSecondaries`

Two topology fields (early BDR-002 draft) with implicit plugin intent; optional global or role-default plugin config.

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 3
    secondaries:
      members: 2
    analyticsSecondaries:
      members: 1
  plugins:
    core: [apoc]
    readReplica: [apoc]
    readGDSReplica: [gds]   # name implies GDS
```

| Advantages | Disadvantages |
|------------|---------------|
| Only two non-primary buckets — bounded | **`analyticsSecondaries` name implies GDS** — cannot express GDS + Bloom cleanly |
| Maps loosely to Helm analytics vs read scaling | **Cannot have two non-primary pools** with different plugin sets |
| Smaller than arbitrary `secondaries[]` | GDS + Bloom on one secondary **does not fit** |
| | Conflates topology sizing with plugin intent |
| | Superseded by `secondaries[]` in BDR-002 amendment |

**Verdict:** **Rejected** for topology (BDR-002). Plugin attachment via fixed role keys (`readGDSReplica`) remains a **degenerate case** of Option C.

---

### Option C — `secondaries[]` + separate `spec.plugins.<poolName>` map

Topology in `secondaries[]`; plugin **assignment** keyed by pool name in a second map. Full `PluginSpec` (license, version, config) in the map.

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 3
    secondaries:
      - name: read-scale
        members: 2
      - name: gds-bloom
        members: 1
  plugins:
    core:
      - name: apoc
    read-scale:
      - name: apoc
    gds-bloom:
      - name: gds
        licenseSecretRef: gds-license
      - name: bloom
        licenseSecretRef: bloom-license
```

| Advantages | Disadvantages |
|------------|---------------|
| Arbitrary pool names + arbitrary plugin combos per pool | **Split YAML** — pool in `topology`, plugins in `plugins.<name>` |
| Supports GDS + Bloom on one pool | Must keep `secondaries[].name` ↔ map key **in sync** |
| `primaries` handled as `plugins.primary` | Orphan / missing keys need validation (PLG-002 class) |
| Full config next to assignment | Repetitive if same plugin (e.g. `apoc`) defined in multiple pools |
| | Poor colocation for GitOps review of a pool |

---

### Option D — `secondaries[]` with inline `plugins[]` on each pool

Plugin **assignment and full configuration** on the pool object.

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 3
      plugins:
        - name: apoc
    secondaries:
      - name: read-scale
        members: 2
        plugins:
          - name: apoc
      - name: gds-bloom
        members: 1
        plugins:
          - name: gds
            licenseSecretRef: gds-license
          - name: bloom
            licenseSecretRef: bloom-license
```

| Advantages | Disadvantages |
|------------|---------------|
| **Single block per pool** — members, role, plugins together | **Repeats license/config** when same plugin appears in multiple pools |
| No name sync between sections | `topology` section grows; secrets mixed with sizing |
| Clear for small deployments | Changing GDS license means editing every pool that uses `gds` |
| | Duplicates `PluginSpec` structure under `primaries` and each pool |
| | Standalone needs a third shape (`spec.plugins[]` flat list) |

---

### Option E — fixed `secondaries` pools with plugin **refs** + central plugin definitions

**Proposer direction:** pools declare **which** plugins (by catalog id); **how** to install them (license Secret, version, JAR, config) lives in `spec.pluginDefinitions`.

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 1
      plugins: [apoc]
    secondaries:
      analytics:
        members: 1
        plugins: [gds, bloom]
      read:
        members: 1
        plugins: [apoc]
  pluginDefinitions:
    apoc: {}
    gds:
      licenseSecretRef: gds-license
    bloom:
      licenseSecretRef: bloom-license
```

**Standalone:**

```yaml
spec:
  topology:
    mode: Standalone
  plugins: [apoc, gds]
  pluginDefinitions:
    apoc: {}
    gds:
      licenseSecretRef: gds-license
```

| Advantages | Disadvantages |
|------------|---------------|
| **Separation** — assignment (pools) vs configuration (definitions) | Two sections to read for full picture |
| **DRY** — one `gds` definition, many pools can reference it | Must validate refs → existing definition keys |
| Pool block stays readable: `plugins: [gds, bloom]` | Catalog id namespace shared across CR — naming discipline |
| License rotation: update `pluginDefinitions.gds` once | Slightly more abstract for beginners |
| Aligns with “list integrated, config elsewhere” feedback | OpenAPI: `plugins: [string]` vs `PluginSpec` — need merge rules |
| Operator resolves: pool refs + definitions → init container JAR set | Unused definitions in map — warn or allow? |

**Resolution rule (operator):** for each plugin id in `pool.plugins[]`, look up `pluginDefinitions[id]`, merge with catalog defaults for `name`, install on all pods in that pool.

---

### Option F — `spec.plugins` map with `enabledOn` targets

**Plugin-centric model:** one entry per plugin id; each entry holds **configuration** and an **`enabledOn`** list. Targets: `primaries`, `analytics`, `read` (fixed pool keys per BDR-002).

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 3
    secondaries:
      - name: read-scale
        members: 2
      - name: analytics
        members: 1
  plugins:
    apoc:
      enabledOn:
        - primaries
        - read-scale
        - analytics
    gds:
      licenseSecretRef: gds-license
      enabledOn:
        - analytics
    bloom:
      licenseSecretRef: bloom-license
      enabledOn:
        - analytics
```

**Standalone:**

```yaml
spec:
  topology:
    mode: Standalone
  plugins:
    apoc:
      enabledOn: [standalone]
    gds:
      licenseSecretRef: gds-license
      enabledOn: [standalone]
```

| Advantages | Disadvantages |
|------------|---------------|
| **Plugin-first view** — license + deployment targets in one block per plugin | **Pool-first view is indirect** — to see analytics plugins, scan every `plugins.*.enabledOn` |
| **DRY** — one `gds` entry with one `licenseSecretRef` (invariant §2) | `enabledOn` entries must match `secondaries[].name` — sync / orphan validation (PLG-002) |
| `enabledOn: [primaries]` maps cleanly to all primary pods | Standalone needs a sentinel target (`standalone`) — extra convention |
| Natural validation: reject `gds.enabledOn` containing `primaries` (invariant §1) | OpenAPI: fixed plugin keys vs extensible catalog — same as E |
| No separate `pluginDefinitions` section — fewer top-level fields than E | Unused plugin keys in map — warn or allow? |
| Easy license rotation — edit `plugins.gds` once | Homogeneous primaries: all primary pods get the same plugins listed under `enabledOn: [primaries]` — cannot mix per-primary plugin sets (Neo4j rule anyway) |

**Resolution rule (operator):** for each `plugins.<id>`, merge with catalog defaults; for each target in `enabledOn`, install on all pods in that pool (`primaries` → primary StatefulSet ordinals; pool name → matching `secondaries[]`).

**Relation to other options:** inverted **Option C** (pool-keyed map → plugin-keyed map with `enabledOn`). Combines assignment + config like **Option D**, but grouped by plugin like **Option E** without a second section.

---

## Comparison matrix

| Criterion | A — global | B — fixed roles | C — map by name | D — inline | E — refs + defs | F — enabledOn |
|-----------|------------|-----------------|-----------------|------------|-----------------|---------------|
| GDS not on primaries | ❌ | ⚠️ | ✅ | ✅ | ✅ | ✅ |
| GDS + Bloom same pool | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Multiple non-primary pools | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Colocation (pool + plugins) | n/a | ⚠️ | ❌ | ✅ | ✅ (refs only) | ❌ |
| DRY license / JAR config | ✅ | ⚠️ | ❌ | ❌ | ✅ | ✅ |
| Name sync burden | — | — | ❌ | — | ✅ (refs only) | ⚠️ (`enabledOn` ↔ pool names) |
| API minimalism | ✅ | ✅ | ⚠️ | ⚠️ | ⚠️ | ⚠️ |
| Standalone simplicity | ✅ | ⚠️ | ⚠️ | ✅ | ✅ | ⚠️ (`standalone` target) |
| Helm `NEO4J_PLUGINS` mapping | ✅ | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ |
| Operator complexity | Low | Low | Medium | Medium | Medium | Medium |

---

## Cross-cutting validation (any option except A for Cluster)

| Rule | Rationale |
|------|-----------|
| **`gds` MUST NOT be on `topology.primaries`** — cluster cannot run GDS on primary members | Neo4j GDS docs — primaries serve transactional quorum; GDS is OLAP-only on secondaries |
| `gds` / `bloom` not on primary pods (same as above for other analytics-only plugins) | Plugin invariants §1 |
| Licensed plugins (`gds`, `bloom`, …) require `licenseSecretRef`; **same Secret** for all pods using that plugin id | Plugin invariants §2 — shared license, mounted per instance |
| Plugin version compatible with `spec.version` | Neo4j plugin compatibility |
| `secondaries.analytics` with `gds` → analytics server config | Neo4j 5.x GDS on secondary |

---

## Decision

**Not decided.** Options C, D, E, and F remain viable given [BDR-002](002-neo4j-crd-topology.md) fixed pools `analytics` / `read`.

**Leaning from review discussion:** **Option E** addresses the feedback that inline license/JAR config in pools (Option D) is noisy, while keeping plugin **assignment** visible on each pool. **Option F** offers the same DRY license model with a **plugin-centric** `enabledOn` list — preferable when operators think per-plugin rather than per-pool.

**Next steps before locking:**

1. Confirm field names: `pluginDefinitions` (E) vs unified `plugins.<id>.enabledOn` (F) vs split `PluginDefinition` CRD (out of scope V1).
2. Validate Options E and F with 2–3 real PS deployment YAMLs (HA + analytics, GDS + Bloom).
3. Update `09-crd-spec/neo4j/spec.md` **after** decision — current spec is **draft** and may not match final BDR-004.

---

## Consequences

*(To be completed when an option is chosen.)*

---

## References

- [BDR-002](002-neo4j-crd-topology.md) — `secondaries.analytics` / `secondaries.read`
- [Neo4j GDS — cluster deployment](https://neo4j.com/docs/graph-data-science/current/production-deployment/neo4j-cluster/)
- [Neo4j — Configure plugins](https://neo4j.com/docs/operations-manual/current/configuration/plugins/)
- [`20-operator-proposal.md`](../../20-operator-proposal.md) §V2 auto plugin management
