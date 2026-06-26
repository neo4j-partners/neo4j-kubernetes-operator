# `podDisruptionBudget`

## Client need

Operators protect Neo4j availability during voluntary disruptions (node drains, cluster upgrades) by limiting how many pods may be unavailable. Helm PDB is per-release (single pod); cluster-wide protection needs a shared label selector across members.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/clustering/) — Cluster availability

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-pdb.yaml`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: PodDisruptionBudget (policy/v1)
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | disruption tolerance | neo4j.minimumClusterSize, topology |; | CONCERN-TOPOLOGY | quorum during drain | neo4j.minimumClusterSize |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podDisruptionBudget`
- **Notes**: CRD defaults enabled=true for cluster ≥3 members; Helm default enabled=false.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: breaking
- **Rationale**: Helm per-release PDB vs operator single-STS cluster-wide PDB with minAvailable derived from topology.

## FR / AC

- FR: NEO-2-008; NEO-2-002-CSZ-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Operator auto-compute minAvailable from primaries.members + quorum rules?
