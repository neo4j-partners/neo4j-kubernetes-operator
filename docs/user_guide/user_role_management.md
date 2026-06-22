# Declarative User & Role Management

This guide walks through managing Neo4j users, roles, and privileges as Kubernetes resources via the `Neo4jUser` and `Neo4jRole` CRDs.

If you've previously bootstrapped users by `kubectl exec`-ing into a pod and running Cypher, this is the GitOps replacement: every user, every role, every privilege expressed as YAML in your repo, reconciled by the operator, drift-corrected automatically.

## Concepts at a glance

| Resource | Owns | Mirrors |
|---|---|---|
| `Neo4jUser` | identity, password, status, home DB, role bindings, external auth providers | `CREATE/ALTER/DROP USER`, `GRANT/REVOKE ROLE` |
| `Neo4jRole` | role existence, privileges (GRANT/DENY) on the role | `CREATE/DROP ROLE`, `GRANT/DENY/REVOKE` |
| `Neo4jRoleBinding` | role grants for users the operator does NOT own (SSO/LDAP first-login users) | `GRANT/REVOKE ROLE` only |

**Three CRDs, one design rule:** privileges live on `Neo4jRole`, not on `Neo4jUser` or `Neo4jRoleBinding`. Putting privileges on users would re-implement Neo4j's RBAC inside-out and create merge conflicts when two CRs touch the same role. Always model "what can be done" as a role and "who can do it" as a user-to-role binding.

