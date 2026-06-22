# Operations Knowledge Base — Runtime Invariants (rules 1–39)

> One home, no duplication. This file re-homes the non-backup half of the legacy
> CLAUDE.md regression checklist (rules 1–39). Backup/restore/sharding rules
> (40–79) live in their own knowledge file. CLAUDE.md now points here instead of
> carrying the checklist.
>
> **`id`** keeps the original CLAUDE.md rule number (stable cross-reference — do
> not renumber). **`scope`** is the verified file(s) the invariant lives in.
> **`pinned-by`** names a test that fails if the invariant regresses; every test
> named below was grep-verified to exist in the current tree on branch
> `fix/wire-plugin-validator-164`. **`enforcement`** says how a violation is
> caught (unit test / integration test / validator / code review).
>
> The 5 project-wide hard invariants (NO webhooks, KIND only, ENTERPRISE images
> only, V2_ONLY discovery, server-based architecture with Job-per-CR backups)
> sit above this file in AGENTS.md — they are not repeated here.

## Standalone

### id 1 — Ready gate before database ops
- **scope:** `internal/controller/neo4jenterprisestandalone_controller.go`
- **rule:** A standalone instance must reach `status.phase="Ready"` before any database operation (CREATE DATABASE, user/role ops, etc.) runs against it.
- **why:** Bolt/Cypher against a not-yet-bootstrapped instance fails or races the password/auth setup; gating on Ready makes downstream ops deterministic.
- **pinned-by:** standalone controller integration specs in `internal/controller/neo4jenterprisestandalone_controller_test.go` (Ready-phase gating).
- **enforcement:** integration test + code review.

### id 2 — Backup uses `--to-path` (5.26+ syntax)
- **scope:** `internal/controller/neo4jbackup_controller.go` (`buildToPath`, lines ~1334/1501/1585) — NOTE: this is the unified Neo4jBackup (Job-per-CR) path, not standalone-controller-local logic. See FLAG in notes.
- **rule:** `neo4j-admin database backup` uses `--to-path=<base>/<chain-root>/`; never the deprecated 4.x `--backup-dir`. All runs of one Neo4jBackup CR share a single `--to-path` directory (DIFF chaining) — never per-run subfolders.
- **why:** 5.26+ neo4j-admin dropped the old backup flags; DIFF chaining requires the prior FULL to sit in the same directory.
- **pinned-by:** `TestBackupRunIDEnvVar`, `TestJobToBackupRun` (backup reconciler unit tests).
- **enforcement:** unit test.

### id 3 — Always stamp `ObservedGeneration`
- **scope:** `internal/controller/neo4jenterprisecluster_controller.go` (~L1414), `internal/controller/neo4jenterprisestandalone_controller.go` (~L1818, L1885)
- **rule:** Set `status.observedGeneration = latest.Generation` on every status update, in BOTH controllers.
- **why:** Without it, `kubectl wait`/clients can't tell whether the operator has observed the latest spec; stale-generation status is indistinguishable from up-to-date.
- **pinned-by:** controller reconcile integration specs assert ObservedGeneration tracks Generation.
- **enforcement:** integration test + code review.

### id 4 — Name-length validation
- **scope:** `internal/validation/cluster_validator.go` (`maxClusterNameLength = 56`, ~L140), `internal/validation/database_validator.go` (`maxDatabaseNameLength = 65`, regex `neo4jDatabaseNamePattern = ^[a-zA-Z][a-zA-Z0-9.\-]*$`)
- **rule:** Cluster name ≤ 56 chars (DNS-label 63 minus the `-server` suffix); standalone ≤ 63; database name ≤ 65 and must match `^[a-zA-Z][a-zA-Z0-9.\-]*$`.
- **why:** Generated resources append suffixes (`-server`, etc.); exceeding the K8s DNS-label limit breaks StatefulSet/Service creation. Database names beyond Neo4j's limit are rejected by the DB engine.
- **pinned-by:** `cluster_validator_test.go` ("no more than 56 characters"), `database_validator_test.go` ("no more than 65").
- **enforcement:** validator (inline in reconciler) + unit test.

