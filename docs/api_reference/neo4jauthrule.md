# Neo4jAuthRule

Declarative attribute-based access control (ABAC) for the Neo4j Kubernetes Operator. A `Neo4jAuthRule` represents a Neo4j `AUTH RULE` that conditionally grants one or more roles when the user's OIDC token claims satisfy a Cypher condition expression.

## Overview

- **API version**: `neo4j.neo4j.com/v1beta1`
- **Kind**: `Neo4jAuthRule`
- **Scope**: Namespaced
- **Short names**: `n4jauthrule`, `n4jauthrules`
- **Categories**: `neo4j`
- **Supported Neo4j Versions**: 2026.03 and later (CalVer; ABAC is unavailable on 5.26 LTS or any earlier CalVer release)
- **Reconciliation**: Auth rule existence, condition, enabled flag, and granted-roles drift via `SHOW AUTH RULES`

ABAC complements role-based access control (RBAC). RBAC determines *what* a role can do; ABAC determines *who gets the role* — the binding from external identity (OIDC claims) to internal roles is expressed declaratively rather than per-user.

See the upstream Neo4j docs for the full feature: <https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/>.

## Prerequisites

Before applying a `Neo4jAuthRule`:

1. The referenced cluster must run Neo4j **2026.03 or later**. The reconciler refuses to operate against older clusters and surfaces `AuthRuleVersionTooOld=True` on the rule's status.
2. The cluster's `spec.config` must set `dbms.security.abac.authorization_providers` to the name of at least one configured OIDC provider. Without it, every rule sits in `OIDCProviderConfigured=False`. The operator does **not** auto-set this key — it's the cluster owner's decision which provider(s) to wire up.
3. Each role listed in `spec.grantedRoles` must exist (as a `Neo4jRole` in the same namespace, or directly in Neo4j). Rules referencing a missing role enter `PendingDependencies=True` and reconcile automatically once the role lands.

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required.** Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` in the same namespace. |
| `name` | `string` | Auth rule name as it appears in `SHOW AUTH RULES`. Defaults to `metadata.name`. Must match `^[a-zA-Z][a-zA-Z0-9_-]*$` and be ≤ 65 characters. |
| `condition` | `string` | **Required.** Cypher expression evaluated against the user's OIDC token at authentication time. Must be a pure expression — DDL keywords (`CREATE`, `DROP`, `ALTER`, `GRANT`, `DENY`, `REVOKE`, `SHOW`, `RENAME`) and statement separators (`;`) are rejected by the validator. |
| `enabled` | `*bool` | Whether the rule actively grants roles. Defaults to `true`. Setting `false` preserves the rule but disables it. |
| `grantedRoles` | `[]string` | **Required.** Roles to grant when the condition evaluates true. Must contain at least one role. |
| `enforceRoles` | `boolean` | Reconcile drift on the rule's role list. Defaults to `true`. When `false`, the controller adds missing roles but never revokes anything attached out-of-band. |
| `deletionPolicy` | `string` | `Drop` (default) executes `DROP AUTH RULE` on CR deletion; `Retain` leaves the rule in Neo4j and only releases the finalizer. |

## Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | `Pending`, `Ready`, `Failed`, or `PendingDependencies`. |
| `message` | `string` | Short human-readable explanation of `phase`. |
| `observedGeneration` | `int64` | `metadata.generation` observed at the last successful reconcile. |
| `appliedRoles` | `[]string` | Roles last observed as granted via `SHOW AUTH RULES`. Sorted ascending. |
| `appliedEnabled` | `*bool` | Enabled flag observed on the rule. |
| `conditions[]` | `metav1.Condition` | See below. |

### Conditions

| Type | Meaning |
|---|---|
| `Ready` | Rule exists with the desired condition, enabled flag, and granted roles. |
| `RolesSynced` | Granted roles match `spec.grantedRoles`. |
| `OIDCProviderConfigured` | Cluster has `dbms.security.abac.authorization_providers` set. |
| `AuthRuleVersionTooOld` | Cluster version is below 2026.03; reconcile is paused. |
| `ClusterNotReady` | Cluster has not reached its `Ready` phase. |
| `PendingDependencies` | One or more `grantedRoles` do not yet exist. |

## Cypher condition syntax

Conditions are arbitrary Cypher expressions that resolve to a boolean. The most useful primitive is:

```
abac.oidc.user_attribute('<claim_key>')
```

This returns the value of the named claim from the user's OIDC token. Conditions can combine claim lookups with operators (`=`, `IN`, `<`, `>`, `<=`, `>=`, `<>`, `IS NULL`), boolean connectives (`AND`, `OR`, `NOT`), and a curated set of Cypher functions:

- **Predicates**: `all`, `any`, `none`, `single`, `isEmpty`
- **String**: `split`, `substring`, `trim`, `upper`, `lower`, `replace`
- **Numeric**: `abs`, `ceil`, `floor`, `round`, `sign`, `isNaN`
- **Lists**: `range`, `reduce`, `reverse`, `tail`, `toBooleanList`, `toFloatList`, `toIntegerList`, `toStringList`
- **Temporal**: `date`, `time`, `datetime` and their `*.transaction()` variants for server-side time
- **Scalar**: `coalesce`, `size`, `toBoolean`, `toInteger`, `toFloat`, `toString`

Conditions cannot contain DDL — the operator rejects them defensively before sending to Neo4j.

## Examples

### Simple attribute matching

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jAuthRule
metadata:
  name: sales-team
spec:
  clusterRef: production
  name: sales_team
  condition: abac.oidc.user_attribute('department') = 'sales'
  grantedRoles:
    - reader
```

