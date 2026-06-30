# `services.neo4j.cleanup`

## Client need

Pre-delete hook to remove shared LoadBalancer service on uninstall (avoids orphaned LB when helm.sh/resource-policy: keep).

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: delete-loadbalancer-hook.yaml
- **Go model**: release_values.go (cleanup nested under Neo4jService — verify at render)
- **K8s resources**: Job, ServiceAccount, Role, RoleBinding (helm hook)
- **Neo4j mechanism**: kubectl delete service on pre-delete when cluster + LB enabled.

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | LB lifecycle on uninstall | services.neo4j |

## CRD mapping (draft)

- **Target**: `N/A (operator finalizer)`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: deferred
- **Rationale**: Operator uninstall semantics TBD.

## FR / AC

- FR: NEO-2-018
- AC: AC-NEO-UNINSTALL

## Open questions

- Replace Helm hook with operator finalizer for shared exposure resources.
