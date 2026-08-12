# `neo4j.operations.ssl`

## Client need

When Neo4j Bolt uses TLS with custom CAs or hostname verification settings, the operations Job must mirror those TLS client options so ENABLE SERVER succeeds. The `ssl` sub-map controls hostname verification skip and insecure-verify flags for the Job only.

## Neo4j documentation

- [SSL framework](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — Bolt TLS policies
- [Configuration — `dbms.ssl.policy.bolt`](https://neo4j.com/docs/operations-manual/current/configuration/) — verify_hostname

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-operations.yaml` L60–65 (`SSL_DISABLE_HOSTNAME_VERIFICATION`, `SSL_INSECURE_SKIP_VERIFY` env when `operations.ssl` set)
- **Go model**: `release_values.go` — `Operations.SSL` (`OperationsSSL` struct)
- **K8s resources**: Job pod env
- **Neo4j mechanism**: Client-side TLS options for operations Go driver; should match server `dbms.ssl.policy.bolt.verify_hostname`

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Cluster ops Job TLS | `neo4j.operations.protocol`, `ssl.bolt`, `neo4j.minimumClusterSize` |

## CRD mapping (draft)

- **Target**: N/A — operator derives from `spec.trust`
- **Notes**: Sub-fields: `disableHostnameVerification`, `insecureSkipVerify`

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.operations.protocol`, `ssl.bolt`

## Versioning

- **Classification**: safe
- **Rationale**: Internal Job configuration

## FR / AC

- FR: NEO-2-005, NEO-2-011
- AC: AC-NEO-TLS, AC-NEO-SCALE

## Open questions

- Production guidance: avoid `insecureSkipVerify` — document operator equivalent?
