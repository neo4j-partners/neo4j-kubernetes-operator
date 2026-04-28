# Neo4jRoleBinding API Reference

The `Neo4jRoleBinding` Custom Resource Definition (CRD) declaratively manages role grants for users the operator does NOT own as a `Neo4jUser`. Use this for SSO/LDAP/OIDC users that are provisioned by Neo4j itself on first login (or by an external pipeline) — the operator keeps their Neo4j role assignments in sync with `spec.roles` without ever creating, dropping, or otherwise managing the user record.

## Overview

- **API Version**: `neo4j.neo4j.com/v1beta1`
- **Kind**: `Neo4jRoleBinding`
- **Scope**: Namespaced
- **Short names**: `n4jrb`, `n4jrolebindings`
- **Categories**: `neo4j`
- **Supported Neo4j Versions**: 5.26.x and 2025.x.x+ (Enterprise edition)
- **Reconciliation**: Role grants/revokes via `GRANT/REVOKE ROLE`, with optional exclusive enforcement of the user's full role set

## When to use this vs. Neo4jUser

| You want to manage… | Use |
|---|---|
| Identity, password, status, home DB, role grants for a user the operator owns | `Neo4jUser` |
| Role grants for a user that already exists in Neo4j (created by SSO/LDAP first-login or by another tool) | `Neo4jRoleBinding` |

The validator rejects a `Neo4jRoleBinding` whose `spec.username` overlaps with a `Neo4jUser` in the same namespace targeting the same `clusterRef`. Pick one.

## Related Resources

- [`Neo4jUser`](neo4juser.md) — Manages users + their role grants
- [`Neo4jRole`](neo4jrole.md) — Manages roles + their privileges
- [User & Role Management Guide](../user_guide/user_role_management.md)

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required**. Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in the same namespace. |
| `username` | `string` | **Required**. Username in Neo4j to manage role grants for. Must already exist when the binding reconciles (it enters `UserNotFound` and waits otherwise). Pattern `^[a-zA-Z][a-zA-Z0-9_.\-]*$`, max 65 chars. |
| `roles` | `[]string` | **Required**, MinItems=1. Role names to grant. Mix built-ins and custom names (the latter must correspond to existing `Neo4jRole` CRs or roles created out-of-band). PUBLIC is implicit. |
| `enforceExclusive` | `boolean` | Default `false`. When true, `.spec.roles` is authoritative for the user's full role set: roles granted out-of-band and not listed here are revoked on every reconcile. When false, the binding only adds and removes the roles it knows about; extra grants from other sources are tolerated. |
| `deletionPolicy` | `string` | One of `Revoke` (default) or `Retain`. With `Revoke`, deleting the CR revokes every role this binding granted. With `Retain`, the finalizer is released without revoking. |

## Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | One of `Pending`, `Ready`, `Failed`. |
| `message` | `string` | Short summary of the current phase. |
| `observedGeneration` | `int64` | `metadata.generation` last reconciled. |
| `grantedRoles` | `[]string` | Subset of `.spec.roles` actually granted (and observed via `SHOW USERS YIELD roles`). On CR deletion this list dictates what to revoke (so we never strip grants we did not add, except when `enforceExclusive: true`). |
| `conditions` | `[]Condition` | See [Conditions](#conditions). |

### Conditions

| Type | Reasons | Meaning |
|---|---|---|
| `Ready` | `BindingReady`, `RolesPending`, `UserNotFound`, `ClusterNotReady`, `ConnectionFailed`, `BindingFailed`, `ValidationFailed` | True when the user exists and all desired roles are granted. |
| `RolesSynced` | `RolesMatch`, `RolesPending` | True when `grantedRoles` covers `.spec.roles`. |
| `PendingDependencies` | `RolesPending`, `AllDependenciesPresent` | True when one or more `.spec.roles` correspond to a Neo4jRole CR that does not yet exist. |
| `UserNotFound` | `UserNotFound`, `UserPresent` | True when the user named by `.spec.username` is absent from Neo4j. |
| `ClusterNotReady` | `ClusterNotReady`, `ClusterReady` | Mirrors the readiness of the referenced cluster. |

## Validation rules

- `clusterRef` must resolve to a cluster or standalone in the same namespace.
- `username` matches `^[a-zA-Z][a-zA-Z0-9_.\-]*$`, max 65 characters; reserved names rejected.
- `roles` must be non-empty (kubebuilder MinItems=1) and contain no empty strings.
- A `Neo4jUser` in the same namespace targeting the same `clusterRef` and `username` is rejected at validate-time. (Manage role grants there instead.)

## Lifecycle

| CR Event | Cypher Issued |
|---|---|
| Create | `GRANT ROLE r TO username` for each role in `.spec.roles` (skipping built-ins not yet listed in current grants is fine; PUBLIC is always skipped) |
| Update (`.spec.roles` changes) | `GRANT` for additions; `REVOKE` for removals from `.spec.roles` *and* roles previously listed in `status.grantedRoles` |
| Update (`enforceExclusive: true`) | Same as above + revoke any extra roles on the user that are not in `.spec.roles` |
| Delete (`Revoke` policy) | `REVOKE ROLE r FROM username` for each role in `status.grantedRoles`, then remove finalizer |
| Delete (`Retain` policy) | Remove finalizer only |
| User absent | No-op; `UserNotFound` condition; reconcile every 30s until the user appears |
| Cluster not Ready | No-op; `ClusterNotReady` condition; reconcile every 30s |
| Missing custom role | Skip the grant for that role; `PendingDependencies` condition; the binding controller watches `Neo4jRole` and re-reconciles when the role lands |

## Examples

### Bind an SSO user to the editor role

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRoleBinding
metadata:
  name: alice-editor
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: alice@example.com   # provisioned by Neo4j on first OIDC login
  roles:
    - editor
```

### Multiple roles, exclusive enforcement

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRoleBinding
metadata:
  name: alice-baseline
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: alice@example.com
  enforceExclusive: true       # revoke any role not listed below
  roles:
    - editor
    - analytics_reader
```

With `enforceExclusive: true`, if someone grants `admin` to `alice` directly via Cypher, the next reconcile will revoke it.

### Combine with a custom Neo4jRole

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: analytics-reader
  namespace: prod
spec:
  clusterRef: prod-cluster
  privileges:
    - "GRANT ACCESS ON DATABASE analytics TO analytics-reader"
    - "GRANT MATCH {*} ON GRAPH analytics NODES * TO analytics-reader"
---
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRoleBinding
metadata:
  name: alice-analytics
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: alice@example.com
  roles:
    - analytics-reader
```

Apply order doesn't matter — the binding waits in `PendingDependencies` until the role lands.

## Troubleshooting

**`UserNotFound=True`**: the named user doesn't exist in Neo4j yet. With external auth providers (LDAP/OIDC), users are typically provisioned on first login. Either wait for the user to log in, or pre-create them via Neo4j Cypher / a `Neo4jUser` CR (in which case use `Neo4jUser` for the role grants).

**Bound roles are revoked unexpectedly**: check whether `enforceExclusive: true` is set. With exclusive mode, any role not in `.spec.roles` is revoked on every reconcile, even if added by a different process.

**Cannot delete the CR — finalizer hangs**: the controller is trying to revoke roles but cannot reach the cluster. Check the cluster's Ready phase; if the cluster is permanently gone, edit the CR to set `deletionPolicy: Retain` (or remove the finalizer manually only as a last resort).

## See also

- [User & Role Management Guide](../user_guide/user_role_management.md)
- [`Neo4jUser`](neo4juser.md)
- [`Neo4jRole`](neo4jrole.md)
