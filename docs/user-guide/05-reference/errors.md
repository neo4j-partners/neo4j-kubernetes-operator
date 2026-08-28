# Error reference

Every stable `status.conditions[].reason` the operator emits. Match on **Reason** in runbooks,
alerts and tests: it is a contract and will not change silently, whereas the human-readable message
may be reworded at any time.

The two tables below are generated from the operator's own catalog of reasons, and the operator
cannot emit a reason that is missing from it. So this is the whole surface: if you read a reason in
`kubectl describe` and it is not on this page, you are looking at a different version.

## How to read a failure

```bash
kubectl get neo4j <name> -n <ns> -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}'
kubectl describe neo4j <name> -n <ns>   # Events carry the same Reason as the condition
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager --tail=200
```

Rejected Secrets (`SecretNotMountable`, `SecretNotDelegated`, `AuthSecretInvalid`) also emit a `Warning` Event under
the same reason — the operator stops before creating any StatefulSet, so `kubectl describe` is
usually the fastest way to see why nothing was deployed.

Reasons with **no condition** (`— (Event only)` in the table) are Event-only: the CR is healthy, but
the operator resolved something silently and wants you to know. They appear in `kubectl describe`
and in the operator log; nothing turns `Ready` false.

The **Surface** column says where a reason shows up. `condition+event` means the same identifier is
written to `status.conditions[].reason` *and* recorded as an Event, so `kubectl describe` and any
automation gating on the condition agree on one string.

Structured logs use `msg`, `level`, and logger names (`neo4j`, `pipeline`, domain).
See [Operator logs](../04-troubleshooting/02-operator-logs.md).

## Catalog

Reasons that report a problem, a decision, or an operation in progress.

<!-- BEGIN GENERATED oracle:catalog -->
| Condition | Reason | Severity | Surface | Meaning |
|-----------|--------|----------|---------|---------|
| Ready | MembersNotReady | warn | condition | Fewer servers ready than desired; the message carries both counts |
| Ready | TLSNotReady | warn | condition | Held back by TLSReady — trust material is missing or still being issued |
| Ready | OfflineMaintenance | info | condition | `spec.maintenance.offlineMode` is true, so the Neo4j process is not running |
| Ready | ReconcileError | error | condition | Ready cleared because reconcile failed |
| Reconciling | Failed | error | condition | Reconciling stopped after failure |
| Installed | Pending | info | condition | No StatefulSet observed yet; expected on a first pass, a symptom if it lasts |
| Error | ReconcileFailed | error | condition+event | A pipeline step returned an error |
| Error | SecretNotMountable | error | condition+event | Referenced Secret lacks the `neo4j.com/mountable-by-operator` opt-in label (NEO-005) |
| Error | SecretNotDelegated | error | condition+event | BYO auth Secret is not delegated to this Neo4j via `neo4j.com/allowed-for` (ADD-01) |
| Error | AuthSecretInvalid | error | condition+event | Auth Secret holds a `NEO4J_AUTH` value the Neo4j image entrypoint cannot use; the pod would crash-loop |
| StorageReady | PVCPending | warn | condition | Data PVC not Bound yet; the message names the StorageClass, or reports that none is set |
| TLSReady | SecretMissing | error | condition | Required TLS/auth Secret is missing or incomplete |
| TLSReady | CertificatePending | warn | condition | Waiting for cert-manager to issue the certificate into the operator-provisioned Secret |
| ClusterFormed | EnablingServer | info | condition | `ENABLE SERVER` in progress for a server that joined the pool |
| ClusterFormed | BoltUnavailable | warn | condition | Cannot reach Bolt to form or align the cluster |
| ClusterFormed | BootstrapGateTooHigh | error | condition | `topology.minimumMembers` asks for more primaries than the pool has, so the system database never bootstraps and Bolt never answers |
| ClusterFormed | ShowServersFailed | error | condition | `SHOW SERVERS` failed over Bolt |
| ClusterFormed | UnsupportedSystemScaleUp | error | condition | Cannot grow the system database from a single primary via `ENABLE SERVER` alone |
| ClusterFormed | WaitingSystemLeader | warn | condition | Waiting for a system database leader |
| ClusterFormed | WaitingQuorum | warn | condition | Waiting for primary quorum, or for an enable to complete |
| ClusterFormed | UnsupportedSinglePrimary | error | condition | Neo4j forbids shrinking to a single primary |
| ServersPendingDrain | UnsupportedSinglePrimary | error | condition | Drain blocked — it would leave a single primary |
| ServersPendingDrain | ShrinkingTopology | info | condition | Scale-in in progress |
| ServersPendingDrain | Draining | info | condition | Server drain / `DEALLOCATE DATABASES` in progress |
| ServersPendingDrain | AwaitingSTSShrink | info | condition | Waiting for the StatefulSet replica shrink after drain |
| BackupReady | BackupInProgress | info | condition | The backup Job is running |
| BackupReady | BackupJobFailed | error | condition+event | The backup Job failed; the message carries the failure detail |
| BackupReady | BackupTargetNotFound | warn | condition | `spec.neo4jRef` does not resolve to a Neo4j in this namespace yet |
| BackupReady | BackupEditionUnsupported | error | condition+event | Backup requires Enterprise edition; the target is community |
| BackupReady | BackupListenerDisabled | warn | condition | The target has no backup listener; set `features.backup` and `connectivity.listeners.backup` |
| BackupReady | BackupDestinationUnsupported | error | condition+event | The `destination` cannot be realized (e.g. PVC provisioning is not yet supported; use an existing claimName) |
| — (Event only) | DuplicateEntry | warn | event | Two values collided on the same key in a spec field; the Event names the field, the value kept and the one dropped |
| — (Event only) | DatabaseTopologyResized | warn | event | A scale-in forced `ALTER DATABASE SET TOPOLOGY` on a database wider than the remaining pool; the Event names the database and both counts, before and after |
| — (Event only) | InsecureAdminConnection | warn | event | The operator's own admin Bolt connection is unencrypted because `trust.insecureAdminConnection` is true (NEO-004) |
| — (Event only) | AdminBoltTLSRequired | warn | event | The operator refuses to dial admin Bolt without `trust.certificates.bolt` or `trust.insecureAdminConnection` (NEO-004) |
<!-- END GENERATED oracle:catalog -->

