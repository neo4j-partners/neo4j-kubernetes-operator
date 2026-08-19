# Clustering

A cluster is several Neo4j servers organised in pools. Primaries hold the quorum that keeps the
`system` database consistent and accept writes; secondaries are read-only copies, split into a
read pool for query fan-out and an analytics pool for Graph Data Science and Bloom workloads.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Cluster
    primaries:
      members: 3
    secondaries:
      read:
        members: 2
      analytics:
        members: 1
        plugins: [gds]
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 100Gi
  auth:
    generatePassword: true
```

Runnable variants: [`examples/cluster/`](../../../examples/cluster/) — start from
[`01-minimal-3-primaries.yaml`](../../../examples/cluster/01-minimal-3-primaries.yaml),
[`03-pools-analytics-read.yaml`](../../../examples/cluster/03-pools-analytics-read.yaml) for pools, and
[`14-full.yaml`](../../../examples/cluster/14-full.yaml) for everything at once.

## Pools and the objects they produce

Each pool with at least one member becomes its own StatefulSet, headless Service and ConfigMap. For
a resource named `prod`:

| Pool | Field | StatefulSet | Pods |
|------|-------|-------------|------|
| Primary | `topology.primaries.members` | `prod-primary` | `prod-primary-0`, `prod-primary-1`, … |
| Read | `topology.secondaries.read.members` | `prod-read` | `prod-read-0`, … |
| Analytics | `topology.secondaries.analytics.members` | `prod-analytics` | `prod-analytics-0`, … |

There is one client Service for the whole deployment, named after the resource (`prod`), plus an
admin Service (`prod-admin`) that Cluster mode always creates for operational access. Pools you do
not declare produce nothing.

`primaries.members` must be **odd** — quorum needs it, and even counts are rejected at admission.
Secondaries have no such constraint.

## How members find each other

Every pod gets a per-pod internals Service, and Neo4j discovers peers by looking those up through
Kubernetes rather than through a static list. Advertised addresses are fully-qualified names built
from the headless Service, the namespace and the cluster DNS domain, which defaults to
`cluster.local`. On a cluster installed with a different domain, set it once:

```yaml
spec:
  connectivity:
    clusterDomain: k8s.internal
```

Discovery for primaries is deliberately restricted to primaries. Secondaries also look up
primaries, but primaries ignore secondaries during system bootstrap — otherwise a secondary with a
lower server id could win the bootstrap election and formation would stall.

Because addresses are derived from Kubernetes names, listen and advertised addresses are
operator-owned: setting them in `spec.config.neo4j` is either rejected or overridden. See
[Operator-owned settings](../05-reference/operator-owned-config.md).

## The operator needs an admin session

Everything the operator does to a cluster it does through an authenticated Bolt session as `neo4j` on
the `system` database: `ENABLE SERVER` when a member joins, `SHOW SERVERS` and `SHOW DATABASES` to
observe, `DEALLOCATE`/`DROP SERVER` when one leaves. Without that session there is no formation, no
scale-out and no scale-in — Neo4j would still start, but the resource would sit on
`ClusterFormed=False` and the operator would drive nothing.

So a Cluster has to declare how the operator may open it, one of two ways. Either give Bolt real
certificates, which is what production should do:

```yaml
spec:
  trust:
    enabled: true
    certificates:
      bolt:
        privateKey:
          secretName: prod-bolt-key
        publicCertificate:
          secretName: prod-bolt-cert
```

Or accept an unencrypted session on the pod network, which is what every example in `examples/cluster`
does:

```yaml
spec:
  trust:
    insecureAdminConnection: true
```

A Cluster with neither is **rejected at admission** — it could never be operated, so there is no
point letting it start. Be clear-eyed about what the flag does and does not do: it does not open
anything on the network. If Bolt has no TLS, the port is already in cleartext for every client. The
flag is your acknowledgement that the operator's own admin password will travel that way too. It has
no effect in Standalone mode, where the operator opens no admin session at all.

## Formation and readiness

Two separate numbers decide how a cluster comes up, and conflating them is the most common
misunderstanding. Only one of them is yours to set.

| Number | Controls | Value |
|--------|----------|-------|
| `topology.minimumMembers` | How many primaries must meet before the `system` database can form | Derived when unset: `1` for a single primary, `3` for any larger cluster |
| `topology.defaultPrimariesCount` | How many primaries host a **standard** database created without an explicit topology | `1` unless you set it |

So a three-primary cluster forms its `system` database across all three, but by default the `neo4j`
database lives on **one** of them. That is intentional: it keeps write latency low and lets you
raise redundancy per database rather than paying for it everywhere. If you want the default database
replicated across all three primaries, ask for it:

```yaml
spec:
  topology:
    primaries:
      members: 3
    defaultPrimariesCount: 3
