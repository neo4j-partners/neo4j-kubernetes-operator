# Architecture Overview

This guide provides a comprehensive overview of the Neo4j Enterprise Operator's architecture, design principles, and current implementation.

## Core Design Principles

The Neo4j Enterprise Operator follows cloud-native best practices with a focus on:

- **Production Stability**: Optimized reconciliation frequency and efficient resource management
- **Performance**: Intelligent rate limiting and status update optimization
- **Server-Based Architecture**: Unified server deployments with self-organizing roles
- **Resource Efficiency**: On-demand backup Jobs (no long-running backup pod or sidecar)
- **Observability**: Comprehensive monitoring and operational insights
- **Validation**: Proactive resource validation and recommendations

## Current Architecture

### Server-Based Architecture

The operator has evolved to use a **unified server-based architecture** where Neo4j servers self-organize into primary/secondary roles:

#### Key Changes from Legacy Architecture
- **Before**: Separate primary/secondary StatefulSets with complex orchestration
- **After**: Single `{cluster-name}-server` StatefulSet with self-organizing servers
- **Benefit**: Simplified resource management, improved scaling, reduced complexity

#### Current Implementation
```yaml
# Neo4jEnterpriseCluster topology
topology:
  servers: 3  # Creates: my-cluster-server StatefulSet (replicas: 3)
  # Pods: my-cluster-server-0, my-cluster-server-1, my-cluster-server-2
```

```yaml
# Neo4jEnterpriseStandalone deployment
# Creates: my-standalone StatefulSet (replicas: 1)
# Pod: my-standalone-0
```

### Backup architecture

Backups are owned by the dedicated **`Neo4jBackup` CRD**: each CR (one-shot or scheduled via `spec.schedule`) spawns a Kubernetes `Job` (or `CronJob` → child Jobs) that runs `neo4j-admin database backup` from the same Neo4j Enterprise image as the cluster. No sidecar containers, no persistent backup pod. The Job connects to each `{cluster}-server-N` Pod on port 6362 (`server.backup.listen_address=0.0.0.0:6362`, configured automatically) and streams artifacts to the destination (PVC, S3, GCS, or Azure).

All runs of a single Neo4jBackup CR share a `<base>/<cr-name>/` directory so `neo4j-admin` can chain `--type=DIFF` backups off the prior `FULL`. Per-run identity is preserved via the timestamp `neo4j-admin` embeds in each `.backup` filename.

## Custom Resource Definitions (CRDs)

The operator defines eleven CRDs located in `api/v1beta1/`:

### Core Deployment CRDs

#### Neo4jEnterpriseCluster (`neo4jenterprisecluster_types.go`)
- **Purpose**: High-availability clustered Neo4j Enterprise deployments
- **Architecture**: Server-based with `{cluster-name}-server` StatefulSet
- **Minimum Topology**: 2+ servers (enforced by validation)
- **Server Organization**: Servers self-organize into primary/secondary roles for databases
- **Scaling**: Horizontal scaling supported with topology validation
- **Discovery**: LIST resolver with static pod FQDNs; V2_ONLY explicitly set for 5.26.x, implicit for 2025.x+
- **Resource Pattern**: Single StatefulSet replaces complex multi-StatefulSet architecture

**Key Fields**:
```go
type Neo4jEnterpriseClusterSpec struct {
    Image    ImageSpec              `json:"image"`
    Topology TopologyConfiguration  `json:"topology"`  // servers: N
    Storage  StorageSpec           `json:"storage"`
    // Outbound TLS trust + escape hatches (see "Security Architecture
    // > 6. Outbound trust" further down for the full lifecycle):
    TrustedCASecrets  []TrustedCASecret    `json:"trustedCASecrets,omitempty"`
    ExtraVolumes      []corev1.Volume      `json:"extraVolumes,omitempty"`
    ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`
    // ... additional fields
}
```

#### Neo4jEnterpriseStandalone (`neo4jenterprisestandalone_types.go`)
- **Purpose**: Single-node Neo4j Enterprise deployments
- **Architecture**: Uses clustering infrastructure but fixed at 1 replica
- **Use Cases**: Development, testing, simple production workloads
- **StatefulSet**: `{standalone-name}` (no "-server" suffix)
- **Configuration**: Modern clustering approach with single member (Neo4j 5.26+)
- **Restrictions**: Cannot scale beyond 1 replica
- **Shared API surface**: Mirrors the cluster spec for `TrustedCASecrets` /
  `ExtraVolumes` / `ExtraVolumeMounts`. The wire-up differs slightly: cluster
  pods receive `-Djavax.net.ssl.trustStore=…` via the `NEO4J_server_jvm_additional`
  env var, while standalone pods receive the same flags as `server.jvm.additional=…`
  lines emitted into the ConfigMap-backed neo4j.conf.

### Database Management CRDs

#### Neo4jDatabase (`neo4jdatabase_types.go`)
- **Purpose**: Manages database lifecycle within clusters and standalone deployments
- **Dual Support**: Works with both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone
- **Enhanced Validation**: DatabaseValidator supports automatic deployment type detection
- **Neo4j 5.26+ Syntax**: Uses modern `TOPOLOGY` clause for database creation
- **Standalone Fix**: Added NEO4J_AUTH environment variable for automatic authentication

**Key Features**:
```go
type Neo4jDatabaseSpec struct {
    ClusterRef string           `json:"clusterRef"`     // References cluster OR standalone
    Name       string           `json:"name"`           // Database name
    Topology   DatabaseTopology `json:"topology"`       // Primary/secondary counts
    IfNotExists bool            `json:"ifNotExists"`    // CREATE IF NOT EXISTS
}
```

#### Neo4jPlugin (`neo4jplugin_types.go`)
- **Purpose**: Manages Neo4j plugin installation and configuration
- **Dual Architecture Support**: Enhanced for server-based cluster + standalone compatibility
- **Deployment Detection**: Automatic cluster vs standalone recognition
- **Resource Naming**: Handles `{cluster-name}-server` vs `{standalone-name}` patterns
- **Plugin Sources**: Official, community, custom registry, direct URL support

### Backup & Restore CRDs

#### Neo4jBackup (`neo4jbackup_types.go`)
- **Purpose**: Manages backup operations for both clusters and standalone deployments
- **Job-per-CR architecture**: Each `Neo4jBackup` (one-shot or scheduled) spawns a Kubernetes Job (or CronJob → child Jobs). No sidecar containers, no persistent backup pod.
- **Target Support**: Cluster OR standalone (`spec.target.kind` resolves the type; the controller probes the cluster ref and routes to `BuildStandaloneBackupFromAddress` for standalones, `BuildBackupFromAddresses` for clusters)
- **Neo4j 5.26+ Support**: Modern backup syntax with `--to-path` parameter
- **Shared-directory layout**: All runs of a single CR write into `<base>/<cr-name>/` so `neo4j-admin` can chain differential backups off the prior full. Per-run identity is preserved via the ISO-8601 timestamp `neo4j-admin` embeds in each `.backup` filename (also captured into `status.history[].artifactFilename` / `shardArtifacts[].filename` via Pod-log parsing).

#### Neo4jRestore (`neo4jrestore_types.go`)
- **Purpose**: Manages database restoration from backups
- **Point-in-Time Recovery**: Supports `--restore-until` for precise recovery
- **Cross-Deployment Support**: Works with both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` (auto-detected via `clusterRef`)
- **Source types**: `backup` (reference a `Neo4jBackup` CR), `storage` (local PVC), `s3`/`gcs`/`azure` (cloud URIs), `pitr` (point-in-time)