## Steady-state reasons

The other half of the contract: the reasons a healthy CR carries. Nothing here needs acting on, and
they are listed so that a reason you read in `kubectl describe` is always documented somewhere — and
so automation can tell "fine" from "not fine" without a hardcoded list.

<!-- BEGIN GENERATED oracle:steady -->
| Condition | Reason | Severity | Surface | Meaning |
|-----------|--------|----------|---------|---------|
| Ready | AllMembersReady | info | condition | Every desired server is ready and the CR is serving |
| Reconciling | InProgress | info | condition | A reconcile pass is running |
| Reconciling | Completed | info | condition | The last reconcile pass finished |
| Installed | ObjectsCreated | info | condition | The operands exist — at least one StatefulSet was observed |
| Error | NoError | info | condition | No pipeline error on the last pass |
| StorageReady | PVCBound | info | condition | The data PVC is Bound |
| TLSReady | TrustDisabled | info | condition | `trust.enabled` is false, so there is nothing to verify |
| TLSReady | SecretsPresent | info | condition | Required TLS secrets and keys are present |
| ClusterFormed | Formed | info | condition | All desired servers are enabled in the Neo4j cluster |
| ServersPendingDrain | NoDrain | info | condition | No server is waiting to be drained |
| BackupReady | BackupSucceeded | info | condition | The backup Job completed and artifacts were written |
| — (Event only) | SecretMounted | info | event | A labelled Secret is being mounted into the Neo4j pods; the Event names the Secret and the opt-in label |
<!-- END GENERATED oracle:steady -->

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
`initial.dbms.default_primaries_count`, `initial.dbms.default_secondaries_count`), TLS policies derived from `spec.trust`
(`dbms.ssl.policy.*`, `server.bolt.tls_level`), log config paths (`server.logs.*.config`) and the
listener toggles not already refused by CEL (`server.backup.enabled`,
`server.metrics.prometheus.*`). Set those through the dedicated spec fields instead.

