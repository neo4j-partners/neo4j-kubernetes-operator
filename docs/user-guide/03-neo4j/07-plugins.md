# Plugins

Three plugins are supported by name: `apoc`, `gds` for Graph Data Science, and `bloom`. You declare
which ones you want and the operator installs them, points Neo4j at the plugin directory, and
allowlists their procedures.

## Standalone

Plugins are instance-wide, because there is only one server:

```yaml
spec:
  plugins:
    - apoc
    - gds
```

Example: [`examples/standalone/13-plugins-apoc.yaml`](../../../examples/standalone/13-plugins-apoc.yaml).

## Cluster

`spec.plugins` is **rejected** in Cluster mode. Plugins are declared on the pool that should run them:

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 3
      plugins: [apoc]
    secondaries:
      analytics:
        members: 1
        plugins: [gds, bloom]
      read:
        members: 2
        plugins: [apoc]
```

Two placement rules are enforced at admission, both about keeping heavy compute away from the
quorum:

| Rule | Why |
|------|-----|
| `gds` and `bloom` cannot go on `primaries` | Graph algorithms allocate large heaps and saturate CPU. On a primary that competes with transaction processing and Raft heartbeats, and a primary that stalls costs you availability |
| `gds` and `bloom` cannot go on `secondaries.read` | The read pool serves latency-sensitive queries. Analytics is the pool intended for this work, and it exists precisely so the two do not share resources |

APOC is fine anywhere. The analytics pool also gets GDS procedures opened even when you did not list
the plugin, since that is the pool's purpose.

Example: [`examples/cluster/11-plugins-apoc.yaml`](../../../examples/cluster/11-plugins-apoc.yaml).

## What the operator configures for you

For the plugins assigned to a pool, the rendered `neo4j.conf` gets the plugin directory and the
procedure allowlists — `apoc.*`, `gds.*`, `bloom.*` as appropriate, in both
`dbms.security.procedures.unrestricted` and `dbms.security.procedures.allowlist`. GDS additionally
gets its HTTP auth allowlist entry.

You can still add your own settings; they merge with the generated ones and yours win. See
[Configuration](06-configuration.md#when-two-layers-set-the-same-key).

## Persisting downloaded plugins

Plugins are fetched into `/plugins` when the container starts, which means they are downloaded again
on every restart and require the pod to reach the download source. Keep them on disk instead:

```yaml
spec:
  storage:
    volumes:
      plugins:
        mode: Share
        shareFrom: data
```

`Share` puts `/plugins` in a subdirectory of the data volume, so it costs no extra claim. This is
worth doing anywhere restarts matter, and necessary on clusters with no egress to the internet.

Example: [`examples/storage/12-aux-share-plugins-apoc.yaml`](../../../examples/storage/12-aux-share-plugins-apoc.yaml).

## Licensed plugins

Bloom requires a licence. Graph Data Science runs in its Community form without one and needs a
licence for Enterprise features. Supply it as a Secret and reference it per plugin:

```yaml
spec:
  plugins: [gds, bloom]
  pluginDefinitions:
    gds:
      licenseSecretRef: gds-license
      config:
        gds.enterprise.license_file: /licenses/gds/gds.license
    bloom:
      licenseSecretRef: bloom-license
      config:
        server.unmanaged_extension_classes: com.neo4j.bloom.server=/bloom
```

Each licence Secret is mounted under `/licenses/<plugin>`, and — like every Secret the operator
mounts — must carry `neo4j.com/mountable-by-operator: "true"`:

```bash
kubectl label secret gds-license -n default neo4j.com/mountable-by-operator=true
```

The reasoning behind that label is in
[Security](05-security.md#why-the-operator-requires-opt-in-labels); a ready-made manifest is in
[`examples/secrets/plugin-licenses.yaml`](../../../examples/secrets/plugin-licenses.yaml).

`pluginDefinitions` is also where per-plugin settings live, keyed by plugin id, which keeps plugin
configuration next to the plugin instead of scattered through `spec.config.neo4j`. Its `version`
field is not honoured yet: plugin versions currently follow the Neo4j image.

## Verifying

```bash
kubectl exec -n default dev-server-0 -- ls /plugins

kubectl exec -n default dev-server-0 -- \
  cypher-shell -u neo4j -p "$PASSWORD" "RETURN apoc.version()"

kubectl exec -n default dev-server-0 -- \
  cypher-shell -u neo4j -p "$PASSWORD" "CALL gds.version()"
```

`Unknown function` or `no procedure with that name` after a successful start usually means the plugin
is not on that member. In a cluster, check you queried the pool that carries it — a GDS call routed to
a primary will fail by design.

## Next

[Configuration](06-configuration.md) · [Storage](03-storage.md) · [Clustering](02-clustering.md)