### id 5 — Standalone `UpgradeStrategy`
- **scope:** `api/v1beta1/neo4jenterprisestandalone_types.go` (`UpgradeStrategy *UpgradeStrategySpec`), `UpgradeStrategySpec` in `api/v1beta1/neo4jenterprisecluster_types.go` (~L1155); pre-upgrade check uses `Client.VerifyConnectivity` (`internal/neo4j/client.go` ~L592)
- **rule:** Standalone upgrades run a pre-upgrade health check via `VerifyConnectivity`; `autoPauseOnFailure` blocks the upgrade when the check fails; the StatefulSet update strategy comes from the spec.
- **why:** Upgrading an unhealthy single node risks unrecoverable data loss; the gate forces an operator decision before proceeding.
- **pinned-by:** standalone upgrade integration specs in `neo4jenterprisestandalone_controller_test.go`.
- **enforcement:** integration test + code review.

### id 6 — Standalone health probes via `/conf/health.sh`
- **scope:** `internal/resources/cluster.go` (`buildHealthScript`, ConfigMap key `"health.sh"` ~L646; probe Exec `/conf/health.sh` ~L2542/2561/2645; ConfigMap volume `DefaultMode: 0o755` ~L1348)
- **rule:** Readiness/liveness/startup probes shell out to `/conf/health.sh` (checks both the Neo4j process and HTTP 7474). The ConfigMap carries `health.sh` alongside `neo4j.conf` with `DefaultMode: 0755` so it's executable.
- **why:** A bare TCP probe can't tell "process up" from "DB serving"; the script verifies HTTP readiness. 0755 is required or the kubelet can't exec the mounted script.
- **pinned-by:** `internal/resources/cluster_startup_test.go` (asserts `health.sh` present and non-empty); `neo4jenterprisestandalone_controller_test.go` (probes reference `/conf/health.sh`).
- **enforcement:** unit test + integration test.

### id 7 — Validator REJECTS deprecated `spec.config` keys
- **scope:** `internal/validation/config_validator.go` (`ConfigValidator`) — wired into the **cluster** validator only (`cluster_validator.go`); the **standalone** validator has its OWN independent `validateConfig` (`standalone_validator.go`). The two are NOT shared.
- **rule:** On the **cluster** path, `ConfigValidator` rejects deprecated keys as `field.Invalid`: `dbms.logs.query.enabled` (use `db.logs.query.enabled`), `dbms.default_database` (use the `dbms.setDefaultDatabase()` procedure), and `dbms.integrations.cloud_storage.s3.region`; `db.format` is rejected as `field.Forbidden` (NOT `field.Invalid`). The **standalone** `validateConfig` independently rejects `db.format` (Forbidden), `dbms.mode`, clustering keys, SSL keys, and control chars — but does **not** reject those three deprecated cluster keys. Always use the `db.*` namespace for 5.x+.
- **why:** These keys silently no-op or fail in 5.26+; rejecting at admission time surfaces the mistake before the pod crash-loops.
- **pinned-by:** validator unit tests for deprecated-key rejection.
- **enforcement:** validator (inline) + unit test.

### id 8 — Storage expansion is orphan-delete + PVC patch
- **scope:** `internal/controller/storage_expansion.go` (~L293), `internal/controller/standalone_storage_expansion.go` (~L220); orphan-delete note in `neo4jenterprisecluster_controller.go` (~L556)
- **rule:** To grow storage: orphan-delete the StatefulSet (not a regular delete — keep the pods/PVCs), compare spec size vs ACTUAL PVC size (not old-spec vs new-spec), wrap PVC patches in `retry.RetryOnConflict`, validate the StorageClass has `allowVolumeExpansion=true` BEFORE patching, and never shrink a PVC.
- **why:** StatefulSet `volumeClaimTemplates` are immutable; orphan-delete lets you recreate the STS pointing at resized PVCs without destroying data. Patching a PVC on a non-expandable StorageClass wedges it.
- **pinned-by:** storage-expansion integration specs.
- **enforcement:** integration test + validator (`allowVolumeExpansion` precheck) + code review.

## TLS & Bolt client

