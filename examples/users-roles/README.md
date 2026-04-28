# Users & Roles examples

End-to-end YAML samples for the `Neo4jUser` and `Neo4jRole` CRDs. See the [User & Role Management Guide](../../docs/user_guide/user_role_management.md) for the full walkthrough.

| File | Demonstrates |
|---|---|
| [`01-readonly-user.yaml`](01-readonly-user.yaml) | The minimum: a Secret + a `Neo4jUser` bound to the built-in `reader` role. |
| [`02-custom-role-with-user.yaml`](02-custom-role-with-user.yaml) | Canonical pattern — a `Neo4jRole` carrying privileges, plus a `Neo4jUser` bound to it. |
| [`03-suspended-user.yaml`](03-suspended-user.yaml) | Setting `accountStatus: suspended` to disable a user without dropping it. |
| [`04-external-auth.yaml`](04-external-auth.yaml) | A user authenticatable only via OIDC + LDAP (no native password). |
| [`05-adopt-builtin.yaml`](05-adopt-builtin.yaml) | Adopting the built-in `editor` role with `adoptBuiltin: true` to tighten its privileges. |
| [`06-rolebinding-sso-user.yaml`](06-rolebinding-sso-user.yaml) | Bind an externally-provisioned SSO user to roles via `Neo4jRoleBinding` (the user is NOT a `Neo4jUser` CR). |

All examples assume a `Neo4jEnterpriseCluster` named `prod-cluster` exists in the `prod` namespace. Replace the cluster name and namespace as appropriate.
