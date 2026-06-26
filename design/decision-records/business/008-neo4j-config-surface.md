# BDR-008 — Neo4j config surface (`spec.config` + `spec.jvm`)

| | |
|---|---|
| **Status** | proposed — **no decision yet** |
| **Date** | 2026-06-26 |
| **Depends on** | [BDR-002](002-neo4j-crd-topology.md) — operator injects topology / discovery keys; [ADR-001](../architecture/001-crd-validation-process.md) — CEL-first validation |
| **Helm scope** | `config` (map), `jvm.*` — `AGG-CONFIG-SURFACE`, register **BC-007** |
| **Constraints** | `NEO-2-003`, `NEO-3-003-CFG-01..03`, `NEO-3-003-JVM-01..02`; AC-NEO-CONFIG, AC-NEO-CONFIG-CHANGE; Neo4j configuration-settings docs |

---

## Context

The Helm chart exposes Neo4j tuning through two co-located but differently shaped surfaces, both rendered into the `{release}-user-config` ConfigMap (`helm-charts/neo4j/templates/neo4j-config.yaml`):

- **`config`** — a free-form `map[string]string` (`HelmValues.Config`) of arbitrary `neo4j.conf` keys. The chart deliberately **rejects** `server.jvm.additional` here and redirects it to `jvm.additionalJvmArguments`.
- **`jvm`** — a structured object (`Jvm` struct) with `useNeo4jDefaultJvmArguments` (bool) and `additionalJvmArguments` ([]string), rendered into the multi-line `server.jvm.additional` block.

In the operator the same two surfaces become `Neo4j.spec.config` and `Neo4j.spec.jvm`. The open question (config field doc, `breaking-change-register` BC-007) is **how much of `neo4j.conf` the CRD lets a user set directly**:

| Force | Detail |
|-------|--------|
| **Escape hatch** | `config` is the primary way to tune memory, query cache, feature flags, TLS reload without forking the chart. Operators expect parity with Helm `config`. |
| **Operator-owned keys** | The operator must inject and own topology / discovery / cluster keys (BDR-002), TLS policy keys when `spec.tls.*` secrets are present (CONCERN-TLS), and volume / path keys. User config must **not** fight these. |
| **Validation** | ADR-001 prefers CEL at admission; a curated allowlist enables strong typing and early rejection, a passthrough map defers errors to Neo4j startup. |
| **Migration** | Helm users have existing `config` maps. A strict allowlist that drops unknown keys is a migration cliff; passthrough is a drop-in. |
| **Forward compat** | Neo4j adds / renames `neo4j.conf` settings every minor. An allowlist baked into the CRD lags the database; passthrough never lags. |

`jvm` is the simpler half: both options agree it stays a structured object (`spec.jvm.useDefaults`, `spec.jvm.additionalArguments`). The contested surface is **`spec.config`**, and the JVM object is included here because it sets the precedent for "structured first-class field vs free-form key".

---

## Cross-cutting rules (all options)

| Rule | Rationale |
|------|-----------|
| Operator-injected keys are **reserved** — user `config` MUST NOT override topology / discovery / cluster keys derived from `spec.topology` | BDR-002; user config fighting primaries count breaks the cluster |
| TLS policy keys (`dbms.ssl.policy.*`, connector `*.enabled`) are operator-owned when `spec.tls.*` is set | CONCERN-TLS; avoids user/operator drift |
| `server.jvm.additional` is **never** accepted in `spec.config` — use `spec.jvm.additionalArguments` | Matches Helm rejection; single source of truth for JVM flags |
| `spec.jvm` stays a structured object regardless of `spec.config` decision | Multi-line rendering + bool default need typing |

The reserved-key set is the same under every option; the options differ only in **what user-supplied keys are allowed through**.

---

## Options under review

### Option A — Full passthrough map (+ reserved denylist)

`spec.config` is `map[string]string` accepting any `neo4j.conf` key. The operator merges user map → topology-derived defaults → discovery settings, and **rejects only the reserved keys** above. Everything else passes straight to `neo4j.conf`.

```yaml
spec:
  config:
    server.memory.heap.max_size: "4G"
    server.memory.pagecache.size: "2G"
    db.tx_log.rotation.retention_policy: "2 days"
    dbms.security.tls_reload_enabled: "true"
  jvm:
    useDefaults: true
    additionalArguments:
      - "-XX:+HeapDumpOnOutOfMemoryError"
```

| Advantages | Disadvantages |
|------------|---------------|
| **Drop-in Helm parity** — existing `config` maps migrate unchanged | No early typing; bad keys/values fail at Neo4j **startup**, not admission |
| Never lags Neo4j minor releases — any setting works day one | Easy to set a key that conflicts with a future operator-managed feature |
| Smallest CRD surface — one map field | Reserved denylist must be **maintained** as the operator owns more keys |
| Lowest operator complexity | Weak guardrails for novice users (typos silently mis-tune) |

### Option B — Curated allowlist of first-class fields

No free-form map. Each supported setting is promoted to a typed CRD field (or a bounded enum-keyed map). Unknown keys are rejected at admission.

```yaml
spec:
  config:
    memory:
      heapMaxSize: "4G"
      pageCacheSize: "2G"
    txLog:
      retentionPolicy: "2 days"
    security:
      tlsReloadEnabled: true
  jvm:
    useDefaults: true
    additionalArguments:
      - "-XX:+HeapDumpOnOutOfMemoryError"
```

