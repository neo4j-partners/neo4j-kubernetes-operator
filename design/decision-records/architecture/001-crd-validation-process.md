# ADR-001 — CRD validation process (CEL + webhook)

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-22 |
| **Depends on** | [BDR-001](../business/001-single-neo4j-crd.md) — single `Neo4j` CRD with mode-dependent `spec` |
| **Constraints** | Kubebuilder scaffold; `EST-DEV-002` (API types & validation); week-1 gate G3 |

---

## Context

The `Neo4j` operator exposes a rich `spec` ([BDR-001](../business/001-single-neo4j-crd.md), [BDR-002](../business/002-neo4j-crd-topology.md), [BDR-004](../business/004-neo4j-plugin-topology.md)). Many fields are **conditional on `topology.mode`**, cross-reference each other (topology ↔ plugins ↔ TLS), and reference Kubernetes objects (Secrets, StorageClasses).

We must decide **where** each validation rule runs:

| Layer | When it runs | What it can see |
|-------|--------------|-----------------|
| **OpenAPI schema** | Apply / dry-run | Types, required fields, enums |
| **CEL** (`x-kubernetes-validations`) | Admission (API server) | Current object; `oldSelf` on update |
| **Validating webhook** | Admission (operator) | API server client — other K8s resources |
| **Mutating webhook** | Admission (operator) | Defaults, normalisation |
| **Reconciler** | Async reconcile | Live cluster state, Neo4j runtime |

**Forces:**

- [BDR-001](../business/001-single-neo4j-crd.md) chose one CRD with mode-dependent shapes → conditional validation is mandatory.
- Admission must be **fast and reliable** — webhook outages block all writes to the CRD.
- Rules must be **traceable** — each rule has an ID in [`09-crd-spec/neo4j/validation.md`](../../09-crd-spec/neo4j/validation.md).
- Kubebuilder generates CRD + webhook scaffold; CEL is embedded in CRD YAML at build time.

---

## Analysis

### Option A — Validating webhook only

| Advantages | Disadvantages |
|------------|---------------|
| Full Go expressiveness — semver, API lookups, complex logic | Webhook is a **single point of failure** for admission |
| One place to read all rules | Harder for users to discover rules without reading operator code |
| Easy Secret / StorageClass existence checks | Higher latency on every create/update |
| | envtest must spin webhook for admission tests |

### Option B — CEL only

| Advantages | Disadvantages |
|------------|---------------|
| No webhook dependency for structural rules | Cannot check Secret / StorageClass **existence** |
| Rules live in CRD — visible to `kubectl`, OpenAPI tooling | No semver parsing for plugin ↔ Neo4j version match |
| Fast — evaluated by API server | Cannot express some scale-in policies against live PVC state |
| Works offline in `kubectl apply --dry-run=server` when CEL enabled | CEL expressions become hard to maintain if overused |

### Option C — CEL first, webhook for the rest (chosen)

| Advantages | Disadvantages |
|------------|---------------|
| Structural + cross-field rules are **declarative** and versioned with the CRD | Two mechanisms to maintain — clear ownership table required |
| Webhook stays **thin** — only rules CEL cannot express | Implementers must know which layer owns a new rule |
| Secret/TLS/StorageClass checks stay accurate | Mutating + validating webhooks still required for defaults |
| Aligns with Kubernetes direction (CRD validation GA) | Some rules (warnings) belong in neither — reconciler only |

### What CEL can express (V1 examples)

Cross-field and mode-dependent rules on the **same object**:

```yaml
# Standalone forbids topology member blocks (TOPO-001)
- rule: |
    !(self.topology.mode == 'Standalone') ||
    !has(self.topology.primaries) && !has(self.topology.secondaries) &&
    !has(self.topology.minimumMembers)
  message: members fields are not allowed when mode is Standalone

# Cluster: no GDS on primaries (PLG-001)
- rule: |
    self.topology.mode != 'Cluster' ||
    !has(self.topology.primaries) || !has(self.topology.primaries.plugins) ||
    self.topology.primaries.plugins.all(p, p != 'gds' && p != 'bloom')
  message: GDS and Bloom cannot be installed on primary members in Cluster mode

# Immutability (TOPO-013)
- rule: self.topology.mode == oldSelf.topology.mode
  message: topology.mode cannot change
```

CEL supports: `has()`, `all()`, `exists()`, list/map operations, arithmetic (`% 2 == 1` for odd primary count), string equality, `oldSelf` for updates.

### What CEL cannot express (webhook or reconciler)

| Need | Owner | Example rule IDs |
|------|--------|------------------|
| Referenced Secret exists | **Webhook** | AUTH-002, TLS-002, PLG-010 |
| StorageClass exists | **Webhook** | STO-003 |
| Semver major.minor match | **Webhook** | PLG-006 |
| Version downgrade blocked | **Webhook** | VER-002 |
| PVC shrink blocked (needs live PVC) | **Webhook** | STO-004 |
| Scale-in below formed cluster | **Webhook** | TOPO-010 |
| GDS on secondary pool needs analytics config | **Webhook** | PLG-008, EDT-005 |
| Non-HA topology guidance | **Reconciler** → `TopologyWarning` | TOPO-011, TOPO-012 |
| Unused `pluginDefinitions` key | **Reconciler** → warning | PLG-011 |
| License Secret rotation → rolling restart | **Reconciler** | PLG-012 |

---

## Decision

We will use a **three-layer validation pipeline** for all V1 CRDs (`Neo4j` first; same pattern for `Neo4jBackup`, `Neo4jRestore`, …):

### 1. OpenAPI schema (structural)