```

`defaultPrimariesCount` is a default, not a rule imposed on your databases. Once a database exists,
its topology belongs to whoever set it: `CREATE DATABASE orders TOPOLOGY 3 PRIMARIES` or a later
`ALTER DATABASE` stands, whatever the field says, and the operator will not pull it back. The one
exception is a scale-in, described under [Scaling members](#scaling-members). Editing the field
changes what the databases you create *next* get, and applies to a running cluster — the operator
carries the new value to Neo4j itself.

Check what you actually got:

```bash
kubectl exec -n default prod-primary-0 -- \
  cypher-shell -d system -u neo4j -p "$PASSWORD" \
  "SHOW DATABASES YIELD name, currentPrimariesCount, requestedPrimariesCount"
```

### The system bootstrap gate

Neo4j needs a minimum number of primaries to have found each other before it will create the
`system` database. Leave `topology.minimumMembers` unset and the operator picks that number for you:
`1` for a single-primary cluster, `3` for every larger one, whatever the pool size.

The derived value deliberately ignores `primaries.members`, for two reasons. First, the gate is
written into `neo4j.conf`, so a value that followed the pool would change the file every time you
scaled, rolling the whole primary pool in the middle of a resize — precisely when you least want
members restarting. Held at `3`, scaling `3 → 5 → 3` leaves the configuration untouched and no pod
restarts.

Second, a lower gate costs no redundancy. The `system` database has no topology of its own; it
spreads to every primary the operator enables. A five-primary cluster gated at `3` forms as soon as
three members meet, then extends `system` onto the fourth and fifth as they are enabled — you can
see it yourself, `SHOW DATABASES` reports `system` with a `currentPrimariesCount` of 5.

Set the field only to **raise** the bar, when you would rather have the DBMS refuse to come online
than form on too few members — typically to hold bootstrap until members are spread across
availability zones:

```yaml
spec:
  topology:
    primaries:
      members: 5
    minimumMembers: 5
```

Any value from `1` to `primaries.members` is accepted, even ones — Neo4j takes any integer here, and
a two-server cluster has to be gated at `2`. A gate of `1` is only valid on a single-primary cluster,
since a multi-primary `system` database cannot bootstrap on one server.

The field is **immutable**: Neo4j reads it at first bootstrap and ignores it afterwards, so changing
it would rewrite `neo4j.conf` and roll every member for no effect. It follows that a later scale-in
may leave the gate above the pool — `minimumMembers: 5` with `primaries.members` back to `3` is
accepted and harmless, and the operator caps its own quorum check accordingly.

The one way to get this wrong is to gate a cluster above the pool **at creation**. Neo4j then waits
for primaries that will never exist, the `system` database is never created, and nothing answers on
Bolt. The API server refuses it when the validating webhook is enabled; otherwise the operator says
so on the resource:

```
ClusterFormed=False (BootstrapGateTooHigh)
  topology.minimumMembers 5 exceeds the 3 primaries in the pool: the system database cannot
  bootstrap, so Bolt never answers
```

Cluster formation is slow compared to a single instance — several minutes is normal, more on large
stores — and the default probes are sized for that. Follow progress on the resource:

```bash
kubectl get neo4j prod -n default \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

`ClusterFormed` is the condition to watch, and `status.members` plus `status.clusterInfo` describe
what the operator observed.

## Connecting to a cluster

Use the `neo4j://` scheme against the client Service, never `bolt://` against a single pod. Routing
is what makes a cluster usable: the driver asks any member for the routing table and is told which
servers can serve reads and which can serve writes.

```
neo4j://prod.default.svc.cluster.local:7687
```

This matters concretely. With `defaultPrimariesCount: 1`, a `bolt://` connection to a primary that
does not host the `neo4j` database fails, while `neo4j://` to the same member succeeds because it
routes you to the one that does. Ready-to-use URIs are published in `status.endpoints`.

## Scaling members

Edit the member count of the pool you want to change:

```yaml
spec:
  topology:
    primaries:
      members: 5
```

