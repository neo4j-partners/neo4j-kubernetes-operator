# internal/neo4j

Bolt client (`github.com/neo4j/neo4j-go-driver/v5`) plus the Cypher/DDL helpers
the controllers use to drive a live Neo4j Enterprise server: version parsing,
database/user/role/privilege/auth-rule management, server scale-down, fleet
management, and error classification.

## Key files

| File | What it does |
|---|---|
| `client.go` | `Client` (driver + circuit breaker + pool metrics); constructors `NewClientForEnterprise`, `NewClientForEnterpriseStandalone`, `NewClientForPod`; `VerifyConnectivity`, `CreateDatabase*`, `DropDatabase`, `ExecuteCypher*`, fleet-management calls. |
| `version.go` | `Version` struct + `ParseVersion`, `IsSupported`, `Compare`, `GetImageVersion`, the `Supports*` capability gates, and `GetBackupCommand`/`GetRestoreCommand`. |
| `users.go` | `Client` methods for users & roles: `ShowUser`, `CreateUserAdvanced`, `AlterUser` (+ `AlterUserOptions` builder), `ShowRole`, `CreateRoleAdvanced`, `DropRoleIfExists`, `ListUsers`/`ListRoles`, `ShowRolePrivileges`. |
| `privileges.go` | Pure string helpers: `CanonicalisePrivilegeStatement`, `DerivePrivilegeRevoke`, `PrivilegeStatementMatchesRole`, `PrivilegeStatementVerb`. No I/O. |
| `auth_rules.go` | ABAC `Neo4jAuthRule` DDL: `ShowAuthRule`, `ListAuthRules`, `CreateOrReplaceAuthRule`, `AlterAuthRule`, `DropAuthRuleIfExists`, `Grant/RevokeRolesFromAuthRule`. |
| `server_management.go` | Cluster scale-down: `CordonServer`, `DeallocateServers` (supports `DRYRUN`), `DropServer`, `MinimumSystemPrimaries`. |
| `error_classify.go` | `IsTransientError`, `IsHostUnresolvableError`, `IsConnectivityError`, `IsNotFoundError` — drive finalizer/retry decisions in controllers. |
| `*_test.go` | Unit tests (see below). |

## Key types & functions

- `Client` (`client.go`) — wraps `neo4j.DriverWithContext`; always `defer c.Close()`.
- `ParseVersion(string) (*Version, error)` and `(*Version).IsSupported()` — sole arbiter of which Neo4j versions are accepted (5.26.x SemVer, or any CalVer `>= 2025`).
- `(*Version).IsCalver` — set by `ParseVersion` when `major >= 2025`; gates CalVer-only behaviour (e.g. `SupportsCypherLanguageVersion`, `SupportsAuthRules`).
- `RegisterFleetManagementToken` / `IsFleetManagementInstalled` (`client.go`) — Aura fleet two-phase reconcile lives here, not in the controller.
- `CanonicalisePrivilegeStatement` / `DerivePrivilegeRevoke` (`privileges.go`) — REVOKEs are derived textually from GRANT/DENY text, never user-supplied.

## Conventions & gotchas

- **`ParseVersion` sets `IsCalver = major >= 2025`** — handles 2026.x+ automatically. Never special-case a single CalVer year; branch on `IsCalver` and the `Supports*` gates.
- **Cypher must be 5.26+/2025.x syntax.** Use `CREATE DATABASE ... TOPOLOGY n PRIMARIES [m SECONDARIES]` — never the 4.x `OPTIONS {primaries: …}`. Read cluster state with `SHOW DATABASES` / `SHOW SERVERS` — never the removed `CALL dbms.cluster.role()`. (See CLAUDE.md "Neo4j Database Syntax" and "Default Database Behavior".)
- **DDL runs against the `system` database.** Every admin session sets `DatabaseName: "system"` (CREATE/DROP DATABASE, user/role/privilege/auth-rule, server management). Don't run DDL against a user DB.
- **Identifier safety:** user/role/auth-rule names are backtick-escaped (`EscapeBackticks`); server ids are validated against `serverIDPattern` (UUID) before interpolation because DEALLOCATE/DROP SERVER don't accept query parameters. Prefer query parameters (`$id`) where the command allows them.
- **Version detection is via `CALL dbms.components()`** (edition + version), not the CRD image tag, for the live edition check (invariant 3: Enterprise only).
- **Discovery is V2_ONLY on port 6000** (invariant 4). Backup/restore are per-Job (invariant 5) — `GetBackupCommand`/`GetRestoreCommand` build the `neo4j-admin` command line; there is no long-running backup pod.
- **Wrap conflicts** with `retry.RetryOnConflict` in the *controller*; here, surface transient failures so callers can classify them via `error_classify.go`.
- `privileges.go` is pure string manipulation — keep it free of driver/I/O imports so it stays trivially unit-testable.

## Tests

All unit-level, Kind/cluster-free. Mix of standard `testing.T` table tests
(`TestParseVersion`, `TestIsConnectivityError`, …) and one Ginkgo suite
(`TestClient` in `client_test.go`). Run the package:

```bash
go test ./internal/neo4j/...        # or: make test-unit
```

## See also

- `../../AGENTS.md` and `../../CLAUDE.md` — repo invariants & domain reference.
- `docs/knowledge/invariants.md`, `docs/knowledge/operations.md`, `docs/knowledge/backup-restore.md` — enforcement-tagged regression rules.
- `internal/validation/` — all CR validation (NO webhooks; invariant 1) that gates these calls before a reconciler invokes the client.
- `internal/controller/` — callers; `internal/resources/` — StatefulSet/config builders.
