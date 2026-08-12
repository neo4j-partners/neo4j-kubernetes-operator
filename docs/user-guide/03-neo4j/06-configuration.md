# Configuration

Neo4j settings, JVM arguments, APOC settings and log4j configuration. The operator renders them into
ConfigMaps mounted by the pods, so what you write here ends up in `neo4j.conf`, `apoc.conf` and the
logging XML rather than in environment variables.

## neo4j.conf settings

```yaml
spec:
  config:
    neo4j:
      server.memory.heap.initial_size: 2g
      server.memory.heap.max_size: 2g
      server.memory.pagecache.size: 4g
      db.transaction.timeout: 60s
```

Keys and values are passed through as strings, so quote anything YAML would otherwise coerce —
`"true"`, `"7687"`, `"60s"` are safer written explicitly. The operator does not validate Neo4j
setting names, so a typo becomes a Neo4j start-up error rather than an admission error; check the pod
log if a server refuses to start after a configuration change.

Memory settings are the ones people come here for. Set heap and page cache explicitly in production
rather than relying on Neo4j's automatic sizing, and keep them consistent with the container limits
you set in [Operations](09-operations.md#sizing-the-container) — a heap larger than the memory limit
gets the container killed.

### Settings the operator owns

Some settings are not yours to choose, because the operator derives them from other fields to make
Neo4j work inside Kubernetes. They fall in two groups.

**Rejected at admission.** Listen addresses and connector toggles, plus `server.jvm.additional`,
because there is a dedicated field for each:

```
server.bolt.listen_address     server.bolt.enabled
server.http.listen_address     server.http.enabled
server.https.listen_address    server.https.enabled
server.backup.listen_address   server.jvm.additional
```

You get an immediate error naming the field to use instead — `connectivity.listeners.*` for the
addresses, `config.jvm.additionalArguments` for JVM flags.

**Silently overridden.** Cluster discovery, routing, advertised addresses, the default primaries
count and TLS policy keys are injected after your configuration, so setting them has no effect on
the rendered file. That used to be invisible; now the operator reports it. The full list, and which
direction each key resolves, is in
[Operator-owned settings](../05-reference/operator-owned-config.md).

### When two layers set the same key

The rendered `neo4j.conf` is a merge of four layers, in order: operator defaults, plugin
configuration, your `spec.config.neo4j`, and operator injections. Your values beat defaults and
plugin configuration; injections beat everything.

Whenever a layer replaces a value with a different one, the operator says so — a Warning Event with
reason `DuplicateEntry` on the resource, and the same line in its log:

```bash
kubectl describe neo4j dev -n default | grep DuplicateEntry
```

```
spec.config.neo4j: duplicate entry for dbms.routing.enabled —
  kept "true" (operator-injected), dropped "false" (user)
```

Read the origins: `kept (user)` means your value won over a default, which is usually what you
intended. `kept (operator-injected)` means your value was discarded, and you should remove it — the
setting is managed for you.

## JVM arguments

```yaml
spec:
  config:
    jvm:
      useDefaults: true
      additionalArguments:
        - "-XX:+UseG1GC"
        - "-XX:MaxGCPauseMillis=200"
```

`useDefaults` defaults to `true` and injects the JVM arguments Neo4j ships with, which are tuned for
the container and worth keeping. Your `additionalArguments` are appended after them.

Setting `useDefaults: false` gives you a clean slate: none of Neo4j's default arguments are rendered
and you own the whole list. That is occasionally what a tuning exercise needs and rarely what a
production deployment wants.

When one of your arguments carries the same key as a default — `-Xmx`, or a `-XX:` flag, or a
`-D` property — yours replaces it **in place**, keeping the position of the original so ordering
stays deterministic. As with `neo4j.conf`, that replacement is reported with reason `DuplicateEntry`,
naming `spec.config.jvm.additionalArguments`, the key, the value kept and the value dropped. The same
applies if you list the same key twice yourself, in which case the last one wins.

Do not use `server.jvm.additional` in `spec.config.neo4j`; it is rejected in favour of this field.

Examples: [`examples/standalone/12-config-jvm.yaml`](../../../examples/standalone/12-config-jvm.yaml),
[`examples/cluster/10-config-jvm.yaml`](../../../examples/cluster/10-config-jvm.yaml).

## APOC configuration

APOC reads its own file, so its settings have their own field and land in `apoc.conf`:

```yaml
spec:
  plugins: [apoc]
  config:
    apoc:
      apoc.import.file.enabled: "true"
      apoc.export.file.enabled: "true"
```

One exception is deliberate: procedure allowlisting (`dbms.security.procedures.*`) belongs to
`neo4j.conf`, and the operator writes those keys there for the plugins you declared. You do not need
to allowlist `apoc.*` yourself. See [Plugins](07-plugins.md).

## Neo4j logging

Without configuration, Neo4j uses the log4j files bundled in the image, and both server and user logs
go to standard output where `kubectl logs` can read them. Override either side when you need
different appenders, levels or formats.

Inline, for small changes:

```yaml
spec:
  logging:
    serverLogsXml: |
      <?xml version="1.0" encoding="UTF-8"?>
      <Configuration>
        ...
      </Configuration>
```

Or from a ConfigMap you manage separately, which is easier to review and reuse:

```yaml
spec:
  logging:
    serverLogsConfigMapRef:
      name: neo4j-logging
      key: server-logs.xml
    userLogsConfigMapRef:
      name: neo4j-logging
      key: user-logs.xml
```

Per side it is one or the other: providing both the inline XML and a ConfigMap reference for the same
side is rejected. The two sides are independent, so overriding server logs while leaving user logs on
the image default is fine.

The content is full log4j2 configuration, not a fragment — an invalid file leaves Neo4j unable to
initialise logging and the container will not start.

Examples: [`examples/standalone/19-custom-logging.yaml`](../../../examples/standalone/19-custom-logging.yaml),
[`examples/standalone/20-logging-configmap-ref.yaml`](../../../examples/standalone/20-logging-configmap-ref.yaml).

This is Neo4j's logging. The operator's own log is a separate matter, covered in
[Operator logs](../04-troubleshooting/02-operator-logs.md).

## What was actually rendered

```bash
kubectl get configmap dev-config -n default -o jsonpath='{.data.neo4j\.conf}'
kubectl get configmap dev-apoc-config -n default -o jsonpath='{.data.apoc\.conf}'
```

In Cluster mode each pool has its own ConfigMap — `dev-primary-config`, `dev-read-config`,
`dev-analytics-config` — because pool-specific settings such as the secondary mode constraint differ.

## Applying a change

Editing configuration triggers a rolling restart: the operator notices the rendered content changed
and rolls the StatefulSets so the new file is picked up. In Cluster mode that is one member at a
time; on Standalone it is an outage for the length of one restart. See
[Operations](09-operations.md#changing-configuration).

## Next

[Plugins](07-plugins.md) · [Operations](09-operations.md) ·
[Operator-owned settings](../05-reference/operator-owned-config.md)
