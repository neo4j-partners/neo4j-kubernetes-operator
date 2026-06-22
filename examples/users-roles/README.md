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
| [`07-authrule-abac.yaml`](07-authrule-abac.yaml) | Attribute-based access control (`Neo4jAuthRule`) mapping OIDC/LDAP claims to roles. |

## Prerequisites

All examples assume a `Neo4jEnterpriseCluster` named `prod-cluster` exists in the
`prod` namespace, and that the namespace itself exists. The examples do **not**
create either — set them up first (replace the names/namespace as appropriate):

```bash
kubectl create namespace prod
# Deploy a cluster named prod-cluster into it (see ../clusters/), then create
# its admin secret, etc.
```

The user-password Secrets referenced by `spec.passwordSecretRef` (examples 01 and
02) are likewise created out-of-band — each file's header shows the
`kubectl create secret generic …` command. They are intentionally **not** bundled
inline: `kubectl apply -f` would re-apply and overwrite them on every run.
