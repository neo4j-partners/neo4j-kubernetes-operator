# Neo4jUser API Reference

The `Neo4jUser` Custom Resource Definition (CRD) provides declarative management of Neo4j users — username, password, account state, home database, role bindings, and external authentication providers — for both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` deployments.

## Overview

- **API Version**: `neo4j.neo4j.com/v1beta1`
- **Kind**: `Neo4jUser`
- **Scope**: Namespaced
- **Short names**: `n4juser`, `n4jusers`
- **Categories**: `neo4j`
- **Supported Neo4j Versions**: 5.26.x and 2025.x.x+ (Enterprise edition only — `SET STATUS` and external auth providers are Enterprise-only features)
- **Reconciliation**: User existence, password (rotated via Secret hash), `SET STATUS`, `SET HOME DATABASE`, role bindings (`GRANT/REVOKE ROLE`), external auth providers (`SET AUTH`)

## Design rules

1. **Privileges live on `Neo4jRole`, not on `Neo4jUser`.** Bind users to roles; never inline grants on a user.
2. **Passwords come from `Secret`, never from the spec.** The Secret value is hashed (SHA-256) and stored on `status.passwordSecretHash` to detect rotation; the password itself is never echoed back.
3. **Same-namespace `clusterRef` only.** Cross-namespace references are rejected.
4. **`PUBLIC` is implicit.** It is auto-assigned by Neo4j and never granted/revoked by the controller. Listing it in `spec.roles` produces a warning.

## Related Resources

- [`Neo4jRole`](neo4jrole.md) — Manages roles and their privileges
- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) — Target cluster deployment
- [`Neo4jEnterpriseStandalone`](neo4jenterprisestandalone.md) — Target standalone deployment
- [User & Role Management Guide](../user_guide/user_role_management.md) — End-to-end walkthrough

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required**. Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in the same namespace. |
| `username` | `string` | Username in Neo4j. Defaults to `metadata.name`. Pattern `^[a-zA-Z][a-zA-Z0-9_.\-]*$`, max 65 characters. |
| `passwordSecretRef` | [`SecretKeyRef`](#secretkeyref) | References a Secret holding the native-auth password. Required unless one or more `externalAuth` entries are provided. |
| `requirePasswordChange` | `boolean` | Force password change on next login (`SET PASSWORD CHANGE REQUIRED`). Default `false`. |
| `accountStatus` | `string` | One of `active` (default), `suspended`. Maps to `SET STATUS ACTIVE|SUSPENDED`. Suspending a native user revokes role assignments client-side; reactivating restores them. |
| `homeDatabase` | `string` | Sets `SET HOME DATABASE`. Removing this field after it was set issues `REMOVE HOME DATABASE`. |
| `roles` | `[]string` | Role names to grant. Mix built-ins (`reader`, `editor`, `publisher`, `architect`, `admin`) and custom role names from `Neo4jRole` CRs. `PUBLIC` is implicit and need not be listed. |
| `externalAuth` | [`[]ExternalAuthProvider`](#externalauthprovider) | Bind the user to non-native authentication providers (LDAP, OIDC, SSO). Translates to `SET AUTH 'provider' { SET ID 'id' }` clauses. |
| `deletionPolicy` | `string` | One of `Delete` (default) or `Retain`. With `Retain`, deleting the CR releases the finalizer without dropping the user from Neo4j. |

### SecretKeyRef

| Field | Type | Description |
|---|---|---|
| `name` | `string` | **Required**. Name of a `Secret` in the same namespace. |
| `key` | `string` | Key inside the Secret's data. Defaults to `password`. Empty values are rejected. |

### ExternalAuthProvider

Configures a single non-native authentication provider for the user. The provider must already be configured at the DBMS level (`dbms.security.authentication_providers` etc.).

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | **Required**. Provider name, e.g. `oidc-okta`, `ldap1`, `saml1`. The literal `native` is rejected — use `passwordSecretRef` for native authentication. |
| `id` | `string` | **Required**. The user's identifier within that provider (e.g. an OIDC `sub` claim or an LDAP DN). |

## Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | One of `Pending`, `Ready`, `Failed`. |
| `message` | `string` | Short human-readable summary of the current phase. |
| `observedGeneration` | `int64` | `metadata.generation` observed during the last reconcile. |
| `currentRoles` | `[]string` | Roles currently granted to the user, as observed via `SHOW USERS YIELD roles`. |
| `passwordSecretHash` | `string` | SHA-256 hex digest of the password value last applied (used to detect Secret rotation; never the password itself). |
| `passwordLastRotated` | `time` | Last time `ALTER USER ... SET PASSWORD` was executed. |
| `conditions` | `[]Condition` | See [Conditions](#conditions). |

### Conditions

| Type | Reasons | Meaning |
|---|---|---|
| `Ready` | `UserReady`, `RolesPending`, `ClusterNotReady`, `ConnectionFailed`, `UserSyncFailed`, `ValidationFailed` | True when user exists, password and roles are in sync. |
| `RolesSynced` | `RolesMatch`, `RolesPending` | True when granted roles equal `spec.roles` (PUBLIC excluded). |
| `PasswordSynced` | `PasswordMatchesSecret` | True when last-applied hash matches the current Secret. |
| `PendingDependencies` | `RolesPending`, `AllDependenciesPresent` | True when one or more `spec.roles` reference a custom role that does not yet exist. |
| `ClusterNotReady` | `ClusterNotReady`, `ClusterReady` | Mirrors the readiness of the referenced cluster. |

## Validation rules

- `clusterRef` must resolve to a `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in the same namespace.
- `username` (or `metadata.name` fallback) matches `^[a-zA-Z][a-zA-Z0-9_.\-]*$`, max 65 characters.
- The reserved name `system` is rejected.
- At least one of `passwordSecretRef` or `externalAuth` must be set (Neo4j requires ≥1 auth provider per user).
- `passwordSecretRef`: the Secret must exist and contain a non-empty value at the named key. Values shorter than 8 characters produce a warning (Neo4j's default minimum is 8).
- `externalAuth[].provider` cannot be `native`.
- `homeDatabase`, when set, must be a valid Neo4j database name.
- `accountStatus` must be one of `active` or `suspended` (enforced by kubebuilder enum).
- `deletionPolicy` must be one of `Delete` or `Retain` (enforced by kubebuilder enum).

## Lifecycle

| CR Event | Cypher Issued |
|---|---|
| Create | `CREATE USER name [SET PASSWORD ...] [SET STATUS ...] [SET HOME DATABASE ...] [SET AUTH ...]`, then `GRANT ROLE r TO name` for each role |
| Update (spec only) | One compound `ALTER USER` for changed fields (REMOVEs before SETs); incremental `GRANT/REVOKE ROLE` for role changes |
| Update (Secret only) | `ALTER USER name SET PASSWORD $pw`, then update `passwordSecretHash` and `passwordLastRotated` |
| Delete (`Delete` policy) | `DROP USER name IF EXISTS`, then remove finalizer |
| Delete (`Retain` policy) | Remove finalizer only |
| Cluster not Ready | No-op; requeue every 30s with `ClusterNotReady` condition |
| Missing custom role | Skip the grant for that role; set `PendingDependencies` condition; reconcile triggers when the role lands (the user controller watches `Neo4jRole`) |

## Examples

### Read-only application user

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-reader-creds
  namespace: prod
type: Opaque
stringData:
  password: "ChangeMe123!"
---
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: app-reader
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: app_reader
  passwordSecretRef:
    name: app-reader-creds
  roles: [reader]
```

### Service account with home database

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: ingestion-service
  namespace: prod
spec:
  clusterRef: prod-cluster
  passwordSecretRef:
    name: ingestion-creds
  homeDatabase: telemetry
  roles: [publisher]
```

### Suspending an account during a security incident

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: compromised-user
  namespace: prod
spec:
  clusterRef: prod-cluster
  passwordSecretRef:
    name: compromised-user-creds
  accountStatus: suspended    # was: active
  roles: [publisher]
```

Suspending a native user removes role assignments client-side; flipping back to `active` restores them.

### External authentication via OIDC + LDAP

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: alice
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: alice
  externalAuth:
    - provider: oidc-okta
      id: alice@example.com
    - provider: ldap1
      id: "uid=alice,ou=people,dc=example,dc=com"
  roles: [editor]
  # No passwordSecretRef — native auth is not enabled for this user.
```

### Bound to a custom Neo4jRole

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: analytics-reader
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: analytics_reader
  passwordSecretRef:
    name: analytics-reader-creds
  roles:
    - analytics_reader   # corresponds to a Neo4jRole CR's spec.name
```

If the `Neo4jRole` does not yet exist, the user enters `PendingDependencies` and reconciles automatically when the role lands.

## See also

- [User & Role Management Guide](../user_guide/user_role_management.md)
- [`Neo4jRole`](neo4jrole.md)
