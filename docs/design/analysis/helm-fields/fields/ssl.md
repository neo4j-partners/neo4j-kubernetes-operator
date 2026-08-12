# `ssl`

## Client need

Configure TLS certificate material for Neo4j connectors (bolt, https, cluster) via Kubernetes Secrets.

## Neo4j documentation

- [SSL framework](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — SSL framework
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _ssl.tpl; neo4j-statefulset.yaml; neo4j-config.yaml
- **Go model**: release_values.go: Ssl (Bolt, HTTPS; cluster in values.yaml)
- **K8s resources**: StatefulSet (Secret/projected volumes), ConfigMap (neo4j.conf SSL policies)
- **Neo4j mechanism**: Mounts certs under `/var/lib/neo4j/certificates/{policy}/`; enables dbms.ssl.policy.* when privateKey set.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TLS | TLS root map | ssl.bolt, ssl.https, ssl.cluster, config.dbms.security.tls_reload_enabled |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.trust`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-TLS-TRUST
- **Must decide with**: AGG-TLS-TRUST

## Versioning

- **Classification**: breaking
- **Rationale**: TLS trust model is security-critical.

## FR / AC

- FR: NEO-2-005
- AC: AC-NEO-TLS

## Open questions

- BDR-006: unified trust model per workload.
