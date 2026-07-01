# Examples Catalog

The repository ships a catalog of ready-to-apply YAML manifests under
[`examples/`](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples)
— one file per scenario, covering every CRD the operator manages. Use them as
copy-paste starting points: pick the closest match, copy it, and adjust image
tag, storage, and resources for your environment.

Apply an example straight from GitHub without cloning the repo:

```bash
# Pin to a release tag for production; use main for the latest examples
kubectl apply -f https://raw.githubusercontent.com/priyolahiri/neo4j-kubernetes-operator/main/examples/clusters/minimal-cluster.yaml
```

Or from a local clone:

```bash
kubectl apply -f examples/clusters/minimal-cluster.yaml
```

!!! note "Create Secrets out-of-band first"
    The examples intentionally do **not** define credential Secrets inline —
    re-applying a manifest would overwrite a real password with the placeholder.
    Most examples expect an admin Secret to exist before you apply them:

    ```bash
    kubectl create secret generic neo4j-admin-secret \
      --from-literal=username=neo4j \
      --from-literal=password='<your-secure-password>'
    ```

    Each file's header comments list any additional Secrets it needs
    (cloud credentials, LDAP bind accounts, Aura tokens, …) with the exact
    `kubectl create secret` command.

!!! tip "Neo4j version"
    Most examples pin `5.26.0-enterprise` (the LTS), but every one works on a
    CalVer release too — just change the image `tag` (e.g. `2026.04-enterprise`);
    the operator auto-detects CalVer. Property sharding examples require
    Neo4j 2025.12+.

All GitHub links below point at the `main` branch.

## Standalone

Single-node `Neo4jEnterpriseStandalone` deployments for development, testing, and simple production workloads.

| Example | What it shows | Notable fields |
|---|---|---|
| [`single-node-standalone.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/standalone/single-node-standalone.yaml) | Minimal single-node deployment; also works as a `Neo4jDatabase` target via `clusterRef` | `storage.size`, `resources`, default-StorageClass inheritance |
| [`tls-standalone.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/standalone/tls-standalone.yaml) | Standalone with cert-manager TLS and native auth | `tls.mode: cert-manager`, `tls.issuerRef`, heap/pagecache `config` |
| [`ldap-standalone.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/standalone/ldap-standalone.yaml) | LDAP + native authentication on a standalone instance | `auth.ldap`, `authorization.systemAccountSecretRef` |
| [`standalone-with-trusted-ca.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/standalone/standalone-with-trusted-ca.yaml) | Trusting an internal CA (e.g. a corporate OIDC IdP cert) via the operator's JKS truststore; bundles the cert-manager `Certificate` | `trustedCASecrets`, CalVer image tag |

## Clusters

`Neo4jEnterpriseCluster` topologies from a 2-server minimum to production-tuned multi-zone deployments.

