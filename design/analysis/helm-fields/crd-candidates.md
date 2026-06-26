# CRD candidates (Phase 3 synthesis)

Synthesized from [`_index.csv`](_index.csv) by **crd-synthesis-analyst** — 93 Helm paths, 55 safe / 28 breaking / 10 deferred.

---

## V1 CRD inventory

| CRD | Lifecycle | Decision | Helm coverage |
|-----|-----------|----------|---------------|
| `Neo4j` | Workload | [BDR-001](../../../decision-records/business/001-single-neo4j-crd.md) **accepted** | ~85 in-scope paths embedded in `spec` |
| `Neo4jDatabase` | Logical DB | `20-operator-proposal.md` | Not in neo4j chart values |
| `Neo4jBackup` / `Neo4jBackupSchedule` | Backup jobs | Separate CRD | `volumes.backups` triggers only |
| `Neo4jRestore` | One-shot restore | Separate CRD | Not in chart values |

**Rejected**: `Neo4jPlugin`, `Neo4jPersistence`, per-pool CRDs — [BDR-001](../../../decision-records/business/001-single-neo4j-crd.md), [BDR-004](../../../decision-records/business/004-neo4j-plugin-topology.md) Option E.

---

## `Neo4j.spec` section mapping

| `spec` section | AGG groups | Helm paths | BDR | versioning |
|----------------|------------|------------|-----|------------|
| `edition`, `version`, `license` | neo4j-core | 3 | — | breaking |
| `topology` | AGG-TOPO-ROLES | 10 | BDR-002, BDR-009 | breaking |
| `pluginDefinitions` + pool `plugins` | AGG-TOPO-PLUGINS | 5 | BDR-004 | breaking |
| `image` | AGG-IMAGE | 8 | — | safe |
| `auth` | AGG-AUTH | 3 | — | safe |
| `persistence` | AGG-STORAGE-* | 17 | BDR-005 | breaking (data mode) |
| `resources`, `jvm`, `config` | AGG-CONFIG-SURFACE + core | 6 | BDR-008 | breaking (config) |
| `trust` | AGG-TLS-TRUST | 4 | BDR-006 | breaking |
| `connectivity` | AGG-EXPOSURE | 11 | BDR-007 | breaking |
| `scheduling`, `podDisruptionBudget` | AGG-SCHEDULING | 16 | — | safe |
| `probes` | AGG-HEALTH-PROBES | 3 | — | safe |
| `security` | security domain | 2 | — | safe |
| `monitoring` | AGG-OBSERVABILITY | 4 | — | safe |
| `maintenance` | neo4j-core | 1 | — | safe |
| `podTemplate` | scheduling escape | 1+ | — | safe |

---

## Operator-internal (no user `spec` field)

| Helm path | Operator behavior |
|-----------|-------------------|
| `neo4j.operations.*` | Reconcile Job replacing Helm post-install hook |
| `podSpec.loadbalancer` | Eliminated — single STS, no per-pod LB include label |
| `services.neo4j.cleanup` | Finalizer on external Service |
| `nameOverride`, `fullnameOverride`, `disableLookups` | N/A — use `metadata.name` |
| `logInitialPassword` | Status / one-time Secret — not spec |

---

## Deferred V1 (`versioning=deferred`)

| helm_path | notes |
|-----------|-------|
| `neo4j.operations.image/protocol/ssl` | Operator Job implementation detail |
| `services.neo4j.multiCluster` | `spec.connectivity.multiCluster` — V1.1+ |
| `ldapPassword*` | LDAP auth V2 |
| `image.imageCredentials` | Chart pull-secret helper — prefer `imagePullSecrets` |
| Community edition paths | `neo4j.edition: community` — enterprise-only V1 |

---

## Separate CRD evaluation

| Candidate | Helm trigger | Verdict |
|-----------|--------------|---------|
| `Neo4jPlugin` | GDS/Bloom license blocks | **Deferred V2** — BDR-004 Option E |
| `Neo4jPersistence` | `volumes.*` | **Rejected** — embedded `persistence` |
| `Neo4jService` / exposure CRD | `services.*` | **Rejected** — embedded `connectivity` |
| Per-pool CRD | analytics / read | **Rejected** — fixed keys in `topology` |

---

## `secondaries.read` — operator net-new

No direct Helm equivalent. Helm read-scale is modeled as additional single-replica releases or `minimumClusterSize` primaries only. Operator adds **`secondaries.read`** as fixed pool key ([BDR-002](../../../decision-records/business/002-neo4j-crd-topology.md)).

---

## Per-field mapping

Authoritative columns: `_index.csv` → `crd_target`, `aggregation_group`, `versioning`, `bdr_id`.

Field narratives: [`fields/`](fields/).

**Naming alignment** (Phase 6): `crd_target` uses `spec.persistence` and `spec.connectivity` per [`spec.md`](../../../09-crd-spec/neo4j/spec.md) — not `storage` / `networking`.
