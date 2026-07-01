# BDR-008 — Neo4j config surface (`spec.config` + `spec.jvm` + `spec.apoc`)

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-26 (accepted 2026-06-22; amended 2026-06-22 — three-file model) |
| **Depends on** | [BDR-002](002-neo4j-crd-topology.md) — operator injects topology / discovery keys; [BDR-004](004-neo4j-plugin-topology.md) — APOC installed per pool via `pluginDefinitions`; [ADR-001](../architecture/001-crd-validation-process.md) — CEL-first validation |
| **Helm scope** | `config`, `jvm.*`, `apoc_config`, `apoc_credentials` — `AGG-CONFIG-SURFACE` + `AGG-TOPO-PLUGINS`; register **BC-007** |
| **Constraints** | `NEO-2-003`, `NEO-3-003-CFG-01..03`, `NEO-3-003-JVM-01..02`, `NEO-3-003-APOC-01..02`; AC-NEO-CONFIG, AC-NEO-CONFIG-CHANGE; Neo4j configuration-settings + APOC config docs |

---

## Context

The Helm chart exposes Neo4j tuning through **three co-located surfaces**, rendered into **two ConfigMap files** on the pod (`helm-charts/neo4j/templates/neo4j-config.yaml`):

| Helm path | Rendered file | Shape |
|-----------|---------------|--------|
| **`config`** | `neo4j.conf` | free-form `map[string]string` of arbitrary `neo4j.conf` keys |
| **`jvm`** | `neo4j.conf` (`server.jvm.additional` block) | structured `Jvm` struct — `useNeo4jDefaultJvmArguments`, `additionalJvmArguments` |
| **`apoc_config`** | **`apoc.conf`** (separate file) | free-form map of `apoc.conf` key=value pairs |

Helm deliberately **rejects** `server.jvm.additional` in both `config` and `apoc_config` and redirects JVM flags to `jvm.additionalJvmArguments`. APOC credentials (`apoc_credentials`) mount Secrets and generate URL lines in `apoc.conf` — handled under [BDR-004](004-neo4j-plugin-topology.md) `pluginDefinitions.apoc.credentials`, not in this BDR's passthrough maps.

In the operator the same three surfaces become:

| CRD field | File | Option A shape |
|-----------|------|----------------|
| **`spec.config`** | `neo4j.conf` | `map[string]string` passthrough |
| **`spec.jvm`** | `neo4j.conf` (JVM block) | structured object |
| **`spec.apoc`** | **`apoc.conf`** | `map[string]string` passthrough |

**Plugin attachment** (which pools run APOC) stays in [BDR-004](004-neo4j-plugin-topology.md) (`plugins: [apoc]` + `pluginDefinitions`). **`spec.apoc`** only holds **tuning** for `apoc.conf` — same separation Helm uses between `apoc_config` and `NEO4J_PLUGINS`.

The open question (BC-007) is **how much** of each file the CRD exposes:

| Force | Detail |
|-------|--------|
| **Escape hatch** | `config` / `apoc` are primary tuning paths without forking templates. Operators expect Helm parity. |
| **Operator-owned keys** | Topology, connectivity listeners, trust, volume paths — user maps must **not** fight injected settings. |
| **Separate files** | `apoc.conf` keys MUST NOT leak into `spec.config`; `neo4j.conf` keys MUST NOT leak into `spec.apoc`. |
| **Validation** | ADR-001: CEL denylist at admission; non-reserved keys validated at Neo4j/APOC startup. |
| **Migration** | Helm `config` and `apoc_config` maps migrate drop-in under Option A. |
| **Forward compat** | Passthrough maps never lag Neo4j / APOC minor releases. |

`jvm` is the structured half: all options agree on `spec.jvm.useDefaults` + `spec.jvm.additionalArguments`. The contested surfaces are **`spec.config`** and **`spec.apoc`** — both use **Option A** (passthrough + denylist).

---

## Cross-cutting rules (all options)