### Identity, Access & Sharding CRDs

#### Neo4jUser (`neo4juser_types.go`)
- **Purpose**: Declarative user lifecycle (create/update/drop) with password rotation via Secret hash
- **Dual Support**: Cluster or standalone (auto-detected; `clusterRef` is namespace-scoped)
- **Authoritative spec**: password (via Secret), `accountStatus`, `homeDatabase`, `roles[]`, `externalAuth`. Drift is reverted every reconcile.
- **No inline privileges**: privileges live on `Neo4jRole`, never on `Neo4juser` — users carry only `roles: []` bindings.

#### Neo4jRole (`neo4jrole_types.go`)
- **Purpose**: Declarative role + privilege management
- **Authoritative spec**: `privileges[]` when `enforcePrivileges: true` (default). Diff-based reconciliation via `SHOW ROLE ... PRIVILEGES AS COMMANDS`; revokes are derived textually (`DerivePrivilegeRevoke`).
- **Built-in role guard**: names in `{PUBLIC, reader, editor, publisher, architect, admin}` rejected unless `spec.adoptBuiltin: true`. Adopted built-ins are NEVER dropped on CR delete.

#### Neo4jRoleBinding (`neo4jrolebinding_types.go`)
- **Purpose**: Grants existing externally-provisioned users (SSO / LDAP first-login) into operator-managed roles, without creating or dropping the user
- **Overlap guard**: validator rejects bindings whose `clusterRef`+`username` match an existing `Neo4jUser` CR in the same namespace

#### Neo4jAuthRule (`neo4jauthrule_types.go`)
- **Purpose**: Attribute-based access control (ABAC) — declarative `AUTH RULE` DDL
- **Version-gated**: requires Neo4j 2026.03+ (`SupportsAuthRules`); controller refuses to reconcile against older versions
- **Cypher**: every statement prepends `CYPHER 25` because system DB defaults to Cypher 5 on 2026.x, which can't parse `AUTH` keywords

#### Neo4jShardedDatabase (`neo4jshardeddatabase_types.go`)
- **Purpose**: Sharded database management (property sharding via `db.shard.*`)
- **Version-gated**: requires Neo4j 2025.12+ images and 5+ servers with 4-8Gi/server, 2+ CPU/server

## Controllers Architecture

### Core Controllers (`internal/controller/`)

#### Neo4jEnterpriseCluster Controller (`neo4jenterprisecluster_controller.go`)
**Primary cluster management controller with server-based architecture:**

**Performance Optimizations**:

- **Efficient Reconciliation**: Reduced from ~18,000 to ~34 reconciliations per minute
- **Smart Status Updates**: Only updates when cluster state changes
- **ConfigMap Debouncing**: 2-minute debounce prevents restart loops
- **Resource Version Conflict Handling**: Retry logic for concurrent updates

**Server-Based Implementation**:

- **Single StatefulSet**: Creates `{cluster-name}-server` instead of separate primary/secondary
- **Self-Organizing Servers**: Neo4j servers automatically assign database hosting roles
- **Simplified Resource Management**: Unified pod templates and configuration
- **Certificate DNS**: Includes all server pod names in TLS certificates

**Split-Brain Detection**:

- **Location**: `internal/controller/splitbrain_detector.go`
- **Multi-Pod Analysis**: Connects to each server to compare cluster views
- **Automatic Repair**: Restarts orphaned pods to rejoin main cluster
- **Production Ready**: Comprehensive logging and fallback mechanisms

**Scale-Down Drain** (`internal/controller/scale_down.go`):

Lowering `spec.topology.servers` does not immediately shrink the StatefulSet.
Deleting a pod that still hosts database copies orphans data and can break
quorum, so the controller drains the outgoing servers through Neo4j's own
server-lifecycle commands *before* lowering `replicas`.

- **Replica hold**: while a drain is in progress the controller skips writing
  `sts.Spec.Replicas` (gated by `scaleDownDrainInProgress` + the
  `neo4j.com/scale-down-draining-servers` annotation, a CSV of the outgoing
  `serverId`s). Targets are tracked by **serverId**, not pod ordinal, because a
  server's `address` goes NULL once it enters `Deallocating` — an ordinal-based
  map would lose it and prematurely declare the drain complete.
- **State machine** (`planScaleDownStep` over `SHOW SERVERS`): `cordon` every
  outgoing server → once all are `Cordoned`, `DEALLOCATE DATABASES FROM SERVER`
  (relocates their database copies) → wait while they still host any user
  database → `DROP SERVER` once a server hosts **only `system`** (a system-DB
  primary stays `Deallocating` forever and never reaches `Deallocated`, so DROP
  is gated on *hosting only `system`*, not on the `Deallocated` state) → finally
  lower `replicas`. Scaling back up mid-drain cancels it (clears the annotation).
