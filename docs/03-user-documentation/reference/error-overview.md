# Error overview (test oracle)

Stable `status.conditions[].reason` values emitted by the operator. Prefer matching
**Reason** in tests and runbooks — messages are free-form and may change.

Source of truth in code: `src/internal/status/oracle.go` (`ErrorOracle`).

## How to read a failure

```bash
kubectl get neo4j <name> -n <ns> -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}'
kubectl describe neo4j <name> -n <ns>   # Events carry the same Reason as the condition
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager --tail=200
```

Rejected Secrets (`SecretNotMountable`, `SecretNotDelegated`) also emit a `Warning` Event under
the same reason — the operator stops before creating any StatefulSet, so `kubectl describe` is
usually the fastest way to see why nothing was deployed.

Reasons with **no condition** (`—` in the table) are Event-only: the CR is healthy, but the
operator resolved something silently and wants you to know. They appear in `kubectl describe`
and in the operator log; nothing turns `Ready` false.

Structured logs use `msg`, `level`, and logger names (`neo4j`, `pipeline`, domain).
See [Operator logging](../operator/05-logging.md).

## Catalog

| Condition | Reason | Severity | Meaning |
|-----------|--------|----------|---------|
| Error | ReconcileFailed | error | A pipeline step returned an error |
| Error | SecretNotMountable | error | Referenced Secret lacks the `neo4j.com/mountable-by-operator` opt-in label (NEO-005) |
| Error | SecretNotDelegated | error | BYO auth Secret is not delegated to this Neo4j via `neo4j.com/allowed-for` (ADD-01) |
| — (Event only) | DuplicateEntry | warn | Two values collided on the same key in a spec field; the Event names the field, the value kept and the one dropped |
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

## DuplicateEntry

One reason for every spec field whose rendering merges layers, so a value never disappears in
silence. The Event message carries the field, the key, and both values with their origin
(`user`, `neo4j-default`, `operator-default`, `plugin-definition`, `operator-injected`):

```text
Warning  DuplicateEntry  spec.config.jvm.additionalArguments: duplicate entry for
-Djdk.nio.maxCachedBufferSize — kept "-Djdk.nio.maxCachedBufferSize=2048" (user),
dropped "-Djdk.nio.maxCachedBufferSize=1024" (neo4j-default)
```

Currently reported for `spec.config.jvm.additionalArguments` (same flag twice, or a flag
replacing a Neo4j default — NEO-3-003-JVM-01) and `spec.config.neo4j` (a key set twice across
the defaults, plugin, user and operator-injected layers — BDR-008).

Two directions matter. `kept (user)` means your value won as intended, over an operator default
(`server.default_listen_address`, `server.directories.plugins`,
`dbms.security.procedures.*`) or a plugin definition. `kept (operator-injected)` means the key
is operator-owned and your setting was discarded — cluster discovery, routing and advertised
addresses (`dbms.cluster.*`, `dbms.routing.*`, `dbms.kubernetes.*`, `server.*.advertised_address`,
`initial.dbms.default_primaries_count`), TLS policies derived from `spec.trust`
(`dbms.ssl.policy.*`, `server.bolt.tls_level`), log config paths (`server.logs.*.config`) and the
listener toggles not already refused by CEL (`server.backup.enabled`,
`server.metrics.prometheus.*`). Set those through the dedicated spec fields instead.

## Test usage

Assert on reasons from this table (or import `status.ErrorOracle` in Go tests):

```go
c := meta.FindStatusCondition(neo4j.Status.Conditions, status.ConditionStorageReady)
if c.Reason != "PVCPending" { ... }
```

Operational playbooks: [Troubleshooting](../operator/04-troubleshooting.md).