That is the only field to touch. Nothing else about the cluster shape needs to follow, and in
particular the [system bootstrap gate](#the-system-bootstrap-gate) stays where it is, so a scale does
not rewrite `neo4j.conf` and does not restart the members that are staying.

A shrink must keep room for `defaultPrimariesCount`: the API server refuses any update where
`defaultPrimariesCount` would end up above `primaries.members`, since no standard database could be
allocated on that pool. Lower both in one patch when you shrink past the current default:

```bash
kubectl patch neo4j prod -n default --type merge \
  -p '{"spec":{"topology":{"primaries":{"members":3},"defaultPrimariesCount":3}}}'
```

**Scaling out** grows the StatefulSet, waits for the new members to appear as Free servers and runs
`ENABLE SERVER` for each. Existing databases stay where they are: the new servers are available
hosts, and it is up to you to use them with `ALTER DATABASE ... SET TOPOLOGY` on the databases that
should spread wider.

**Scaling in** works in reverse, and does so before any pod disappears: it narrows the database
topologies that no longer fit the smaller pool, deallocates and drops the tail servers — one at a
time, highest ordinal first — and only then allows the StatefulSet to shrink. That ordering is what
prevents a member from vanishing while it still holds the only copy of a database. The gate lives in
operator-owned status (`status.drainOK`), so it cannot be forced from an annotation on the resource.
Watch `ServersPendingDrain` while it runs.

That narrowing is the only case where the operator rewrites a topology you own, and it goes no
further than the number of servers left in the pool. It is not quiet either: every rewrite emits a
`DatabaseTopologyResized` Warning Event naming the database and both counts, plus a matching
operator log entry. After a scale-in, read them back with:

```bash
kubectl describe neo4j prod -n default | grep DatabaseTopologyResized
```

See [Errors and conditions](../05-reference/errors.md#databasetopologyresized).

Example: [`examples/cluster/13-scale-out.yaml`](../../../examples/cluster/13-scale-out.yaml).

### Primary counts that cannot change

Neo4j cannot move a database from several primaries down to exactly one, because that dissolves the
Raft group. This is a database-level rule, not a consequence of the cluster size: on a five-server
cluster with room to spare, narrowing a three-primary database to one is still refused, by Neo4j
itself rather than by the operator.

```text
51N49: cannot alter database `orders`.
  51N41: Reason: Can't go from multiple primaries to one primary.
```

Widening is not restricted — `ALTER DATABASE orders SET TOPOLOGY 3 PRIMARIES` on a single-primary
database is routine. To go the other way, use `CALL dbms.cluster.recreateDatabase('orders', ...)`,
or drop and recreate the database.

A scale-in that would force that narrowing is therefore refused too: the operator stops before
draining and reports reason `UnsupportedSinglePrimary` on `ClusterFormed` and `ServersPendingDrain`.
Keep `primaries.members` at three or more, or recreate the deployment at the size you want.

The mirror case also holds: a cluster created with `primaries.members: 1` — a legitimate shape for a
lab or an analytics-only deployment — cannot grow its primaries later. That attempt reports
`UnsupportedSystemScaleUp` and the primary pool is held. Bootstrap at the final primary count when
you know you will need high availability.

Secondary pools have neither restriction. Scaling read and analytics members up and down is routine.

### Volumes when an ordinal comes back

A dropped Neo4j server UUID can never be enabled again, so scale-in must not hand the old store back
to a member that returns. Which behaviour you get depends on how the data volume is provisioned:

| Data volume | After scale-in | When that ordinal returns |
|-------------|----------------|---------------------------|
| `Dynamic` with `whenScaled: Delete` | The operator deletes the drained ordinal's claims | A fresh volume produces a new UUID, and `ENABLE SERVER` succeeds |
| `Dynamic` with `whenScaled: Retain` (default) | Claims are kept for recovery | The operator detects the dropped UUID and recycles that ordinal's pod and claim, then enables it |
| `Existing` with `claimName` | Never deleted by the operator | The same store returns with a dropped UUID and enabling fails until you wipe or replace the claim yourself |

For elastic pools that should not leave disks behind:

```yaml
spec:
  storage:
    volumeClaimRetention:
      whenScaled: Delete
```

Prefer `Dynamic`, or `Existing` with a `volumeClaimTemplate` that provisions one claim per ordinal.
A single `claimName` suits Standalone and is a poor fit for a pool that scales. See
[Storage](03-storage.md).

## Read pool and the scale subresource

The read pool is wired to the Kubernetes scale subresource, so it responds to the standard verb:

```bash
kubectl scale neo4j prod -n default --replicas=4
```

That maps to `spec.topology.secondaries.read.members`, and `status.readPoolReplicas` reports what is
observed. Other pools are changed by editing the spec.

## Plugin placement

In Cluster mode `spec.plugins` is rejected; plugins are declared on the pool that should run them.
Graph Data Science and Bloom must go on the analytics pool — not on primaries, and not on the read
pool — and admission enforces it. The reasoning and examples are in [Plugins](07-plugins.md).

## Availability during maintenance

Enable a PodDisruptionBudget so a node drain cannot take out quorum:

```yaml
spec:
  podDisruptionBudget:
    enabled: true
    minAvailable: 2
```

`minAvailable` must be satisfiable — strictly below the pool size, and never `100%`, because an
impossible budget blocks node drains forever instead of protecting anything. See
[Operations](09-operations.md#pod-disruption-budget).

## Clean up

```bash
kubectl delete neo4j prod -n default
```

Claims are preserved by default, for every pool. See
[Uninstall](../02-operator-installation/05-uninstall.md#persistentvolumeclaim-retention).
