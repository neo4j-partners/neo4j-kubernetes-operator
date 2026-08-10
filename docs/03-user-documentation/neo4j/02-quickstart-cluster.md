# Quickstart — Cluster Neo4j

Deploy a multi-member Neo4j cluster (Enterprise) with primary and optional secondary pools.

Assumes the operator is already installed. Standalone path: [01-quickstart-standalone.md](01-quickstart-standalone.md).

## Sample manifest

[`config/samples/neo4j_v1beta1_neo4j_cluster.yaml`](../../../config/samples/neo4j_v1beta1_neo4j_cluster.yaml)

It defines:

- `topology.mode: Cluster`
- Primary + analytics (GDS/Bloom) + read pool
- `pluginDefinitions` for licensed plugins
- Dynamic data volume
- ClusterIP connectivity (bolt + http)
- Optional BYO cluster TLS via `spec.trust`

## Apply

Sample omits `metadata.namespace` — deploys to **`default`**:

```bash
kubectl apply -f config/samples/neo4j_v1beta1_neo4j_cluster.yaml
kubectl get neo4j -n default -w
```

Expect one StatefulSet per pool, headless + client Services, and `status.conditions[Ready]` once members form.

Storage variants (Existing, aux volumes): [`examples/storage/`](../../../examples/storage/).

## Scaling members

Edit `topology.primaries.members` (or a secondary pool’s `members`). **Do not change `topology.minimumMembers`** — it is immutable after create (bootstrap / system formation size only). Changing it rewrites `dbms.cluster.minimum_initial_system_primaries_count`, rolls every primary via the config checksum, and can strand scale-out members waiting on a system Raft snapshot.

The operator:

1. **Scale-out** — grows the pool StatefulSet, then runs `ENABLE SERVER` for each new Free member, then `ALTER DATABASE` so topologies match pool sizes (primaries + analytics/read secondaries).
2. **Scale-in** — shrinks database topologies to fit, then `DEALLOCATE` → `DROP` for tail ordinals, then allows StatefulSet scale-down (gated by operator-owned `status.drainOK` / `status.drainOKGeneration` — ADD-02; do not set `neo4j.com/drain-ok` on the CR).

Watch `ClusterFormed` and `ServersPendingDrain` on the CR. Example: [`examples/cluster/13-scale-out.yaml`](../../../examples/cluster/13-scale-out.yaml). Analytics/read members only host user databases after topology requests secondaries — the operator sets that automatically for standard databases (not `system`).

### Multi-primary → one primary

Neo4j **cannot** use `ALTER DATABASE … SET TOPOLOGY` to go from multiple primaries to **one** primary (Raft quorum). The operator detects this on scale-in and sets `ServersPendingDrain` / `ClusterFormed` reason `UnsupportedSinglePrimary` without draining. Keep `topology.primaries.members` at **3+** (odd), or recreate the cluster at the desired size.

### One system primary → many

Deploying with **`primaries.members: 1`** (analytics-style, with or without secondaries) is supported. **Scaling that cluster’s primaries** (1→3, or later N→1) is **not** — the operator sets `UnsupportedSystemScaleUp` / `UnsupportedSinglePrimary` and holds the primary pool. Bootstrap with `primaries.members` / `minimumMembers` at the final size when you need HA primaries. Scaling **secondaries** only is fine.

### Storage and re-scaling the same ordinal

A Neo4j server UUID that has been `DROP`ped **cannot be enabled again**. Scale-in therefore must not remount the old store when that ordinal comes back.

| Data mode | After scale-in | Scale-out of the same ordinal |
|-----------|----------------|-------------------------------|
| **Dynamic** | Operator deletes the drained ordinal’s Dynamic PVCs | New empty volume → new server UUID → `ENABLE SERVER` works |
| **Existing** (`claimName` / pre-bound claims) | Operator **never** deletes those PVCs | Same store remounts → UUID stays `Dropped` → `ENABLE` fails until **you** wipe or replace the claim’s data (or point the ordinal at a fresh claim) |

Prefer **Dynamic** (or Existing `volumeClaimTemplate` that provisions a new claim per ordinal) for clusters that scale members down and later back up. Existing `claimName` remains Standalone-oriented and a poor fit for elastic cluster pools.

## Clean up

```bash
kubectl delete -f config/samples/neo4j_v1beta1_neo4j_cluster.yaml
```

PVCs may remain until explicitly deleted — see [Uninstall](../operator/03-uninstall.md).