- **Fail-fast pre-flight**: before cordoning anything, a `DRYRUN DEALLOCATE`
  checks the move is feasible, and the **system-DB voting floor**
  (`dbms.cluster.minimum_initial_system_primaries_count`, read live via
  `MinimumSystemPrimaries`) is enforced — if the requested `servers` is below the
  floor, or a database has a sole primary on an outgoing server that Neo4j cannot
  relocate, the controller refuses with a `ScaleDownBlocked` condition and an
  actionable remedy instead of stranding a half-deallocated server. Neo4j has no
  primitive that relocates a *sole primary* (`REALLOCATE` and
  `deallocateDatabaseFromServer` both refuse it), so the operator never
  auto-changes a database's `TOPOLOGY` — that would silently alter durability.
- **Formation floor**: `EffectiveMinSystemPrimaries()` writes
  `minimum_initial_system_primaries_count = min(3, servers)` at formation
  (overridable via `spec.topology.minSystemPrimaries`, clamped to `[2, servers]`),
  so a cluster of 3+ can scale down to 3 (the recommended odd quorum) rather than
  being pinned at its birth size.
- **PVC retention**: the server StatefulSet sets
  `persistentVolumeClaimRetentionPolicy: {whenScaled: Delete, whenDeleted: Retain}`
  so a drained server's PVC is reclaimed on scale-down but cluster-wide deletion
  still retains data.

#### Neo4jEnterpriseStandalone Controller (`neo4jenterprisestandalone_controller.go`)
**Single-node deployment controller:**

**Key Features**:

- **Clustering Infrastructure**: Uses same infrastructure as clusters (Neo4j 5.26+ approach)
- **Single Member Configuration**: Sets up clustering with single server
- **Resource Management**: Handles ConfigMap, Service, and StatefulSet
- **Status Tracking**: Comprehensive status updates for standalone instances

#### Database Controller (`neo4jdatabase_controller.go`)
**Enhanced for dual deployment support:**

- **Automatic Detection**: Tries cluster lookup first, then standalone fallback
- **Neo4j Client Creation**: `NewClientForEnterprise()` vs `NewClientForEnterpriseStandalone()`
- **Authentication Handling**: Manages NEO4J_AUTH for standalone deployments
- **Syntax Support**: Neo4j 5.26+ and 2025.x database creation syntax

#### Plugin Controller (`plugin_controller.go`)
**Manages plugin lifecycle with architecture compatibility:**

- **DeploymentInfo Abstraction**: Unified handling of cluster/standalone types
- **Resource Naming**: Correct StatefulSet names (`{cluster-name}-server` vs `{standalone-name}`)
- **Pod Labels**: Applies appropriate labels for each deployment type
- **Plugin Sources**: Official, community, custom registries, direct URLs

#### Backup Controller (`neo4jbackup_controller.go`)
**Backup management:**

- **Architecture**: Job-per-`Neo4jBackup`-CR. No persistent backup pod or sidecars.
- **Cross-Deployment Support**: Backs up both clusters and standalone deployments
- **Modern Syntax**: Neo4j 5.26+ compatible backup commands

#### Restore Controller (`neo4jrestore_controller.go`)
**Database restoration management:**

- **Point-in-Time Recovery**: Supports precise timestamp restoration via `--restore-until`
- **Flexible Targets**: Cluster OR standalone (auto-detected through `getClusterRef`; standalone is converted into a synthetic cluster representation with `Topology.Servers=1` via `standaloneAsCluster`)
- **Validation**: Ensures target deployment compatibility

**Two restore paths.** `startRestore` branches on `isRestoreTargetTrueCluster`:
true clusters take a **Cypher path** (no Job — `neo4j-admin restore`'s
`--overwrite-destination` is documented as unsafe on a cluster); standalone
targets take the **Job path** below.

**Cluster Cypher restore** (`startClusterCypherRestore`, clusters only): no
Job, no `stopCluster`/scale-down. The controller seeds each server from a
single backup-file URI — `RecreateDatabaseWithSeedURI` when the database
already exists, `CreateDatabaseWithSeedURIOptions` when it doesn't. Because
`dbms.recreateDatabase` is async, the controller then polls
`SHOW DATABASE` online-state across requeues via `pollClusterRestoreOnline`
before marking the restore `Completed`.

**Standalone restore lifecycle** (Job path, post-Job-success):

1. **Restore Job** — `neo4j-admin database restore` runs in a Pod that mounts the server-0 data PVC (`neo4j-data-{name}-0` for standalone, `data-{name}-server-0` for the synthetic cluster representation — the only PVC the operator writes restored data to). `--from-path` is resolved at Pod startup via shell substitution `$(ls /backup/<run>/<dbname>-*.backup | tail -1)` (`tail -1` picks the LATEST timestamped artifact) so a single artifact file is passed even when the directory holds multiple `.backup` files. Both the path and database name go through `shellQuote()`. `--temp-path=/tmp/restore-tmp` is defaulted for PVC sources because the backup PVC is mounted ReadOnly.

2. **Scale-up** — `startCluster` reads the `neo4j.neo4j.com/original-replicas` annotation off the StatefulSet and scales it back. The annotation is deleted on first successful scale-up; the second concurrent reconcile finds it missing and treats this as idempotent success (avoids a regression where a race terminal-failed the restore on a benign empty-annotation).

3. **Wait for Ready** — `waitForClusterReady` polls status until `Phase=Ready`.

4. **Database bring-up** — `createOrStartDatabase` issues `CREATE DATABASE` (if absent) or `START DATABASE` (if it existed and was stopped) via the Bolt client against the system DB.