- Field types, `required`, enums, `minimum` / `maximum` where static.
- Generated from kubebuilder markers on `api/v1beta1/*_types.go`.

### 2. CEL — primary admission guard

We will embed CEL rules in CRD `x-kubernetes-validations` for:

- **Required fields** conditional on `topology.mode`
- **Forbidden fields** per mode (Standalone vs Cluster)
- **Topology** — odd `primaries.members`, unique pool names, `minimumMembers` bounds *when expressible from spec alone*
- **Plugin placement** — GDS/Bloom forbidden on `primaries.plugins` in Cluster; `licenseSecretRef` required when `gds` referenced
- **Edition / enum** guards — V1 `enterprise` only, `license.accept` values
- **TLS structure** — `trust.enabled` + Cluster ⇒ `cluster` cert ref; HTTPS port ⇒ `trust.enabled`
- **Immutability** — `topology.mode` cannot change after create

**Authoring:** rule IDs and CEL sketches live in [`09-crd-spec/<crd>/validation.md`](../../09-crd-spec/neo4j/validation.md); implementers copy into kubebuilder `+kubebuilder:validation:XValidation` markers or generated CRD YAML.

### 3. Admission webhooks — secondary guard

**Mutating webhook** (defaults only — no rejection except parse errors):

- Apply defaults from `validation.md` § Defaults (e.g. `minimumMembers`, `auth.generatePassword`, empty plugin lists).
- **Must not** inject `primaries` / `secondaries` when `mode: Standalone`.

**Validating webhook** (reject):

- Referenced Kubernetes objects **exist** (Secrets, StorageClasses)
- **Semantic checks** CEL cannot do (semver compatibility, downgrade, analytics config coherence)
- **Warnings** that should surface at apply time (optional V1: PDB on single member, offline mode on cluster) — may move to reconciler if too noisy

### 4. Reconciler — runtime only (not admission)

- `status.conditions` — `TopologyWarning`, `Ready`, `TLSReady`, …
- Preflight against live cluster (cluster formed, members ready)
- Cert/license expiry warnings, unused plugin definition keys

**We will not** duplicate CEL rules in the validating webhook. If a rule is expressible in CEL, it belongs in CEL only.

---

## Validation flow

```
kubectl apply / API POST|PUT
        │
        ▼
┌───────────────────┐
│ OpenAPI schema    │  types, required, enums
└─────────┬─────────┘
          │ pass
          ▼
┌───────────────────┐
│ Mutating webhook  │  defaults (operator)
└─────────┬─────────┘
          │ pass
          ▼
┌───────────────────┐
│ CEL validations   │  cross-field, mode, topology, plugins (API server)
└─────────┬─────────┘
          │ pass
          ▼
┌───────────────────┐
│ Validating webhook│  K8s API lookups, semver, scale policy (operator)
└─────────┬─────────┘
          │ pass
          ▼
     etcd persist
          │
          ▼
┌───────────────────┐
│ Reconciler        │  warnings, runtime checks → status.conditions
└───────────────────┘
```

---

## Rule ownership summary (`Neo4j` V1)

| Mechanism | Approx. rule count | Domains |
|-----------|-------------------|---------|
| **CEL** | ~35 | TOPO, PLG (structural), EDT, STO (static), AUTH (structural), TLS (structural), NET, JVM |
| **Webhook** | ~12 | Secret/SC existence, semver, downgrade, shrink, scale-in, analytics config, PDB warnings |
| **Reconciler** | ~8 | TopologyWarning, unused defs, license rotation, ServiceMonitor CRD absent, upgrade preflight |

Full per-rule assignment: [`09-crd-spec/neo4j/validation.md`](../../09-crd-spec/neo4j/validation.md).

---

## Consequences

### Positive

- Fast, declarative rejection of invalid specs before etcd — most user mistakes caught without operator round-trip.
- CEL rules are **reviewable in CRD YAML** and testable with `kubectl apply --dry-run=server`.
- Webhook stays small — easier to test, less blast radius if webhook pod is down (CEL still protects structural rules).
- Clear pattern for additional CRDs in V1.

### Negative

- Two authoring surfaces (CEL markers + Go webhook) — new rules need an explicit mechanism choice.
- CEL debugging can be opaque — messages must be clear; keep expressions short.
- `TOPO-009` (`minimumMembers > total`) may need CEL with `secondaries.map(p, p.members).sum()` — verify against target K8s version CEL library.

### Neutral

- envtest: CEL rules tested via CRD apply; webhook rules tested via envtest admission suite.
- E2E: invalid manifests must be rejected at API — separate test catalog rows per rule ID where P0.

---

## Adding a new rule (checklist)

1. Assign rule ID in `09-crd-spec/<crd>/validation.md` (e.g. `TOPO-014`).
2. Choose mechanism:
   - Same-object, no external lookup → **CEL**
   - Needs Secret/SC/other API object → **Webhook**
   - Runtime / warning only → **Reconciler**
3. Implement: kubebuilder marker or webhook handler.
4. Test: table-driven unit test (CEL via envtest apply; webhook via envtest admission).

---

## References

- [`09-crd-spec/neo4j/validation.md`](../../09-crd-spec/neo4j/validation.md) — rule catalog and CEL sketches
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md) — field semantics validated by these rules
- [BDR-001](../business/001-single-neo4j-crd.md) — mode-dependent single CRD
- [BDR-002](../business/002-neo4j-crd-topology.md) — topology rules (TOPO-*)
- [BDR-004](../business/004-neo4j-plugin-topology.md) — plugin rules (PLG-*)
- [Kubernetes CRD validation](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation)
- `EST-DEV-002` — API types & validation deliverable
