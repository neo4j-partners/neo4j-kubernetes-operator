# Neo4jRole API Reference

The `Neo4jRole` Custom Resource Definition (CRD) provides declarative management of Neo4j roles and the privileges granted to them, for both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` deployments.

## Overview

- **API Version**: `neo4j.neo4j.com/v1beta1`
- **Kind**: `Neo4jRole`
- **Scope**: Namespaced
- **Short names**: `n4jrole`, `n4jroles`
- **Categories**: `neo4j`
- **Supported Neo4j Versions**: 5.26 LTS and any CalVer release (2025.x, 2026.x, and onward) — Enterprise edition only (RBAC roles are an Enterprise feature)
- **Reconciliation**: Role existence and privilege drift via `SHOW ROLE <r> PRIVILEGES AS COMMANDS`

## Design rules

1. **Privileges live on the role, not on individual users.** Bind users to roles via `Neo4jUser.spec.roles`.
2. **Source of truth is the spec.** On every reconcile the controller diffs `spec.privileges` against the live state from `SHOW ROLE PRIVILEGES AS COMMANDS` and applies the difference. Manual changes are reverted unless `enforcePrivileges: false`.
3. **Built-in roles are protected by default.** `PUBLIC`, `reader`, `editor`, `publisher`, `architect`, `admin` cannot be managed unless `adoptBuiltin: true`. Adopted built-ins are never dropped on CR delete.
4. **Immutable privileges are detected and skipped.** Privileges created with `GRANT IMMUTABLE` cannot be revoked while authentication is enabled; the controller filters them out of the revoke set and surfaces drift via `status.privilegeDrift` plus a `PrivilegesDriftKept` event.

## Related Resources

- [`Neo4jUser`](neo4juser.md) — Bind users to roles
- [User & Role Management Guide](../user_guide/user_role_management.md) — End-to-end walkthrough

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required**. Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in the same namespace. |
| `name` | `string` | Role name in Neo4j. Defaults to `metadata.name`. Pattern `^[a-zA-Z][a-zA-Z0-9_]*$`. |
| `copyOf` | `string` | Existing role to seed privileges from at creation time only (`CREATE ROLE name AS COPY OF other`). Ignored on subsequent reconciles. |
| `privileges` | `[]string` | Desired set of `GRANT` and `DENY` statements. Each entry must be a complete Cypher statement starting with `GRANT` or `DENY` and ending with `TO <spec.name>`. |
| `enforcePrivileges` | `boolean` | Reconcile drift back to spec. Default `true`. When `false`, the controller applies missing privileges but never revokes anything added out-of-band. |
| `adoptBuiltin` | `boolean` | Allow `name` to be a built-in role (`PUBLIC`, `reader`, `editor`, `publisher`, `architect`, `admin`). Default `false`. Adopted roles are never dropped on CR delete. |
| `deletionPolicy` | `string` | One of `Delete` (default) or `Retain`. With `Retain`, deleting the CR releases the finalizer without dropping the role from Neo4j. |

## Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | One of `Pending`, `Ready`, `Failed`. |
| `message` | `string` | Short human-readable summary of the current phase. |
| `observedGeneration` | `int64` | `metadata.generation` observed during the last reconcile. |
| `appliedPrivileges` | `[]string` | Canonicalised privilege statements last observed via `SHOW ROLE PRIVILEGES AS COMMANDS`. |
| `privilegeDrift` | `boolean` | True when one or more privileges could not be reconciled (e.g. immutable extras). |
| `conditions` | `[]Condition` | See [Conditions](#conditions). |

### Conditions

| Type | Reasons | Meaning |
|---|---|---|
| `Ready` | `RoleReady`, `RoleSyncFailed`, `ClusterNotReady`, `ConnectionFailed`, `ValidationFailed` | True when role exists and privileges are reconciled. |
| `PrivilegesSynced` | `PrivilegesMatch`, `PrivilegesDrifted` | True when live privileges match `spec.privileges`. |
| `ClusterNotReady` | `ClusterNotReady`, `ClusterReady` | Mirrors the readiness of the referenced cluster. |

## Validation rules

- `clusterRef` must resolve to a `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in the same namespace.
- `name` (or `metadata.name` fallback) matches `^[a-zA-Z][a-zA-Z0-9_]*$`.
- Built-in role names are rejected unless `adoptBuiltin: true`.
- Each `privileges[]` entry:
  - Must start with `GRANT` or `DENY` (case-insensitive). `REVOKE` is rejected — revokes are derived automatically when a privilege disappears from the spec.
  - Must end with `TO <name>` matching the role's effective name.
  - Trailing semicolons produce a warning and are stripped during canonicalisation.

## Privilege diff engine

The controller treats `spec.privileges` as the source of truth and reconciles drift on every loop:

1. **Read live**: `SHOW ROLE <name> PRIVILEGES AS COMMANDS YIELD command, immutable`.
2. **Canonicalise** both desired (spec) and live (Neo4j) statements:
   - Whitespace runs collapsed to a single space (outside quoted strings).
   - Reserved keywords upper-cased (`grant` → `GRANT`, `database` → `DATABASE`, etc.).
   - Trailing semicolons stripped.
3. **Diff sets**:
   - Desired ∖ live → execute the original `GRANT/DENY` statement.
   - Live ∖ desired → derive a `REVOKE` form by replacing the leading verb (`GRANT`/`DENY`) with `REVOKE GRANT`/`REVOKE DENY` and `TO role` with `FROM role`. Skip immutable rows.
4. **Update status** with `appliedPrivileges` set to the post-apply canonical list and `privilegeDrift` set when any extras could not be revoked.

