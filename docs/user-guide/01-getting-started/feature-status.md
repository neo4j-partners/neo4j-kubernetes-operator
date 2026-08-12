# What works today

The `Neo4j` CRD is intentionally wider than the current implementation. Fields for planned
capabilities are already in the schema so that manifests stay forward-compatible, which means the
API server can accept a field the operator does not act on yet. This page tells you which is
which.

Read it as a snapshot of the code, not as a roadmap commitment.

## Confidence levels

| Level | Meaning |
|-------|---------|
| **Verified** | Implemented, covered by unit tests, and exercised end to end against a real Neo4j on Kubernetes |
| **Implemented** | Implemented and unit tested; not yet covered by an end-to-end scenario |
| **Inert** | The field exists and passes validation, but no controller code reads it |
| **Planned** | Not in the API surface you should rely on |

## Deployment and topology

| Capability | Status | Where |
|------------|--------|-------|
| Standalone — single instance | Verified | [Standalone](../03-neo4j/01-standalone.md) |
| Cluster — primaries, plus analytics and read pools | Verified | [Clustering](../03-neo4j/02-clustering.md) |
| Scale out — grow a pool, enable new members, align database topology | Verified | [Clustering](../03-neo4j/02-clustering.md#scaling-members) |
| Scale in — shrink topology, drain and drop members, then shrink the StatefulSet | Implemented | [Clustering](../03-neo4j/02-clustering.md#scaling-members) |
| Enterprise edition | Verified | `spec.edition: enterprise` is the only accepted value |
| Community edition | Planned | Rejected at admission |

## Storage

| Capability | Status | Where |
|------------|--------|-------|
| Dynamic data volume, with or without an explicit StorageClass | Verified | [Storage](../03-neo4j/03-storage.md) |
| Existing data volume — `claimName`, `volumeClaimTemplate`, or an inline volume | Verified | [Storage](../03-neo4j/03-storage.md#existing-volumes) |
| Auxiliary volumes — logs, metrics, backups, import, licenses, plugins | Verified | [Storage](../03-neo4j/03-storage.md#auxiliary-volumes) |
| `Share` mode — auxiliary directories on the data volume | Verified | [Storage](../03-neo4j/03-storage.md#auxiliary-volumes) |
| Extra mounts and Secret projections | Verified | [Storage](../03-neo4j/03-storage.md#extra-mounts) |
| PVC retention on delete and on scale-in | Verified | [Operations](../03-neo4j/09-operations.md#deleting-a-neo4j-resource) |
| Cloud object storage credentials | Planned | — |

## Connectivity

| Capability | Status | Where |
|------------|--------|-------|
| ClusterIP, NodePort and LoadBalancer Services | Verified | [Connectivity](../03-neo4j/04-connectivity.md) |
| Bolt and HTTP listeners | Verified | [Connectivity](../03-neo4j/04-connectivity.md#listeners) |
| HTTPS listener, with TLS material | Implemented | [Connectivity](../03-neo4j/04-connectivity.md#listeners) |
| Backup listener and admin Service | Implemented | [Connectivity](../03-neo4j/04-connectivity.md#the-admin-service) |
| Metrics listener | Implemented | [Monitoring](../03-neo4j/08-monitoring.md) |
| Cluster-internal Services, routing and advertised addresses | Verified | [Clustering](../03-neo4j/02-clustering.md#how-members-find-each-other) |
| Ingress and reverse proxy | Inert | Fields exist under `spec.connectivity`; nothing is rendered |
| Multi-cluster | Planned | `spec.connectivity.multiCluster.enabled: true` is rejected |

## Security

| Capability | Status | Where |
|------------|--------|-------|
| Generated initial password | Verified | [Security](../03-neo4j/05-security.md#authentication) |
| Bring your own password Secret | Verified | [Security](../03-neo4j/05-security.md#bring-your-own-password) |
| Opt-in labels on every Secret the operator mounts | Verified | [Security](../03-neo4j/05-security.md#why-the-operator-requires-opt-in-labels) |
| Bring your own TLS for Bolt, HTTPS and cluster traffic | Implemented | [Security](../03-neo4j/05-security.md#transport-security) |
| Client certificate authentication and trusted certificate bundles | Implemented | [Security](../03-neo4j/05-security.md#transport-security) |
| Pod and container security contexts, ServiceAccount, NetworkPolicy | Implemented | [Security](../03-neo4j/05-security.md#pod-and-container-hardening) |
| TLS certificate reload without restart | Implemented | `spec.trust.reload.enabled` |
| cert-manager issued certificates | Inert | Fields exist under `spec.trust.certManager`; no Certificate is created |
| LDAP authentication | Inert | Fields exist under `spec.auth.ldap` |
| Neo4j users, roles and privileges | Planned | Manage them with Cypher for now |

## Configuration and extensions

| Capability | Status | Where |
|------------|--------|-------|
| `neo4j.conf` passthrough | Verified | [Configuration](../03-neo4j/06-configuration.md) |
| JVM defaults and additional arguments | Verified | [Configuration](../03-neo4j/06-configuration.md#jvm-arguments) |
| `apoc.conf` passthrough | Verified | [Configuration](../03-neo4j/06-configuration.md#apoc-configuration) |
| Custom log4j configuration, inline or from a ConfigMap | Implemented | [Configuration](../03-neo4j/06-configuration.md#neo4j-logging) |
| Plugins — APOC, Graph Data Science, Bloom | Implemented | [Plugins](../03-neo4j/07-plugins.md) |
| Plugin licence Secrets | Implemented | [Plugins](../03-neo4j/07-plugins.md#licensed-plugins) |
| Rolling restart when configuration changes | Verified | [Operations](../03-neo4j/09-operations.md#changing-configuration) |

## Workload shaping and operations

| Capability | Status | Where |
|------------|--------|-------|
| Scheduling — node selectors, tolerations, anti-affinity presets, spread constraints | Implemented | [Operations](../03-neo4j/09-operations.md#placing-pods) |
| Resource requests and limits | Implemented | [Operations](../03-neo4j/09-operations.md#sizing-the-container) |
| Default probes, and per-probe overrides | Verified | [Operations](../03-neo4j/09-operations.md#probes) |
| Pod disruption budget | Implemented | [Operations](../03-neo4j/09-operations.md#pod-disruption-budget) |
| Offline maintenance mode | Implemented | [Operations](../03-neo4j/09-operations.md#offline-maintenance) |
| Init containers, sidecars and extra environment variables | Implemented | [Operations](../03-neo4j/09-operations.md#escape-hatches) |
| Status conditions and Kubernetes Events | Verified | [Errors](../05-reference/errors.md) |
| Neo4j version upgrades | Planned | `spec.version` is honoured at install; changing it is not orchestrated |
| Backup and restore workflows | Planned | Only the backup listener and the `backups` volume exist |
| CSV, JMX and Graphite metrics | Inert | Fields exist under `spec.features.monitoring` |
| Prometheus metrics and ServiceMonitor | Implemented | [Monitoring](../03-neo4j/08-monitoring.md) |

## Operator itself

| Capability | Status | Where |
|------------|--------|-------|
| Install from manifests or Helm chart | Verified | [Install](../02-operator-installation/03-install.md) |
| Watching an explicit list of namespaces | Verified | [Watch scope](../02-operator-installation/04-operator-scope.md) |
| Watching the whole cluster | Planned | `WATCH_NAMESPACE=*` is refused at start-up, by design |
| Uninstall preserving data | Verified | [Uninstall](../02-operator-installation/05-uninstall.md) |
| Published operator image | Planned | Build and push the image yourself — [Build the image](../02-operator-installation/02-build-image.md) |
| Operator upgrades | Planned | Re-deploy the new image; no migration is performed |

## If you need something marked Inert or Planned

Inert fields are safe to leave out of your manifests; setting them does nothing and may become
meaningful in a later release, at which point the behaviour would start applying to resources
that already carry the field. Prefer omitting them until the capability is documented as
implemented.

For planned capabilities there is usually a manual path: run Cypher yourself for users and roles,
call `neo4j-admin` in a Job of your own for backups, and recreate a resource at the target
version instead of upgrading in place.
