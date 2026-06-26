# `ssl.https`

## Client need

TLS for HTTPS (Browser / HTTP API) — mount cert material and enable HTTPS connector.

## Neo4j documentation

- [HTTPS SSL](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — HTTPS SSL
- [Connectors](https://neo4j.com/docs/operations-manual/current/configuration/connectors/) — Connectors

## Helm implementation

- **Templates**: _ssl.tpl; neo4j-config.yaml (server.https.enabled when privateKey set)
- **Go model**: release_values.go: Ssl.HTTPS
- **K8s resources**: StatefulSet Secret volumes at /var/lib/neo4j/certificates/https/
- **Neo4j mechanism**: Enables `dbms.ssl.policy.https` and `server.https.enabled` when key secret present.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TLS | HTTPS TLS policy | ssl.bolt, services.neo4j.ports.https |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.trust.certificates.https.secretRef`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-TLS-TRUST
- **Must decide with**: AGG-TLS-TRUST

## Versioning

- **Classification**: breaking
- **Rationale**: HTTPS enablement changes client access.

## FR / AC

- FR: NEO-2-005; NEO-3-005-TLS-02
- AC: AC-NEO-TLS; AC-NEO-NETWORKING-PORTS-HTTPS

## Open questions

- None identified.
