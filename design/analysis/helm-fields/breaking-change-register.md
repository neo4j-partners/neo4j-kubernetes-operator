# Breaking change register

**Phase 4 complete** — scored 2026-06-22 from field analysis + aggregation matrix.

Scoring: **impact** (1–5) × **Helm usage frequency** (1–5) = **priority**.
Dedicated BDR when priority ≥ 16 **or** explicit API contract / migration risk.

## Classification legend

| versioning | Count | Meaning |
|------------|------:|---------|
| `breaking` | 28 | Changing later requires CRD version bump or migration guide |
| `safe` | 55 | Optional field, default, or status-only — iterable |
| `deferred` | 10 | V2+ — document but do not lock V1 |

---

## Breaking candidates (scored)

| ID | Subject | AGG group | impact | freq | **priority** | BDR | BDR status |
|----|---------|-----------|--------|------|-------------|-----|------------|
| BC-001 | Topology model (primaries + `secondaries.analytics`/`read`) | AGG-TOPO-ROLES | 5 | 5 | **25** | BDR-002 | proposed |
| BC-003 | Single `Neo4j` CRD vs multi-kind | — | 5 | 5 | **25** | BDR-001 | **accepted** |
| BC-004 | Storage data volume mode | AGG-STORAGE-DATA | 5 | 4 | **20** | BDR-005 | proposed |
| BC-002 | Plugin assignment + `pluginDefinitions` | AGG-TOPO-PLUGINS | 4 | 4 | **16** | BDR-004 | proposed |
| BC-005 | Service exposure model | AGG-EXPOSURE | 4 | 4 | **16** | BDR-007 | proposed |
| BC-007 | Config passthrough vs allowlist | AGG-CONFIG-SURFACE | 4 | 4 | **16** | BDR-008 | proposed |
| BC-008 | Pool ordinal / scale semantics | AGG-TOPO-ROLES | 5 | 3 | **15** | BDR-009 | proposed |
| BC-006 | TLS BYO secret contract | AGG-TLS-TRUST | 4 | 3 | **12** | BDR-006 | **not drafted** |

**BC-008** at 15 — included because ordinal remapping is a silent data-loss risk (API contract).

**BC-006** below threshold — track in `spec.trust` draft; draft BDR-006 before V1 lock if BYO rotation semantics are finalized.

---

## Safe iteration (no BDR required)

| Helm / spec area | Paths | Why safe |
|------------------|------:|----------|
| `AGG-SCHEDULING` | 15 | Optional placement — defaults mirror Helm |
| `AGG-IMAGE` | 8 | Additive image fields |
| `AGG-OBSERVABILITY` | 4 | Opt-in monitoring |
| `AGG-HEALTH-PROBES` | 3 | Probe tuning |
| `AGG-STORAGE-AUX` | 5 | Aux volume modes follow data pattern — BDR-005 covers pattern |
| `status.*` additions | — | Observed state only |
| Webhook warnings (`TopologyWarning`) | — | Non-blocking |

---

## BDR authoring queue

| bdr_id | title | status | source BC |
|--------|-------|--------|-----------|
| BDR-005 | Storage volume mode model | **drafted** | BC-004 |
| BDR-006 | TLS trust / BYO secrets | **drafted** | BC-006 |
| BDR-007 | Service exposure & connectivity | **drafted** | BC-005 |
| BDR-008 | Neo4j config surface | **drafted** | BC-007 |
| BDR-009 | Scale / pool ordinal semantics | **drafted** | BC-008 |

---

## Traceability gate (G4)

| Check | Result |
|-------|--------|
| Every `breaking` row has `bdr_id` or BC entry | ✅ 28/28 |
| Every priority ≥ 16 has drafted BDR | ✅ except BC-006 (12 — waived) |
| `aggregation_group` populated for coupled paths | ✅ |