### Multiple conditions (AND)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jAuthRule
metadata:
  name: engineering-uk
spec:
  clusterRef: production
  name: engineering_uk
  condition: |
    abac.oidc.user_attribute('department') = 'engineering'
    AND abac.oidc.user_attribute('location') = 'UK'
  grantedRoles:
    - editor
```

### List membership

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jAuthRule
metadata:
  name: country-access
spec:
  clusterRef: production
  name: country_access
  condition: |
    any(country IN abac.oidc.user_attribute('citizenshipCountries')
        WHERE country IN ['US', 'GB', 'DE'])
  grantedRoles:
    - reader
```

### Time-bounded role grants

Combine claim matching with `time.transaction('UTC')` to grant a role only during business hours:

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

## Drift reconciliation

The reconciler reads `SHOW AUTH RULES` on every loop and converges any drift:

- **Condition or enabled flag changed in Neo4j** → `CREATE OR REPLACE AUTH RULE …` re-applies the spec. (CREATE OR REPLACE clears existing role grants, so the controller re-grants the spec list immediately afterwards.)
- **Role attached out-of-band** with `enforceRoles: true` → revoked.
- **Role missing from Neo4j** → granted.
- **Rule disabled out-of-band** with `spec.enabled: true` → re-enabled.

Set `enforceRoles: false` to disable the revoke step (additions still happen) — useful when layering manual grants on top.

## Lifecycle

- **Create**: the controller adds the `neo4j.com/authrule-finalizer`, runs `CREATE OR REPLACE AUTH RULE`, then grants `spec.grantedRoles`.
- **Update**: edits to `spec` trigger drift reconciliation as described above.
- **Delete (`Drop`, default)**: `DROP AUTH RULE … IF EXISTS` on the cluster, finalizer released. If the cluster is gone or downgraded below 2026.03, the finalizer is released without remote action.
- **Delete (`Retain`)**: finalizer released; rule left in Neo4j.

## Limitations

- **OIDC only.** Neo4j's `abac.oidc.user_attribute()` reads from OIDC tokens; LDAP and Kerberos providers are not supported by the upstream feature.
- **Cluster-side OIDC config is the user's responsibility.** The operator detects whether `dbms.security.abac.authorization_providers` is set but does not edit the cluster's `spec.config`. Wire up the cluster's OIDC provider in `Neo4jEnterpriseCluster.spec.config` before applying any `Neo4jAuthRule`.
- **Roles with DENY privileges cannot be granted.** Neo4j refuses to attach deny-bearing roles to auth rules to prevent privilege escalation if a rule unexpectedly fails. The operator surfaces the resulting cypher error on the rule's `Failed` status; verify the granted role's privileges contain only `GRANT` statements.
- **Existing sessions don't pick up rule changes.** Newly-applied rules take effect at the next OIDC authentication for each user; running sessions retain the role set evaluated at their original login.
- **PUBLIC role.** Neo4j auto-grants PUBLIC; the operator does not list it in `appliedRoles` and ignores it during diff.