| Rule | Rationale |
|------|-----------|
| Operator-injected keys are **reserved** in `spec.config` — MUST NOT override topology / discovery / cluster keys | BDR-002 |
| **Port-owned keys** reserved in `spec.config` when `spec.connectivity.listeners` is set | [BDR-007](006-service-exposure-connectivity.md) CFG-LISTENER-* |
| TLS policy keys reserved in `spec.config` when `spec.trust` is set | [BDR-006](007-tls-trust-model.md) |
| `server.jvm.additional` is **never** in `spec.config` or `spec.apoc` — use `spec.jvm.additionalArguments` | Helm parity; single JVM source |
| **`spec.apoc` keys MUST NOT appear in `spec.config`** — wrong file (`apoc.conf` vs `neo4j.conf`) | Helm separates `apoc_config` from `config` |
| **`neo4j.conf` keys MUST NOT appear in `spec.apoc`** — admission rejects `server.*`, `dbms.*` (non-apoc), `db.*` in apoc map | File boundary |
| `spec.apoc` rendered **only** when APOC is assigned (`spec.plugins` / pool `plugins` includes `apoc`) | No orphan `apoc.conf` without plugin |
| `spec.jvm` stays structured regardless of passthrough decision | Multi-line rendering + bool default |

The reserved-key set is the same under every option; options differ only in **what user-supplied keys pass through** in each map.

---

## Options under review

### Option A — Full passthrough maps (+ reserved denylists) — **accepted**

`spec.config` and `spec.apoc` are each `map[string]string` accepting any key valid for their target file. The operator merges user maps with injected defaults and **rejects only reserved keys**. Everything else passes straight through.

```yaml
spec:
  config:
    server.memory.heap.max_size: "4G"
    server.memory.pagecache.size: "2G"
    db.tx_log.rotation.retention_policy: "2 days"
  jvm:
    useDefaults: true
    additionalArguments:
      - "-XX:+HeapDumpOnOutOfMemoryError"
  apoc:
    apoc.trigger.enabled: "true"
    apoc.import.file.enabled: "true"
  topology:
    mode: Cluster
    primaries:
      members: 3
      plugins: [apoc]
  pluginDefinitions:
    apoc: {}
```

| Surface | Target file | Option A |
|---------|-------------|----------|
| `spec.config` | `neo4j.conf` | passthrough map + denylist |
| `spec.jvm` | `neo4j.conf` (`server.jvm.additional`) | structured |
| `spec.apoc` | **`apoc.conf`** | passthrough map + denylist |

| Advantages | Disadvantages |
|------------|---------------|
| **Drop-in Helm parity** — `config` + `apoc_config` migrate unchanged | Bad keys/values fail at **startup**, not admission |
| Never lags Neo4j / APOC minors | Reserved denylists must be **maintained** per file |
| Clear **three-surface** mental model matching two files on disk | Typos in maps silently mis-tune |
| `apoc.conf` stays separate from `neo4j.conf` — matches Neo4j layout | APOC credentials still need `pluginDefinitions` (not in map) |

### Option B — Curated allowlist of first-class fields

No free-form maps. Each setting promoted to typed CRD fields. Unknown keys rejected at admission.

Rejected — migration cliff for both `config` and `apoc_config`.

### Option C — Hybrid: passthrough + promoted hot-path fields — **not adopted**

Passthrough maps plus typed fields for hot paths. Not adopted for V1 — user chose pure passthrough for both files.

---

## Comparison

| Criterion | A — passthrough | B — allowlist | C — hybrid |
|-----------|-----------------|---------------|------------|
| Helm parity (`config` + `apoc_config`) | ✅ drop-in | ❌ cliff | ✅ drop-in |
| Separate `apoc.conf` surface | ✅ `spec.apoc` | ⚠️ typed apoc fields | ✅ map |
| Admission-time validation | ⚠️ denylists only | ✅ all | ⚠️ mixed |
| Lags Neo4j / APOC releases | ✅ never | ❌ yes | ✅ never (maps) |
| CRD surface size | ✅ three fields | ❌ large | ⚠️ medium |

---

## Decision

**Accepted — Option A** — Charles Boudry, 2026-06-22.

**We will implement Option A** for the **three-surface config model**:

1. **`spec.config`** — free-form `map[string]string` → **`neo4j.conf`** (Helm `config` drop-in).
2. **`spec.jvm`** — structured object (`useDefaults`, `additionalArguments`) → **`server.jvm.additional`** in `neo4j.conf`.
3. **`spec.apoc`** — free-form `map[string]string` → **`apoc.conf`** (Helm `apoc_config` drop-in). Rendered only when APOC is installed on the workload.

