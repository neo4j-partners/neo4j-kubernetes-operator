# Operations

Day-to-day concerns: sizing, placement, probes, restarts, maintenance and deletion.

## Sizing the container

```yaml
spec:
  resources:
    requests:
      cpu: "2"
      memory: 8Gi
    limits:
      cpu: "4"
      memory: 8Gi
```

These are the standard Kubernetes requests and limits, applied to the Neo4j container. Nothing is set
by default, which means an unsized Neo4j is scheduled anywhere and can be evicted under node
pressure — fine for a laptop, wrong for production. Put a `ResourceQuota` on watched namespaces
([`examples/standalone/23-namespace-quota.yaml`](../../../examples/standalone/23-namespace-quota.yaml))
so one CR cannot exhaust the node or the Service CIDR. The CRD also caps `primaries.members` at 15,
each secondary pool at 25, and Dynamic PVC size at 16Ti (NEO-014).

Coordinate them with the JVM. The heap and page cache you set in
[Configuration](06-configuration.md#neo4jconf-settings) must fit inside the memory limit, with room to
spare for the JVM's own overhead and for the operating system. A heap larger than the limit does not
fail at start-up; it gets the container killed under load, which looks like a mysterious restart.

Setting `requests` equal to `limits` for memory is the usual choice for a database, because it puts the
pod in the guaranteed QoS class and takes eviction pressure out of the picture.

Example: [`examples/standalone/21-resources.yaml`](../../../examples/standalone/21-resources.yaml).

## Placing pods

```yaml
spec:
  scheduling:
    nodeSelector:
      workload: database
    tolerations:
      - key: dedicated
        operator: Equal
        value: neo4j
        effect: NoSchedule
    affinity:
      podAntiAffinity: soft
    topologySpreadConstraints:
      - maxSkew: 1
        topologyKey: topology.kubernetes.io/zone
        whenUnsatisfiable: DoNotSchedule
        labelSelector:
          matchLabels:
            app.kubernetes.io/instance: prod
    priorityClassName: high
    terminationGracePeriodSeconds: 3600
```

`podAntiAffinity` takes a preset rather than the full Kubernetes structure: `soft` prefers spreading
members across nodes, `hard` requires it, and `custom` lets you supply your own `affinity.custom`
block when the presets do not fit.

Prefer `soft` unless you have enough nodes to guarantee `hard` can be satisfied. With `hard` and fewer
nodes than members, the surplus pods stay Pending forever — which is correct behaviour and a poor
surprise.

`terminationGracePeriodSeconds` defaults to 3600. That is intentionally generous: Neo4j flushes and
checkpoints on shutdown, and killing it mid-checkpoint means a longer recovery on the next start.
Shorten it only if you know your store is small.

If you use taints for database nodes, remember the operator itself has to tolerate them or it will
never run — see [Install](../02-operator-installation/03-install.md#scheduling-the-controller).

Examples: [`examples/standalone/10-scheduling.yaml`](../../../examples/standalone/10-scheduling.yaml),
[`examples/cluster/08-scheduling.yaml`](../../../examples/cluster/08-scheduling.yaml).

## Probes

The operator configures all three probes as TCP checks against the Bolt port, with thresholds sized
for a database rather than a stateless service:

| Probe | Failure threshold | Period | Effective budget |
|-------|------------------|--------|------------------|
| Startup | 1000 | 5s | Roughly 83 minutes to finish starting |
| Readiness | 20 | 5s | About 100 seconds before being pulled from Services |
| Liveness | 40 | 5s | About 200 seconds before a restart |

The long startup budget is the important one. Recovering a large store, or forming a cluster, can take
a long time, and a probe that gives up first produces a restart loop that never converges.

Override any of them with the standard Kubernetes probe structure:

```yaml
spec:
  probes:
    readiness:
      tcpSocket:
        port: 7687
      periodSeconds: 10
      failureThreshold: 6
    liveness:
      tcpSocket:
        port: 7687
      periodSeconds: 10
      failureThreshold: 12
```

An override replaces the default entirely, so specify the whole probe, and be careful about tightening
the startup probe on a large database.

Examples: [`examples/standalone/11-probes-custom.yaml`](../../../examples/standalone/11-probes-custom.yaml),
[`examples/cluster/09-probes-custom.yaml`](../../../examples/cluster/09-probes-custom.yaml).

## Pod disruption budget

```yaml
spec:
  podDisruptionBudget:
    enabled: true
    minAvailable: 2
```

This protects against voluntary disruption — node drains, cluster upgrades — not against crashes.
`minAvailable` must be satisfiable: an integer strictly below the pool size, and never `100%`. An
impossible budget is rejected, because it would block node drains indefinitely while protecting
nothing.

On Standalone a budget cannot help: with one replica, any value either blocks all drains or allows the
only pod to go. Accept the interruption instead, or run a cluster.

Example: [`examples/cluster/15-pdb.yaml`](../../../examples/cluster/15-pdb.yaml).

## Changing configuration

When a change alters what is rendered — a `neo4j.conf` key, a JVM argument, logging XML, resources,
scheduling — the operator updates the objects and rolls the StatefulSets so pods pick up the new
content. Configuration is tracked by a checksum, so a change that renders identically causes no
restart at all.

In Cluster mode pods roll one at a time, in reverse ordinal order, waiting for each to become ready.
Clients using `neo4j://` follow the routing table through it. On Standalone the single pod restarts,
and that is an outage.

Follow a rollout:

```bash
kubectl rollout status statefulset/prod-primary -n default
kubectl get neo4j prod -n default -w
```

The resource reports `Reconciling=True` while work is in flight and returns to `Ready=True` when the
new generation is fully applied. `status.observedGeneration` matching `metadata.generation` is the
precise signal that your change has been processed.

## Version changes

`spec.version` selects the image tag at install time. Changing it later is **not orchestrated**: there
is no upgrade state machine, no version-compatibility check, no pause between members. The pods will
roll onto the new image, which for a Neo4j version change is not a safe way to upgrade a database.

Until upgrades are implemented, treat the version as fixed for the lifetime of a deployment. To move
versions, take a backup, create a resource at the target version, and load the data. The status fields
`status.upgrade` and `status.lastUpgradeTime` exist for the future implementation and stay empty.

## Offline maintenance

Sometimes you need the pods and volumes present but Neo4j stopped — filesystem surgery, an offline
`neo4j-admin` operation, or investigating a store that crashes on start.

```yaml
spec:
  maintenance:
    offlineMode: true
```

The container starts an idle loop instead of Neo4j, so the pod exists with its volumes mounted and no
database running. The liveness and startup probes are removed so the kubelet does not restart the idle
container, while readiness stays on Bolt — the pod therefore stays NotReady and is removed from Service
endpoints, which keeps clients from being routed to a server that will never answer. The termination
grace period drops to zero, since there is nothing to flush.

While in this mode you can work in the pod:

```bash
kubectl exec -it -n default dev-server-0 -- bash
```

Set `offlineMode: false` and re-apply to bring Neo4j back. Expect recovery time on the first start,
proportional to what was in flight when it stopped.

Example: [`examples/standalone/17-offline-maintenance.yaml`](../../../examples/standalone/17-offline-maintenance.yaml).

## Pulling images from a private registry

The operator allowlists Neo4j image repositories (NEO-012). Defaults are `neo4j`,
`docker.io/neo4j`, and `index.docker.io/neo4j`. For a private mirror, add the repository
prefix on the **operator** Helm values, then reference it on the CR:

```yaml
# charts/neo4j-operator values
allowedImageRepositories:
  - neo4j
  - docker.io/neo4j
  - myregistry.example.com/neo4j
```

```yaml
spec:
  image:
    repository: myregistry.example.com/neo4j
    # Optional digest pin (skips :tag in the image ref):
    # digest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    pullPolicy: IfNotPresent
    pullSecrets:
      - regcred
```

Without the allowlist entry, reconcile fails with an allowlist error even if the cluster can
pull the image. The image reference is built from the repository and `spec.version` (or
`repository@digest` when `digest` is set), with the Enterprise suffix added for tag pulls.
`pullSecrets` names image pull Secrets that already exist in the workload namespace:

```bash
kubectl create secret docker-registry regcred -n default \
  --docker-server=myregistry.example.com \
  --docker-username=<user> --docker-password=<password>
```

These Secrets are not subject to the mountable label, because they are handed to the kubelet rather
than mounted into the container — Kubernetes, not the operator, reads them.

Example: [`examples/standalone/14-image-pullsecrets.yaml`](../../../examples/standalone/14-image-pullsecrets.yaml).

## Escape hatches

For anything the spec does not model, add containers or environment variables directly:

```yaml
spec:
  podTemplate:
    initContainers:
      - name: seed
        image: busybox:1.36
        command: ["sh", "-c", "cp /seed/* /import/"]
        volumeMounts:
          - name: import
            mountPath: /import
    sidecars:
      - name: log-shipper
        image: fluent-bit:latest
    env:
      - name: EXTRA_SETTING
        value: "value"
```

Init containers run before Neo4j, which makes them the right place to seed `/import` or prepare a
volume. Sidecars run alongside it. Environment variables are added to the Neo4j container, and the
ones the operator sets for configuration paths and cluster identity take precedence.

Treat these as a last resort: they bypass the validation the rest of the spec gives you, and an init
container that fails leaves the pod stuck in `Init` with no explanation on the resource.

## Deleting a Neo4j resource

```bash
kubectl delete neo4j dev -n default
```

Everything the resource owns is garbage-collected — StatefulSets, Services, ConfigMaps, the generated
auth Secret. PersistentVolumeClaims are preserved by default, and reclaiming them is an explicit act.
The full behaviour, including how to opt into deletion at creation time, is in
[Uninstall](../02-operator-installation/05-uninstall.md#persistentvolumeclaim-retention).

Deleting a resource while the operator is not running leaves the owned objects behind: garbage
collection follows owner references and needs no operator, but the operator's own cleanup of retained
claims and cluster deallocation does not happen. Prefer deleting resources before uninstalling the
operator.

## Next

[Troubleshooting](../04-troubleshooting/01-common-issues.md) ·
[Error reference](../05-reference/errors.md) · [Monitoring](08-monitoring.md)
