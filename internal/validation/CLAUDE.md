# internal/validation

The **only** validation layer for the operator. Every CRD spec is checked here, inline from the
reconciler — there are no admission webhooks (invariant 1, `docs/knowledge/invariants.md`). One
file per concern; the per-CRD aggregators (`ClusterValidator`, `StandaloneValidator`,
`DatabaseValidator`, …) compose the smaller field validators.

## Key files

| File | What it does |
|---|---|
| `cluster_validator.go` | `ClusterValidator` — composes edition/topology/image/storage/tls/auth/upgrade/memory/resource/config sub-validators; entry points `ValidateCreate`/`ValidateUpdate`/`ValidateCreateWithWarnings`/`ApplyDefaults`. |
| `standalone_validator.go` | `StandaloneValidator` — single-node equivalent (`ValidateCreate`/`ValidateUpdate`). |
| `database_validator.go` | `DatabaseValidator` for `Neo4jDatabase` — tries cluster lookup first, then standalone. |
| `image_validator.go` | Rejects `-community` tags (`isCommunityTag`) + Neo4j version support check. |
| `edition_validator.go` | Intentional no-op (edition field removed; enterprise-only). |
| `topology_validator.go` | Server count, `serverModeConstraint`/`serverRoles` index/dup/all-SECONDARY checks. |
| `config_validator.go` | Rejects deprecated / operator-managed / per-pod runtime `spec.config` keys. |
| `memory_validator.go`, `resource_validator.go`, `storage_validator.go` | Memory floors, scaling resource checks, PVC/storage checks. |
| `tls_validator.go`, `truststore_validator.go`, `security_validator.go` | TLS mode/issuer/strict-peer rules, trusted CA secrets, security context. |
| `auth_validator.go`, `user_validator.go`, `role_validator.go`, `rolebinding_validator.go`, `authrule_validator.go` | Auth providers and the user/role/binding/authrule CRDs. |
| `backup_validator.go` | `BackupValidator` + standalone helper `ValidateNeo4jVersion`. |
| `upgrade_validator.go`, `plugin_validator.go`, `fleet_validator.go`, `mcp_validator.go`, `shardeddatabase_validator.go`, `sharding.go` | Version-upgrade rules, plugins, Aura fleet, MCP, sharded DB, `IsClusterShardingReady`. |
| `*_validator_test.go`, `sharding_test.go` | Table-driven unit tests (one per validator). |

## Key types & functions

- Per-field validator pattern: `New<X>Validator()` returns a struct; `Validate(...) field.ErrorList`
  using `k8s.io/apimachinery/pkg/util/validation/field` (`field.Invalid`/`Required`/`NotSupported`).
- Aggregators wrap errors into a single `error` via `allErrs.ToAggregate()` and surface warnings
  through `ClusterValidationResult{Errors, Warnings}` (see `ValidateCreateWithWarnings`).
- `ImageValidator.isCommunityTag` / `Validate`; `ValidateNeo4jVersion` (in `backup_validator.go`);
  `IsClusterShardingReady` (in `sharding.go`, reused by the property-sharding gate).

## Conventions & gotchas

- **No webhooks** — these run inline from reconcilers, e.g. `r.Validator.ValidateCreateWithWarnings`
  in `internal/controller/neo4jenterprisecluster_controller.go`; the standalone/backup/plugin
  controllers hold a `*validation.X` field. Don't add `_webhook.go` or webhook configs.
- `cluster_validator.go` **fails fast on image errors**: if `imageValidator.Validate` returns errors
  it returns early, so later validators don't run.
- `image_validator.go` rejects only the unambiguous `-community` signal; a bare/retagged Enterprise
  tag passes (runtime `CALL dbms.components()` is the backstop). Version must be 5.26.x or 2025.01.0+.
- `edition_validator.go` is a deliberate no-op — keep it; the edition field was removed.
- `DatabaseValidator.Validate` resolves cluster then standalone, and returns warnings (not errors)
  for topology/secondaries on standalone targets.
- Add new sub-validators to the right aggregator's `New...Validator` constructor and `validateCluster`
  (or `validateStandalone`) chain — a validator not wired in is dormant (cf. the previously-unwired
  `configValidator` / `PluginValidator`).

## Tests

Pure unit tests, no cluster needed:

```bash
go test ./internal/validation/...        # whole package
make test-unit                           # all unit tests
```

## See also

- `../../AGENTS.md` and root `CLAUDE.md` — invariants, domain reference.
- `docs/knowledge/invariants.md` (hard constraints, incl. NO WEBHOOKS / ENTERPRISE-IMAGES).
- `../controller/` — the reconcilers that call these validators.
- `../neo4j/` — `ParseVersion`/`IsSupported`/`IsCalver` used by image and version checks.
