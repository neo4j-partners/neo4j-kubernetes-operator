# Security Best Practices Guide

This comprehensive guide covers security best practices for Neo4j clusters deployed with the Neo4j Kubernetes Operator, including authentication, authorization, network security, and compliance considerations.

## Overview

Security for Neo4j in Kubernetes involves multiple layers:
- **Cluster Security**: TLS encryption, certificates, and secure communication
- **Authentication**: User management and identity providers
- **Authorization**: Role-based access control (RBAC)
- **Network Security**: Network policies, ingress, and service mesh
- **Data Security**: Encryption at rest, backup security, and data governance
- **Compliance**: Auditing, logging, and regulatory requirements

## TLS/SSL Configuration

### Certificate Management with cert-manager

The operator integrates with cert-manager for automated certificate lifecycle management:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: secure-cluster
spec:
  # TLS configuration with cert-manager
  tls:
    mode: cert-manager
    issuerRef:
      name: production-issuer    # Your production ClusterIssuer
      kind: ClusterIssuer

    # Certificate duration and renewal settings
    duration: "8760h"          # 1 year
    renewBefore: "2160h"       # 90 days before expiry

    # Certificate subject fields
    subject:
      organizations:
        - "Your Organization"
      organizationalUnits:
        - "Database Team"
```

### Production TLS Setup

```yaml
# Production ClusterIssuer (using Let's Encrypt or internal CA)
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: production-issuer
spec:
  ca:
    secretName: ca-key-pair     # Your internal CA certificate
---
# Neo4j cluster with production TLS
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: production-issuer
      kind: ClusterIssuer

  config:
    # SSL policy configuration
    dbms.ssl.policy.https.enabled: "true"
    dbms.ssl.policy.https.base_directory: "/ssl"
    dbms.ssl.policy.https.client_auth: "REQUIRE"

    # Bolt SSL configuration
    dbms.ssl.policy.bolt.enabled: "true"
    dbms.ssl.policy.bolt.base_directory: "/ssl"
    dbms.ssl.policy.bolt.client_auth: "REQUIRE"

    # Cluster SSL (for server-to-server communication)
    dbms.ssl.policy.cluster.enabled: "true"
    dbms.ssl.policy.cluster.base_directory: "/ssl"
    dbms.ssl.policy.cluster.trust_all: "false"   # Validate peer certificates against the cluster truststore at /ssl/cluster/trusted/. When `tls.mode: cert-manager` is set on the cluster spec, the operator issues the cert via cert-manager, mounts the resulting Secret under /ssl/, and writes the CA bundle into /ssl/cluster/trusted/. Set to "true" only for development.
```

## Authentication Configuration

The operator provides first-class, typed configuration for external authentication providers. When you use the typed fields (described below), the operator automatically generates the correct `neo4j.conf` entries — you do not need to manually place `dbms.security.*` keys in `spec.config`.

**Where to start:**
- **Most deployments**: [Native Authentication](#native-authentication) — just create an admin secret and go.
- **Corporate SSO**: [OIDC / SSO Integration](#oidc-sso-integration) — Okta, Azure AD, Google, or any OIDC provider.
- **Active Directory / LDAP**: [LDAP Integration](#ldap-integration) — bind templates, group mapping, nested groups.
- **Internal CAs**: [JVM TrustStore](#jvm-truststore-for-internal-cas) — if your LDAP/OIDC endpoints use certificates signed by a private CA.

### Multi-Provider Support

Neo4j evaluates authentication providers in order. You can combine providers for fallback (e.g., LDAP first, then native for the initial admin user):

```yaml
spec:
  auth:
    authenticationProviders:    # Ordered list — tried in sequence
      - ldap
      - native
    authorizationProviders:     # Can differ from authentication
      - ldap
      - native
    adminSecret: neo4j-admin-secret
```

For OIDC providers, reference them as `oidc-<name>` where `<name>` matches a key in the `oidc` map:

```yaml
    authenticationProviders:
      - oidc-okta
      - native
```

If you omit `authenticationProviders`, the operator defaults to `["native"]`.

### Native Authentication

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: secure-cluster
spec:
  auth:
    # When authenticationProviders is omitted, defaults to ["native"]
    adminSecret: neo4j-admin-secret
    authCacheTTL: "10m"
```

The admin secret must contain `username` and `password` keys:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: neo4j-admin-secret
type: Opaque
stringData:
  username: neo4j
  password: "MySecurePassword123!"
```

### LDAP Integration

The `spec.auth.ldap` block provides typed fields that map directly to Neo4j's `dbms.security.ldap.*` configuration keys. The operator generates the correct config automatically.

#### Active Directory Example

```yaml
spec:
  auth:
    authenticationProviders: [ldap, native]
    authorizationProviders: [ldap, native]
    adminSecret: neo4j-admin-secret
    ldap:
      host: "ldaps://ad.corp.example.com:636"
      authentication:
        userDNTemplate: "{0}@corp.example.com"       # UPN-style for AD
        cacheEnabled: true
      authorization:
        userSearchBase: "dc=corp,dc=example,dc=com"
        userSearchFilter: "(&(objectClass=user)(sAMAccountName={0}))"
        groupMembershipAttributes: [memberOf]
        groupToRoleMapping:
          "cn=Neo4j Admins,ou=Groups,dc=corp,dc=example,dc=com": "admin"
          "cn=Neo4j Devs,ou=Groups,dc=corp,dc=example,dc=com": "editor,publisher"
          "cn=Neo4j Readers,ou=Groups,dc=corp,dc=example,dc=com": "reader"
        accessPermittedGroup: "cn=Neo4j Users,ou=Groups,dc=corp,dc=example,dc=com"
        useSystemAccount: true
        systemAccountSecretRef: ldap-system-account   # Secret with username + password keys