5. **Multi-server re-seed** *(`recreateRestoredDatabaseOnCluster`, gated on `Topology.Servers >= 2`)* — calls `dbms.[cluster.]recreateDatabase($db, {seedingServers: [$server0Id]})` to force every server to re-sync from server-0's restored data. Server-0 is resolved via `SHOW SERVERS YIELD ... address`, matched by `cluster.Name + "-server-0"`. The procedure name is version-gated by `version.RecreateDatabaseProcedure()`:
   - **`dbms.cluster.recreateDatabase`** — SemVer 5.24+ (incl. 5.26 LTS), CalVer 2025.02–2025.03
   - **`dbms.recreateDatabase`** — CalVer 2025.04+ and 2026.x+ (the `cluster.*` form was deprecated in 2025.04)
   - **Empty string** (skipped silently) — pre-5.24 SemVer / pre-2025.02 CalVer

   The step is non-fatal — restore still transitions to `Completed` if the procedure fails, but emits a Warning event. In the Job path a standalone target has `Topology.Servers=1`, so this step is a no-op there; it survives for the synthetic-cluster representation only.

Standalone targets are auto-detected: `getClusterRef` accepts a standalone name and returns a synthetic `Neo4jEnterpriseCluster` via `standaloneAsCluster` (with `Spec.Topology.Servers=1`) so every downstream builder (command, env vars, volumes) works without a separate code path.

**Standalone backup**: `neo4jbackup_controller.go::isStandaloneTarget` detects when `spec.target` references a `Neo4jEnterpriseStandalone`. The `--from` address resolution switches from `BuildBackupFromAddresses` (cluster: comma-separated FQDNs) to `BuildStandaloneBackupFromAddress` (single FQDN). The rest of the backup flow — Job spec, PVC/cloud storage, shared `<base>/<chain-root>/` directory, history population — is identical.

**Integration test coverage caveat**: the `test/integration` suite covers backup and restore of clusters only. Standalone backup/restore is exercised by unit tests in `internal/controller/*_test.go` and manual smoke tests. End-to-end Ginkgo coverage for the standalone path is a known follow-up — the code paths are the same so failures would surface in the cluster specs, but a dedicated standalone round-trip would harden the contract.

**Race-tolerance**: AlreadyExists on Job creation is treated as "another reconcile got there first" rather than terminal-failure. Concurrent reconciles during the stopCluster cycle (10s scale-down delay queues a fresh reconcile before the original finishes) are common; without this tolerance the loser would flip the restore to `Failed` and the "Restore previously failed" guard would pin it permanently.

#### Neo4jUser / Neo4jRole / Neo4jRoleBinding Controllers (`neo4juser_controller.go`, `neo4jrole_controller.go`, `neo4jrolebinding_controller.go`)
**Identity & access management:**

- **Shared cluster resolver**: `cluster_resolver.go` — `ResolveClusterRef` handles cluster + standalone lookup
- **Watches**: the user controller watches `Neo4jRole` so users referencing missing custom roles re-reconcile when the role lands (sets `PendingDependencies` condition meanwhile, not terminal-fail)
- **Source-of-truth model**: Privileges live on `Neo4jRole` (never on users). Built-in roles require `adoptBuiltin: true` to be managed; never dropped on CR delete
- **`Neo4jRoleBinding` is non-destructive**: never creates or drops users — only grants/revokes role assignments on externally-provisioned (SSO / LDAP) users

#### Neo4jAuthRule Controller (`neo4jauthrule_controller.go`)
**ABAC / attribute-based access control:**

