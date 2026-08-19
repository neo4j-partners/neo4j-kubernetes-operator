# Standalone

One Neo4j server, one pod, one data volume. This is the default shape and the right one for
development, for single-instance production where the cost of a cluster is not justified, and for
anything where you can tolerate the restart window of a single member.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 10Gi
  auth:
    generatePassword: true
```

Runnable variants: [`examples/standalone/`](../../../examples/standalone/), from
[`01-minimal.yaml`](../../../examples/standalone/01-minimal.yaml) to
[`15-full.yaml`](../../../examples/standalone/15-full.yaml) which exercises most of the surface at once.

Standalone is also the only shape Community runs: set `edition: community`, drop the `license` block
entirely, and the operator pulls the unsuffixed image tag and passes no licensing environment. What
you give up beyond clustering is `features.backup` and `features.monitoring.prometheus`, both
rejected at admission on Community since a Community server refuses to start once its configuration
mentions Enterprise settings. See
[`24-community.yaml`](../../../examples/standalone/24-community.yaml).

## What gets created

For a resource named `dev`:

| Object | Name |
|--------|------|
| StatefulSet, one replica | `dev-server` |
| Headless Service | `dev-server` |
| Client Service | `dev` |
| ConfigMap with `neo4j.conf` | `dev-config` |
| Auth Secret, when generated | `dev-auth` |
| PersistentVolumeClaim | `data-dev-server-0` |

The pod is `dev-server-0`, and that name is stable across restarts because it comes from a
StatefulSet. Applications should connect through the `dev` Service rather than to the pod.

## What Standalone means inside Neo4j

The server runs without clustering: no discovery, no Raft, no `SHOW SERVERS` peers. Databases are
single-copy, so `spec.topology` accepts no member fields at all. Setting `primaries`,
`secondaries`, `minimumMembers` or `defaultPrimariesCount` in Standalone mode is rejected at
admission, with a message
naming the field.

Plugins are declared instance-wide with `spec.plugins`, since there are no pools to place them on:

```yaml
spec:
  plugins:
    - apoc
    - gds
```

That is the opposite of Cluster mode, where `spec.plugins` is rejected and plugins are declared per
pool. See [Plugins](07-plugins.md).

## Restarts and availability

A single member means any change that rewrites the pod — a new version, a configuration change, a
resource change — is a full outage for the duration of the restart, typically a minute or two plus
recovery time proportional to your store size. Nothing routes around it, and a PodDisruptionBudget
cannot protect a single replica from a node drain.

Two consequences worth planning for:

- Apply configuration changes in a window you control. The operator restarts the pod as soon as the
  rendered configuration changes; see [Operations](09-operations.md#changing-configuration).
- Node maintenance will take the database down. The pod is rescheduled and reattaches to the same
  PersistentVolumeClaim, so no data is lost, but the interruption is real.

## When Standalone is the wrong choice

`spec.topology.mode` is immutable. Once created, a Standalone resource cannot become a cluster: you
would create a new resource in Cluster mode and move the data yourself. So decide up front.

Choose Cluster when you need any of:

- Continuity across the loss of one server or one availability zone.
- Read scale-out beyond a single machine.
- Analytics workloads that should not compete with transactional traffic, which is what the
  analytics pool is for.

Choose Standalone when a restart-scale interruption is acceptable, and note that you still get
persistent storage, TLS, plugins, metrics and every other feature — clustering is the only
difference.

## Next

[Storage](03-storage.md) · [Connectivity](04-connectivity.md) · [Security](05-security.md) ·
[Clustering](02-clustering.md)