| Advantages | Disadvantages |
|------------|---------------|
| Strong typing + CEL validation; errors caught at admission | **Migration cliff** — any Helm key without a CRD field is unsupported |
| Self-documenting API; discoverable via `kubectl explain` | CRD **lags Neo4j** — new settings need an operator release |
| Operator fully controls the config contract | Large, ever-growing field surface to design and version |
| No risk of users setting operator-owned keys | High maintenance; bikeshedding on field names vs Neo4j keys |

### Option C — Hybrid: passthrough map + reserved set + promoted hot-path fields (recommended)

`spec.config` stays a free-form `map[string]string` (Option A migration story), **plus** a small set of high-value or correctness-critical settings are promoted to typed first-class fields (the Option B benefit where it matters most). The operator:

1. accepts the typed fields and renders them with validation,
2. rejects reserved/operator-owned keys in the map,
3. **warns** (does not reject) on keys that duplicate a promoted first-class field, with the typed field winning,
4. passes all remaining map keys straight through.

```yaml
spec:
  # promoted, typed, validated
  jvm:
    useDefaults: true
    additionalArguments:
      - "-XX:+HeapDumpOnOutOfMemoryError"
  # free-form escape hatch for everything else
  config:
    server.memory.heap.max_size: "4G"
    server.memory.pagecache.size: "2G"
    db.tx_log.rotation.retention_policy: "2 days"
    dbms.security.tls_reload_enabled: "true"
```

| Advantages | Disadvantages |
|------------|---------------|
| **Helm parity preserved** — map accepts arbitrary keys, no migration cliff | Two ways to express a promoted setting → needs precedence + warning rules |
| Hot-path / correctness keys get typing + CEL (e.g. memory, strict validation) | Slightly more operator logic than A (merge + dedup + warn) |
| Never lags Neo4j — unknown keys still pass through the map | Must decide **which** keys to promote (scoping debate, but bounded) |
| Incremental — promote more fields over time **without breaking** the map | First-class set still needs versioned naming discipline |
| Matches `jvm` precedent (already a structured object beside `config`) | |

---

## Comparison

| Criterion | A — passthrough | B — allowlist | C — hybrid |
|-----------|-----------------|---------------|------------|
| Helm parity (`config` migration) | ✅ drop-in | ❌ cliff | ✅ drop-in |
| Admission-time validation | ⚠️ reserved keys only | ✅ all | ✅ promoted + reserved |
| Lags Neo4j minor releases | ✅ never | ❌ yes | ✅ never (map) |
| API discoverability (`kubectl explain`) | ❌ opaque map | ✅ typed | ⚠️ mixed |
| CRD surface size | ✅ minimal | ❌ large | ⚠️ medium, grows |
| Operator complexity | Low | Medium–High | Medium |
| Guardrails vs operator-owned keys | ⚠️ denylist | ✅ closed set | ✅ denylist + typed |
| Breaking risk vs Helm | Low | **High** | Low |

---

## Decision

**Not decided.** Options A and C both preserve Helm parity; B is retained mainly to document why a pure allowlist is rejected for V1 (migration cliff + lag).

**Proposer direction:** Option **C (hybrid)** — keep `spec.config` as a free-form passthrough map for parity and forward-compat, reject the reserved operator-owned key set, and promote a **small, bounded** group of correctness-critical settings (memory sizing, `server.config.strict_validation.enabled`, `dbms.security.tls_reload_enabled`) to typed fields. `spec.jvm` is the existing precedent for this pattern.

**Recommendation:** Adopt **Option C** for V1 with a **minimal** promoted set (avoid premature field explosion — that is the Option B failure mode). Lock the V1 promoted-field list and the reserved-key denylist before finalizing `09-crd-spec/neo4j/spec.md`. If scoping the promoted set proves contentious, **fall back to Option A** (pure passthrough) for V1 and promote fields additively later — additive promotion is non-breaking, so A → C is a safe iteration path, whereas B → A/C is not.

---

## Consequences

### Positive
- Helm users migrate `config` maps unchanged (no cliff under A or C).
- Correctness-critical keys gain admission-time CEL validation (C).
- Forward-compatible with new Neo4j settings via the passthrough map.

### Negative
- The reserved operator-owned key denylist must be maintained as the operator manages more of `neo4j.conf`.
- Under C, precedence + warning rules between typed fields and map keys add reconcile logic and need clear documentation.

### Neutral
- `spec.jvm` shape (`useDefaults`, `additionalArguments`) is unaffected by the chosen option.
- Promotion of additional first-class fields is an additive, non-breaking change post-V1.

---

## References

- `design/analysis/helm-fields/fields/config.md`, `jvm.md`, `jvm.additionalJvmArguments.md`, `jvm.useNeo4jDefaultJvmArguments.md`
- `design/analysis/helm-fields/_index.csv` — rows `config`, `jvm`, `jvm.useNeo4jDefaultJvmArguments`, `jvm.additionalJvmArguments`
- `design/analysis/helm-fields/aggregation-matrix.md` — `AGG-CONFIG-SURFACE`
- `design/analysis/helm-fields/breaking-change-register.md` — BC-007
- [Neo4j — Configuration settings](https://neo4j.com/docs/operations-manual/current/configuration/configuration-settings/)
- [Neo4j — Configuration validation](https://neo4j.com/docs/operations-manual/current/configuration/validation/) (`server.config.strict_validation.enabled`)
- [Neo4j — JVM configuration](https://neo4j.com/docs/operations-manual/current/configuration/jvm-configuration/)
- [Neo4j — SSL framework / TLS reload](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/)
- [BDR-002](002-neo4j-crd-topology.md) — operator-injected topology keys
- [ADR-001](../architecture/001-crd-validation-process.md) — CEL-first validation