| Example | What it shows | Notable fields |
|---|---|---|
| [`minimal-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/minimal-cluster.yaml) | Smallest possible cluster — 2 servers that self-organize | `topology.servers: 2`, optional `serverModeConstraint` |
| [`three-node-simple.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/three-node-simple.yaml) | 3 servers with TLS disabled — simplest HA setup for local testing without cert-manager | `topology.servers: 3`, `tls.mode: disabled` |
| [`three-node-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/three-node-cluster.yaml) | 3-server HA cluster with cert-manager TLS and production sizing | `tls.mode: cert-manager`, `resources` (4Gi/1 CPU requests) |
| [`tls-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/tls-cluster.yaml) | Minimal 2-server cluster with cert-manager TLS | `tls.issuerRef`, `auth.authenticationProviders` |
| [`multi-server-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/multi-server-cluster.yaml) | 5-server production cluster with TLS and automatic role organization (CR name: `multi-primary-cluster`) | `topology.servers: 5`, production `resources` |
| [`production-optimized-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/production-optimized-cluster.yaml) | Production tuning — transaction memory limits, JVM settings, data retention; bundles a `Neo4jDatabase` | `storage.retentionPolicy: Retain`, tuned `config` |
| [`cluster-with-read-replicas.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/cluster-with-read-replicas.yaml) | 5 servers sized for read-heavy workloads so databases can run secondary (read) replicas | `topology.servers: 5` |
| [`topology-placement-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/topology-placement-cluster.yaml) | Multi-zone placement with spread constraints and hard anti-affinity | `topology.placement.topologySpread`, `placement.antiAffinity`, `availabilityZones` |
| [`auth-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/auth-example.yaml) | One cluster with native auth, plus commented LDAP and OIDC/SSO `auth:` variants to swap in | `auth.adminSecret`, commented `auth.ldap` / `auth.oidc` blocks |
| [`cluster-with-trusted-cas.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/cluster-with-trusted-cas.yaml) | Trusting internal CAs declaratively (cert-manager `Certificate` → Secret → JVM truststore) | `trustedCASecrets` |
| [`storage-expansion.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/storage-expansion.yaml) | Online PVC expansion walkthrough — patch `storage.size` on a running cluster | `storage.size`, requires `allowVolumeExpansion: true` StorageClass |

## External Access

Exposing a cluster outside Kubernetes via the four supported Service exposure modes. These paths need real cluster networking — validate them in your own environment.

| Example | What it shows | Notable fields |
|---|---|---|
| [`loadbalancer-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/loadbalancer-cluster.yaml) | LoadBalancer Service for cloud environments (AWS, GCP, Azure) | `service.type: LoadBalancer` |
| [`nodeport-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/nodeport-cluster.yaml) | NodePort exposure for on-premises or development environments | `service.type: NodePort` |
| [`ingress-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/ingress-cluster.yaml) | Neo4j Browser behind an Ingress controller (e.g. nginx) with cert-manager TLS | `service.ingress` |
| [`route-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/route-cluster.yaml) | OpenShift Route with edge TLS termination over a ClusterIP Service | `service.route.enabled`, `route.tls.termination`, `securityContext` |

## Databases & Sharding

`Neo4jDatabase` creation, topology, and seed-from-backup, plus `Neo4jShardedDatabase` property sharding (Neo4j 2025.12+).

| Example | What it shows | Notable fields |
|---|---|---|
| [`database-2025x.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-2025x.yaml) | CalVer-only default Cypher language on a database | `defaultCypherLanguage: "25"` |
| [`database-standalone.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-standalone.yaml) | Two databases created on a `Neo4jEnterpriseStandalone` via the same `clusterRef` field | `clusterRef`, `ifNotExists`, `wait` |
| [`database-with-topology.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-with-topology.yaml) | Custom database topology distributed across cluster servers | `topology.primaries: 2`, `topology.secondaries: 1` |
| [`database-from-s3-seed.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-from-s3-seed.yaml) | Seeding a database from an S3 backup with explicit credentials | `seedURI: s3://…`, `seedCredentials.secretRef` |
| [`database-from-gcs-seed.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-from-gcs-seed.yaml) | Seeding from Google Cloud Storage using workload identity, with point-in-time `restoreUntil` | `seedURI: gs://…`, `seedConfig.restoreUntil: "txId:…"` |
| [`database-from-azure-seed.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-from-azure-seed.yaml) | Seeding from Azure Blob Storage — storage-key and SAS-token auth variants | `seedURI: azb://…`, `seedCredentials.secretRef` |
| [`database-from-http-seed.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-from-http-seed.yaml) | Seeding over HTTPS (authenticated), HTTP, and FTP — three variants | `seedURI: https://…`, `seedCredentials` |
| [`database-dump-vs-backup-seed.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/databases/database-dump-vs-backup-seed.yaml) | Side-by-side comparison of seeding from `.dump` vs `.backup` files | `seedURI`, `seedConfig.config` (compression, validation, buffer size) |
| [`basic-property-sharding.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/property_sharding/basic-property-sharding.yaml) | Minimal working property-sharding setup — cluster + `Neo4jShardedDatabase` | `propertySharding.enabled`, 4Gi+ memory per server |
| [`development-property-sharding.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/property_sharding/development-property-sharding.yaml) | Property sharding at minimum viable resources for dev/learning | `propertySharding`, dedicated namespace |
| [`advanced-property-sharding.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/property_sharding/advanced-property-sharding.yaml) | Production-grade sharding — high-performance sizing, TLS, monitoring | `propertySharding`, `tls`, `monitoring` |
| [`property-sharding-with-backup.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/property_sharding/property-sharding-with-backup.yaml) | Backing up a sharded database with a `Neo4jBackup` CR | `Neo4jShardedDatabase` + `Neo4jBackup` target |

## Backup & Restore

Job-per-CR `Neo4jBackup` (one-shot and scheduled) and `Neo4jRestore` scenarios. See the [Backup & Restore guide](guides/backup_restore.md) for the concepts.

| Example | What it shows | Notable fields |
|---|---|---|
| [`backup-pvc-simple.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-pvc-simple.yaml) | One-shot backup of every database on an instance to a PVC | `instanceRef` + `allDatabases: true`, `storage.type: pvc` |
| [`backup-s3-basic.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-s3-basic.yaml) | S3 backup — explicit-credentials and IRSA/workload-identity variants | `storage.cloud.credentialsSecretRef`, `cloud.identity.autoCreate` |
| [`backup-scheduled-daily.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-scheduled-daily.yaml) | Scheduled daily backup to S3 (plus a weekly variant) | `schedule: "0 2 * * *"` (flat cron string), `retention` |
| [`backup-incremental.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-incremental.yaml) | Incremental (AUTO) backups of one database every 6 hours | `instanceRef` + `database`, `options.backupType: AUTO` |
| [`backup-with-type.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-with-type.yaml) | Explicit FULL backups with compression, page-cache sizing, and retention | `options.backupType: FULL`, `options.pageCache`, `retention.maxAge/maxCount` |
| [`backup-minio.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/backup-minio.yaml) | Backups to MinIO / any S3-compatible store — full, scheduled, and external-TLS-endpoint variants | `cloud.endpointURL`, `cloud.forcePathStyle: true` |
| [`restore-from-backup.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/restore-from-backup.yaml) | Restore from a `Neo4jBackup` reference, with pre/post Cypher hooks and a post-restore validation Job variant (hooks are standalone-only) | `source.backupRef`, `options.preRestore`/`postRestore` |
| [`restore-overwrite.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/restore-overwrite.yaml) | Destructive restore over an existing database on a cluster (Cypher/seedURI path — no Job) | `options.replaceExisting: true` |
| [`restore-pitr-basic.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/restore-pitr-basic.yaml) | Point-in-time recovery on a standalone target, plus an advanced storage-based variant with hooks | `source.type: pitr`, `pointInTime`, `pitr.logStorage` |
| [`pitr-setup-complete.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/backup-restore/pitr-setup-complete.yaml) | End-to-end PITR environment — base backup with validation, log-retention notes, and the restore | `options.validate`, `options.includeMetadata`, `source.type: pitr` |

!!! warning "PITR is standalone-only via `Neo4jRestore`"
    `source.type: pitr` runs `neo4j-admin database restore --restore-until` as a
    Job and applies only to `Neo4jEnterpriseStandalone` targets. For a cluster,
    do point-in-time recovery by creating a `Neo4jDatabase` with
    `seedConfig.restoreUntil` instead — see the database seed examples above.

## Users, Roles & Auth

Declarative user, role, and access management via `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`, and `Neo4jAuthRule`. Numbered for a natural reading order; see [User & Role Management](user_role_management.md).

| Example | What it shows | Notable fields |
|---|---|---|
| [`01-readonly-user.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/01-readonly-user.yaml) | Read-only application user bound to the built-in `reader` role | `passwordSecretRef`, `roles: [reader]` |
| [`02-custom-role-with-user.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/02-custom-role-with-user.yaml) | The canonical pattern — privileges on a `Neo4jRole`, user references it by name | `Neo4jRole.spec.privileges` (GRANT/DENY ending in `TO <role>`) |
| [`03-suspended-user.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/03-suspended-user.yaml) | Suspending a user during a security incident without deleting the CR | `accountStatus: suspended` |
| [`04-external-auth.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/04-external-auth.yaml) | User authenticated only via OIDC and LDAP providers — no native password | `externalAuth[].provider/id`, no `passwordSecretRef` |
| [`05-adopt-builtin.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/05-adopt-builtin.yaml) | Adopting the built-in `editor` role to tighten its privileges (never dropped on CR delete) | `adoptBuiltin: true` |
| [`06-rolebinding-sso-user.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/06-rolebinding-sso-user.yaml) | Granting roles to an externally-provisioned (SSO/LDAP) user the operator does not manage | `Neo4jRoleBinding.spec.roles`, optional `enforceExclusive` |
| [`07-authrule-abac.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/07-authrule-abac.yaml) | Attribute-based access control — mapping OIDC token claims to roles at login (Neo4j 2026.03+) | `Neo4jAuthRule` condition expressions over token claims |

## Plugins

`Neo4jPlugin` examples — `clusterRef` can point at either a cluster or a standalone.

| Example | What it shows | Notable fields |
|---|---|---|
| [`apoc-plugin-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/plugins/apoc-plugin-example.yaml) | APOC with env-var-based configuration (the 5.26+ way — APOC config no longer lives in `neo4j.conf`) | `config.apoc.*` → `NEO4J_APOC_*` env vars |
| [`gds-plugin-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/plugins/gds-plugin-example.yaml) | Graph Data Science with automatic procedure allowlisting | `source.type: community`, `gds.enterprise.license_file` |
| [`bloom-plugin-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/plugins/bloom-plugin-example.yaml) | Bloom with its required `neo4j.conf` settings | `dbms.bloom.license_file`, role restriction |
| [`genai-plugin-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/plugins/genai-plugin-example.yaml) | GenAI plugin for vector embeddings and AI integrations (download needs internet egress) | `name: genai`, `source.type: official` |
| [`standalone-plugin-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/plugins/standalone-plugin-example.yaml) | GDS on a standalone deployment, with a plugin dependency declaration | `dependencies[].versionConstraint` |
| [`cluster-plugin-example.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/plugins/cluster-plugin-example.yaml) | **Deprecated** legacy APOC config approach, kept for reference — use `apoc-plugin-example.yaml` instead | — |

## Security

Workload-namespace NetworkPolicies and Kyverno conformance policies (the operator validates inline; these are defence-in-depth at admission time).

| Example | What it shows | Notable fields |
|---|---|---|
| [`networkpolicy-cluster.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/networkpolicy-cluster.yaml) | Hand-applied NetworkPolicy for a cluster namespace — Bolt/HTTP for clients, discovery/RAFT pod-to-pod, metrics for the operator, default-deny egress | Ports 7687/7474/7473, 6000/7000, 2004 |
| [`networkpolicy-standalone.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/networkpolicy-standalone.yaml) | Same shape for the single-pod standalone case (no intra-cluster ports) | `podSelector` on `app: <standalone-name>` |
| [`policies/01-enterprise-image.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/policies/01-enterprise-image.yaml) | Kyverno audit: image tag must be an Enterprise image | `validationFailureAction: Audit` |
| [`policies/02-tls-required.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/policies/02-tls-required.yaml) | Kyverno audit: flags CRs with TLS disabled | `spec.tls.mode` check |
| [`policies/03-monitoring-enabled.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/policies/03-monitoring-enabled.yaml) | Kyverno audit: recommends `spec.monitoring` so diagnostics and health conditions populate | `spec.monitoring` check |
| [`policies/04-runAsNonRoot-not-disabled.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/policies/04-runAsNonRoot-not-disabled.yaml) | Kyverno audit: catches `securityContext` overrides that disable `runAsNonRoot` | `spec.securityContext` check |
| [`policies/05-resource-limits.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/security/policies/05-resource-limits.yaml) | Kyverno audit: requires memory and CPU limits (Enterprise needs ≥ 1.5Gi) | `spec.resources.limits` check |

## Fleet Management

Registering self-managed deployments with [Aura Fleet Management](aura_fleet_management.md) so they appear in the Aura console. Requires a token from your Aura tenant.

| Example | What it shows | Notable fields |
|---|---|---|
| [`cluster-with-fleet-management.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/fleet-management/cluster-with-fleet-management.yaml) | 3-server cluster registered with Aura Fleet Management | `auraFleetManagement.enabled`, `tokenSecretRef.name/key` |
| [`standalone-with-fleet-management.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/fleet-management/standalone-with-fleet-management.yaml) | Single-node deployment registered with Aura Fleet Management | `auraFleetManagement.enabled`, `tokenSecretRef.name` |

## End-to-End

Multi-resource scenarios that compose the CRDs above into complete environments.

| Example | What it shows | Notable fields |
|---|---|---|
| [`complete-deployment.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/end-to-end/complete-deployment.yaml) | Full production stack — namespace, TLS cluster, two databases, two backups, a plugin, and Prometheus `ServiceMonitor` wiring | Cluster + `Neo4jDatabase` + `Neo4jBackup` + `Neo4jPlugin` + monitoring |
| [`development-workflow.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/end-to-end/development-workflow.yaml) | Lightweight dev setup — standalone instance plus a dev database and a separate integration-test database, with cypher-shell workflow notes | Standalone + two `Neo4jDatabase` CRs |
| [`disaster-recovery.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/end-to-end/disaster-recovery.yaml) | DR proactive phase — scheduled compressed off-site S3 backup, with recovery-phase restore snippets in comments | `Neo4jBackup` every 4 hours, commented `Neo4jRestore` recipes |
| [`multi-tenancy.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/end-to-end/multi-tenancy.yaml) | Logical multi-tenancy — one shared cluster, a database per tenant, per-tenant backup cadence; honest about what it does *not* isolate | Cluster + four `Neo4jDatabase` + three `Neo4jBackup` CRs |

## See also

- [Examples README](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples) — prerequisites, customization, and topology guidance
- [Getting Started](getting_started.md) — first deployment walkthrough
- [Configuration Guide](configuration.md) — every spec field explained