When `enforcePrivileges: false`, step 3's revoke pass is skipped — adds still happen.

## Lifecycle

| CR Event | Cypher Issued |
|---|---|
| Create (custom role) | `CREATE ROLE name IF NOT EXISTS [AS COPY OF other]`, then privilege apply |
| Create (built-in, `adoptBuiltin: true`) | Skip create; reconcile privileges |
| Update | Privilege diff + apply |
| Delete (`Delete` policy, custom role) | `DROP ROLE name IF EXISTS`, then remove finalizer |
| Delete (built-in or `Retain`) | Remove finalizer only |
| Cluster not Ready | No-op; requeue every 30s with `ClusterNotReady` condition |
| Drop refused (role still in use) | Reconcile fails with the Neo4j error; remove the role from each `Neo4jUser.spec.roles` first |

## Examples

### A read-only role for the analytics database

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: analytics-reader
  namespace: prod
spec:
  clusterRef: prod-cluster
  privileges:
    - "GRANT ACCESS ON DATABASE analytics TO analytics_reader"
    - "GRANT MATCH {*} ON GRAPH analytics NODES * TO analytics_reader"
    - "DENY WRITE ON GRAPH analytics TO analytics_reader"
```

### A role copied from `editor` with extra restrictions

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: junior-editor
  namespace: prod
spec:
  clusterRef: prod-cluster
  copyOf: editor                # honoured at create time only
  privileges:
    - "DENY DROP ON GRAPH * TO junior_editor"
    - "DENY DELETE ON GRAPH * NODES Customer TO junior_editor"
```

### Adopting the built-in `editor` role to lock it down

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: editor
  namespace: prod
spec:
  clusterRef: prod-cluster
  name: editor
  adoptBuiltin: true
  privileges:
    - "GRANT MATCH {*} ON GRAPH * TO editor"
    - "GRANT WRITE ON GRAPH * TO editor"
    - "DENY DROP ON GRAPH * TO editor"
```

The built-in `editor` is not dropped when this CR is deleted; only its privilege drift reconciliation stops.

### Disabling drift reconciliation

Useful when you intentionally layer manual privilege overrides on top of a baseline:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: legacy-role
  namespace: prod
spec:
  clusterRef: prod-cluster
  enforcePrivileges: false
  privileges:
    - "GRANT ACCESS ON DATABASE legacy TO legacy_role"
```

## Property-Based Access Control (PBAC)

`Neo4jRole.spec.privileges` accepts the full Cypher privilege grammar, including the `FOR pattern WHERE …` clause used by [property-based access control](https://neo4j.com/docs/operations-manual/current/authentication-authorization/property-based-access-control/). PBAC refines the existing `MATCH`, `READ`, and `TRAVERSE` privileges with per-row conditions on node or relationship properties.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: redacted-reader
spec:
  clusterRef: production
  name: redacted_reader
  privileges:
    # Only allow traversal of Email nodes whose classification has been set
    - "GRANT TRAVERSE ON GRAPH * FOR (n:Email) WHERE n.classification IS NOT NULL TO redacted_reader"
    # Hide anything not classified UNCLASSIFIED or PUBLIC
    - "DENY READ {*} ON GRAPH * FOR (n) WHERE NOT n.classification IN ['UNCLASSIFIED', 'PUBLIC'] TO redacted_reader"
    # Filter relationships by property
    - "GRANT READ {since} ON GRAPH * FOR ()-[o:OWNS]-() WHERE o.classification = 'UNCLASSIFIED' TO redacted_reader"
```

PBAC privileges flow through the same diff-and-reconcile loop as standard privileges. The canonicaliser upper-cases `WHERE`, `IS`, `NULL`, `IN`, `NOT`, `AND`, `OR`, and `FOR` so spec strings round-trip equal against `SHOW ROLE PRIVILEGES AS COMMANDS` regardless of input casing.

**Limitations** (per upstream Neo4j docs):

- **Single property per rule.** Each `WHERE` clause references one property only; chained comparisons across properties require multiple privileges.
- **Sharded property databases are unsupported.** The role validator rejects PBAC privileges naming a `Neo4jShardedDatabase` and warns when `ON GRAPH *` is used in combination with `FOR pattern WHERE …` (silent ineffectiveness on any sharded DBs in scope).
- **Performance overhead.** PBAC adds per-row evaluation, especially significant on `TRAVERSE` rules and on disk-backed storage. Profile before deploying widely; consider `block` storage format and label-based privileges where the row count is high.
- **Property immutability.** If a user can write the property used in a PBAC rule, they can bypass the rule. Pair PBAC with a corresponding `DENY SET PROPERTY` privilege.

## Troubleshooting

**`validation failed: ... privilege statement must end with TO <role>`**: every privilege must name the role being defined. Update the statement to end with `TO <spec.name>`.

**`cannot revoke immutable privilege ...`**: the controller cannot revoke `GRANT IMMUTABLE` privileges while auth is enabled. Either include them in `spec.privileges` (so they're no longer drift) or set `enforcePrivileges: false`.

**`drop role failed: role X is assigned to user Y`**: Neo4j refuses to drop a role still in use. Remove the role from each `Neo4jUser.spec.roles` first; once the operator has revoked the role from all users, deletion succeeds.

**`Pending` phase, `ClusterNotReady=True`**: the referenced `Neo4jEnterpriseCluster`/`Neo4jEnterpriseStandalone` is not yet `Ready`. The role controller will reconcile automatically when the cluster transitions.

## See also

- [User & Role Management Guide](../user_guide/user_role_management.md)
- [`Neo4jUser`](neo4juser.md)