```

#### OpenLDAP Example

```yaml
    ldap:
      host: "ldap://ldap.example.com:389"
      useStartTLS: true                               # STARTTLS with ldap:// scheme
      authentication:
        userDNTemplate: "uid={0},ou=users,dc=example,dc=com"
      authorization:
        userSearchBase: "ou=users,dc=example,dc=com"
        userSearchFilter: "(&(objectClass=*)(uid={0}))"
        groupMembershipAttributes: [gidNumber]
        groupToRoleMapping:
          "501": "admin"
          "502": "reader"
```

#### Nested Groups (Active Directory)

```yaml
    ldap:
      host: "ldaps://ad.corp.example.com:636"
      authentication:
        userDNTemplate: "cn={0},cn=Users,dc=example,dc=com"
      authorization:
        userSearchBase: "dc=example,dc=com"
        userSearchFilter: "(&(objectClass=*)(uid={0}))"
        nestedGroupsEnabled: true
        nestedGroupsSearchFilter: "(&(objectclass=group)(member:1.2.840.113556.1.4.1941:={0}))"
        groupToRoleMapping:
          "cn=Neo4j Admins,dc=example,dc=com": "admin"
```

#### LDAP System Account Secret

When `useSystemAccount: true`, you must create a Secret with the bind credentials. The operator injects these as environment variables — they never appear in the ConfigMap.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ldap-system-account
type: Opaque
stringData:
  username: "cn=neo4j-svc,cn=Users,dc=corp,dc=example,dc=com"   # Full DN
  password: "LdapServicePassword123"
```

#### LDAP Field Reference

| Field | Neo4j Config Key | Description |
|-------|-----------------|-------------|
| `ldap.host` | `dbms.security.ldap.host` | LDAP server URL (`ldap://` or `ldaps://`) |
| `ldap.useStartTLS` | `dbms.security.ldap.use_starttls` | Use STARTTLS with `ldap://` (typically port 389) to upgrade the connection to TLS after connection. Use `ldaps://` (typically port 636) for immediate TLS negotiation at connect time. These approaches are mutually exclusive; do not enable STARTTLS when using `ldaps://`. |
| `ldap.authentication.userDNTemplate` | `dbms.security.ldap.authentication.user_dn_template` | DN template, `{0}` = username |
| `ldap.authentication.searchForAttribute` | `dbms.security.ldap.authentication.search_for_attribute` | Use attribute search instead of DN template |
| `ldap.authentication.attribute` | `dbms.security.ldap.authentication.attribute` | Attribute to search (e.g., `samaccountname`) |
| `ldap.authentication.cacheEnabled` | `dbms.security.ldap.authentication.cache_enabled` | Cache auth results |
| `ldap.authorization.userSearchBase` | `dbms.security.ldap.authorization.user_search_base` | Base DN for user search |
| `ldap.authorization.userSearchFilter` | `dbms.security.ldap.authorization.user_search_filter` | LDAP filter, `{0}` = username |
| `ldap.authorization.groupMembershipAttributes` | `dbms.security.ldap.authorization.group_membership_attributes` | Attributes with group membership |
| `ldap.authorization.groupToRoleMapping` | `dbms.security.ldap.authorization.group_to_role_mapping` | Map of LDAP groups to Neo4j roles |
| `ldap.authorization.accessPermittedGroup` | `dbms.security.ldap.authorization.access_permitted_group` | Restrict access to this group |
| `ldap.authorization.useSystemAccount` | `dbms.security.ldap.authorization.use_system_account` | Use system account for lookups |
| `ldap.authorization.systemAccountSecretRef` | *(env var injection)* | Secret with system account credentials |
| `ldap.authorization.nestedGroupsEnabled` | `dbms.security.ldap.authorization.nested_groups_enabled` | Recursive group resolution |
| `ldap.authorization.nestedGroupsSearchFilter` | `dbms.security.ldap.authorization.nested_groups_search_filter` | Filter for nested groups |
| `ldap.debugGroupLogging` | `dbms.security.logs.ldap.groups_at_debug_level_enabled` | Debug logging (disable in production) |

### OIDC / SSO Integration

> **Prerequisite (Neo4j 2026.x): OIDC endpoints must use HTTPS.** Configure TLS for your identity provider (or place it behind a TLS-terminating proxy) before enabling OIDC. Neo4j validates every OIDC URI at config-parse time; any `http://` value causes startup to fail with `Error evaluating value for setting … does not have required scheme 'https'`. There is no insecure-mode override.

The operator supports one or more OIDC providers via the `spec.auth.oidc` map. Each key becomes the provider name in Neo4j's config (`dbms.security.oidc.<name>.*`).