- **Version requirement**: refuses to reconcile against Neo4j versions where `SupportsAuthRules()` returns false (pre-2026.03)
- **Production wiring**: always present in `setupProductionControllers`; dev mode (`--controllers`) MUST list `authrule` or local deployments will silently accept `Neo4jAuthRule` CRs without reconciling them
- **Cypher prefix**: every AUTH RULE DDL prepends `CYPHER 25` (system DB defaults to Cypher 5 on 2026.x, which can't parse `AUTH` keywords)

#### Neo4jShardedDatabase Controller (`neo4jshardeddatabase_controller.go`)
**Property-sharding management:**

- **Version-gated**: requires Neo4j 2025.12+ images
- **Resource requirements**: 5+ servers, 4-8Gi memory per server, 2+ CPU per server

## Validation Framework (`internal/validation/`)

### Comprehensive Validation Architecture

#### Core Validators:
- **TopologyValidator** (`topology_validator.go`): Cluster topology and server count validation
- **ClusterValidator** (`cluster_validator.go`): Cluster-specific configuration validation
- **MemoryValidator** (`memory_validator.go`): Neo4j memory settings vs container limits
- **ResourceValidator** (`resource_validator.go`): CPU, memory, and storage validation
- **TLSValidator** (`tls_validator.go`): TLS/SSL configuration validation
- **TruststoreValidator** (`truststore_validator.go`): unique Secret names in `spec.trustedCASecrets` (the name doubles as the keytool alias) plus reserved-mount-path collision check for `spec.extraVolumeMounts` (`/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j/...`)
- **DatabaseValidator** (`database_validator.go`): Database creation and topology validation
- **AuthRuleValidator** (`authrule_validator.go`): Neo4jAuthRule name pattern + DDL-keyword guard on the condition expression (rejects CREATE / DROP / ALTER / GRANT / DENY / REVOKE / SHOW / RENAME and `;` injection)
- **RoleValidator / UserValidator / RoleBindingValidator** (`role_validator.go`, `user_validator.go`, `rolebinding_validator.go`): privilege list, identifier rules, cross-CR overlap with `Neo4jUser`

#### Enhanced Validation Features:
- **Dual CRD Validation**: Separate validation rules for cluster vs standalone
- **Server-Based Topology**: Validates server counts instead of primary/secondary counts
- **Resource Recommendations**: Suggests optimal resource allocation
- **Configuration Restrictions**: Prevents clustering settings in standalone deployments
- **Neo4j Version Compatibility**: Validates settings against Neo4j 5.26+ and 2025.x

### Database Validator Enhancements
- **Automatic Deployment Detection**: Tries cluster first, then standalone
- **Appropriate Client Creation**: Uses correct client type for deployment
- **Clear Error Messages**: Distinguishes between cluster and standalone validation failures

## Neo4j Version Compatibility

### Supported Versions
- **Neo4j 5.26.x**: Last semver LTS release (5.26.0, 5.26.1, etc.) — no 5.27+ semver versions exist
- **Neo4j 2025.x+**: Calver format (2025.01.0, 2025.02.0, etc.)

### Version-Specific Configuration

#### Discovery Configuration (LIST resolver, injected by startup script):

| Setting | 5.26.x (SemVer) | 2025.x+ / 2026.x+ (CalVer) |
|---|---|---|
| `dbms.cluster.discovery.resolver_type` | `LIST` | `LIST` |
| `dbms.cluster.discovery.version` | `V2_ONLY` (explicit) | *(omitted — V2 is only protocol)* |
| Endpoints key | `dbms.cluster.discovery.v2.endpoints` | `dbms.cluster.endpoints` |
| Endpoint port | **6000** (tcp-tx) | **6000** (tcp-tx) |
| Bootstrap hint | `internal.dbms.cluster.discovery.system_bootstrapping_strategy=me/other` | *(not used)* |

Port 5000 (`tcp-discovery`) is the **deprecated V1 discovery port — never used by this operator**.
CalVer detection: `ParseVersion()` → `IsCalver` (`major >= 2025`) covers 2026.x+ automatically.

#### Modern Configuration Standards:
- **Memory**: `server.memory.*` (not deprecated `dbms.memory.*`)
- **TLS/SSL**: `server.https.*` and `server.bolt.*` (not `dbms.connector.*`)
- **Database Format**: `db.format: "block"` (not deprecated formats)
- **Discovery**: managed entirely by operator startup script — do not set in `spec.config`

#### Version-gated Cypher procedures + flags:

| Feature | First available |
|---|---|
| `dbms.cluster.recreateDatabase` (post-restore re-seed) | SemVer 5.24+ (incl. 5.26 LTS), CalVer 2025.02–2025.03 |
| `dbms.recreateDatabase` (replaces `cluster.*`) | CalVer 2025.04+ and 2026.x+ |
| `AUTH RULE` (ABAC) | CalVer 2026.03+ |
| Backup `--parallel-download`, `--skip-recovery` | CalVer 2025.11+ |
| Backup `--remote-address-resolution` | CalVer 2025.09+ |
| Backup `--prefer-diff-as-parent` | CalVer 2025.04+ |
| Restore `--source-database` filter | CalVer 2025.02+ |

Picked at runtime by helpers in `internal/neo4j/version.go` (`SupportsParallelDownload`, `SupportsAuthRules`, `RecreateDatabaseProcedure`, etc.). Code paths that depend on these helpers MUST fall back gracefully when the version doesn't support the feature — never hard-fail a reconcile.

### Database Creation Syntax

#### Neo4j 5.26+ (Cypher 5):
```cypher
CREATE DATABASE name [IF NOT EXISTS]
[TOPOLOGY n PRIMAR{Y|IES} [m SECONDAR{Y|IES}]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]
```

#### Neo4j 2025.x (Cypher 25):
```cypher
CREATE DATABASE name [IF NOT EXISTS]
[[SET] DEFAULT LANGUAGE CYPHER {5|25}]
[[SET] TOPOLOGY n PRIMARIES [m SECONDARIES]]
[OPTIONS "{" option: value[, ...] "}"]
[WAIT [n [SEC[OND[S]]]]|NOWAIT]
```

## Resource Management Architecture

### Intelligent Resource Handling

#### Resource Builders (`internal/resources/`):
These are free `Build*ForEnterprise` / `Build*ForStandalone` functions, not stateful builder types. The standalone path reuses the `cluster.go` builders (StatefulSet, ConfigMap, Services) via the `standaloneAsCluster` synthetic representation rather than a dedicated `standalone.go` file.

- **StatefulSet / Services / ConfigMap** (`cluster.go`): `BuildServerStatefulSetForEnterprise`, `BuildClientServiceForEnterprise`, `BuildDiscoveryServiceForEnterprise`, `BuildConfigMapForEnterprise` — server-based resources for both clusters and (via the synthetic cluster) standalone.
- **NetworkPolicy / Route / MCP** (`networkpolicy.go`, `route.go`, `mcp.go`): include `*ForStandalone` variants for standalone-specific wiring.
- **Backup Job**: built inline by `neo4jbackup_controller.go` (per-`Neo4jBackup`-CR Kubernetes Job — no persistent backup pod, no sidecars, no dedicated `resources/` builder).
- **TruststoreBuilder** (`cluster.go:BuildTrustStoreInitContainer / BuildTrustStoreVolumes / CollectTrustedCASecrets`): emits the per-Secret volume mounts, the writable `/truststore` EmptyDir, and the `truststore-init` init container (seeds `/truststore/truststore.jks` from `$JAVA_HOME/lib/security/cacerts`, then `keytool -import` for each `spec.trustedCASecrets` entry using the Secret name as alias). Reused by both cluster (env var) and standalone (ConfigMap) wire-up paths via `CollectTrustedCASecrets`, which folds the legacy singular `spec.auth.trustStore` into the new plural list.

#### Server-Based Resource Patterns:
- **StatefulSet Naming**: `{cluster-name}-server` for clusters, `{standalone-name}` for standalone
- **Pod Naming**: `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc.
- **Service Names**: `{cluster-name}-client`, `{cluster-name}-discovery`
- **Backup Resources**: per-CR Kubernetes Jobs spawned by the `Neo4jBackup` controller (one-shot: `<cr-name>-backup`; CronJob children: `<cr-name>-<unix-seconds>`). No persistent backup pod or sidecars.
- **Truststore mount**: `/truststore/truststore.jks` (read-only, populated by the `truststore-init` init container; password is the JVM default `changeit`)
- **User-supplied volumes**: `spec.extraVolumes` are appended to the pod spec verbatim; `spec.extraVolumeMounts` are appended to the Neo4j container's mounts after operator-managed mounts. Operator-managed paths (`/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j/...`) are off-limits and rejected by the validator.

### Performance Optimizations

#### Reconciliation Efficiency:
- **Rate Limiting**: Intelligent rate limiting prevents API server overload
- **Status Update Efficiency**: Only updates when state actually changes
- **Event Filtering**: Reduces unnecessary reconciliation triggers
- **ConfigMap Hashing**: Hash-based change detection prevents unnecessary updates

#### Startup Optimization:
- **Parallel Pod Management**: All server pods start simultaneously
- **`minimum_initial_system_primaries_count = TOTAL_SERVERS`**: Set only on initial cluster formation (when `/data/databases/system` doesn't exist). Forces RAFT to wait until every configured server is visible before electing a bootstrap leader — eliminates the split-brain window when multiple pods come up in parallel. Skipped on restart so a single server can rejoin without waiting on its peers.
- **PublishNotReadyAddresses**: Discovery includes pending pods
- **Resource Version Conflict Retry**: Handles concurrent updates gracefully

## Security Architecture

### RBAC Configuration (`config/rbac/`)

#### Core RBAC Resources:
- **Principle of Least Privilege**: Minimal required permissions
- **ClusterRole Design**: Cross-namespace operations support
- **Service Account Security**: Dedicated accounts with specific roles

#### Discovery RBAC (Critical):
Each cluster gets automatic RBAC creation:

- **ServiceAccount**: `{cluster-name}-discovery`
- **Role**: Services and endpoints permissions
- **RoleBinding**: Links account to role
- **Endpoints Permission**: **CRITICAL** for cluster formation

### TLS/SSL Support

The operator integrates with [cert-manager](https://cert-manager.io/) for the
full certificate lifecycle. The flow is the same for clusters and standalones,
with one structural difference noted at the end.

#### 1. Activation

TLS is opt-in via `spec.tls`:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```

Validation (`internal/validation/tls_validator.go`) runs inline during
reconciliation — no admission webhooks are used (CLAUDE.md "NO WEBHOOKS" hard constraint). When
`mode: disabled`, the entire TLS path is skipped and the deployment uses plain
`bolt://` and `http://`.

#### 2. Certificate creation

`BuildCertificateForEnterprise` (`internal/resources/cluster.go`) emits a
`cert-manager.io/v1` `Certificate` whose `dnsNames` cover every endpoint clients
or peers may connect to:

- The headless discovery service (`{cluster}-discovery`)
- The client service (`{cluster}-client`)
- Each individual server pod FQDN (`{cluster}-server-0.{cluster}-discovery.{ns}.svc.cluster.local`, …)
- LoadBalancer hostnames where applicable

The `Certificate` references the user-supplied `issuerRef` and writes its
material into a Secret named `{resource-name}-tls-secret` (`tls.crt`, `tls.key`,
`ca.crt`). cert-manager owns rotation; the operator never touches expiry.

#### 3. Mounting into Neo4j pods

The StatefulSet builder mounts the Secret read-only at `/ssl/`
(`internal/resources/cluster.go:~1208`). Neo4j is then pointed at this directory
via `server.directories.certificates=/ssl` along with three SSL policies that
share the same key/cert/CA bundle:

- `dbms.ssl.policy.bolt.*` — client traffic (`bolt+s://`)
- `dbms.ssl.policy.https.*` — Browser/HTTP API
- `dbms.ssl.policy.cluster.*` — RAFT and discovery between server pods

The cluster SSL policy posture is governed by `spec.tls.strictPeerValidation`
(default `true`):

- **Strict (default)** — emits `trust_all=false`, `client_auth=REQUIRE`
  (mutual TLS), and `verify_hostname=true`. The cert-manager Secret is
  projected via `Secret.items[]` so its `ca.crt` lands at
  `/ssl/trusted/ca.crt`, which is the directory Neo4j's cluster SSL policy
  reads when `trust_all=false`. Matches Neo4j's canonical SSL framework
  guidance.
- **Legacy (`strictPeerValidation: false`)** — reverts to `trust_all=true`
  + `client_auth=NONE`. Provided as an opt-out for installations whose
  external issuer does not populate `ca.crt`. Neo4j's own docs call this
  posture "debugging only, since it does not offer security." The
  controller-side preflight `verifyTLSSecretHasCA` refuses the strict
  config when `ca.crt` is missing and surfaces a clear status message
  rather than silently rolling the STS into a no-trust-anchor strict
  configuration.

When TLS is enabled, `server.bolt.tls_level=REQUIRED` is also set — plain
`bolt://` connections are rejected (regression checklist items #9–#13, TLS / Bolt client).

#### 4. Operator-side Bolt connection (outgoing)

The operator reconciles the cluster by issuing Cypher commands over Bolt.
Two separate concerns layer here: which scheme the URI uses (routing vs
direct) and which CA the client trusts.

**URI scheme — routing vs direct.** `buildConnectionURIForEnterprise`
(`internal/neo4j/client.go`) uses the **routing scheme**:

| Spec | Scheme |
|---|---|
| `spec.tls.mode: disabled` (or unset) | `neo4j://` |
| `spec.tls.mode: cert-manager` | `neo4j+s://` |

The routing scheme is mandatory. Cluster admin commands (CREATE/DROP USER,
GRANT/REVOKE, CREATE/ALTER/DROP DATABASE, AUTH RULE management, etc.) must
execute on the cluster leader; the Go driver routes write transactions to
the leader **only under `neo4j://`**. Under the direct `bolt://` scheme,
`AccessMode: AccessModeWrite` is silently ignored and connections land
wherever K8s steered them via the `{cluster}-client` ClusterIP. The
operator's Bolt clients used to use `bolt://`, which produced
`Neo.ClientError.Cluster.NotALeader` on N-1 of every N reconciles and
visible Ready ↔ Failed status flicker on the role/user/auth-rule
controllers. See checklist item #11.

The **single legitimate `bolt://` consumer** is
`internal/controller/splitbrain_detector.go:createPodSpecificNeo4jClient`,
which intentionally bypasses the routing layer to query each pod's RAFT
view individually — the whole point of split-brain detection is to compare
per-pod state, not to talk to the leader. Standalone deployments use the
routing scheme too, for symmetry; on a single-member topology
`getRoutingTable` reports the lone member as both reader and writer, so
behavior is equivalent to direct connection.

**Driver timeouts.** `NewClientForEnterprise` /
`NewClientForEnterpriseStandalone` configure:

- `ConnectionAcquisitionTimeout = 10s` — full budget for getting a
  connection (includes routing-table fetch retries under `neo4j://`)
- `SocketConnectTimeout = 5s` — TCP connect to a router member
- `MaxTransactionRetryTime = 15s` — retry budget for transient errors

These values are deliberately tight: an unreachable cluster fails fast
instead of stalling the controller's reconcile queue behind hung Bolt
calls. Healthy clusters complete the routing handshake in well under one
second. See checklist item #12.

**TLS.** `buildTLSConfig` (`internal/neo4j/client.go`) governs which CA the
client trusts:

1. **Auto-discovery**: load `ca.crt` from the `{resource-name}-tls-secret`
   Secret and pin it as the trusted CA for outgoing connections. This is
   the default path — no user configuration required.
2. **Override**: `spec.tls.trustedCASecret` lets users point at a different
   Secret (e.g. when bringing their own CA outside cert-manager).
3. **Fallback**: `InsecureSkipVerify` is used only during the brief window
   before the Secret has been populated by cert-manager (regression
   checklist item #9).

All three Bolt entry points — `NewClientForEnterprise`,
`NewClientForEnterpriseStandalone`, and `NewClientForPod` (split-brain
detector) — go through `buildTLSConfig`, so the scheme switches
dynamically between TLS-enabled and plain variants based on `spec.tls.mode`
(checklist item #10).

#### 5. Standalone differences

`Neo4jEnterpriseStandalone` follows the same flow with two structural
differences:

- A single pod, so `dnsNames` is shorter (one server FQDN + the client service).
- Neo4j configuration is delivered via a ConfigMap rather than StatefulSet env
  vars; the `health.sh` probe (mounted alongside `neo4j.conf` with mode `0755`)
  also lives in this ConfigMap (checklist item #6).

#### 6. Outbound trust — `spec.trustedCASecrets` & `spec.extraVolumes`

The cert-manager flow above governs Neo4j-the-server's *inbound* TLS (Bolt,
HTTPS, intra-cluster RAFT). Neo4j also makes *outbound* TLS calls — to OIDC
providers, LDAPS servers, Aura Fleet Management, plugin download mirrors, and
in some cluster topologies to peer clusters for replication. When those
endpoints use a CA the JDK doesn't trust by default, the operator wires a
custom JVM truststore.

**Sources of truth in code:**

| Concern | Source |
|---|---|
| API types | `api/v1beta1/neo4jenterprisecluster_types.go:TrustedCASecret`, plus `Neo4jEnterpriseClusterSpec.TrustedCASecrets / ExtraVolumes / ExtraVolumeMounts` (mirrored on standalone) |
| Validation | `internal/validation/truststore_validator.go` (unique Secret names, reserved-mount-path collision check) |
| Init container + volumes | `internal/resources/cluster.go:BuildTrustStoreInitContainer / BuildTrustStoreVolumes / CollectTrustedCASecrets` |
| JVM-additional wire-up (cluster) | `internal/resources/cluster.go` — env var `NEO4J_server_jvm_additional` |
| JVM-additional wire-up (standalone) | `internal/controller/neo4jenterprisestandalone_controller.go:createConfigMap` — written as `server.jvm.additional=...` lines in the ConfigMap-backed neo4j.conf |

**Init container flow** (one container, runs before Neo4j, image is the same
Neo4j image so `keytool` is guaranteed to be present):

1. `cp $JAVA_HOME/lib/security/cacerts /truststore/truststore.jks` — seeds
   the writable JKS with the JDK's default trust roots so public CAs (Let's
   Encrypt, DigiCert, etc.) keep working. Without this seed step Neo4j would
   *lose* trust in public infrastructure when any custom CA was added.
2. For each `TrustedCASecret`: `keytool -import -trustcacerts -alias <secret-name>
   -file /trusted-ca/<secret-name>/<key>` (default key `ca.crt` matches the
   layout of cert-manager-issued Secrets).
3. The resulting JKS is mounted read-only at `/truststore/truststore.jks`
   into the main Neo4j container.

**JVM args**: `-Djavax.net.ssl.trustStore=/truststore/truststore.jks
-Djavax.net.ssl.trustStorePassword=changeit` — appended to whatever the user
supplied via `spec.config["server.jvm.additional"]`. Cluster pods receive
these via the `NEO4J_server_jvm_additional` env var; standalone pods receive
them as `server.jvm.additional=...` lines written into the ConfigMap-backed
neo4j.conf.

**Backward compatibility**: the older singular `spec.auth.trustStore`
(`*SecretKeyRef`) is folded into the new list at reconcile time via
`CollectTrustedCASecrets`. Both paths produce the same volumes, init
container, and JVM flags. Names from the explicit `trustedCASecrets` list
win on duplication.

**Reserved paths for `ExtraVolumeMounts`**: `/data`, `/logs`, `/conf`,
`/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j` (and
its `data/`, `logs/`, `conf/`, `plugins/`, `certificates/` subdirectories)
are all rejected by the validator — silently overlaying them would either
destroy operator-managed content or fight the truststore-init flow.

**Why this is needed for ABAC**: Neo4j 2026.04 hard-requires `https://` for
every `dbms.security.oidc.<name>.*` URI and rejects `http://` at config-parse
time, before boot. Test environments and self-hosted OIDC providers therefore
need a TLS-fronted stub plus a trusted CA — `trustedCASecrets` is the
ergonomic path; `extraVolumes` is the escape hatch when a Neo4j SSL policy
references a per-policy `truststore_path`.

#### 7. Adjacent integrations

- **ExternalSecrets**: when `spec.auth.adminSecret` references a Secret managed
  by ExternalSecrets, the operator resolves it identically — TLS material
  remains under cert-manager's control regardless.
- **`Neo4jAuthRule` (ABAC) and OIDC trust**: the auth-rule reconciler talks to
  Neo4j via Bolt and does not directly interact with the JVM truststore.
  However, the cluster *itself* needs trust to fetch the OIDC well-known
  document — that's what `trustedCASecrets` configures.

#### TLS/SSL quick reference

| Concern | Source |
|---|---|
| Validation | `internal/validation/tls_validator.go`, `internal/validation/truststore_validator.go` |
| Certificate CR shape | `internal/resources/cluster.go:BuildCertificateForEnterprise` |
| `/ssl/` mount + SSL policies | `internal/resources/cluster.go` (~line 1208, ~line 1594) |
| Operator outgoing Bolt TLS | `internal/neo4j/client.go:buildTLSConfig` |
| Neo4j-server outgoing TLS truststore | `internal/resources/cluster.go:BuildTrustStoreInitContainer` (init container) + `NEO4J_server_jvm_additional` env var |
| `spec.trustedCASecrets` API | `api/v1beta1/neo4jenterprisecluster_types.go:TrustedCASecret` |
| `spec.extraVolumes` / `spec.extraVolumeMounts` API | same file, on the cluster + standalone specs |
| Regression invariants | CLAUDE.md checklist items #6, #9, #10, #11, #12, #13 |

## Monitoring & Observability

### Resource Monitoring (`internal/monitoring/`):
- **ResourceMonitor** (`resource_monitor.go`): Real-time utilization tracking
- **Performance Metrics**: Controller performance and reconciliation efficiency
- **Operational Insights**: ConfigMap update patterns and debounce effectiveness

### Status Management:
- **Enhanced Status Updates**: Detailed cluster state tracking
- **Condition Management**: Comprehensive status conditions with proper transitions
- **Event Recording**: Structured events for debugging and monitoring
- **Connection Examples**: Automatic generation of connection strings

### Monitoring and Live Diagnostics

The `MonitoringSpec` field (`spec.monitoring`) drives two distinct
responsibilities inside the cluster controller:

**1. Infrastructure setup** (`ReconcileMonitoring`):
Creates Kubernetes resources for metrics collection:

- `{cluster-name}-metrics` Service — exposes port 2004 for Prometheus scraping
- `{cluster-name}-monitoring` ServiceMonitor — tells the Prometheus Operator to scrape the metrics service
- Neo4j config flags (`server.metrics.prometheus.enabled=true`, `prometheus.io/*` annotations)

Runs on every reconcile regardless of cluster phase.

**2. Live diagnostics** (`CollectDiagnostics`):
Runs `SHOW SERVERS` and `SHOW DATABASES` via the Bolt client when the cluster is `Ready`:

- Writes results to `status.diagnostics` (`ClusterDiagnosticsStatus`)
- Sets `ServersHealthy` condition (`True` when all servers are `state=Enabled` and `health=Available`)
- Sets `DatabasesHealthy` condition (`True` when all user databases have `status=online`; the `system` database is excluded)
- Updates `neo4j_operator_server_health` Prometheus gauge per server (labels: `cluster_name`, `namespace`, `server_name`, `server_address`)
- Non-fatal: collection errors are surfaced in `status.diagnostics.collectionError` and the conditions are set to `Unknown` with reason `DiagnosticsUnavailable`

The diagnostics Bolt client is created fresh per-reconcile and closed with `defer`. It
never shares state with the cluster formation or upgrade clients.

**Architecture invariant:** All status writes in `CollectDiagnostics` use
`retry.RetryOnConflict` to handle concurrent updates without panicking.

**Condition constants** (defined in `internal/controller/conditions.go`):

- `ConditionTypeServersHealthy = "ServersHealthy"`
- `ConditionTypeDatabasesHealthy = "DatabasesHealthy"`
- Reason values: `AllServersHealthy`, `ServerDegraded`, `AllDatabasesOnline`, `DatabaseOffline`, `DiagnosticsUnavailable`

## Integration Architecture

### External System Integration:
- **Cert-Manager**: TLS certificate lifecycle management
- **Prometheus**: Metrics collection and alerting
- **External Secrets**: Secret management integration
- **Storage Classes**: Persistent volume provisioning
- **Cloud Providers**: AWS, GCP, Azure LoadBalancer optimizations

### Kubernetes Integration:
- **Network Policies**: Pod-to-pod communication security
- **Service Mesh**: Istio/Linkerd compatibility
- **Ingress Controllers**: External traffic routing with connection examples
- **Node Affinity**: Topology spread and anti-affinity rules

## Testing Architecture

### Test Strategy:
- **Unit Tests**: Controller logic and helper functions
- **Integration Tests**: Full workflow testing with envtest
- **End-to-End Tests**: Real cluster testing with Kind
- **Performance Tests**: Reconciliation efficiency validation

### Test Infrastructure:
- **Ginkgo/Gomega**: BDD-style testing framework
- **Envtest**: Kubernetes API server for integration testing
- **Kind Clusters**: Development and test cluster automation
- **Test Cleanup**: Automatic finalizer removal and namespace cleanup

## Migration & Compatibility

### Legacy Architecture Migration:
- **Backward Compatibility**: Existing clusters continue to work
- **Gradual Migration**: No breaking changes for existing deployments
- **Resource Name Updates**: New deployments use server-based naming
- **Configuration Migration**: Automatic handling of deprecated settings

### Future Extensibility:
- **Plugin System**: Neo4j plugin management framework
- **Custom Metrics**: Extensible monitoring capabilities
- **Event Handling**: Pluggable event system for custom integrations
- **Multi-Architecture**: Support for different deployment patterns

## Development Best Practices

### Code Organization:
- **Controller Pattern**: Standard Kubernetes controller pattern
- **Builder Pattern**: Resource builders for clean separation
- **Validation Framework**: Centralized validation with clear error messages
- **Testing Strategy**: Comprehensive test coverage with multiple levels

### Performance Considerations:
- **Memory Usage**: Optimized for large-scale deployments
- **API Efficiency**: Minimal API calls with intelligent caching
- **Resource Creation**: Parallel resource creation where possible
- **Error Handling**: Graceful error handling with proper recovery

This architecture provides a solid foundation for managing Neo4j Enterprise deployments in Kubernetes with high performance, reliability, and operational simplicity.
