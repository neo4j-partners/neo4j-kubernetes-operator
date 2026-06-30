# Validation gaps

Helm fields or client needs **without** a matching FR, or **spec / CEL** not yet written.

**Phase 4b** — synced with [`fr-coverage-report.md`](fr-coverage-report.md).

---

## FR gaps

| helm_path | client_need | suggested_fr_id | notes |
|-----------|-------------|-----------------|-------|
| — | — | — | **No in-scope FR gaps** — see out-of-scope table below |

### Out of scope (no FR needed)

| helm_path | client_need | status |
|-----------|-------------|--------|
| `nameOverride` | Predictable K8s name prefix | `metadata.name` |
| `fullnameOverride` | Full resource prefix override | `metadata.name` |
| `disableLookups` | Helm/ArgoCD lookup workaround | operator N/A |

---

## Spec / CEL gaps (V1 lock)

| gap_id | subject | helm_path / concern | blocked by | suggested rule |
|--------|---------|---------------------|------------|----------------|
| VAL-001 | Storage mode immutability | `volumes.data.mode` | BDR-005 proposed | CEL: `data.mode` + binding immutable after create |
| VAL-002 | GDS/Bloom pool restriction | `analytics` + plugins | BDR-004 proposed | TOPO-005 — only on `secondaries.analytics` |
| VAL-003 | Config reserved keys | `config` map | BDR-008 accepted (Option A) | Denylist: topology, ports, TLS, `server.jvm.additional` — see BDR-008 |
| VAL-004 | External exposure default | `services.neo4j.enabled` | BDR-007 proposed | `services.neo4j.type: ClusterIP` by default |
| VAL-005 | Per-pool scale / ordinal safety | `topology.*.members` | BDR-009 accepted (Option B) | Each pool scales independently; no cross-pool ordinal shift |
| VAL-006 | TLS secret contract | `ssl.*` | BDR-006 proposed | `spec.trust` shape + TLS-001..003 |
| VAL-008 | Escape hatch reserved paths | `additionalVolumeMounts`, `secretMounts` | BDR-005 Option E | STO-009, STO-010 |

---

## Index inventory gaps

| item | notes |
|------|-------|
| `neo4j.labels` | In `_domains.yaml` — **missing** from `_index.csv` (add in inventory refresh) |
| `neo4j.operations.*` sub-fields | Partially indexed — image/protocol/ssl marked deferred |

---

## Pre-known semantic gaps (verified)

| Area | Notes |
|------|-------|
| `analytics.*` | Mapped to `secondaries.analytics` — `secondaries.read` is operator-only |
| `podSpec.loadbalancer` | No CRD field — BDR-007 N/A |
| Helm `replicas: 1` vs operator N replicas | Document in `11-helm-mapping.md` |