All three CRDs are **namespace-scoped** and reference their target Neo4j cluster via `spec.clusterRef` (must be in the same namespace). See [Cluster vs namespace scope](#cluster-vs-namespace-scope) below for the design rationale.

## Prerequisites

- A `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in `Ready` phase.
- The operator running with the `user` and `role` controllers enabled (production mode loads them by default; in dev mode pass `--controllers=cluster,standalone,database,user,role,...`).
- Enterprise edition (role management and `SET STATUS SUSPENDED` are Enterprise-only features).

## Quick start: a read-only user

Three resources, applied in order:

```yaml
# 1. The password lives in a Secret, never in the CR.
apiVersion: v1
kind: Secret
metadata:
  name: analytics-reader-creds
  namespace: prod
type: Opaque
stringData:
  password: "ChangeMe123!"
---
# 2. Optional: a custom role with explicit privileges.
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
---
# 3. The user, bound to the role.
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
    - analytics-reader   # references the Neo4jRole above
```

Apply with `kubectl apply -f`. The operator will:

1. Wait for `prod-cluster` to be `Ready`.
2. Create the role `analytics_reader` and apply the three privileges.
3. Create the user `analytics_reader` with the password from the Secret.
4. Grant `analytics_reader` the role `analytics_reader`.
5. Set status conditions `Ready=True`, `RolesSynced=True`, `PasswordSynced=True` on the user; `Ready=True`, `PrivilegesSynced=True` on the role.

If the role does not yet exist when the user is reconciled, the user enters `PendingDependencies` and waits — when the role lands, the user reconciles automatically.

## Common patterns

### Built-in roles (no Neo4jRole needed)

The six Neo4j built-ins (`reader`, `editor`, `publisher`, `architect`, `admin`, `PUBLIC`) always exist. Bind to them directly without a `Neo4jRole`:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: app-service
  namespace: prod
spec:
  clusterRef: prod-cluster
  passwordSecretRef:
    name: app-service-creds
  roles: [publisher]
```

`PUBLIC` is granted to every user automatically. Listing it has no effect; the controller emits a warning event and skips it.

### Adopting a built-in role to manage its privileges

Built-in role privileges *can* be customised, but the operator refuses by default to prevent accidents. Opt in with `adoptBuiltin: true`:

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
    - "DENY DELETE ON GRAPH * TO editor"   # extra restriction (DELETE = remove graph data; DROP is for schema objects)
```

Adopted built-in roles are never dropped on CR delete; only their privileges are reconciled.

### Password rotation

Update the Secret. The operator detects a change in the password's SHA-256 hash (stored in `status.passwordSecretHash`) and issues `ALTER USER ... SET PASSWORD ...` automatically:

```bash
kubectl create secret generic analytics-reader-creds \
  --from-literal=password='NewStrongPassword!' \
  --dry-run=client -o yaml | kubectl apply -f -
```

The next reconcile (≤30s by default) sets `status.passwordLastRotated` and emits a `PasswordRotated` event.

### Suspending a user (security incident)

```yaml
spec:
  accountStatus: suspended
```

The operator issues `ALTER USER ... SET STATUS SUSPENDED`; the user can no longer authenticate. Role grants in `spec.roles` continue to be reconciled (they are not revoked). Setting `accountStatus: active` issues `SET STATUS ACTIVE` and the user can authenticate again.

### Setting a home database

```yaml
spec:
  homeDatabase: analytics
```

Removing the field after it was set issues `ALTER USER ... REMOVE HOME DATABASE` and reverts to the DBMS default.

### External authentication (LDAP / OIDC / SSO)

Configure the provider at the DBMS level (`dbms.security.authentication_providers` etc.), then bind the user to provider-specific IDs:

```yaml
spec:
  clusterRef: prod-cluster
  username: alice
  externalAuth:
    - provider: oidc-okta
      id: alice@example.com
    - provider: ldap1
      id: "uid=alice,ou=people,dc=example,dc=com"
  # No passwordSecretRef — alice authenticates via OIDC/LDAP only.
  roles: [reader]
```

You may combine `passwordSecretRef` and `externalAuth`; alice will then be authenticatable via either path.

### Bind roles to a user the operator does not own

When a user is provisioned externally — typically by Neo4j on first OIDC/LDAP login, or by a bulk import outside the operator — there is no `Neo4jUser` CR for them. Use `Neo4jRoleBinding` instead:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRoleBinding
metadata:
  name: alice-binding
  namespace: prod
spec:
  clusterRef: prod-cluster
  username: alice@example.com    # SSO-provisioned, not a Neo4jUser CR
  roles: [editor, analytics-reader]
  # enforceExclusive: true       # opt in: revoke any role NOT listed here
```

Key differences from `Neo4jUser`:

- **Never creates or drops the user.** If the user does not exist at reconcile time the binding sits in the `UserNotFound` condition and reconciles automatically when the user appears.
- **Default is non-exclusive.** Other tools or manual grants on the user are tolerated; only the roles named in `.spec.roles` (and roles previously granted by this binding) are managed. Set `enforceExclusive: true` to make the spec authoritative for the user's complete role set.
- **Validator forbids overlap with `Neo4jUser`.** If a `Neo4jUser` in the same namespace targets the same `clusterRef`/`username`, the binding is rejected. Manage role grants in one place.

On CR delete, `deletionPolicy: Revoke` (default) revokes only the roles this binding granted (recorded in `status.grantedRoles`); use `deletionPolicy: Retain` to release the finalizer without revoking.

### Retain on delete

By default, deleting the CR also drops the underlying Neo4j user/role. To detach without dropping (useful during migrations):

```yaml
spec:
  deletionPolicy: Retain
```

The controller will only remove the finalizer; the Neo4j user/role lives on.

### Creating a role from another role

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: junior-editor
  namespace: prod
spec:
  clusterRef: prod-cluster
  copyOf: editor                 # honoured ONLY at creation time
  privileges:
    - "DENY DELETE ON GRAPH * TO junior_editor"
```

`copyOf` is consulted only when the role does not yet exist. Once created, `.privileges` is the source of truth for ongoing reconciliation.

## Privilege drift reconciliation

The role controller treats `.privileges` as the source of truth. On every reconcile it:

1. Reads `SHOW ROLE <name> PRIVILEGES AS COMMANDS` from Neo4j.
2. Canonicalises both the desired set (from spec) and the live set (from Neo4j) — whitespace, case, trailing semicolons all normalised.
3. Applies the difference: missing privileges get a fresh `GRANT/DENY`; extra privileges get a derived `REVOKE`.

If you `kubectl exec` into a pod and run `REVOKE ACCESS ON DATABASE x FROM analytics_reader` directly, the controller will re-apply that grant within ~30 seconds. To opt out per-role:

```yaml
spec:
  enforcePrivileges: false
```

With `enforcePrivileges: false`, the controller still creates the role and applies the initial `.privileges` list, but never revokes anything added out-of-band. Useful when you intend to layer manual privilege overrides on top.

### Immutable privileges

Privileges created with `GRANT IMMUTABLE` cannot be revoked while authentication is enabled. The controller detects these (via the `immutable` column of `SHOW ROLE PRIVILEGES`) and:

- Skips them in the revoke set.
- Emits a `PrivilegesDriftKept` warning event listing each kept privilege.
- Sets `status.privilegeDrift: true` and condition `PrivilegesSynced=False, reason=PrivilegesDrifted`.

This is informational, not fatal — the role's `Ready` condition still reflects whether the *requested* privileges have been applied.

### Attribute-based access control (ABAC)

Where `Neo4jUser` and `Neo4jRoleBinding` map specific usernames to roles, **`Neo4jAuthRule`** maps *anyone whose OIDC token matches a condition* to roles. It's the operator's binding for Neo4j's [attribute-based access control](https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/), introduced in Neo4j 2026.03.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jAuthRule
metadata:
  name: emea-business-hours
spec:
  clusterRef: production
  name: emea_business_hours
  condition: |
    abac.oidc.user_attribute('region') = 'EMEA'
      AND time.transaction('UTC').hour >= 6
      AND time.transaction('UTC').hour < 18
  grantedRoles:
    - reader
```

**Prerequisites** before any `Neo4jAuthRule` will reach `Ready`:

1. The cluster runs Neo4j 2026.03 or later. Older clusters cause the rule to sit in `AuthRuleVersionTooOld=True`.
2. The cluster's `spec.config` sets `dbms.security.abac.authorization_providers` to a configured OIDC provider name. The operator surfaces this as `OIDCProviderConfigured=True/False` on the rule's status; it does not auto-edit the cluster spec.
3. Each role in `spec.grantedRoles` exists as a `Neo4jRole` in the same namespace, or directly in Neo4j. Missing roles park the rule in `PendingDependencies=True` until they land.

**Drift reconciliation**: the controller reads `SHOW AUTH RULES` and converges. Editing the condition out-of-band, disabling the rule, or attaching extra role grants are all reverted on the next reconcile (set `enforceRoles: false` on the spec to stop revoking out-of-band grants).

> **Manual queries need a `CYPHER 25` prefix.** AUTH RULE syntax is only parsed under Cypher 25. Neo4j 2026.x defaults the system database to Cypher 5, so a hand-typed `kubectl exec … cypher-shell -- "SHOW AUTH RULES"` returns `42I06: Invalid input 'AUTH'` even when the cluster is fully configured. The operator prefixes its own AUTH RULE statements automatically; for ad-hoc diagnostics, prepend `CYPHER 25` yourself:
>
> ```bash
> kubectl exec mycluster-server-0 -- cypher-shell --format plain -u neo4j -p ... \
>   "CYPHER 25 SHOW AUTH RULES YIELD name, condition, enabled, roles RETURN name, condition, enabled, roles"
> ```
>
> Alternatively, set the system DB's default language permanently with `ALTER DATABASE system SET DEFAULT LANGUAGE CYPHER 25`.

See the [`Neo4jAuthRule` API reference](../api_reference/neo4jauthrule.md) for the full spec, condition syntax, and limitations.

### Property-based access control (PBAC)

`Neo4jRole.spec.privileges` accepts the full Cypher privilege grammar, including the `FOR pattern WHERE …` clause used by [property-based access control](https://neo4j.com/docs/operations-manual/current/authentication-authorization/property-based-access-control/). PBAC refines `MATCH`, `READ`, and `TRAVERSE` privileges with per-row conditions on node or relationship properties:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: redacted-reader
spec:
  clusterRef: production
  name: redacted_reader
  privileges:
    - "GRANT TRAVERSE ON GRAPH * FOR (n:Email) WHERE n.classification IS NOT NULL TO redacted_reader"
    - "DENY READ {*} ON GRAPH * FOR (n) WHERE NOT n.classification IN ['UNCLASSIFIED', 'PUBLIC'] TO redacted_reader"
```

PBAC privileges flow through the same drift-reconciliation loop as ordinary privileges. The role validator rejects PBAC privileges that name a `Neo4jShardedDatabase` (PBAC is unsupported on sharded property databases) and warns when `ON GRAPH *` is combined with a PBAC `FOR pattern WHERE …` clause, since the privilege would silently no-op against any sharded DBs in scope. See the [`Neo4jRole` API reference](../api_reference/neo4jrole.md#property-based-access-control-pbac) for examples and the full list of upstream limitations (single-property rules, performance overhead, property-immutability requirement).

## Status conditions reference

### `Neo4jUser`

| Condition | Meaning |
|---|---|
| `Ready` | User exists in Neo4j, password and roles in sync |
| `RolesSynced` | Granted roles equal `spec.roles` (PUBLIC excluded) |
| `PasswordSynced` | Last-applied password hash matches the Secret |
| `PendingDependencies` | One or more `spec.roles` reference custom roles that don't yet exist |
| `ClusterNotReady` | `spec.clusterRef` exists but is not in `Ready` phase |

### `Neo4jRole`

| Condition | Meaning |
|---|---|
| `Ready` | Role exists, privileges in sync |
| `PrivilegesSynced` | Live privileges match `spec.privileges` (immutable extras excluded) |
| `ClusterNotReady` | `spec.clusterRef` exists but is not in `Ready` phase |

### `Neo4jRoleBinding`

| Condition | Meaning |
|---|---|
| `Ready` | User exists and all desired roles are granted |
| `RolesSynced` | `status.grantedRoles` covers `spec.roles` |
| `UserNotFound` | The named user does not exist in Neo4j (waiting for SSO/LDAP first-login) |
| `PendingDependencies` | One or more `spec.roles` reference a custom role that doesn't exist yet |
| `ClusterNotReady` | `spec.clusterRef` exists but is not in `Ready` phase |

The `kubectl get` printer columns surface the most actionable bits:

```bash
$ kubectl get neo4jusers -A
NAMESPACE  NAME                CLUSTER       USERNAME            ACCOUNTSTATUS  PHASE   READY  AGE
prod       analytics-reader    prod-cluster  analytics_reader    active         Ready   True   3m

$ kubectl get neo4jroles -A
NAMESPACE  NAME              CLUSTER       PHASE   READY  DRIFT  AGE
prod       analytics-reader  prod-cluster  Ready   True   false  3m
```

## Lifecycle and ordering

| Event | What happens |
|---|---|
| Apply CR | Controller adds finalizer; creates user/role on next reconcile |
| Update spec | Diffed against live state; only changed fields trigger Cypher |
| Update Secret | Password hash changes → `ALTER USER SET PASSWORD` |
| `kubectl delete` | Finalizer-protected: controller drops user/role first (unless `deletionPolicy: Retain`) |
| Cluster not Ready | `ClusterNotReady` condition; reconcile requeued every 30s |
| Referenced role missing | `PendingDependencies` condition; requeue when role lands |

The user controller watches `Neo4jRole` resources and re-reconciles bound users when a role is created or updated. You don't need to apply CRs in any particular order — the operator converges.

## Cluster vs namespace scope

All three CRDs are **namespace-scoped** with same-namespace `clusterRef` only. This matches the existing pattern of `Neo4jDatabase`, `Neo4jBackup`, `Neo4jPlugin`. It means:

- A team that owns a namespace owns its Neo4j cluster, users, and roles.
- Standard `Role` + `RoleBinding` patterns apply — no `ClusterRole` required.
- Reuse of role definitions across clusters is achieved via Kustomize / Helm templating at the manifest layer, not by sharing a single CR.

If you need a true multi-tenant pattern (one shared Neo4j cluster, per-team user manifests in team namespaces), open an issue — the design has a documented extension path that has been deliberately deferred until there is demand.

## RBAC for the CRDs themselves

Grant teams permission to manage their users without granting cluster admin:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: neo4j-user-manager
  namespace: prod
rules:
  - apiGroups: ["neo4j.neo4j.com"]
    resources: ["neo4jusers", "neo4jroles"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
```

The team can now manage users and roles for the cluster in their namespace, but cannot modify the cluster's infrastructure spec.

## Troubleshooting

**Symptom**: `PendingDependencies` condition stuck on a `Neo4jUser`.

Check that the referenced custom role's `Neo4jRole` CR exists in the same namespace and points at the same `clusterRef`. Built-in role names are case-sensitive (`reader`, not `Reader`).

**Symptom**: Password updates not picked up.

Confirm the Secret's `data.<key>` (default `password`) actually changed; `kubectl describe` the user and look for the `PasswordRotated` event. The controller hashes the bytes — re-applying an identical secret value is a no-op.

**Symptom**: `validation failed: ... privilege statement must end with TO <role>`.

Each entry in `Neo4jRole.spec.privileges` must end with `TO <spec.name>` so the operator can derive the matching `REVOKE`. The role name must match exactly (case-sensitive).

**Symptom**: `cannot revoke immutable privilege` warning.

Some privileges were created with `GRANT IMMUTABLE` outside the operator and cannot be removed while auth is enabled. Either change `.privileges` to include them (so they no longer count as drift) or set `enforcePrivileges: false`.

**Symptom**: Cluster `Ready` but operator can't connect.

The user/role controllers reuse the same connection helper as `Neo4jDatabase`. If `Neo4jDatabase` works against the cluster, these will too. If neither works, check the cluster's `spec.auth.adminSecret` and TLS configuration.

**Symptom**: `RoleSyncFailed` events with `Neo.ClientError.Cluster.NotALeader` on a multi-server cluster.

Admin commands (`GRANT`, `REVOKE`, `CREATE/DROP ROLE`, etc.) must execute on the cluster leader. The operator uses the Neo4j routing scheme (`neo4j://`/`neo4j+s://`) so the driver auto-routes writes to the leader; if you see `NotALeader` errors anyway, the most likely cause is a stuck routing table on the operator's Bolt client (e.g. immediately after a manual leader rotation). The next reconcile (≤30s) refreshes the routing table and the operation succeeds.

If the errors are persistent — not transient — check that `dbms.routing.getRoutingTable` is reachable from the operator pod (it normally is for any Enterprise 5.26+ cluster). Older operator versions used the direct `bolt://` scheme and produced this error symptom continuously on multi-server clusters; if you see persistent `NotALeader` events, ensure the operator image is up to date.

## Limits and non-goals

- **Cluster admin user safety**: the operator refuses to manage usernames matching reserved keywords (`system`). The bootstrap admin user (defined by `cluster.spec.auth.adminSecret`) is technically manageable, but doing so is risky — a misconfigured `Neo4jUser` could lock the operator out of its own cluster. Prefer leaving it alone.
- **Auto-generated passwords**: not supported in v1; you must provide a Secret. (Tracked as a future enhancement.)
- **Cypher-injection of role/user names**: the operator quotes all identifiers with backticks and uses parameters for password and provider IDs. Special characters in names are safe.
- **Cross-cluster role reuse via a single CR**: not supported. Use Kustomize / Helm to template the same `Neo4jRole` into multiple namespaces.

## See also

- [`Neo4jUser` API reference](../api_reference/neo4juser.md)
- [`Neo4jRole` API reference](../api_reference/neo4jrole.md)
- [`Neo4jRoleBinding` API reference](../api_reference/neo4jrolebinding.md)
- [Neo4j docs: authentication and authorization](https://neo4j.com/docs/operations-manual/current/authentication-authorization/)
- [Neo4j docs: managing users](https://neo4j.com/docs/operations-manual/current/authentication-authorization/manage-users/)
- [Neo4j docs: managing roles](https://neo4j.com/docs/operations-manual/current/authentication-authorization/manage-roles/)
- [Neo4j docs: managing privileges](https://neo4j.com/docs/operations-manual/current/authentication-authorization/manage-privileges/)
