# `ssl.cluster`

## Client need

TLS for intra-cluster communication (discovery, raft, backup between members).

## Neo4j documentation

- [Cluster SSL](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — Cluster SSL
- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — Clustering

## Helm implementation

- **Templates**: _ssl.tpl; neo4j-config.yaml (dbms.ssl.policy.cluster.enabled)
- **Go model**: release_values.go (cluster policy in values.yaml; Go struct may lag — verify)
- **K8s resources**: StatefulSet Secret volumes at /var/lib/neo4j/certificates/cluster/
- **Neo4j mechanism**: Enables cluster SSL policy when privateKey secret configured.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TLS | cluster TLS policy | ssl.bolt, ssl.https |
| CONCERN-TOPOLOGY | cluster inter-member encryption | services.internals, neo4j.minimumClusterSize |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.trust.certificates.cluster.secretRef`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-TLS-TRUST
- **Must decide with**: AGG-TLS-TRUST

## Versioning

- **Classification**: breaking
- **Rationale**: Cluster TLS required for secure multi-member deployments.

## FR / AC

- FR: NEO-2-005; NEO-3-005-TLS-03
- AC: AC-NEO-TLS; AC-NEO-CLUSTER

## Open questions

- Go model Ssl struct may need Cluster field added.
