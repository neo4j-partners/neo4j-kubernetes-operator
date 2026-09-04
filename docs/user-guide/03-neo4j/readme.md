# Neo4j

One page per concern, each covering the fields you set, what the operator does with them, and the
constraints that will bite you if you ignore them.

| Page | Covers |
|------|--------|
| [1. Standalone](01-standalone.md) | A single instance: the default shape, and what it cannot do |
| [2. Clustering](02-clustering.md) | Primary and secondary pools, formation, scaling, database allocation |
| [3. Storage](03-storage.md) | Data and auxiliary volumes, existing claims, extra mounts, retention |
| [4. Connectivity](04-connectivity.md) | Listeners, Services, service types, in-cluster and external access |
| [5. Security](05-security.md) | Passwords, the opt-in Secret contract, TLS, pod hardening |
| [6. Configuration](06-configuration.md) | `neo4j.conf`, JVM arguments, APOC, Neo4j logging |
| [7. Plugins](07-plugins.md) | APOC, Graph Data Science, Bloom, and where they may run |
| [8. Monitoring](08-monitoring.md) | Prometheus metrics and ServiceMonitor |
| [9. Operations](09-operations.md) | Sizing, placement, probes, restarts, maintenance, deletion |
| [10. Backup and restore](10-backup-restore.md) | `Neo4jBackup`, `Neo4jBackupSchedule`, `Neo4jRestore`: chains, scheduling, retention, seeding |

If you have not deployed anything yet, start with [Your first Neo4j](../01-getting-started/first-neo4j.md);
these pages assume you have a working instance to modify.

## Two things to know before you start

**The topology mode is immutable.** `spec.topology.mode` cannot change after creation, so a
Standalone instance never becomes a cluster. Choose deliberately, and read
[Standalone](01-standalone.md#when-standalone-is-the-wrong-choice) if you are unsure.

**Some settings belong to the operator.** Anything the operator has to control to make Kubernetes
work — listen addresses, advertised addresses, discovery, paths — is derived from the spec and
overrides what you put in `spec.config.neo4j`. The most important ones are rejected outright at
admission so you find out immediately. The full list is in
[Operator-owned settings](../05-reference/operator-owned-config.md).

## Every field, in one place

The topic pages explain the fields worth explaining. For an exhaustive list, with types, defaults
and validation rules, use the [API reference](../05-reference/api.md).
