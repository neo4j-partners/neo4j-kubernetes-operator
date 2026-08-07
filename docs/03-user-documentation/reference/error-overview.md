# Error overview (test oracle)

Stable `status.conditions[].reason` values emitted by the operator. Prefer matching
**Reason** in tests and runbooks — messages are free-form and may change.

Source of truth in code: `src/internal/status/oracle.go` (`ErrorOracle`).

## How to read a failure

```bash
kubectl get neo4j <name> -n <ns> -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}'
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager --tail=200
```

Structured logs use `msg`, `level`, and logger names (`neo4j`, `pipeline`, domain).
See [Operator logging](../operator/05-logging.md).

## Catalog

| Condition | Reason | Severity | Meaning |
|-----------|--------|----------|---------|
| Error | ReconcileFailed | error | A pipeline step returned an error |
| Ready | ReconcileError | error | Ready cleared because reconcile failed |
| Reconciling | Failed | error | Reconciling stopped after failure |
| TLSReady | SecretMissing | error | Required TLS/auth Secret is missing or incomplete |
| StorageReady | PVCPending | warn | Data PVC not Bound yet (or missing StorageClass) |
| Ready | OfflineMaintenance | info | `spec.maintenance.offlineMode` is true |
| ClusterFormed | BoltUnavailable | warn | Cannot reach Bolt to form/align the cluster |
| ClusterFormed | ShowServersFailed | error | `SHOW SERVERS` failed over Bolt |
| ClusterFormed | UnsupportedSinglePrimary | error | Neo4j forbids shrinking to a single primary |
| ClusterFormed | UnsupportedSystemScaleUp | error | Cannot grow system DB from 1 primary via ENABLE alone |
| ClusterFormed | WaitingSystemLeader | warn | Waiting for a system database leader |
| ClusterFormed | WaitingQuorum | warn | Waiting for primary quorum / enable completion |
| ServersPendingDrain | UnsupportedSinglePrimary | error | Drain blocked — would leave a single primary |
| ServersPendingDrain | ShrinkingTopology | info | Scale-in in progress |
| ServersPendingDrain | Draining | info | Server drain / DEALLOCATE in progress |
| ServersPendingDrain | AwaitingSTSShrink | info | Waiting for StatefulSet replica shrink after drain |

## Test usage

Assert on reasons from this table (or import `status.ErrorOracle` in Go tests):

```go
c := meta.FindStatusCondition(neo4j.Status.Conditions, status.ConditionStorageReady)
if c.Reason != "PVCPending" { ... }
```

Operational playbooks: [Troubleshooting](../operator/04-troubleshooting.md).
