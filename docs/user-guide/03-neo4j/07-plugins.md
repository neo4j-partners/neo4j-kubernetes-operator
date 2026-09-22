# Plugins

Six plugins are supported by name. Unknown ids are rejected. Declaring a catalog plugin sets
`NEO4J_PLUGINS`, and the image entrypoint installs the JAR at container start.

| Plugin | Where the JAR comes from | On `edition: community` |
|---|---|---|
| `apoc` | bundled in both images (`/var/lib/neo4j/labs`) | works |
| `gds` (Graph Data Science) | bundled in Enterprise (`/var/lib/neo4j/products`) | **refused at apply** |
| `bloom` | bundled in Enterprise | **refused at apply** |
| `genai` | bundled in Enterprise | **refused at apply** |
| `fleet-management` | bundled in Enterprise | **refused at apply** |
| `apoc-extended` | **never bundled** — always downloaded | works, needs egress |

A bundled plugin is a local file copy: no egress, nothing to checksum. The distinction matters
because the entrypoint uses its bundled copy only when it finds the file, and downloads otherwise.
The Community image has no `/var/lib/neo4j/products`, so the four Enterprise plugins would fall
through to a download that cannot succeed — and it fails *without failing*, leaving a server that
reports healthy with the plugin missing. The operator refuses that combination at apply instead:

```
bloom, fleet-management, genai and gds ship only in the enterprise image;
set edition to enterprise or drop the plugin
```

`apoc-extended` is downloaded on both editions, so the edition is not what decides whether it
works — the node needs egress. The operator does not checksum a downloaded file (NEO-013).

## When an install does not happen

The operator does not install plugins, so it reports what the entrypoint did. **Two of the three
failures let Neo4j start anyway** — the entrypoint prints the error and carries on — which is why
they get a condition of their own rather than showing up as an unhealthy member. `PluginsReady`
holds `Ready` back, since the spec asked for the plugin:

| `PluginsReady` reason | What happened | What to do |
|---|---|---|
| `PluginDownloadFailed` | The version index was unreachable. The message carries the URL the image tried | Give the node egress to it, or switch to a bundled plugin |
| `PluginVersionIncompatible` | The plugin publishes no build for your `spec.version` — common on the newest Neo4j releases | Pin an older `spec.version`, or drop the plugin |
| `PluginJarUnreadable` | The JAR landed but could not be read, so the container never started Neo4j | Check permissions and the mount on `storage.volumes.plugins` |

```bash
kubectl get neo4j my-db -o jsonpath='{.status.conditions[?(@.type=="PluginsReady")]}'
```

The check reads the first lines of the container's output, where the entrypoint reports. That
evidence is rotated by the kubelet and lost when a pod is replaced, so a log the operator can no
longer read is treated as a successful install rather than a failure — `PluginsReady` will not
hold a CR back on evidence that no longer exists.

A **rejected licence is not** reported by this condition. GDS says nothing about its licence while
starting: the state is a query-time property, so check it yourself with `gds.isLicensed()` or
`gds.license.state` (see [Verifying](#verifying)).

The procedure sandbox stays on unless you opt in via `spec.config.neo4j`
(`dbms.security.procedures.unrestricted`) — see [Licensed plugins](#licensed-plugins).

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

For plugins assigned to a pool, the rendered `neo4j.conf` sets `server.directories.plugins=/plugins`
and **allowlists** procedures (`apoc.*` / `gds.*` / `bloom.*` in
`dbms.security.procedures.allowlist`). It does **not** set `procedures.unrestricted` — that is an
explicit opt-in in `spec.config.neo4j` (NEO-024).

You can still add your own settings; they merge with the generated ones and yours win. See
[Configuration](06-configuration.md#when-two-layers-set-the-same-key).

## How plugins get onto disk

| Path | What happens at container start | Use when |
|------|---------------------------------|----------|
| Catalog id only (ephemeral emptyDir) | JAR copied out of the image, every start | Default |
| `volumes.plugins` Share / Dynamic | Copied on first start, then persisted | Keeping the directory across restarts |
| `volumes.plugins` **Existing** | Nothing — `NEO4J_PLUGINS` is left unset | Pre-seeded or pinned JARs (NEO-013) |
| Custom image with JARs baked in | Nothing (omit catalog ids, or use Existing) | Fully pinned supply chain |

`pluginDefinitions.*.version` is rejected — the image entrypoint cannot pin plugin versions.
Put known-good JARs on an Existing volume or in a derived image.

Egress only matters for a plugin the image does not bundle — `gds` or `bloom` on the Community
image. The destinations are the ones the Neo4j image documents for
[Docker plugins](https://neo4j.com/docs/operations-manual/current/docker/plugins/); allowlist
them if you restrict egress.

## Persisting plugins

`/plugins` is an ephemeral `emptyDir` by default, so the JAR is installed again on every restart.
On the Enterprise image that is a local file copy and costs nothing. Keep the directory on disk
if you want it to survive anyway — or if you are on the Community image, where `gds` and `bloom`
are fetched over the network:

```yaml
spec:
  storage:
    volumes:
      plugins:
        mode: Share
        shareFrom: data
```

`Share` puts `/plugins` in a subdirectory of the data volume, so it costs no extra claim.

To supply your own JARs and stop the image installing anything over them, mount a pre-populated
PVC:

```yaml
spec:
  plugins: [apoc]
  storage:
    volumes:
      plugins:
        mode: Existing
        existing:
          claimName: my-plugins
```

The operator will not set `NEO4J_PLUGINS`. Put the JARs in that claim yourself (or bake them into
a custom image — add the image repository to the operator allowlist, NEO-012).

Example: [`examples/storage/12-aux-share-plugins-apoc.yaml`](../../../examples/storage/12-aux-share-plugins-apoc.yaml).

## Licensed plugins

Bloom requires a licence. Graph Data Science runs in its Community form without one and needs a
licence for Enterprise features. Supply it as a Secret and reference it per plugin:

```yaml
spec:
  plugins: [gds, bloom]
  config:
    neo4j:
      # Both *.license_file keys below are plugin-namespaced, and the server validates its
      # configuration before the plugin that declares them is on the classpath. Without this
      # it refuses to start: "No declared setting with name: gds.enterprise.license_file".
      server.config.strict_validation.enabled: "false"
      # GDS and Bloom procedures that touch database internals need the sandbox lifted.
      # bloom.checkLicenseCompliance() answers 52N34 without it.
      dbms.security.procedures.unrestricted: "gds.*,bloom.*"
  pluginDefinitions:
    gds:
      licenseSecretRef: gds-license
      config:
        gds.enterprise.license_file: /licenses/gds/gds.license
    bloom:
      licenseSecretRef: bloom-license
      config:
        dbms.bloom.license_file: /licenses/bloom/bloom.license
        # Without this Bloom is licensed but never served — /bloom answers 404.
        server.unmanaged_extension_classes: com.neo4j.bloom.server=/bloom
```

Those three `config` entries are not operator quirks to work around; the Neo4j image would
normally write them itself when it installs `gds`/`bloom`, but it writes them to
`$NEO4J_HOME/conf/neo4j.conf` while the server here reads `/config`, so they never take effect.
The [Neo4j Helm chart docs](https://neo4j.com/docs/operations-manual/current/kubernetes/plugins/#install-gds-ee-bloom)
ask for the same keys for the same reason. Setting them is what makes a licensed plugin usable
— `feature-plugins` boots exactly this shape on every CI run.

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
field is rejected: pin JARs with `volumes.plugins` Existing or a custom image (NEO-013).

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
