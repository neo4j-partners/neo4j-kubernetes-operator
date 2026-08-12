# Operator-owned settings

Some `neo4j.conf` settings are derived from the spec and are not yours to set. This page lists them,
so you can tell in advance whether a setting you want belongs to you or to the operator.

The rendered file is a merge of four layers, applied in this order:

1. **Operator defaults** — sensible starting points you may override
2. **Plugin configuration** — from `spec.pluginDefinitions.<id>.config`
3. **Your configuration** — `spec.config.neo4j`
4. **Operator injections** — derived from topology, connectivity and trust

Later layers win, so your values beat layers 1 and 2, and layer 4 beats yours.

## Refused at admission

These keys are rejected in `spec.config.neo4j`, with an error naming the field to use instead. There
is no way to force them, which is the point: a listen address that disagrees with the Service would
produce a database nothing can reach.

| Key | Use instead |
|-----|-------------|
| `server.bolt.listen_address`, `server.bolt.enabled` | `spec.connectivity.listeners.bolt` |
| `server.http.listen_address`, `server.http.enabled` | `spec.connectivity.listeners.http` |
| `server.https.listen_address`, `server.https.enabled` | `spec.connectivity.listeners.https` |
| `server.backup.listen_address` | `spec.connectivity.listeners.backup` |
| `server.jvm.additional` | `spec.config.jvm.additionalArguments` |

## Injected, and winning over your value

Set one of these and it is dropped from the rendered file. The operator reports the collision as a
Warning Event with reason `DuplicateEntry`, showing `kept (operator-injected)` — see the
[error reference](errors.md#duplicateentry).

| Keys | Derived from |
|------|--------------|
| `server.bolt.enabled`, `server.http.enabled`, `server.https.enabled`, `server.backup.enabled`, and their listen addresses | `spec.connectivity.listeners` |
| `server.metrics.prometheus.enabled`, `server.metrics.prometheus.endpoint` | `spec.features.monitoring.prometheus` plus the metrics listener |
| `dbms.cluster.minimum_initial_system_primaries_count` | `spec.topology.minimumMembers` |
| `initial.dbms.default_primaries_count` | `spec.topology.defaultPrimariesCount` |
| `dbms.cluster.discovery.resolver_type`, `dbms.kubernetes.discovery.service_port_name`, `dbms.kubernetes.label_selector`, `dbms.cluster.raft.binding_timeout` | Kubernetes-based cluster discovery |
| `dbms.routing.enabled`, `dbms.routing.default_router`, `dbms.routing.client_side.enforce_for_domains` | Cluster mode and `spec.connectivity.clusterDomain` |
| `server.bolt.advertised_address`, `server.cluster.advertised_address`, `server.cluster.raft.advertised_address`, `server.routing.advertised_address` | Per-member Service names |
| `server.cluster.system_database_mode`, `initial.server.mode_constraint` | Being an analytics or read pool member |
| `dbms.ssl.policy.*`, `server.bolt.tls_level` | `spec.trust` |
| `server.logs.config`, `server.logs.user.config` | `spec.logging` |

Cluster keys are only injected in Cluster mode, and TLS keys only when `spec.trust.enabled` is true,
so in a Standalone deployment without TLS most of this table does not apply.

## Defaults you may override

These are set by the operator and lose to `spec.config.neo4j`. Overriding them is legitimate; the
`DuplicateEntry` Event will show `kept (user)`, confirming your value is the one in effect.

| Key | Default | Set for |
|-----|---------|---------|
| `server.default_listen_address` | `0.0.0.0` | Making Neo4j reachable from outside the pod |
| `server.directories.plugins` | `/plugins` | Deployments with plugins or a plugins volume |
| `dbms.security.procedures.unrestricted`, `dbms.security.procedures.allowlist` | The patterns of the plugins on this pool, such as `apoc.*,gds.*` | Declared plugins |
| `dbms.security.http_auth_allowlist` | `gds.*` | Deployments with Graph Data Science |

Overriding the procedure allowlists replaces them wholesale, so include the plugin patterns you still
need — dropping `apoc.*` from the list disables APOC procedures.

## Why these are managed

Three different reasons, worth distinguishing because they predict what else will be managed later:

**Addresses must match Kubernetes.** Listen and advertised addresses come from Service names, pod
names and the cluster DNS domain. A user-supplied value that disagrees produces a server that starts
and is unreachable, or a cluster member nobody can find.

**Cluster identity must be consistent.** Discovery, routing and quorum settings have to agree across
every member. They are derived from a single spec so they cannot drift between pools.

**Some settings have a better field.** JVM arguments, TLS material and logging configuration have
dedicated fields that are validated, documented, and rendered into the right file. Passing them as
raw configuration would bypass that.

## Checking the result

```bash
kubectl get configmap dev-config -n default -o jsonpath='{.data.neo4j\.conf}'
```

In Cluster mode, read the ConfigMap of the pool you care about — `dev-primary-config`,
`dev-read-config`, `dev-analytics-config` — since injected values differ per pool.

## Related

[Configuration](../03-neo4j/06-configuration.md) · [Connectivity](../03-neo4j/04-connectivity.md) ·
[Error reference](errors.md)