### id 9 — TLS CA auto-discovery from cert-manager Secret
- **scope:** `internal/neo4j/client.go` (`buildTLSConfig`, ~L208; secret name `fmt.Sprintf("%s-tls-secret", resourceName)` ~L220)
- **rule:** `buildTLSConfig()` auto-loads the CA from the cert-manager-generated Secret named `{resourceName}-tls-secret`. `TrustedCASecret` is an explicit override; `InsecureSkipVerify` is a last-resort fallback only.
- **why:** Cluster SSL defaults to strict; the operator's own Bolt client must trust the same CA the pods present, or every reconcile connection fails handshake.
- **pinned-by:** TLS client tests in `internal/neo4j/client_test.go`.
- **enforcement:** unit test + code review.

### id 10 — Every client constructor handles TLS
- **scope:** `internal/neo4j/client.go` — `NewClientForEnterprise` (~L326), `NewClientForEnterpriseStandalone` (~L254), `NewClientForPod` (~L160) all call `buildTLSConfig()`; split-brain detector uses dynamic `bolt+s://`
- **rule:** All three client constructors MUST call `buildTLSConfig()`. The split-brain detector switches scheme to `bolt+s://` when TLS is on.
- **why:** A constructor that skips TLS config silently downgrades to plaintext or fails handshake against a strict cluster; the bug only shows under TLS.
- **pinned-by:** `internal/neo4j/client_test.go` (per-constructor TLS coverage).
- **enforcement:** unit test + code review.

### id 11 — Outbound Bolt URI uses the ROUTING scheme
- **scope:** `internal/neo4j/client.go` (URI builders); only legitimate plain `bolt://` user is `internal/controller/splitbrain_detector.go`
- **rule:** The operator's outbound URI is `neo4j://` / `neo4j+s://` (routing), never `bolt://`. The Go driver only honors `AccessModeWrite` under routing; plain `bolt://` lands wherever the ClusterIP steers it → `Neo.ClientError.Cluster.NotALeader`. The split-brain detector is the ONLY component allowed to use `bolt://` (it must target a specific pod).
- **why:** Writes (CREATE DATABASE, ALTER USER, GRANT …) must reach the leader; routing fetches the leader address, ClusterIP `bolt://` does not.
- **pinned-by:** `internal/neo4j/uri_test.go` — `TestBuildConnectionURIForEnterprise`, `TestBuildConnectionURIForStandalone`.
- **enforcement:** unit test + code review. (See FLAG in notes — CLAUDE.md cites the file, not these exact test names.)

### id 12 — Tight Bolt driver timeouts on the cluster path
- **scope:** `internal/neo4j/client.go` — `NewClientForEnterprise` (cluster): `ConnectionAcquisitionTimeout=10s`, `SocketConnectTimeout=5s`, `MaxTransactionRetryTime=15s` (~L349-355). `NewClientForPod` also uses 10s/5s (~L173-174). NOTE: `NewClientForEnterpriseStandalone` deliberately uses larger 30s/15s/30s (~L271-277) for startup tolerance.
- **rule:** Keep the cluster/pod client at 10s/5s/15s. Under routing these gate routing-table-fetch retries against an unreachable cluster; bumping to 30s+ stalls the reconcile work queue.
- **why:** A slow timeout multiplied by routing retries blocks the controller worker for minutes, starving every other CR.
- **pinned-by:** client timeout assertions in `internal/neo4j/client_test.go`.
- **enforcement:** unit test + code review. (See FLAG in notes — the 10s/5s/15s figures are the CLUSTER path; standalone is intentionally different.)

### id 13 — TLS Secret volume `DefaultMode=0440`
- **scope:** `internal/resources/cluster.go` (TLS volume `DefaultMode: 0o440` ~L1399)
- **rule:** The projected TLS Secret volume uses `DefaultMode=0440` (owner+group read). Neo4j runs as UID/GID 7474 with `FSGroup=7474`.
- **why:** The private key must be readable by the neo4j group but not world-readable; 0440 satisfies both Neo4j's permission check and least-privilege.
- **pinned-by:** `TestBuildStatefulSet_TLSVolumeDefaultMode0440` (`internal/resources/cluster_tls_test.go`).
- **enforcement:** unit test.

## Users / Roles / Privileges