> **⚠️ Neo4j 2026.x requires HTTPS for every OIDC URI.** `wellKnownDiscoveryURI`, `authEndpoint`, `tokenEndpoint`, `jwksURI`, `userInfoURI`, and `issuer` are all validated at config-parse time; an `http://` value causes Neo4j to refuse to start with `Error evaluating value for setting … does not have required scheme 'https'`. There is no insecure-mode override. Self-hosted IDPs that default to HTTP in dev need a TLS-terminating proxy (and the proxy's CA in [`spec.trustedCASecrets`](#jvm-truststore-for-internal-cas)) before the cluster can boot.

#### OIDC and ABAC setup checklist

Wiring up an OIDC provider — especially when used as an ABAC authorization provider — requires *four* coordinated config touchpoints. Missing any one surfaces as a different error from Neo4j:

| Step | Where | What to set | Symptom if missed |
|---|---|---|---|
| 1. Provider block | `spec.config["dbms.security.oidc.<name>.*"]` (or use the typed `spec.auth.oidc.<name>` map below) | `display_name`, `well_known_discovery_uri`, `audience`, `client_id` | `Failed to validate '[<name>]' …: entries must be a valid OIDC authorization provider` |
| 2. Authentication providers | `spec.auth.authenticationProviders` | include `oidc-<name>` | OIDC sign-in is rejected |
| 3. Authorization providers | `spec.auth.authorizationProviders` | include `oidc-<name>` | `entries must exist in dbms.security.authorization_providers. Invalid values: oidc-<name>` |
| 4. ABAC provider list (only for `Neo4jAuthRule`) | `spec.config["dbms.security.abac.authorization_providers"]` | the prefixed name `oidc-<name>` (NOT bare `<name>`) | `entries must be a valid OIDC authorization provider` *or* `entries must exist in dbms.security.authorization_providers` |

A complete worked example for ABAC is at [`examples/users-roles/07-authrule-abac.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/07-authrule-abac.yaml); the integration test fixtures show the same pattern with a self-signed CA at [`test/integration/neo4jauthrule_test.go`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/test/integration/neo4jauthrule_test.go).

#### Single Provider (Okta)

```yaml
spec:
  auth:
    authenticationProviders: [oidc-okta, native]
    authorizationProviders: [oidc-okta, native]
    adminSecret: neo4j-admin-secret
    oidc:
      okta:                                            # → dbms.security.oidc.okta.*
        displayName: "Okta SSO"
        wellKnownDiscoveryURI: "https://dev-123456.okta.com/.well-known/openid-configuration"
        audience: "0oaXXXXXXXXXXXXXX"                 # Your OIDC client ID
        authFlow: pkce                                 # Recommended (default)
        claims:
          username: email                              # JWT claim → Neo4j username
          groups: groups                               # JWT claim → role mapping
        groupToRoleMapping:
          "neo4j-admins": "admin,architect"
          "neo4j-developers": "editor,publisher"
          "neo4j-readers": "reader"
```

#### Multiple Providers (Okta + Azure AD)

```yaml
    oidc:
      okta:
        displayName: "Okta SSO"
        wellKnownDiscoveryURI: "https://dev-123456.okta.com/.well-known/openid-configuration"
        audience: "0oaXXXXXXXXXXXXXX"
        claims:
          username: email
          groups: groups
        groupToRoleMapping:
          "neo4j-admins": "admin"
      azure:
        displayName: "Azure AD"
        wellKnownDiscoveryURI: "https://login.microsoftonline.com/TENANT_ID/v2.0/.well-known/openid-configuration"
        audience: "api://neo4j-app"
        claims:
          username: preferred_username
          groups: roles
        getGroupsFromUserInfo: true                    # Fetch groups from UserInfo endpoint
        groupToRoleMapping:
          "neo4j-admins": "admin"
          "neo4j-users": "reader"
```

Reference both in the provider list:

```yaml
    authenticationProviders: [oidc-okta, oidc-azure, native]
    authorizationProviders: [oidc-okta, oidc-azure, native]
```

#### Manual Endpoints (No Discovery)

If your IdP does not support OIDC Discovery, specify endpoints manually:

```yaml
    oidc:
      custom-idp:
        displayName: "Internal IdP"
        authEndpoint: "https://idp.internal.com/authorize"
        tokenEndpoint: "https://idp.internal.com/token"
        jwksURI: "https://idp.internal.com/.well-known/jwks.json"
        userInfoURI: "https://idp.internal.com/userinfo"
        issuer: "https://idp.internal.com/"
        audience: "neo4j-app"
        claims:
          username: sub
          groups: roles
```

#### OIDC Field Reference

| Field | Neo4j Config Key | Description |
|-------|-----------------|-------------|
| `displayName` | `dbms.security.oidc.<name>.display_name` | Shown on login screen |
| `wellKnownDiscoveryURI` | `dbms.security.oidc.<name>.well_known_discovery_uri` | Auto-configures endpoints |
| `authEndpoint` | `dbms.security.oidc.<name>.auth_endpoint` | Authorization endpoint (manual) |
| `tokenEndpoint` | `dbms.security.oidc.<name>.token_endpoint` | Token endpoint (manual) |
| `jwksURI` | `dbms.security.oidc.<name>.jwks_uri` | JWKS endpoint (manual) |
| `userInfoURI` | `dbms.security.oidc.<name>.user_info_uri` | UserInfo endpoint (manual) |
| `issuer` | `dbms.security.oidc.<name>.issuer` | Issuer identifier (manual) |
| `audience` | `dbms.security.oidc.<name>.audience` | Expected JWT `aud` claim (**required**) |
| `authFlow` | `dbms.security.oidc.<name>.auth_flow` | `pkce` (default) or `implicit` |
| `claims.username` | `dbms.security.oidc.<name>.claims.username` | JWT claim for username (default: `sub`) |
| `claims.groups` | `dbms.security.oidc.<name>.claims.groups` | JWT claim for groups |
| `getGroupsFromUserInfo` | `dbms.security.oidc.<name>.get_groups_from_user_info` | Fetch groups from UserInfo |
| `getUsernameFromUserInfo` | `dbms.security.oidc.<name>.get_username_from_user_info` | Fetch username from UserInfo |
| `groupToRoleMapping` | `dbms.security.oidc.<name>.authorization.group_to_role_mapping` | Map IdP groups to Neo4j roles |
| `authParams` | `dbms.security.oidc.<name>.auth_params` | Extra auth endpoint params |
| `tokenParams` | `dbms.security.oidc.<name>.token_params` | Extra token endpoint params |

### JVM TrustStore for Internal CAs

When Neo4j needs to make outgoing TLS connections to systems whose certificates
are signed by an internal CA — LDAPS servers, OIDC providers behind a corporate
CA, Aura Fleet Management endpoints, plugin download mirrors, cross-cluster
replication peers — you need to add those CAs to Neo4j's JVM truststore.

The operator supports this via two fields, in increasing order of flexibility:

#### `spec.trustedCASecrets` — declarative multi-CA list (recommended)

Reference one or more Secrets that contain a PEM-encoded CA. The default key
is `ca.crt`, which matches the layout cert-manager produces for every
Certificate it issues.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
spec:
  # ... image, topology, etc.
  trustedCASecrets:
    - name: oidc-corporate-ca       # Secret with ca.crt (default key)
    - name: ldap-internal-ca
      key:  ldap.pem                # override the default key
    - name: replica-cluster-ca      # CA of another Neo4j cluster we replicate to
```

For each entry, the operator:

1. Mounts the Secret into the pod at `/trusted-ca/<secret-name>/`.
2. Runs the `truststore-init` init container, which:
   - Copies the JDK's default `cacerts` into a writable JKS at
     `/truststore/truststore.jks`. This preserves trust in public CAs (Let's
     Encrypt, DigiCert, etc.) — Neo4j can still connect to publicly trusted
     endpoints (for example, cloud services and public HTTPS APIs) while also
     trusting your internal CAs.
   - Runs `keytool -import` for each supplied CA, using the **Secret name as
     the keytool alias**. Names must therefore be unique across the list.
3. Sets `NEO4J_server_jvm_additional` (cluster) /
   `server.jvm.additional=...` (standalone) with
   `-Djavax.net.ssl.trustStore=/truststore/truststore.jks
    -Djavax.net.ssl.trustStorePassword=changeit`.

Cert-manager pattern (the most common case):

```yaml
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: oidc-tls
  namespace: neo4j-prod
spec:
  secretName: oidc-tls            # ← name to reference below
  duration: 8760h
  issuerRef:
    name: corp-internal-ca
    kind: ClusterIssuer
  commonName: idp.corp.example.com
  dnsNames: [idp.corp.example.com]
---
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: prod-cluster
  namespace: neo4j-prod
spec:
  # ...
  trustedCASecrets:
    - name: oidc-tls              # default key "ca.crt" matches cert-manager
```

#### `spec.auth.trustStore` — single-CA legacy form (deprecated)

The pre-existing singular field still works for backward compatibility. The
operator folds it into the same truststore alongside `trustedCASecrets` at
reconcile time. New configurations should use `trustedCASecrets`.

```yaml
spec:
  auth:
    trustStore:
      name: corp-ca-cert            # equivalent to trustedCASecrets:[{name: corp-ca-cert}]
      key:  ca.crt
```

#### `spec.extraVolumes` / `spec.extraVolumeMounts` — escape hatch

For the rare case where Neo4j needs a CA at a *specific filesystem path* —
typically because a Neo4j SSL policy references a per-policy `truststore_path`
(e.g. cross-cluster replication policies) — use `extraVolumes` and
`extraVolumeMounts` to wire arbitrary mounts into the Neo4j pod:

```yaml
spec:
  extraVolumes:
    - name: replica-truststore
      secret:
        secretName: replica-cluster-ca
  extraVolumeMounts:
    - name: replica-truststore
      mountPath: /var/lib/neo4j/policies/replica
      readOnly: true
  config:
    dbms.ssl.policy.replica.truststore_path: /var/lib/neo4j/policies/replica/ca.crt
    dbms.ssl.policy.replica.truststore_password: ""
```

Mount paths that collide with operator-managed paths are rejected by the validator at admission time. Reserved paths include `/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, and subdirectories under `/var/lib/neo4j/` such as `data`, `logs`, `conf`, `plugins`, and `certificates`.

### Group-to-Role Mapping

Both LDAP and OIDC support mapping external groups to Neo4j built-in roles:

| Neo4j Role | Permissions |
|-----------|-------------|
| `admin` | Full administrative access |
| `architect` | Schema management + all data access |
| `publisher` | Read + write data |
| `editor` | Read + write (no schema changes) |
| `reader` | Read-only access |

Custom roles must be pre-created in Neo4j via `CREATE ROLE` before they can be used in mappings.

For LDAP, the mapping key is the full group DN:

```yaml
groupToRoleMapping:
  "cn=DBA Team,ou=Groups,dc=corp,dc=com": "admin"
```

For OIDC, the mapping key is the group name as it appears in the JWT claim:

```yaml
groupToRoleMapping:
  "neo4j-admins": "admin,architect"
  "neo4j-readers": "reader"
```

Multiple Neo4j roles can be assigned to a single group (comma-separated).

### Attribute-Based Access Control (ABAC)

Neo4j 2026.03 introduced **attribute-based access control (ABAC)** as a richer alternative to the static group-to-role mapping above. Where group-to-role mapping is a one-line key-value YAML lookup, ABAC lets you write Cypher conditions over arbitrary OIDC token claims, including time of day, list membership, and combinations of attributes.

The operator exposes ABAC through the **[`Neo4jAuthRule`](../api_reference/neo4jauthrule.md)** CRD. Each rule has a Cypher condition that's evaluated against the user's OIDC token at authentication time; when the condition returns `true`, the listed roles are granted for that session.

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
  grantedRoles: [reader]
```

**Prerequisites:**

- Neo4j 2026.03 or later. Older clusters cause the rule to sit in `AuthRuleVersionTooOld=True`.
- The cluster's `spec.config` sets `dbms.security.abac.authorization_providers` to a configured OIDC provider name. Without it the rule sits in `OIDCProviderConfigured=False`.

**Group-to-role mapping vs ABAC:**

|  | Group-to-role mapping | ABAC (`Neo4jAuthRule`) |
|---|---|---|
| **Where it's defined** | `spec.auth.authorizationProviders[].groupToRoleMapping` on the cluster | Stand-alone `Neo4jAuthRule` resource |
| **Input** | Group claim values | Any OIDC token claim, multiple at once |
| **Logic** | Static key-value lookup | Arbitrary Cypher expression (operators, list functions, time, …) |
| **Min Neo4j version** | All supported versions | 2026.03+ |
| **Drift reconciliation** | Cluster-spec-driven (operator re-applies on every reconcile) | Per-rule, via `SHOW AUTH RULES` |

Pick group-to-role mapping when your IdP already emits a clean group claim and the role assignment is a flat lookup. Pick ABAC when you need conditional logic, time-bounded grants, or claims beyond a single group attribute. The two coexist — you can have static group mappings for the bulk of your users and a handful of `Neo4jAuthRule` resources for special cases.

See the [`Neo4jAuthRule` API reference](../api_reference/neo4jauthrule.md) for the full condition syntax, supported Cypher functions, and lifecycle. Worked examples live at [`examples/users-roles/07-authrule-abac.yaml`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/users-roles/07-authrule-abac.yaml).

**Errors in your Cypher condition** are surfaced through the rule's `status` and Kubernetes events:

- A syntactically-invalid condition (or one that calls a function outside the [allowed set](https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/)) is rejected by Neo4j when the operator runs `CREATE OR REPLACE AUTH RULE`. The rule's `status.phase` becomes `Failed`, `status.message` includes the full Neo4j error, and a Warning event with reason `AuthRuleFailed` is recorded. Inspect with `kubectl describe neo4jauthrule <name>`.
- DDL keywords (`CREATE`, `DROP`, `ALTER`, `GRANT`, `DENY`, `REVOKE`, `SHOW`, `RENAME`) and statement separators (`;`) in the condition are caught by the controller-side validator before any Cypher reaches Neo4j. Surfaces as `status.phase: "Failed"`, condition `Ready=False, reason=ValidationFailed`.
- **Runtime evaluation errors** — for example a condition that calls `.hour` on a claim that turns out to be `NULL` for some user — happen at authentication time, *not* at rule creation. The rule's `status.phase` stays `Ready` because the rule itself is correctly installed; the symptom is that affected users fail to authenticate. Diagnose via Neo4j's `security.log`.

### Auth Cache TTL

Control how long authentication results are cached:

```yaml
spec:
  auth:
    authCacheTTL: "5m"    # Maps to dbms.security.auth_cache_ttl
```

Short TTL = changes propagate faster. Long TTL = better performance. Setting to `"0"` disables caching.

### Kerberos Authentication

> **⚠️ Not yet implemented as a typed field.** `spec.auth.kerberos` was previously documented as a typed configuration block; the operator does not currently wire it through to Neo4j config and the typed-spec block has been removed. Use `spec.auth.authenticationProviders: [kerberos, native]` plus `spec.config["dbms.security.kerberos.*"]` keys directly until this is implemented.
>
> A working configuration looks roughly like:
>
> ```yaml
> spec:
>   auth:
>     authenticationProviders: [kerberos, native]
>     authorizationProviders:  [kerberos, native]
>     adminSecret: neo4j-admin-secret
>   config:
>     dbms.security.kerberos.realm: "CORP.EXAMPLE.COM"
>     dbms.security.kerberos.service_principal: "neo4j/neo4j-server.corp.example.com@CORP.EXAMPLE.COM"
>     # Mount the keytab via spec.extraVolumes / spec.extraVolumeMounts and point
>     # dbms.security.kerberos.keytab at the mounted path.
> ```
>
> Full Kerberos support (with operator-managed keytab Secret mount + generated config) is on the roadmap but not currently scheduled.

## Authorization and RBAC

### Neo4j Role-Based Access Control

```cypher
-- Create custom roles for different access levels
CREATE ROLE data_analyst;
CREATE ROLE data_engineer;
CREATE ROLE database_admin;

-- Grant database permissions
GRANT ACCESS ON DATABASE production TO data_analyst;
GRANT START ON DATABASE production TO data_engineer;
GRANT ALL ON DATABASE production TO database_admin;

-- Grant graph permissions
GRANT MATCH {*} ON GRAPH production TO data_analyst;
GRANT WRITE ON GRAPH production TO data_engineer;
GRANT ALL GRAPH PRIVILEGES ON GRAPH production TO database_admin;

-- Create users and assign roles
CREATE USER analyst_user SET PASSWORD 'SecurePassword123!' CHANGE NOT REQUIRED;
GRANT ROLE data_analyst TO analyst_user;
```

### Kubernetes RBAC Integration

```yaml
# ServiceAccount for Neo4j operation
apiVersion: v1
kind: ServiceAccount
metadata:
  name: neo4j-operator
  namespace: neo4j-system
---
# ClusterRole with minimal required permissions
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: neo4j-operator-role
rules:
- apiGroups: [""]
  resources: ["pods", "services", "configmaps", "secrets", "persistentvolumeclaims"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["statefulsets", "deployments"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["cert-manager.io"]
  resources: ["certificates"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
---
# ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: neo4j-operator-binding
subjects:
- kind: ServiceAccount
  name: neo4j-operator
  namespace: neo4j-system
roleRef:
  kind: ClusterRole
  name: neo4j-operator-role
  apiGroup: rbac.authorization.k8s.io
```

## Network Security

### Network Policies

```yaml
# Restrictive NetworkPolicy for Neo4j cluster
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: neo4j-network-policy
  namespace: default
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: neo4j

  policyTypes:
  - Ingress
  - Egress

  ingress:
  # Allow client connections
  - from:
    - podSelector:
        matchLabels:
          app.kubernetes.io/component: client
    ports:
    - protocol: TCP
      port: 7687  # Bolt
    - protocol: TCP
      port: 7474  # HTTP
    - protocol: TCP
      port: 7473  # HTTPS

  # Allow cluster communication
  - from:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: neo4j
    ports:
    - protocol: TCP
      port: 6000  # Cluster discovery
    - protocol: TCP
      port: 7000  # RAFT

  egress:
  # Allow DNS resolution
  - to: []
    ports:
    - protocol: UDP
      port: 53

  # Allow cluster communication
  - to:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: neo4j
```

### Service Mesh Integration (Istio)

```yaml
# DestinationRule for mTLS
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: neo4j-destination-rule
spec:
  host: neo4j-cluster-client.default.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL  # Enable mTLS
---
# PeerAuthentication for strict mTLS
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: neo4j-peer-auth
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  mtls:
    mode: STRICT
```

## Data Encryption and Security

### Encryption at Rest

```yaml
spec:
  storage:
    className: "encrypted-ssd"   # Use encrypted storage class
    size: "500Gi"

  config:
    # Enable transparent data encryption (Neo4j Enterprise)
    dbms.security.transparent_data_encryption.enabled: "true"
    dbms.security.transparent_data_encryption.keystore: "/encryption/keystore"
```

### Backup Security

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: secure-backup
spec:
  target:
    kind: Cluster
    name: secure-cluster

  # Secure backup storage configuration
  storage:
    type: s3
    bucket: "secure-neo4j-backups"
    path: "backups/"
    cloud:
      provider: aws

  # Backup encryption
  options:
    encryption:
      enabled: true
      keySecret: backup-encryption-key

  # Retention policy for compliance
  retention:
    maxCount: 30
```

## Secrets Management

### External Secrets Integration

> **Helm**: the chart does **not** grant the operator RBAC for the
> `external-secrets.io` API group by default. To enable the integration,
> install with `--set rbac.externalSecretsIntegration=true` (or set it in
> your values file). Without this, any `Neo4jEnterpriseCluster` /
> `Neo4jEnterpriseStandalone` that sets `spec.tls.externalSecrets.enabled=true`
> or `spec.auth.externalSecrets.enabled=true` will fail with a clear
> RBAC-denied error when the operator tries to create the `ExternalSecret`.
> The default-off posture follows the November 2025 security review's
> recommendation to keep the operator's RBAC blast radius narrow for
> deployments that don't actually use the integration.

```yaml
# SecretStore for AWS Secrets Manager
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: aws-secrets-manager
  namespace: default
spec:
  provider:
    aws:
      service: SecretsManager
      region: "us-west-2"
      auth:
        jwt:
          serviceAccountRef:
            name: external-secrets-sa
---
# ExternalSecret for Neo4j credentials
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: neo4j-admin-external-secret
  namespace: default
spec:
  refreshInterval: "1h"
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore

  target:
    name: neo4j-admin-secret
    creationPolicy: Owner

  data:
  - secretKey: username
    remoteRef:
      key: "neo4j/admin"
      property: "username"
  - secretKey: password
    remoteRef:
      key: "neo4j/admin"
      property: "password"
```

### Vault Integration

```yaml
# VaultAuth for Kubernetes auth method
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultAuth
metadata:
  name: neo4j-vault-auth
  namespace: default
spec:
  method: kubernetes
  mount: kubernetes
  kubernetes:
    role: neo4j-operator
    serviceAccount: neo4j-service-account
---
# VaultStaticSecret for Neo4j credentials
apiVersion: secrets.hashicorp.com/v1beta1
kind: VaultStaticSecret
metadata:
  name: neo4j-admin-vault-secret
  namespace: default
spec:
  type: kv-v2
  mount: secret
  path: neo4j/admin

  destination:
    name: neo4j-admin-secret
    create: true

  refreshAfter: "1h"
  vaultAuthRef: neo4j-vault-auth
```

## Credential Rotation

The operator does not have a one-click "rotate" button — credential rotation involves both a Kubernetes Secret update and a Neo4j-side state change, and the right sequence depends on what's being rotated. This section walks through each rotation path, what the operator does for you, and what you have to coordinate manually.

### Admin credentials (the Neo4j root account)

**What it is**: the `username`/`password` pair the operator uses to connect to Neo4j for `CREATE DATABASE`, `GRANT`, `SHOW SERVERS`, fleet registration, etc. Lives in a Secret referenced from `spec.auth.adminSecret` on the cluster / standalone CR. Default name: `neo4j-admin-secret`.

**Why rotation is non-trivial**: Neo4j reads `NEO4J_AUTH` **only on first boot** to initialise the root account. Subsequent restarts ignore it; the password is stored in Neo4j's own auth file. So updating the Secret alone does **not** change the password — you'd just produce a mismatch between what's in Kubernetes and what's in Neo4j.

The correct flow is two-step, in this order:

1. **Change the password inside Neo4j** via `ALTER USER`:

   ```bash
   # Read the CURRENT password from the Secret
   OLD_PASSWORD=$(kubectl get secret neo4j-admin-secret -n <namespace> \
       -o jsonpath='{.data.password}' | base64 -d)

   # Connect with the old password and rotate
   NEW_PASSWORD='<choose a strong value>'
   kubectl exec -n <namespace> <cluster>-server-0 -c neo4j -- \
       cypher-shell -u neo4j -p "${OLD_PASSWORD}" \
       "ALTER USER neo4j SET PASSWORD '${NEW_PASSWORD}'"
   ```

   For clusters, run this against any one server pod — `ALTER USER` propagates to the `system` database which replicates across the topology.

2. **Update the Kubernetes Secret** so the operator's next reconcile (and any future pod restart) uses the new password:

   ```bash
   kubectl create secret generic neo4j-admin-secret \
       --from-literal=username=neo4j \
       --from-literal=password="${NEW_PASSWORD}" \
       --namespace <namespace> \
       --dry-run=client -o yaml | kubectl apply -f -
   ```

3. **Verify** by checking the operator's logs — the next reconcile should succeed against the new password:

   ```bash
   kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager --tail=20 | grep -i auth
   ```

   If the operator logs `Neo.ClientError.Security.Unauthorized` after the rotation, the Secret was updated but `ALTER USER` didn't take — repeat step 1.

**Rolling new pods first won't help**. Bringing up a new pod with the new Secret value does NOT re-initialise the password — the data PVC carries the existing auth file forward. `NEO4J_AUTH` is only honoured on a **fresh** data volume.

**External Secrets Operator / Vault integration**: when the Secret is reconciled by an external store (ESO/Vault), step 2 happens automatically once you rotate the source. Step 1 still has to be done manually unless you build a downstream automation that watches the Secret and runs `ALTER USER`.

### Aura Fleet Management token

**What it is**: the API token in the Secret named by `spec.auraFleetManagement.tokenSecretRef.name` (default key `token`). Used once to call `fleetManagement.registerToken()` against the cluster.

**Rotation flow**:

1. Update the Secret with the new token.
2. The operator's next reconcile detects the change via the SHA-256 fingerprint in `status.auraFleetManagement.tokenSecretHash` (analogous to `status.passwordSecretHash` on `Neo4jUser`).
3. It calls `fleetManagement.registerToken($newToken)` against the cluster, replacing the old token.
4. The fingerprint in `status` is updated; subsequent reconciles see no change and stay quiet.

No `ALTER USER`-equivalent is needed — the registration is a single procedure call.

### TLS certificate rotation

**What it is**: the Secret named `{cluster}-tls-secret` (or the configured `spec.tls.certificateSecret`) that holds `tls.crt`, `tls.key`, and optionally `ca.crt`.

**When using `spec.tls.mode: cert-manager`** (recommended): rotation is fully automatic. cert-manager issues a new Certificate when the existing one approaches expiry (`spec.duration` and `spec.renewBefore` on the `Certificate` resource). The new Secret content is picked up on the next pod restart — schedule a rolling restart yourself if your certificate renewal cadence is shorter than your pod lifetime:

```bash
kubectl rollout restart statefulset <cluster>-server -n <namespace>
```

**When using a manually-provisioned Secret**: replace the Secret contents and trigger a rolling restart. The operator does not watch arbitrary TLS Secrets for change.

### Neo4jPlugin `source.authSecret` (VerifiedDownload mode)

**What it is**: the bearer-token or header Secret named by `spec.source.authSecret` on a `Neo4jPlugin` CR with `installMode: VerifiedDownload`. Mounted into the init container at `/etc/plugin-auth`.

**Rotation flow**: update the Secret. Init container reads the file at pod start, so the new value takes effect on the next pod restart (e.g. when you trigger a rolling restart by editing `spec.config` on the cluster CR). No coordination with Neo4j-side state needed — the token is consumed by curl, not by Neo4j.

### Quick reference

| Secret | Trigger pod restart? | Run Cypher? | Operator auto-detects? |
|---|---|---|---|
| `spec.auth.adminSecret` (cluster + standalone) | not required if you've run `ALTER USER`, but recommended for hygiene | **Yes** (`ALTER USER neo4j SET PASSWORD ...`) | No — needs explicit `ALTER USER` |
| `spec.auraFleetManagement.tokenSecretRef` | No | No | **Yes** (token hash in status) |
| `Neo4jUser.spec.passwordSecret` | No | No | **Yes** (`status.passwordSecretHash`) |
| TLS Secret (cert-manager) | Yes (rolling restart on renewal) | No | cert-manager auto-renews |
| `Neo4jPlugin.spec.source.authSecret` | Yes | No | No — picked up on next pod start |

## Operator-labelled Secrets

User-supplied Secrets (the admin Secret, Aura token Secret, plugin `authSecret`, manually-provisioned TLS Secrets) are **not** modified by the operator. The operator reads from them; it does not mutate their labels or annotations. If you want consistent inventory metadata across user-supplied Secrets, apply your own labels at creation time.

The operator does propagate ownership metadata onto the Secrets it produces indirectly:

| Secret | Where the labels come from | Labels stamped |
|---|---|---|
| `{cluster}-tls-secret` / `{standalone}-tls-secret` (cert-manager issued) | `CertificateSpec.SecretTemplate` on the operator-created `Certificate` CR | `app.kubernetes.io/managed-by=neo4j-operator`, `app.kubernetes.io/component=tls`, `neo4j.com/owner-kind=Neo4jEnterpriseCluster\|Standalone`, `neo4j.com/owner-name=<cr-name>` |
| ExternalSecret-managed Secrets (when `spec.tls.externalSecrets.enabled=true` or `spec.auth.externalSecrets.enabled=true`) | The operator-created `ExternalSecret` resource itself carries `app.kubernetes.io/managed-by=neo4j-operator`; the issued target Secret inherits per the ExternalSecret's `target.template` (configure as needed via the external-secrets.io spec) | `app.kubernetes.io/managed-by=neo4j-operator` on the `ExternalSecret`, plus whatever your `target.template` adds to the issued Secret |

Audit query example — list every TLS Secret the operator owns across the cluster:

```bash
kubectl get secrets -A -l app.kubernetes.io/managed-by=neo4j-operator,app.kubernetes.io/component=tls \
    -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,OWNER:.metadata.labels.neo4j\.com/owner-kind,OWNER-NAME:.metadata.labels.neo4j\.com/owner-name'
```

## Security Monitoring and Auditing

### Audit Logging Configuration

```yaml
spec:
  config:
    # Enable security event logging
    dbms.security.log.successful_authentication: "true"
    dbms.logs.security.level: "INFO"
    dbms.logs.security.rotation.keep_number: "10"
    dbms.logs.security.rotation.size: "20m"

    # Query logging for security analysis
    db.logs.query.enabled: "INFO"
    db.logs.query.allocation_logging_enabled: "true"
    db.logs.query.parameter_logging_enabled: "false"  # Avoid logging sensitive data

    # Transaction logging
    db.logs.query.transaction_logging_enabled: "true"
```

### Security Monitoring with Prometheus

```yaml
spec:
  monitoring:
    enabled: true
  config:
    # Security metrics configuration
    metrics.security.authentication.enabled: "true"
    metrics.security.authorization.enabled: "true"
    metrics.security.log.enabled: "true"
```

## Compliance and Governance

### GDPR Compliance Configuration

```yaml
spec:
  config:
    # Data retention policies
    db.transaction.logs.rotation.retention_policy: "7 days"
    db.transaction.logs.rotation.size: "250M"

    # Query logging for data access tracking
    db.logs.query.enabled: "INFO"
    db.logs.query.threshold: "0ms"  # Log all queries for audit
    db.logs.query.allocation_logging_enabled: "true"

    # Security logging
    dbms.logs.security.level: "INFO"
    dbms.security.log.successful_authentication: "true"
    dbms.security.log.failed_authentication: "true"
```

### PCI DSS Compliance

```yaml
spec:
  # Strong TLS configuration
  tls:
    mode: cert-manager
    issuerRef:
      name: pci-compliant-issuer
      kind: ClusterIssuer

  config:
    # TLS 1.2 included for legacy-client compatibility during migration.
    # See the regulatory-context note below the example for guidance on
    # moving to the TLS 1.3-only target state.
    dbms.ssl.policy.bolt.tls_versions: "TLSv1.2,TLSv1.3"
    dbms.ssl.policy.https.tls_versions: "TLSv1.2,TLSv1.3"
    # TLS 1.3-only hardening (target state — recommended once clients migrate):
    # dbms.ssl.policy.bolt.tls_versions: "TLSv1.3"
    # dbms.ssl.policy.https.tls_versions: "TLSv1.3"
    dbms.ssl.policy.bolt.ciphers: "TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256"
    dbms.ssl.policy.https.ciphers: "TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256"

    # Authentication and session management
    dbms.security.auth_minimum_password_length: "12"
    dbms.security.auth_cache_ttl: "15m"

    # Access logging
    db.logs.query.enabled: "INFO"
    dbms.logs.security.level: "INFO"
```

> **Why TLS 1.3 over TLS 1.2.** PCI DSS v4.0 mandates a TLS 1.2 minimum but expects active migration toward TLS 1.3. Several adjacent regimes also discourage TLS 1.2 for new builds — FIPS-140-3 module validations and NIST SP 800-52r2 guidance both recommend TLS 1.3 as the baseline (the latter as guidance, not a strict prohibition). If every client in your environment supports TLS 1.3, drop `TLSv1.2` from the `tls_versions` list — the commented-out TLS 1.3-only target-state lines in the example above show the resulting hardened form.

## Security Best Practices Checklist

### Deployment Security
- [ ] **TLS Encryption**: Enable TLS for all communications (Bolt, HTTP, cluster)
- [ ] **Certificate Management**: Use cert-manager with proper certificate rotation
- [ ] **Strong Authentication**: Implement multi-factor authentication where possible
- [ ] **RBAC**: Configure least-privilege access for all users and services
- [ ] **Network Policies**: Restrict network access with Kubernetes NetworkPolicies
- [ ] **Secrets Management**: Use external secret management (Vault, AWS Secrets Manager)

### Data Security
- [ ] **Encryption at Rest**: Enable storage encryption and transparent data encryption
- [ ] **Backup Encryption**: Encrypt all backups and use secure storage
- [ ] **Data Masking**: Implement data masking for non-production environments
- [ ] **Access Controls**: Implement fine-grained database and graph permissions
- [ ] **Audit Logging**: Enable comprehensive security and query logging
- [ ] **Data Retention**: Implement compliant data retention policies

### Operational Security
- [ ] **Regular Updates**: Keep Neo4j and operator versions current
- [ ] **Security Scanning**: Regular vulnerability scanning of containers
- [ ] **Monitoring**: Comprehensive security monitoring and alerting
- [ ] **Incident Response**: Defined security incident response procedures
- [ ] **Backup Testing**: Regular testing of backup restoration procedures
- [ ] **Access Reviews**: Regular review of user access and permissions

### Compliance Considerations
- [ ] **Regulatory Requirements**: Meet specific compliance requirements (GDPR, HIPAA, PCI DSS)
- [ ] **Documentation**: Maintain security documentation and procedures
- [ ] **Training**: Regular security training for operations teams
- [ ] **Assessments**: Regular security assessments and penetration testing

## Troubleshooting Security Issues

### Common Security Problems

1. **Certificate Issues**:
   ```bash
   # Check certificate status
   kubectl get certificates
   kubectl describe certificate neo4j-tls-secret

   # Verify certificate content
   kubectl get secret neo4j-tls-secret -o yaml | grep tls.crt | base64 -d | openssl x509 -text -noout
   ```

2. **Authentication Failures**:
   ```bash
   # Check authentication logs
   kubectl logs cluster-server-0 | grep -i auth

   # Test authentication
   kubectl exec cluster-server-0 -- cypher-shell -u testuser -p testpass "RETURN 'auth test'"
   ```

3. **Network Access Issues**:
   ```bash
   # Check NetworkPolicy
   kubectl get networkpolicy
   kubectl describe networkpolicy neo4j-network-policy

   # Test network connectivity
   kubectl exec -it test-pod -- nc -zv cluster-server-0 7687
   ```

For additional security guidance, see:
- [Configuration Best Practices](guides/configuration_best_practices.md)
- [TLS Configuration Guide](configuration/tls.md)
- [Backup Security](guides/backup_restore.md#security-best-practices)
- [Split-Brain Recovery](troubleshooting/split-brain-recovery.md)
