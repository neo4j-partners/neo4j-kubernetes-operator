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

## Formation and readiness

Two separate numbers decide how a cluster comes up, and conflating them is the most common
misunderstanding.

| Field | Controls | Default |
|-------|----------|---------|
| `topology.minimumMembers` | How many primaries must be enabled before the `system` database can form. Maps to Neo4j's minimum initial system primaries count | `primaries.members` |
| `topology.defaultPrimariesCount` | How many primaries host each **standard** database, at bootstrap and afterwards | `1` |

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

Check what you actually got:

```bash
kubectl exec -n default prod-primary-0 -- \
  cypher-shell -d system -u neo4j -p "$PASSWORD" \
  "SHOW DATABASES YIELD name, currentPrimariesCount, requestedPrimariesCount"
```

`minimumMembers` cannot exceed `primaries.members`, and it is **immutable after creation**. Changing
it would rewrite `neo4j.conf` on every primary and roll the whole pool, which can strand members
waiting for a system snapshot. Scale with `primaries.members` instead.

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

Do **not** touch `minimumMembers`, for the reason given above.

**Scaling out** grows the StatefulSet, waits for the new members to appear as Free servers, runs
`ENABLE SERVER` for each, then adjusts database topologies so allocation matches the new pool sizes.

**Scaling in** works in reverse, and does so before any pod disappears: it shrinks database
topologies to fit the smaller pool, deallocates and drops the tail servers, and only then allows the
StatefulSet to shrink. That ordering is what prevents a member from vanishing while it still holds
the only copy of a database. The gate lives in operator-owned status (`status.drainOK`), so it
cannot be forced from an annotation on the resource. Watch `ServersPendingDrain` while it runs.

Example: [`examples/cluster/13-scale-out.yaml`](../../../examples/cluster/13-scale-out.yaml).

### Primary counts that cannot change

Neo4j cannot move a database from several primaries down to exactly one, because that dissolves the
Raft group. The operator detects the attempt and refuses instead of draining, reporting reason
`UnsupportedSinglePrimary` on `ClusterFormed` and `ServersPendingDrain`. Keep `primaries.members` at
three or more, or recreate the deployment at the size you want.

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