## DatabaseTopologyResized

A scale-in is the one occasion where the operator rewrites a database topology: a database cannot
keep asking for five primaries on a pool that now holds three, and Neo4j refuses to release the
servers while it does. So the operator runs `ALTER DATABASE ... SET TOPOLOGY` first — on topologies
you may have set yourself. That rewrite is deliberate, but never silent, so each one produces an
Event and a matching operator log entry:

```text
Warning  DatabaseTopologyResized  database "orders": requested topology rewritten from
5 primaries / 0 secondaries to 3 / 0 — the scale-in leaves fewer servers than the topology claimed
```

Nothing else triggers it. Growing a pool, or setting `topology.defaultPrimariesCount` to something
wider than an existing database, leaves that database exactly as it is; the field only decides what
the *next* database gets. Databases are only ever narrowed to the number of servers the pool still
holds, never further, and the `system` and composite databases are never touched.

A database with several primaries cannot be narrowed to exactly one — Neo4j forbids it — which
surfaces as `UnsupportedSinglePrimary` instead. See
[Clustering](../03-neo4j/02-clustering.md) for the scale-in sequence around it.

## AuthSecretInvalid

The Neo4j image sets the initial password from the `NEO4J_AUTH` key of the auth Secret, in the
form `neo4j/<password>`. Its entrypoint is strict, and every rejection below makes the container
exit before Neo4j starts — which without this check would show up only as a `CrashLoopBackOff`
pod and a CR stuck at `0/1 servers ready`. The operator refuses the Secret instead, so the reason
and the Event tell you what to fix:

| Rejected because | Why the container would die |
|------------------|-----------------------------|
| The password starts with `-` | The entrypoint passes it as a positional argument to `neo4j-admin dbms set-initial-password` without a `--` separator, so the CLI parser reads it as an option and reports a missing parameter |
| The value is not `neo4j/<password>`, or the password contains `/` | The entrypoint cannot parse it and exits with `Invalid value for NEO4J_AUTH` |
| The user part is not `neo4j` | The image only accepts the `neo4j` administrator |
| The password is `neo4j` | The image refuses the default password |

Generated passwords are alphanumeric, so they never hit these rules. Bring-your-own Secrets can,
and so can a hand-edited generated Secret, since the operator reuses an existing one as-is.
Messages never quote the value.

See [Security](../03-neo4j/05-security.md) for how auth Secrets are labelled and delegated.

## How often an Event is recorded

Events that restate the spec — `DuplicateEntry`, `SecretMounted`, `InsecureAdminConnection`,
`AdminBoltTLSRequired` — are recorded **once per `metadata.generation`**, not on every reconcile
pass. Edit the spec and they are recorded again; leave it alone and they stay as they are, however
long the operator keeps reconciling.

That is not cosmetic. Kubernetes clients budget Events per object, 25 then one every five minutes,
and the budget is shared by every reason on that object. An advisory repeated on each pass would
exhaust it and the next Event reporting an actual decision — a scale-in narrowing a database
topology, for instance — would be dropped before ever reaching the API server. Events reporting a
decision or a failure are never collapsed this way.

So an advisory Event is a statement about your spec, not a heartbeat: do not read its absence over
the last few minutes as the condition having gone away. The condition and the reason are the
current state; the Event is the announcement.

## Using reasons in automation

Gate on a condition's `type` plus `reason`, never on the message:

```bash
kubectl wait --for=condition=Ready neo4j/dev -n default --timeout=10m

kubectl get neo4j dev -n default \
  -o jsonpath='{range .status.conditions[?(@.type=="StorageReady")]}{.reason}{"\n"}{end}'
```

Severity tells you what deserves an alert. `error` means the operator has stopped making progress
and needs a human; `warn` means it is waiting on something that may resolve itself, such as a claim
being bound; `info` is either narration of an operation in progress or, for every reason in
[Steady-state reasons](#steady-state-reasons), a settled healthy state.

What to do about each symptom: [Troubleshooting](../04-troubleshooting/01-common-issues.md).
The settings behind `kept (operator-injected)`:
[Operator-owned settings](operator-owned-config.md).