## Troubleshooting

### Status conditions

**`AuthRuleVersionTooOld=True`**: the cluster runs a Neo4j version older than 2026.03. Either upgrade the cluster image or remove the `Neo4jAuthRule` resource.

**`OIDCProviderConfigured=False`**: the cluster's `spec.config` does not set `dbms.security.abac.authorization_providers`. Edit the `Neo4jEnterpriseCluster` to add the key with the configured OIDC provider name as its value.

**`PendingDependencies=True`**: one or more `grantedRoles` don't exist. Either create them as `Neo4jRole` resources in the same namespace, or remove them from `spec.grantedRoles`. The rule reconciles automatically when the missing roles land.

### Errors in `spec.condition`

Cypher errors fall into three categories, surfaced differently:

**1. Validator-rejected (status `Failed`, no Cypher run)**

The validator rejects conditions containing DDL keywords (`CREATE`, `DROP`, `ALTER`, `GRANT`, `DENY`, `REVOKE`, `SHOW`, `RENAME`) or statement separators (`;`). Surfaces as:

```bash
kubectl describe neo4jauthrule emea-business-hours
# Status:
#   Phase:    Failed
#   Message:  validation failed: spec.condition: Invalid value: "...": condition must be a pure expression; "CREATE" is not allowed (auth rule conditions cannot contain DDL or multiple statements)
# Events:
#   Warning  ValidationFailed  ...
```

Fix: rewrite the condition as a pure expression that resolves to a boolean. The supported Cypher function set is documented [upstream](https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/).

**2. Neo4j-rejected (status `Failed`, Cypher attempted)**

Conditions that pass the validator but fail Neo4j's parser — typos, unbalanced parentheses, type mismatches, calls to functions outside the allowed set — surface when the controller runs `CREATE OR REPLACE AUTH RULE`:

```bash
kubectl describe neo4jauthrule emea-business-hours
# Status:
#   Phase:    Failed
#   Message:  CREATE OR REPLACE AUTH RULE failed: failed to create or replace auth rule emea_business_hours: Neo.ClientError.Statement.SyntaxError: Invalid input '=': expected ...
# Events:
#   Warning  AuthRuleFailed  CREATE OR REPLACE AUTH RULE failed: ...
```

The full Neo4j error appears in both `status.message` and the `AuthRuleFailed` event. Common causes:

- Missing quotes around string literals: `abac.oidc.user_attribute(department)` instead of `abac.oidc.user_attribute('department')`.
- Comparing a list claim with `=` instead of using `IN` or `any()`.
- Calling functions outside the supported set (most useful are `any`, `all`, `none`, `single`, `coalesce`, `size`, `time.transaction()`, `date()`).

The controller will retry the failing CREATE every 30 seconds until you fix the spec. If you need to silence the retries while diagnosing, set `spec.enabled: false` (which preserves the rule but disables it) — but in the failing case the rule may not exist at all yet, so editing or deleting the CR is usually the right move.

**3. Runtime evaluation errors (rule `Ready`, but auth fails)**

A condition can be syntactically valid yet fail at evaluation time — for example, calling `.hour` on a claim that's `NULL` for some users, or `IN` against a non-list. The rule's `status.phase` stays `Ready` because the rule is correctly *installed*; the symptom is that *some* users fail to authenticate.

These errors are not visible to the operator. To diagnose:

```bash
# Neo4j's security log captures auth failures with the rule that triggered them.
kubectl exec <neo4j-server-pod> -c neo4j -- tail -50 /logs/security.log
```

Mitigate runtime errors with defensive Cypher:

```yaml
# Use coalesce() to default missing claims:
condition: coalesce(abac.oidc.user_attribute('region'), '') = 'EMEA'

# Check list-ness before iterating:
condition: |
  abac.oidc.user_attribute('countries') IS NOT NULL
  AND any(c IN abac.oidc.user_attribute('countries') WHERE c = 'US')
```

## See also

- [User & Role Management Guide](../user_guide/user_role_management.md) — end-to-end walkthrough for `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`, and `Neo4jAuthRule`.
- [`Neo4jRole`](neo4jrole.md) — the role being granted.
- [Upstream ABAC reference](https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/)
