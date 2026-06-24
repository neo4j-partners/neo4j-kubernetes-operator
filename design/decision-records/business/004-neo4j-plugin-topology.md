# BDR-004 — Plugin model: role refs + central definitions

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-24 |
| **Amends** | BDR-004 (2026-06-24) — GDS allowed on any role; analytics is optional capacity |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Depends on** | [BDR-002](002-neo4j-crd-topology.md) — license-driven role counts |
| **Constraints** | GDS/Bloom commercial licensing; `NEO-2-003`; Helm `apoc_config`, `apoc_credentials` |

---

## Context

Plugins install **per server**. Users choose **which plugins run on which role** via `spec.plugins.<role>`.

**GDS can run on primaries, secondaries, or analytics servers** — there is no platform rule restricting GDS to the analytics role only. Many clusters run GDS on primaries; that is valid.

**Commercial licensing** is the constraint that shapes common layouts: GDS is sold **per instance**. A contract for **one** GDS instance means the customer deploys GDS on **one** server — not on every member. How they label that server in topology is a deployment choice:

| Customer layout | Typical CRD shape |
|-----------------|-------------------|
| GDS on primaries (e.g. all primaries, or subset via homogeneous primary plugin set) | `plugins.primaries: [apoc, gds]` |
| **1 primary + 1 GDS server** (PS “analytics” layout) | `primaries.members: 1`, `analytics.members: 1`, `plugins.analytics: [gds]` |
| HA primaries + dedicated GDS server | `primaries.members: 3`, `analytics.members: 1`, `plugins.analytics: [gds]` |
| GDS on a read-scaling secondary | `secondaries.members: N`, `plugins.secondaries: [gds]` |

The **`analytics` role** ([BDR-002](002-neo4j-crd-topology.md)) is an optional **dedicated analytics-capacity server group** — not the only place GDS may run. PS uses it when the GDS-bearing server is a separate member from transactional primaries (e.g. 1 primary + 1 analytics with GDS).

**Configuration** (license Secret, JDBC credentials, plugin settings) lives in **`spec.pluginDefinitions`** — separate from assignment.

---

## Decision

We will use **role refs + `pluginDefinitions`**:

| Concern | Where |
|---------|--------|
| **How many** servers per role | `topology.primaries` / `secondaries` / `analytics` ([BDR-002](002-neo4j-crd-topology.md)) |
| **Which plugins** on each role | `spec.plugins.<role>` |
| **License Secret, config, JDBC credentials** | `spec.pluginDefinitions.<id>` |

### Assignment by mode

| `topology.mode` | Plugin assignment |
|-----------------|-------------------|
| `Standalone` | `spec.plugins: [apoc, gds, …]` |
| `Cluster` | `spec.plugins.primaries`, `spec.plugins.secondaries`, `spec.plugins.analytics` |

Each field is a `[]string` of catalog ids. Omit or empty → no plugins on that role.

### `spec.pluginDefinitions`

```yaml
pluginDefinitions:
  apoc:
    config:
      apoc.trigger.enabled: "true"
  gds:
    licenseSecretRef: gds-license
    config:
      gds.enterprise.license_file: /licenses/gds.key
  bloom:
    licenseSecretRef: bloom-license
  apoc-extended:
    credentials:
      - alias: jdbc
        secretRef: jdbc-credentials
        mountPath: /secrets/jdbc
        key: URL
```

| Field | Description |
|-------|-------------|
| `licenseSecretRef` | Required for `gds`, `bloom` when referenced on any role |
| `version` | Default `spec.version` |
| `config` | Plugin settings map (Helm `apoc_config`, GDS paths, …) |
| `credentials[]` | APOC Extended JDBC/ES secrets (Helm `apoc_credentials`) |

### Example — GDS on primaries

```yaml
topology:
  mode: Cluster
  primaries:
    members: 3
plugins:
  primaries: [apoc, gds]
pluginDefinitions:
  gds:
    licenseSecretRef: gds-license
```

GDS installs on all three primary pods. Customer contract must cover three instances.

### Example — 1 primary + 1 analytics server with GDS (common licensed layout)

One GDS license: transactional primary separate from the GDS-bearing server PS calls “analytics”.

```yaml
topology:
  mode: Cluster
  primaries:
    members: 1
  analytics:
    members: 1
plugins:
  primaries: [apoc]
  analytics: [gds]
pluginDefinitions:
  apoc: {}
  gds:
    licenseSecretRef: gds-license
```

### Example — HA primaries + read secondary + dedicated analytics with GDS

```yaml
topology:
  mode: Cluster
  primaries:
    members: 3
  secondaries:
    members: 1
  analytics:
    members: 1
plugins:
  primaries: [apoc]
  secondaries: [apoc]
  analytics: [gds]
pluginDefinitions:
  gds:
    licenseSecretRef: gds-license
```

### Invariants

1. **GDS / Bloom may appear on any role** — `plugins.primaries`, `plugins.secondaries`, or `plugins.analytics` (or flat `spec.plugins` in Standalone).
2. **Role consistency** — if a plugin id appears in `plugins.<role>`, that role must have `members ≥ 1` (`analytics.members`, `secondaries.members`, or `primaries.members` as applicable).
3. **One `licenseSecretRef` per plugin id** — same Secret mounted on every pod running that plugin.
4. **Homogeneous primaries** — all primary pods receive the same plugin set.
5. **License renewal** — update Secret; operator rolling-restarts pods running affected plugins.

**Out of scope:** the operator does not validate commercial license entitlements. Neo4j validates the license file at startup. **`analytics.members`** sizes the analytics server group — it is not an exclusive GDS slot.

### V1 catalog

| Id | License | May run on |
|----|---------|------------|
| `apoc` | No | any role |
| `gds` | Yes | any role |
| `bloom` | Yes | any role |
| `apoc-extended` | No | any role |

### Operator resolution

For each catalog id in a role’s plugin list: merge `pluginDefinitions[id]` with catalog defaults → install on **all pods in that role** → mount license Secret when set.

---

## Consequences

### Positive

- Matches PS reality: GDS on primaries is valid; 1+1 analytics layout is one pattern, not the only pattern.
- Plugin placement is explicit per role — license instance count = count of pods actually running GDS.

### Negative

- Users must align pod counts with contract manually — no CRD field for commercial limits.

### Neutral

- Unused `pluginDefinitions` keys → reconciler warning.

---

## References

- [BDR-002](002-neo4j-crd-topology.md)
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md)
- [`09-crd-spec/neo4j/validation.md`](../../09-crd-spec/neo4j/validation.md)