### id 14 — `GetUserRoles` is buggy — do not use
- **scope:** `internal/neo4j/client.go` (`GetUserRoles` ~L2139 — buggy); use `internal/neo4j/users.go` `ListUserRoles` (~L612) or `ShowUser` (~L80)
- **rule:** `GetUserRoles` queries `SHOW USER PRIVILEGES YIELD role` and returns one row per privilege (duplicated/wrong). Use `Client.ListUserRoles` or `Client.ShowUser` instead.
- **why:** Privilege-row count ≠ role count; using `GetUserRoles` over-reports roles and breaks drift reconciliation.
- **pinned-by:** `test/integration/neo4juser_test.go` (user controller role-sync specs exercise `ListUserRoles`/`ShowUser` indirectly). NOTE: the `internal/neo4j` user helpers have **no direct unit test** — known gap; do not cite a non-existent `users_test.go`.
- **enforcement:** integration test (indirect) + code review (the buggy `GetUserRoles` is retained but must not gain callers).

### id 15 — Password rotation via Secret hash
- **scope:** `internal/controller/neo4juser_controller.go`; `Neo4jUser.status.passwordSecretHash`
- **rule:** `status.passwordSecretHash` stores the SHA-256 of the referenced Secret value; rotation is detected on hash change. The password is never persisted in a CR field. Skip `SET PASSWORD` entirely when only `externalAuth` is configured.
- **why:** Storing a hash (not the password) lets the controller detect rotation without leaking the secret into etcd or status.
- **pinned-by:** user controller integration specs (rotation-on-hash-change).
- **enforcement:** integration test + code review.

### id 16 — `ALTER USER` clause ordering (REMOVE before SET)
- **scope:** `internal/neo4j/users.go` (`AlterUserOptions` builder ~L166)
- **rule:** On a single `ALTER USER` statement, REMOVE clauses MUST precede SET clauses. Use the `AlterUserOptions` builder — never hand-roll ALTER USER strings.
- **why:** Neo4j rejects SET-before-REMOVE ordering on a compound ALTER; the builder enforces correct ordering centrally.
- **pinned-by:** `test/integration/neo4juser_test.go` (ALTER USER exercised via user-update specs). NOTE: the `AlterUserOptions` builder has **no direct unit test** — known gap worth closing.
- **enforcement:** integration test (indirect) + code review.

### id 17 — Missing custom roles do NOT fail reconcile
- **scope:** `internal/controller/neo4juser_controller.go` (`ConditionTypePendingDependencies` / `ConditionReasonRolesPending` set ~L254; watch on `Neo4jRole` in `SetupWithManager` ~L615)
- **rule:** A referenced custom role that doesn't exist yet must NOT fail the user reconcile. Set the `PendingDependencies` condition and requeue; the user controller watches `Neo4jRole` so the user re-reconciles when the role lands.
- **why:** CRs are applied in arbitrary order; failing hard on a not-yet-created role would wedge legitimate apply-everything-at-once workflows.
- **pinned-by:** user controller integration specs (pending-dependency + watch re-reconcile).
- **enforcement:** integration test + code review. Condition constants in `internal/controller/events.go` (`ConditionTypePendingDependencies` L170, `ConditionReasonRolesPending` L178).

### id 18 — Same-namespace `clusterRef` only
- **scope:** `internal/controller/cluster_resolver.go`; user/role validators in `internal/validation/`
- **rule:** `clusterRef` for users/roles must be in the same namespace — cross-namespace refs are not supported in v1. Multi-tenant access goes through an opt-in `Neo4jClusterAccessGrant` CR.
- **why:** Cross-namespace privilege grants are a security boundary; v1 keeps the blast radius inside one namespace.
- **pinned-by:** validator unit tests (cross-namespace rejection).
- **enforcement:** validator (inline) + unit test.

### id 19 — Identifier quoting in Cypher
- **scope:** `internal/neo4j/auth_rules.go` (`escapeBackticks` ~L144), `internal/neo4j/users.go`, `internal/neo4j/privileges.go`
- **rule:** Role/user names go through `escapeBackticks()` before interpolation into Cypher identifiers. NEVER `fmt.Sprintf` user-controlled names into Cypher unescaped. Passwords and provider IDs go through driver parameters (`$param`), never string interpolation.
- **why:** Cypher identifier injection — a name with a backtick can escape the identifier and execute arbitrary Cypher with admin privileges.
- **pinned-by:** Cypher-escaping unit tests in `internal/neo4j/` (e.g. privileges/users/auth_rules tests).
- **enforcement:** unit test + code review.

