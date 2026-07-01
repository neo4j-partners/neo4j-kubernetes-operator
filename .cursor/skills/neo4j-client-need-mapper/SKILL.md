---
name: neo4j-client-need-mapper
description: >-
  Maps Neo4j Helm values to client needs and official Neo4j documentation. Links
  fields to functional requirements NEO-* in design/01-functional_requirements.csv.
  Use when writing client_need and neo4j_doc_ref for helm field analysis.
---

# Neo4j client need mapper

## Per field

1. **Classify intent**

| Intent | Examples |
|--------|----------|
| Installation | edition, minimumClusterSize, image |
| HA / topology | analytics, cluster size, operations |
| Security | password, ssl, license |
| Day-2 config | config.*, jvm |
| Observability | logging, serviceMonitor |
| Storage | volumes.data.mode |

2. **Find Neo4j docs** (web search / fetch)

Priority sources:

- Operations Manual: https://neo4j.com/docs/operations-manual/current/
- Clustering: https://neo4j.com/docs/operations-manual/current/clustering/
- Configuration: https://neo4j.com/docs/operations-manual/current/configuration/
- GDS cluster: https://neo4j.com/docs/graph-data-science/current/production-deployment/
- Plugins: https://neo4j.com/docs/operations-manual/current/configuration/plugins/

3. **Map to FR** — search `01-functional_requirements.csv`

4. **Map to semantic concern** — if topology-related, link `CONCERN-TOPOLOGY` co-paths
   (e.g. `analytics` end of file + `minimumClusterSize` — cite `neo4j-config.yaml`)

Common mappings:

| Helm area | FR |
|-----------|-----|
| Cluster / analytics | NEO-1-002, NEO-2-002-*, NEO-2-011 |
| Config / JVM | NEO-2-003 |
| Auth / secrets | NEO-2-004 |
| TLS | NEO-2-005 |
| Storage | NEO-2-006 |
| Networking | NEO-2-007 |
| Scheduling | NEO-2-008 |
| Probes | NEO-2-009 |
| Scale | NEO-2-011 |
| Upgrade | NEO-2-012 |
| Backup volumes | NEO-2-013 |

4. **Gaps**

If no FR: add row to `design/analysis/helm-fields/validation-gaps.md`

## Client need writing rules

- Start with **who** (app team, platform, DBA) and **outcome**
- Avoid Helm jargon in `client_need` column
- If uncertain: `UNKNOWN — PS input required` (do not invent)

## Output

Fill `_index.csv`: `client_need`, `neo4j_doc_ref`, `fr_ids`

After field analysis completes, run **fr-helm-coverage-validator** to verify `01-functional_requirements.csv` coverage.