Admission rejects **reserved / operator-owned keys** per file (CEL denylist + webhook where needed). All other keys pass through. Invalid values surface at **Neo4j / APOC startup**, not admission — accepted trade-off.

1. **V1 = Option A** for both passthrough maps. No promoted first-class config fields; no hybrid precedence.
2. **Denylist mandatory** per file — see tables below.
3. **File boundary:** `CFG-APOC-001` rejects `neo4j.conf` keys in `spec.apoc`; `CFG-APOC-002` rejects `apoc.*` keys in `spec.config` (when APOC settings belong in `apoc.conf`).
4. **V1.1+:** typed promotion (Option C) possible per surface without breaking maps.

**Option B** rejected. **Option C** not adopted for V1.

### V1 reserved-key denylist — `spec.config` → `neo4j.conf`

| Group | Examples | Owned by |
|-------|----------|----------|
| Topology / cluster | `initial.dbms.default_primaries_count`, `dbms.kubernetes.*` | `spec.topology` |
| Listen ports | `server.{bolt,http,https,backup}.listen_address`, connector `*.enabled` | `spec.connectivity.listeners` |
| Feature tuning | `server.backup.enabled`, `server.metrics.*` | `spec.features` (CFG-FEAT) |
| TLS | `dbms.ssl.policy.*`, `server.bolt.tls_level` | `spec.trust` |
| JVM | `server.jvm.additional` | `spec.jvm` |
| APOC (wrong file) | `apoc.*` | `spec.apoc` |

### V1 reserved-key denylist — `spec.apoc` → `apoc.conf`

| Group | Examples | Reject / redirect |
|-------|----------|-------------------|
| JVM | `server.jvm.additional` | use `spec.jvm` |
| Core server | `server.*`, `dbms.*` (non-apoc), `db.*` | use `spec.config` |
| Port / TLS / topology | any denylisted `neo4j.conf` key | use owning `spec.*` section |

APOC credential URLs (`apoc.jdbc.*`, `apoc.es.*` from Secrets) — **`pluginDefinitions.apoc.credentials`**, not inline in `spec.apoc` (Helm `apoc_credentials` parity).

### Render merge order (operator)

```
neo4j.conf  ← defaults → spec.config (user) → topology/connectivity/trust injections
            ← spec.jvm → server.jvm.additional block

apoc.conf   ← catalog defaults → spec.apoc (user) → pluginDefinitions.apoc credential lines
            (only if apoc ∈ pool.plugins or spec.plugins)
```

---

## Consequences

### Positive

- Helm users migrate **`config`** and **`apoc_config`** unchanged — full drop-in parity.
- **Three CRD fields** mirror three Helm tuning paths and **two files** on the pod.
- Forward-compatible with new Neo4j / APOC settings via passthrough maps.
- Clear boundary: server tuning vs plugin tuning vs JVM flags.

### Negative

- Two denylists to maintain (`config` + `apoc`).
- Startup-time failures for typos in either map.
- `spec.apoc` without APOC in topology — admission should warn or reject (TBD in validation).

### Neutral

- `pluginDefinitions.apoc` still holds `{}`, `licenseSecretRef`, `credentials` per BDR-004 — `spec.apoc` is tuning only.
- Typed field promotion remains a non-breaking additive path post-V1.

---

## References

- `design/analysis/helm-fields/fields/config.md`, `jvm.md`, `apoc_config.md`, `apoc_credentials.md`
- `design/analysis/helm-fields/_index.csv` — `config`, `jvm`, `apoc_config`, `apoc_credentials`
- `design/analysis/helm-fields/aggregation-matrix.md` — `AGG-CONFIG-SURFACE`, `AGG-TOPO-PLUGINS`
- `design/analysis/helm-fields/breaking-change-register.md` — BC-007
- [Neo4j — Configuration settings](https://neo4j.com/docs/operations-manual/current/configuration/configuration-settings/)
- [APOC — Configuration](https://neo4j.com/docs/apoc/current/config/)
- [Neo4j — JVM configuration](https://neo4j.com/docs/operations-manual/current/configuration/jvm-configuration/)
- [BDR-002](002-neo4j-crd-topology.md) · [BDR-004](004-neo4j-plugin-topology.md) · [BDR-007](006-service-exposure-connectivity.md) · [ADR-001](../architecture/001-crd-validation-process.md)