### id 20 — Privilege drift via `SHOW ROLE PRIVILEGES AS COMMANDS`
- **scope:** `internal/neo4j/privileges.go` (`CanonicalisePrivilegeStatement` ~L44, `DerivePrivilegeRevoke` ~L347); `internal/controller/neo4jrole_controller.go`
- **rule:** Source of truth is `Neo4jRole.spec.privileges`. The controller canonicalises both desired and observed sides (`CanonicalisePrivilegeStatement`), diffs them as sets, and derives REVOKE statements (`DerivePrivilegeRevoke`). Immutable rows are excluded from revokes; drift is surfaced via `status.privilegeDrift`.
- **why:** Privilege statements have many textually-equivalent forms; canonicalising both sides prevents oscillation, and deriving REVOKEs (rather than trusting user input) prevents the controller from revoking immutable system privileges.
- **pinned-by:** `TestCanonicalisePrivilegeStatement`, `TestDerivePrivilegeRevoke`, `TestDerivePrivilegeRevoke_Errors`, `TestCanonicalisePrivilegeStatement_PBAC`, `TestDerivePrivilegeRevoke_PBAC` (`internal/neo4j/privileges_test.go`).
- **enforcement:** unit test.

### id 21 — Privilege statement validation
- **scope:** `internal/validation/role_validator.go`
- **rule:** Entries in `Neo4jRole.spec.privileges` MUST start with `GRANT` or `DENY` (REVOKE is rejected — the operator derives revokes) and MUST end with `TO <spec.name>`.
- **why:** REVOKE-as-input would let a role spec revoke arbitrary privileges; pinning the suffix to the role's own name prevents a role from granting privileges to a different role.
- **pinned-by:** `internal/validation/role_validator_test.go`.
- **enforcement:** validator (inline) + unit test.

### id 22 — `Neo4jRoleBinding` never creates or drops users
- **scope:** `internal/controller/neo4jrolebinding_controller.go`
- **rule:** A RoleBinding only manages role grants for externally-provisioned users (SSO/LDAP first-login). It never creates or drops a user. An absent user → `UserNotFound` and the binding waits.
- **why:** RoleBindings exist to authorize users provisioned by an external IdP; creating users would duplicate `Neo4jUser`'s job and clash on lifecycle.
- **pinned-by:** rolebinding controller integration specs (UserNotFound wait).
- **enforcement:** integration test + code review.

### id 23 — RoleBinding/User overlap rejected
- **scope:** `internal/validation/rolebinding_validator.go`
- **rule:** A `Neo4jRoleBinding` is rejected when its `clusterRef`+`username` match an existing `Neo4jUser` in the same namespace.
- **why:** Two controllers managing the same user's roles would fight; the validator forces one owner.
- **pinned-by:** `internal/validation/rolebinding_validator_test.go`.
- **enforcement:** validator (inline) + unit test.

### id 24 — `enforceExclusive` defaults to false
- **scope:** `internal/controller/neo4jrolebinding_controller.go`; `Neo4jRoleBinding.spec.enforceExclusive`
- **rule:** `enforceExclusive` defaults to false (the binding manages only `.spec.roles` + `status.grantedRoles`). `true` revokes any role on the user not listed in `.spec.roles`. Never flip the default.
- **why:** Default-exclusive would silently revoke roles granted by other tools/IdPs on first reconcile — a destructive surprise.
- **pinned-by:** rolebinding controller integration specs (exclusive vs additive).
- **enforcement:** integration test + code review.

### id 25 — Diagnostics user/role lists are capped
- **scope:** `internal/controller/diagnostics_users_roles.go` (`maxDiagnosticUsers = 50`, `maxDiagnosticRoles = 50` ~L34-35; full count in `UserCount`/`RoleCount`)
- **rule:** User/role lists surfaced in diagnostics are bounded by `maxDiagnosticUsers` / `maxDiagnosticRoles` (50 each); the full count goes in `UserCount` / `RoleCount`. Never remove the caps without a pruning strategy.
- **why:** A cluster with thousands of users would bloat the CR status object (etcd value-size limits) and slow every reconcile that writes status.
- **pinned-by:** diagnostics unit tests in `internal/controller/` (cap behavior).
- **enforcement:** unit test + code review.

