# What works today

The `Neo4j` CRD is intentionally wider than the current implementation. Fields exist for capabilities
the operator does not act on yet, so that manifests stay forward-compatible — which means the API
server can accept a field that changes nothing. This page tells you which is which.

Read it as a snapshot of the code. Where a capability is missing, the status says whether the shape it
will take is settled or still open.

## Status values

| Status | Meaning |
|--------|---------|
| **Verified** | Implemented, unit tested, and exercised end to end against a real Neo4j on Kubernetes |
| **Implemented** | Implemented and unit tested; no end-to-end scenario covers it yet |
| **Planned** | Not implemented. The design is settled, so the field names and behaviour you see documented are the ones you will get |
| **Not decided** | Not implemented and the approach is still open. Anything present in the schema may change shape or disappear |

When a schema field exists for something not yet implemented, the note says so. Setting such a field
validates and does nothing — see [the last section](#if-something-is-planned-or-not-decided).

## Operator itself

| Capability | Status | Where |
|------------|--------|-------|
| Install from manifests or Helm chart | Verified | [Install](../02-operator-installation/03-install.md) |
| Watching an explicit list of namespaces | Verified | [Watch scope](../02-operator-installation/04-operator-scope.md) |
| Uninstall preserving data | Verified | [Uninstall](../02-operator-installation/05-uninstall.md) |
| Published operator image and chart | Verified | GHCR, per release — [Operator installation](../02-operator-installation/readme.md) |
| Operator upgrades | Planned | Re-deploy the new image; no migration is performed |

## Neo4J Deployment and topology

| Capability | Status | Where |
|------------|--------|-------|
| Standalone — single instance | Verified | [Standalone](../03-neo4j/01-standalone.md) |
| Cluster — primaries, plus analytics and read pools | Verified | [Clustering](../03-neo4j/02-clustering.md) |
| Scale out — grow a pool, enable new members, align database topology | Verified | [Clustering](../03-neo4j/02-clustering.md#scaling-members) |
| Scale in — shrink topology, drain and drop members, then shrink the StatefulSet | Implemented | [Clustering](../03-neo4j/02-clustering.md#scaling-members) |
| Enterprise edition | Verified | `spec.edition: enterprise` with `spec.license.accept` |
| Community edition | Implemented | `spec.edition: community`, Standalone only, no licence, no backup or metrics — accepted by admission but not yet exercised end to end |

## Neo4J Features

### Storage

| Capability | Status | Where |
|------------|--------|-------|
| Dynamic data volume, with or without an explicit StorageClass | Verified | [Storage](../03-neo4j/03-storage.md) |
| Existing data volume — `claimName`, `volumeClaimTemplate`, or an inline volume | Verified | [Storage](../03-neo4j/03-storage.md#existing-volumes) |
| Auxiliary volumes — logs, metrics, backups, import, licenses, plugins | Verified | [Storage](../03-neo4j/03-storage.md#auxiliary-volumes) |
| `Share` mode — auxiliary directories on the data volume | Verified | [Storage](../03-neo4j/03-storage.md#auxiliary-volumes) |
| Extra mounts and Secret projections | Verified | [Storage](../03-neo4j/03-storage.md#extra-mounts) |
| PVC retention on delete and on scale-in | Verified | [Operations](../03-neo4j/09-operations.md#deleting-a-neo4j-resource) |
| Cloud object storage credentials | Planned | Cloud IAM annotations on the ServiceAccount are refused today |

### Connectivity

| Capability | Status | Where |
|------------|--------|-------|
| ClusterIP, NodePort and LoadBalancer Services | Verified | [Connectivity](../03-neo4j/04-connectivity.md) |
| Bolt and HTTP listeners | Verified | [Connectivity](../03-neo4j/04-connectivity.md#listeners) |
| HTTPS listener, with TLS material | Implemented | [Connectivity](../03-neo4j/04-connectivity.md#listeners) |
| Backup listener and admin Service | Implemented | [Connectivity](../03-neo4j/04-connectivity.md#the-admin-service) |
| Metrics listener | Implemented | [Monitoring](../03-neo4j/08-monitoring.md) |
| Cluster-internal Services, routing and advertised addresses | Verified | [Clustering](../03-neo4j/02-clustering.md#how-members-find-each-other) |
| Ingress and reverse proxy | Not decided | Fields exist under `spec.connectivity.ingress`; nothing is rendered |

### Security

| Capability | Status | Where |
|------------|--------|-------|
| Generated initial password | Verified | [Security](../03-neo4j/05-security.md#authentication) |
| Bring your own password Secret | Verified | [Security](../03-neo4j/05-security.md#bring-your-own-password) |
| Opt-in labels on every Secret the operator mounts | Verified | [Security](../03-neo4j/05-security.md#why-the-operator-requires-opt-in-labels) |
| Bring your own TLS for Bolt, HTTPS and cluster traffic | Implemented | [Security](../03-neo4j/05-security.md#transport-security) |
| Client certificate authentication and trusted certificate bundles | Implemented | [Security](../03-neo4j/05-security.md#transport-security) |
| Pod and container security contexts, ServiceAccount, NetworkPolicy | Implemented | [Security](../03-neo4j/05-security.md#pod-and-container-hardening) |
| TLS certificate reload without restart | Partial | `spec.trust.reload.enabled` turns on Neo4j's reload for CA bundles. Leaf key/cert are `subPath` mounts, so the operator rolls the StatefulSet when those Secret bytes change — see [Certificate renewal](../03-neo4j/05-security.md#certificate-renewal) |
| cert-manager issued certificates | Implemented | [Security](../03-neo4j/05-security.md#certificates-issued-by-cert-manager) |
| Neo4j users, roles and privileges | Not decided | Dedicated resources, after backup and restore. Use Cypher meanwhile |
| LDAP and external auth providers | Not decided | `spec.auth.ldap` is ignored; configure providers through `spec.config` instead |

### Configuration and extensions

| Capability | Status | Where |
|------------|--------|-------|
| `neo4j.conf` passthrough | Verified | [Configuration](../03-neo4j/06-configuration.md) |
| JVM defaults and additional arguments | Verified | [Configuration](../03-neo4j/06-configuration.md#jvm-arguments) |
| `apoc.conf` passthrough | Verified | [Configuration](../03-neo4j/06-configuration.md#apoc-configuration) |
| Custom log4j configuration, inline or from a ConfigMap | Implemented | [Configuration](../03-neo4j/06-configuration.md#neo4j-logging) |
| Plugins — APOC, Graph Data Science, Bloom | Implemented | [Plugins](../03-neo4j/07-plugins.md) |
| Plugin licence Secrets | Implemented | [Plugins](../03-neo4j/07-plugins.md#licensed-plugins) |
| Rolling restart when configuration changes | Verified | [Operations](../03-neo4j/09-operations.md#changing-configuration) |

### Workload shaping and operations

| Capability | Status | Where |
|------------|--------|-------|
| Scheduling — node selectors, tolerations, anti-affinity presets, spread constraints | Implemented | [Operations](../03-neo4j/09-operations.md#placing-pods) |
| Resource requests and limits | Implemented | [Operations](../03-neo4j/09-operations.md#sizing-the-container) |
| Default probes, and per-probe overrides | Verified | [Operations](../03-neo4j/09-operations.md#probes) |
| Pod disruption budget | Implemented | [Operations](../03-neo4j/09-operations.md#pod-disruption-budget) |
| Offline maintenance mode | Implemented | [Operations](../03-neo4j/09-operations.md#offline-maintenance) |
| Init containers, sidecars and extra environment variables | Implemented | [Operations](../03-neo4j/09-operations.md#escape-hatches) |
| Status conditions and Kubernetes Events | Verified | [Errors](../05-reference/errors.md) |
| Prometheus metrics and ServiceMonitor | Implemented | [Monitoring](../03-neo4j/08-monitoring.md) |
| Backup and restore | Implemented | [Backup and restore](../03-neo4j/10-backup-restore.md) — `Neo4jBackup`, `Neo4jBackupSchedule`, `Neo4jRestore` |
| Neo4j version upgrades | Planned | `spec.version` is honoured at install; changing it is not orchestrated |
| CSV, JMX and Graphite metrics | Not decided | Fields exist under `spec.features.monitoring`; only Prometheus is wired |

Cluster-wide watch is not a gap: it is refused on purpose, as explained in
[Watch scope](../02-operator-installation/04-operator-scope.md).

## If something is Planned or Not decided

Leave the corresponding fields out of your manifests. A field that does nothing today may start doing
something in a later release, and it would then apply to resources that already carry it — a value you
set as a placeholder becomes live without you touching the manifest. Omitting the field keeps that
decision yours.

There is usually a manual path meanwhile:

- **Users and roles** — run the Cypher yourself, or point Neo4j at your identity provider through
  `spec.config`.
- **Version changes** — deploy a new resource at the target version and migrate the data, rather than
  editing `spec.version` in place.
- **Ingress** — write the Ingress object yourself against the client Service.
