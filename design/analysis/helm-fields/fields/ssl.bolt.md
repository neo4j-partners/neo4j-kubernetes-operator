# `ssl.bolt`

## Client need

TLS for Bolt connections — mount private key and public certificate from Secrets and require encrypted Bolt.

## Neo4j documentation

- [Bolt connector SSL](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — Bolt connector SSL
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _ssl.tpl; neo4j-config.yaml (server.bolt.tls_level REQUIRED; dbms.ssl.policy.bolt.enabled)
- **Go model**: release_values.go: Ssl.Bolt
- **K8s resources**: StatefulSet Secret volumes at /var/lib/neo4j/certificates/bolt/
- **Neo4j mechanism**: When privateKey.secretName set: enables bolt SSL policy and `server.bolt.tls_level: REQUIRED`.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TLS | Bolt TLS policy | ssl.https, ssl.cluster, services.neo4j.ports.bolt |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.trust.certificates.bolt.secretRef`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-TLS-TRUST
- **Must decide with**: AGG-TLS-TRUST

## Versioning

- **Classification**: breaking
- **Rationale**: Bolt TLS mode affects all clients.

## FR / AC

- FR: NEO-2-005; NEO-3-005-TLS-01
- AC: AC-NEO-TLS; AC-NEO-NETWORKING-PORTS-BOLT

## Open questions

- None identified.
