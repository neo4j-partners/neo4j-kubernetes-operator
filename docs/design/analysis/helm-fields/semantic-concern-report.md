# Semantic concern report — 2026-06-22

**Validator**: helm-semantic-concern-mapper  
**Helm**: `helm-charts/neo4j/values.yaml`  
**Operator CRD**: `design/09-crd-spec/neo4j/spec.md` (fixed pools `secondaries.analytics` / `secondaries.read`)

---

## CONCERN-TOPOLOGY

### Scattered helm paths

| helm_path | values.yaml region | role in concern | template driver |
|-----------|-------------------|-----------------|-----------------|
| `neo4j.minimumClusterSize` | `neo4j:` ~L28 | HA quorum threshold (≥3 → cluster) | `neo4j.isClusterEnabled` in `_helpers.tpl` |
| `neo4j.operations.enableServer` | `neo4j:` ~L38 | Post-install ENABLE SERVER | operations Job |
| `neo4j.edition` | `neo4j:` ~L24 | enterprise required for cluster | `_helpers.tpl` fail if community + cluster |
| `analytics.enabled` | **end** ~L764 | primary+secondary analytics layout | `$primaryAnalyticsType`, `$secondaryAnalyticsType` |
| `analytics.type.name` | end ~L768 | `primary` \| `secondary` server role | `neo4j-config.yaml` branches |
| `services.internals.enabled` | `services:` mid-file | cluster discovery ports | `neo4j-svc.yaml` with cluster/analytics |
| `services.neo4j.multiCluster` | services | multi-zone cluster | loadbalancer / internals |
| `podSpec.loadbalancer` | `podSpec` | include/exclude pod from LB | STS labels |
| `config` (implicit) | map | cluster primaries count, SECONDARY mode | `neo4j-config.yaml` default-config CM |

### Template proof chain

1. `_helpers.tpl` → `neo4j.isClusterEnabled`: `minimumClusterSize >= 3` + enterprise → `$clusterEnabled`
2. `neo4j-config.yaml` L6–7: `analytics.enabled` + `type.name` → primary vs secondary analytics types
3. L94–102: when `$clusterEnabled`, inject `initial.dbms.default_primaries_count` from `minimumClusterSize`
4. L104–107: primary analytics → force `default_primaries_count: 1`
5. L137–147: secondary analytics → `SECONDARY` mode_constraint + GDS procedures
6. `neo4j-statefulset.yaml` L38: **`replicas: 1`** — Helm HA = multiple releases, not one STS

### Neo4j doc alignment

| Helm signal | Neo4j product concept |
|-------------|----------------------|
| `minimumClusterSize >= 3` | Causal cluster / system primaries |
| `analytics.type: primary` | Single-writer + analytics topology |
| `analytics.type: secondary` | Secondary server (GDS on secondary) |
| `operations.enableServer` | Cluster admin — enable added server |

### BDR-002 exemplar verdict: **amend (applied)**

| Aspect | Helm | Operator CRD (current spec) |
|--------|------|----------------------------|
| Member layout | 1 release = 1 pod | 1 CR = 1 STS, N replicas |
| Secondary intent | `analytics.type` at file end | Fixed keys `secondaries.analytics`, `secondaries.read` |
| Pool naming | N/A (per-release analytics flag) | No free `name` — schema encodes intent |

**Rationale for fixed keys**: Helm scatters topology (`minimumClusterSize` top, `analytics` bottom) but couples them in `neo4j-config.yaml`. Operator aggregates into `spec.topology` with **`analytics`** (GDS/Bloom) and **`read`** (read scale) — aligns with Helm `analytics.type: secondary` vs read-replica semantics without arbitrary pool names.

**Ordinal order (operator)**: primaries → `analytics` → `read`.

### Operator CRD implication

- Map Helm `analytics.type: secondary` + GDS config → `secondaries.analytics`
- Map read scaling (no GDS on primaries) → `secondaries.read`
- Map `minimumClusterSize: 3` → `primaries.members: 3`, `minimumMembers: 3`
- Map `minimumClusterSize: 1` + analytics primary → `primaries.members: 1` + `secondaries.analytics`
- Document **multi-release → single STS** as intentional operator divergence in `11-helm-mapping.md`

---

## CONCERN-PLUGINS-ON-TOPOLOGY

### BDR-004 exemplar verdict: **align + amend**

- Helm injects GDS procedure allowlist when `$secondaryAnalyticsType` (`neo4j-config.yaml` L140–142)
- Operator: `gds`/`bloom` only on `secondaries.analytics.plugins` (CEL TOPO-005)
- `pluginDefinitions` remains central (Option E)

---

## CONCERN-STORAGE-ESCAPE

### Scattered helm paths

| helm_path | role | template driver |
|-----------|------|-----------------|
| `additionalVolumes` | raw StatefulSet `volumes[]` | `_volumeTemplate.tpl` |
| `additionalVolumeMounts` | Neo4j container `volumeMounts[]` | `_volumeTemplate.tpl` |
| `secretMounts` | map → Secret volume + mount | `_secretMounts.tpl` |

### BDR-005 verdict: **Option E (paired mounts + secretMounts map)**

- Merge Helm split lists → `spec.additionalMounts[]`
- `secretMounts` → `spec.secretMounts` (not `trust`, not `persistence`)
- Overlap with `Neo4jRestore.spec.source.credentials` documented — Job vs pod lifecycle

---

## Gates

| Gate | Status |
|------|--------|
| G-SC-1 | PASS — ≥4 paths from ≥2 YAML regions |
| G-SC-2 | PASS — template_evidence documented |
| G-SC-3 | PASS — `analytics.*` linked to topology |
| G-SC-4 | PASS — BDR-002/004 verdict documented |
| G-SC-5 | PASS — CONCERN-STORAGE-ESCAPE documented |
