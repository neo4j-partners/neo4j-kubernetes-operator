# `ssl.bolt`

## Client need

TLS for Bolt connections — mount private key and public certificate from Secrets, require encrypted Bolt, and optionally enforce **mTLS** (client certificate authentication).

## Neo4j documentation

- [Bolt connector SSL](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — Bolt connector SSL; `client_auth` (`NONE` \| `OPTIONAL` \| `REQUIRE`)
- [Kubernetes SSL](https://neo4j.com/docs/operations-manual/current/kubernetes/security/) — `trustedCerts.sources` for client CA material
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _ssl.tpl; neo4j-config.yaml (`server.bolt.tls_level REQUIRED`; `dbms.ssl.policy.bolt.enabled`; **`dbms.ssl.policy.bolt.client_auth: NONE`** hard-coded)
- **Go model**: release_values.go: Ssl.Bolt
- **K8s resources**: StatefulSet Secret volumes at `/var/lib/neo4j/certificates/bolt/`; projected volume for `trustedCerts` → `…/bolt/trusted/`
- **Neo4j mechanism**: When `privateKey.secretName` set: enables bolt SSL policy and `server.bolt.tls_level: REQUIRED`. mTLS requires `trustedCerts` + `client_auth REQUIRE` (today via ad-hoc `config`, not first-class in values).

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TLS | Bolt TLS policy | ssl.https, ssl.cluster, services.neo4j.ports.bolt |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.trust.certificates.bolt` (`privateKey`, `publicCertificate`, `clientAuth`, `trustedCerts.sources`)
- **Notes**: Operator adds first-class mTLS (`clientAuth` + `trustedCerts`); Helm only exposes `trustedCerts` in values, `client_auth` injected as `NONE`.

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