## Truststore / Volumes

### id 26 — Truststore init container seeds from JDK cacerts FIRST
- **scope:** `internal/resources/cluster.go` (`BuildTrustStoreInitContainer` ~L3121; seed script copies `${JAVA_HOME}/lib/security/cacerts` → `/truststore/truststore.jks` ~L3130-3134)
- **rule:** The init container MUST copy `$JAVA_HOME/lib/security/cacerts` to `/truststore/truststore.jks` BEFORE importing user CAs. The seed makes `spec.trustedCASecrets` purely additive.
- **why:** Without seeding, the JKS contains only user CAs and Neo4j loses trust in public CAs (Let's Encrypt etc.) — egress to cloud storage / OIDC providers breaks.
- **pinned-by:** truststore init-container builder tests in `internal/resources/`.
- **enforcement:** unit test + code review.

### id 27 — `trustedCASecrets` Secret-name = keytool alias (must be unique)
- **scope:** `internal/validation/truststore_validator.go`
- **rule:** Each `spec.trustedCASecrets` Secret name is used directly as the keytool alias in the JKS, so names must be unique; the validator rejects duplicate Secret names. Keep the alias derivation statically derivable from the spec.
- **why:** keytool fails on duplicate aliases, breaking the whole truststore build; rejecting duplicates at admission gives a clear error instead of an init-container crash-loop.
- **pinned-by:** `internal/validation/truststore_validator_test.go`.
- **enforcement:** validator (inline) + unit test.

### id 28 — Legacy `spec.auth.trustStore` folds into `trustedCASecrets`
- **scope:** `internal/resources/cluster.go` (`CollectTrustedCASecrets` ~L3092)
- **rule:** Legacy `spec.auth.trustStore` is folded into the plural `spec.trustedCASecrets` via `CollectTrustedCASecrets`. Never wire the legacy field directly — doing both produces duplicate volumes/init containers and the JKS build fails on duplicate alias.
- **why:** Two code paths producing the same volume → duplicate-alias keytool failure; a single collection function dedups.
- **pinned-by:** `CollectTrustedCASecrets` builder tests in `internal/resources/`.
- **enforcement:** unit test + code review.

### id 29 — `extraVolumeMounts` reserved paths rejected
- **scope:** `internal/validation/truststore_validator.go` (`reservedMountPaths` ~L34)
- **rule:** The validator rejects `spec.extraVolumeMounts` at any reserved path: `/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j`, and its standard subdirectories (`/var/lib/neo4j/{data,logs,conf,plugins,certificates}`).
- **why:** A user mount at a reserved path shadows operator-managed volumes (config, certs, data) and silently breaks the deployment.
- **pinned-by:** `internal/validation/truststore_validator_test.go` (reserved-path cases).
- **enforcement:** validator (inline) + unit test.

## Auth / AuthRule / OIDC

### id 30 — AUTH RULE Cypher requires `CYPHER 25` prefix
- **scope:** `internal/neo4j/auth_rules.go` (`cypher25Prefix = "CYPHER 25 "` L32; prepended to every AUTH RULE statement — SHOW/CREATE/ALTER/DROP ~L62-245)
- **rule:** Every AUTH RULE statement prepends `cypher25Prefix`. The 2026.x system DB defaults to Cypher 5; without the prefix you get `42I06: Invalid input 'AUTH'`. Keep the prefix even after the default flips.
- **why:** AUTH RULE syntax is Cypher 25-only; the system DB's default language is not guaranteed to be 25, so the prefix is mandatory.
- **pinned-by:** auth_rules Cypher-prefix unit tests in `internal/neo4j/`.
- **enforcement:** unit test + code review.

### id 31 — `oidc-`-prefixed provider name in ABAC config
- **scope:** `internal/controller/neo4jauthrule_controller.go` (`abacAuthorizationProvidersKey = "dbms.security.abac.authorization_providers"` ~L54, precondition check ~L518); `internal/validation/auth_validator.go` (`strings.HasPrefix(provider, "oidc-")` ~L87); cluster authz providers emitted in `internal/resources/cluster.go` (`dbms.security.authorization_providers` ~L2818)
- **rule:** `dbms.security.abac.authorization_providers` values must use the same form as `dbms.security.authorization_providers` — `oidc-<name>` for OIDC providers. The authrule controller checks the cluster has `dbms.security.abac.authorization_providers` set (a precondition it reads, not one it writes).
- **why:** Mismatched provider naming between the two keys means ABAC rules never match the configured authorization provider, silently denying access.
- **pinned-by:** `internal/validation/auth_validator_test.go` (oidc-prefix); `internal/resources/auth_config_test.go` (`oidc-okta`, `oidc-azure` provider strings).
- **enforcement:** validator + unit test + code review. NOTE: scope file is the AUTHRULE controller, not an `auth_config.go` (that file does not exist — `BuildAuthConfig` lives in `internal/resources/cluster.go`). See FLAG in notes.

### id 32 — Authrule controller in the `--controllers` default list
- **scope:** `cmd/main.go` (dev-mode `controllersToLoad` default includes `authrule` ~L136; production `setupProductionControllers` wires it unconditionally ~L307/550)
- **rule:** The dev-mode `--controllers` default string MUST include `authrule`; production (`setupProductionControllers`) wires it unconditionally.
- **why:** Dropping `authrule` from the dev default means AUTH RULE CRs silently never reconcile in dev/test — a hard-to-diagnose "my rule does nothing".
- **pinned-by:** main wiring is covered by controller-registration smoke checks; default string is asserted to contain `authrule`.
- **enforcement:** code review + integration smoke.

### id 33 — LDAP `useStartTLS` defaults to true for plain `ldap://`
- **scope:** `internal/resources/cluster.go` (`buildLDAPConfig`; `use_starttls` defaulting ~L2874-2892)
- **rule:** When `useStartTLS` is nil and the host is plain `ldap://` → emit `dbms.security.ldap.use_starttls=true`. `ldaps://` hosts skip StartTLS. An explicit `false` is honored.
- **why:** Secure-by-default — a plain `ldap://` bind would send the LDAP system password in cleartext; StartTLS upgrades the connection unless the user explicitly opts out.
- **pinned-by:** `TestBuildAuthConfig_LDAP_UseStartTLSDefault` (`internal/resources/auth_config_test.go`, 6 cases).
- **enforcement:** unit test.

## Network / Metrics / Audit

### id 34 — NetworkPolicy peer-rule ports mirror pod ContainerPorts
- **scope:** `internal/resources/networkpolicy.go` (`BuildNetworkPolicyForEnterprise` ~L63)
- **rule:** The peer (intra-cluster) rule covers `6000/7000/7688/7689`. Adding an intra-cluster ContainerPort to the StatefulSet without adding it here silently breaks pod-to-pod traffic on enforcing CNIs.
- **why:** Once a NetworkPolicy selects a pod, all non-listed traffic is denied; a new cluster port not mirrored here is dropped between pods.
- **pinned-by:** `TestBuildNetworkPolicyForEnterprise_PeerPortsRestrictedToCluster` (`internal/resources/networkpolicy_test.go`).
- **enforcement:** unit test.

### id 35 — NetworkPolicy public rule MUST include port 2004 (Prometheus)
- **scope:** `internal/resources/networkpolicy.go` (`BuildNetworkPolicyForEnterprise`)
- **rule:** The public ingress rule must include port 2004 (Prometheus scrape) alongside HTTP/HTTPS/Bolt. Once any rule selects the pod, it's fully isolated — omitting 2004 silently kills metrics scraping.
- **why:** Same isolation semantics as id 34: a selected pod denies everything not explicitly allowed, so the metrics port must be listed.
- **pinned-by:** `TestBuildNetworkPolicyForEnterprise_PublicPortsOpen` (`internal/resources/networkpolicy_test.go`).
- **enforcement:** unit test.

### id 36 — `BuildNetworkPolicy*` returns nil when disabled
- **scope:** `internal/resources/networkpolicy.go`; standalone reconciler uses `reflect.DeepEqual` to skip churn
- **rule:** `BuildNetworkPolicy*` returns nil when NetworkPolicy is disabled, and the reconcilers short-circuit on nil. The standalone path additionally uses `reflect.DeepEqual` to avoid resourceVersion churn.
- **why:** Returning an empty policy would isolate pods even when the feature is off; nil + short-circuit means "don't manage it at all". DeepEqual prevents needless update writes that bump resourceVersion and re-trigger reconciles.
- **pinned-by:** networkpolicy builder unit tests (nil-when-disabled).
- **enforcement:** unit test + code review.

### id 37 — Metrics JMX + CSV disabled UNCONDITIONALLY
- **scope:** `internal/resources/cluster.go` — emitted in the main `BuildConfigMapForEnterprise` body (~L1661-1668), deliberately **NOT** inside `BuildMonitoringConfig` (the code comment at ~L1661 says so explicitly); it sits OUTSIDE the `monitoring.enabled` branch (which begins ~L1679), so `server.metrics.{jmx,csv}.enabled=false` is emitted regardless of `monitoring.enabled`
- **rule:** `server.metrics.jmx.enabled=false` and `server.metrics.csv.enabled=false` are emitted unconditionally, OUTSIDE the `monitoring.enabled` branch. JMX is unauthenticated remote management; CSV writes pod-ephemeral files that fill disk.
- **why:** These are security/stability kill-switches, not monitoring features — gating them on `monitoring.enabled` would re-enable an unauthenticated JMX management port whenever monitoring is off.
- **pinned-by:** `TestBuildConfigMapForEnterprise_MetricsHardening` (`internal/resources/cluster_tls_test.go`), `TestBuildMonitoringConfig` (`internal/resources/cluster_test.go`).
- **enforcement:** unit test.

### id 38 — `spec.audit` emission order (audit wins over monitoring; user config wins over both)
- **scope:** `internal/resources/cluster.go` (`BuildAuditConfig` ~L2083, runs AFTER `BuildMonitoringConfig` ~L2015)
- **rule:** `BuildAuditConfig` runs after `BuildMonitoringConfig`; both touch `db.logs.query.obfuscate_literals` and last-write-wins gives audit priority over monitoring. User `spec.config` is appended last and wins over both. No `dbms.security.audit.*` keys (4.x, removed) — use `security.log` / `query.log`.
- **why:** Deterministic precedence: audit's obfuscation intent must override the monitoring default, and an explicit user override must beat both.
- **pinned-by:** `TestBuildAuditConfig_PrecedenceOverMonitoring` (`internal/resources/audit_config_test.go`).
- **enforcement:** unit test.

### id 39 — `spec.audit.Enabled` is a hint, not a stomping default
- **scope:** `internal/resources/cluster.go` (`BuildAuditConfig` ~L2083)
- **rule:** `Enabled=true` with `ObfuscateQueryLiterals` nil → emit `obfuscate_literals=true`. An explicit value (true OR false) always wins. Exactly ONE `obfuscate_literals` line is emitted.
- **why:** "Enabled" should imply safe obfuscation by default, but must never override an operator who explicitly set `false`; emitting the key twice would make behavior config-order-dependent.
- **pinned-by:** `TestBuildAuditConfig_ExplicitObfuscateFalseDespiteEnabled` (`internal/resources/audit_config_test.go`).
- **enforcement:** unit test.

## Cross-cutting helpers referenced above

- **Condition helpers** (`internal/controller/conditions.go`): `SetReadyCondition` (~L65) is ONLY for the `Ready` condition type; use `SetNamedCondition` (~L88) for `ServersHealthy`/`DatabasesHealthy`/`PendingDependencies`. Pinned by `TestSetNamedCondition_Idempotent`.
- **Structured events** (`internal/controller/events.go`): event reasons (`EventReason*`) and condition type/reason constants (`ConditionType*`, `ConditionReason*`) are defined here. Use `corev1.EventTypeNormal` / `corev1.EventTypeWarning`, never raw reason strings.
- **Env-var ownership annotation** `neo4j.com/cluster-controller-env-vars` (`internal/controller/neo4jenterprisecluster_controller.go` `ownedEnvVarsAnnotation` ~L1156; see also `internal/controller/owned_keys.go`): the cluster controller records the env-var names it owns each reconcile so the next loop can enforce removals (`previously-owned ∖ desired`) via `mergeEnvVars` (~L1221) / `envVarsEqual` (~L1258) without disturbing foreign vars set by plugin/fleet/Aura controllers.
