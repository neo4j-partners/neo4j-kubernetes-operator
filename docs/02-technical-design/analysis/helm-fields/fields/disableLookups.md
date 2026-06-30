# `disableLookups`

## Client need

GitOps controllers (Argo CD) and `helm template --dry-run` workflows cannot execute Helm `lookup` calls against the live API server. Teams need a flag to skip pre-install validation that depends on cluster state (existing Secrets, Nodes, Services) so manifests render without failing.

## Neo4j documentation

- [Neo4j Helm charts](https://neo4j.com/docs/operations-manual/current/kubernetes/) — install prerequisites

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (password Secret lookup, nodeSelector validation, passwordFromSecret validation); `_secretMounts.tpl`, `neo4j-auth.yaml`, `neo4j-imagePullSecret.yaml`, `_ldap.tpl`, `_apocCredentials.tpl`, `neo4j-loadbalancer.yaml`
- **Go model**: `helm-charts/internal/model/release_values.go` — `HelmValues.DisableLookups` (default `false`)
- **K8s resources**: Affects whether chart validates Secret/Node/Service existence at render time; no dedicated resource
- **Neo4j mechanism**: None — install-time Helm behaviour

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Helm install ergonomics | `neo4j.passwordFromSecret`, `nodeSelector`, `image.imagePullSecrets` |

## CRD mapping (draft)

- **Target**: N/A — operator validates via API server reconciliation, not Helm lookup
- **Notes**: Operator should surface equivalent validation errors in status conditions

## Aggregation

- **Group**: none
- **Must decide with**: —

## Versioning

- **Classification**: safe
- **Rationale**: Not part of workload spec

## FR / AC

- FR: —
- AC: —

## Open questions

- Document operator equivalent for Argo CD users (admission vs reconcile-time checks)?
