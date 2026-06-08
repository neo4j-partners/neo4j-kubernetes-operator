# Security Guide

Security configuration for Neo4j clusters managed by the operator: TLS, authentication, authorization, network policies, audit logging, and secrets management.

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

```

> **Note — SSL policy configuration is operator-managed.**
> Do NOT set `dbms.ssl.policy.*`, `server.bolt.tls_level`, or
> `server.directories.certificates` in `spec.config`. The cluster
> validator rejects every key in that namespace with a `Forbidden`
> error at apply time. The reason: `server.config.strict_validation.
> enabled=false` lets Neo4j silently honour duplicate-key overrides,
> so a user value would shadow operator-managed configuration without
> any warning at startup.
>
> When `spec.tls.mode: cert-manager` and `spec.tls.strictPeerValidation:
> true` (the default), the operator emits Neo4j's canonical production
> SSL configuration automatically:
>
> | Setting | Value | Source |
> |---|---|---|
> | `dbms.ssl.policy.bolt.*` | mounted from `/ssl/`, `client_auth=NONE`, TLSv1.3/1.2 | Bolt serves external drivers — `NONE` so drivers don't need client certs |
> | `dbms.ssl.policy.https.*` | mounted from `/ssl/`, `client_auth=NONE`, TLSv1.3/1.2 | HTTPS serves the Browser and HTTP API |
> | `dbms.ssl.policy.cluster.trust_all` | `false` | strict peer validation; trust anchor at `/ssl/trusted/ca.crt` |
> | `dbms.ssl.policy.cluster.client_auth` | `REQUIRE` | mutual TLS between server pods |
> | `dbms.ssl.policy.cluster.verify_hostname` | `true` | explicit (Neo4j default differs across 5.26 / 2025.x) |
> | `server.bolt.tls_level` | `REQUIRED` | plain `bolt://` connections are rejected |
>
> Set `spec.tls.strictPeerValidation: false` to revert the cluster SSL
> policy to the legacy `trust_all=true` + `client_auth=NONE` posture.
> This is the only legitimate override path — useful when an external
> issuer doesn't populate `ca.crt` in the Secret it issues. Neo4j's own
> documentation flags `trust_all=true` as *"debugging only, since it
> does not offer security."*

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
| `ldap.useStartTLS` | `dbms.security.ldap.use_starttls` | Use STARTTLS with `ldap://` (typically port 389) to upgrade the connection to TLS after connection. Use `ldaps://` (typically port 636) for immediate TLS negotiation at connect time. These approaches are mutually exclusive; do not enable STARTTLS when using `ldaps://`. **Secure-by-default**: when `host` starts with `ldap://` and `useStartTLS` is unset, the operator emits `use_starttls=true` automatically (per the Neo4j security checklist). Set `useStartTLS: false` explicitly to opt out — required for dev setups with mock LDAP that doesn't speak StartTLS. `ldaps://` hosts are unaffected (already encrypted at the protocol level). |
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

> **⚠️ Prerequisite (Neo4j 2026.x): OIDC endpoints must use HTTPS.** `wellKnownDiscoveryURI`, `authEndpoint`, `tokenEndpoint`, `jwksURI`, `userInfoURI`, and `issuer` are all validated at config-parse time; any `http://` value causes Neo4j to refuse to start with `Error evaluating value for setting … does not have required scheme 'https'`. There is no insecure-mode override. Configure TLS for your identity provider — or place it behind a TLS-terminating proxy — before enabling OIDC. Self-hosted IDPs that default to HTTP in dev need a TLS-terminating proxy (and the proxy's CA in [`spec.trustedCASecrets`](#jvm-truststore-for-internal-cas)) before the cluster can boot.

The operator supports one or more OIDC providers via the `spec.auth.oidc` map. Each key becomes the provider name in Neo4j's config (`dbms.security.oidc.<name>.*`).

#### OIDC and ABAC setup checklist

Wiring up an OIDC provider — especially when used as an ABAC authorization provider — requires *four* coordinated config touchpoints. Missing any one surfaces as a different error from Neo4j:

| Step | Where | What to set | Symptom if missed |
|---|---|---|---|
| 1. Provider block | `spec.config["dbms.security.oidc.<name>.*"]` (or use the typed `spec.auth.oidc.<name>` map below) | `display_name`, `well_known_discovery_uri`, `audience`, `client_id` | `Failed to validate '[<name>]' …: entries must be a valid OIDC authorization provider` |
| 2. Authentication providers | `spec.auth.authenticationProviders` | include `oidc-<name>` | OIDC sign-in is rejected |
| 3. Authorization providers | `spec.auth.authorizationProviders` | include `oidc-<name>` | `entries must exist in dbms.security.authorization_providers. Invalid values: oidc-<name>` |
| 4. ABAC provider list (only for `Neo4jAuthRule`) | `spec.config["dbms.security.abac.authorization_providers"]` | the prefixed name `oidc-<name>` (NOT bare `<name>`) | `entries must be a valid OIDC authorization provider` *or* `entries must exist in dbms.security.authorization_providers` |

A complete worked example for ABAC is at [`examples/users-roles/07-authrule-abac.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/users-roles/07-authrule-abac.yaml); the integration test fixtures show the same pattern with a self-signed CA at [`test/integration/neo4jauthrule_test.go`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/test/integration/neo4jauthrule_test.go).

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

For the rare case where Neo4j needs an extra file at a *specific filesystem path* — typically a non-SSL artifact like a custom procedure JAR, a JVM `cacerts` overlay loaded by user code, or a configuration fragment consumed by an `apoc-extended` plugin — use `extraVolumes` and `extraVolumeMounts` to wire arbitrary mounts into the Neo4j pod:

```yaml
spec:
  extraVolumes:
    - name: corp-jvm-cacerts
      secret:
        secretName: corp-jvm-cacerts
  extraVolumeMounts:
    - name: corp-jvm-cacerts
      mountPath: /var/lib/neo4j/plugins/cacerts
      readOnly: true
```

**Do not use `extraVolumes` to override Neo4j's SSL policy paths.** The operator owns `dbms.ssl.policy.*`, `server.bolt.tls_level`, and `server.directories.certificates` end-to-end; the cluster validator rejects any of those keys in `spec.config` with `Forbidden`. Per-policy `truststore_path` overrides are not configurable via the operator today — for an additive trust anchor, use `spec.trustedCASecrets` (above), which feeds the operator-managed JKS truststore at `/truststore/truststore.jks`.

Mount paths that collide with operator-managed paths are rejected by the controller during reconciliation (the project does not use admission webhooks — see CLAUDE.md rule 26). Reserved paths include `/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, and subdirectories under `/var/lib/neo4j/` such as `data`, `logs`, `conf`, `plugins`, and `certificates`. A CR with a colliding mount is accepted into the API but its `status.phase` moves to `Failed` with a message naming the offending path.

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

See the [`Neo4jAuthRule` API reference](../api_reference/neo4jauthrule.md) for the full condition syntax, supported Cypher functions, and lifecycle. Worked examples live at [`examples/users-roles/07-authrule-abac.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/users-roles/07-authrule-abac.yaml).

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

Kerberos is **not currently supported through operator-typed configuration**. Neo4j implements Kerberos via the separate [Neo4j Kerberos Add-On](https://neo4j.com/docs/kerberos-add-on/current/), which uses its own `conf/kerberos.conf` file (not `neo4j.conf`), a Kerberos plugin JAR in `/plugins/`, and an external `krb5.conf` — a configuration shape that doesn't fit the operator's `spec.config` / typed-field model cleanly.

You can in principle assemble it by hand using `spec.extraVolumes` to mount the plugin JAR + `kerberos.conf` + `krb5.conf`, plus `spec.auth.authenticationProviders: [plugin-Neo4j-Kerberos, native]`. We haven't shipped a worked example; follow [issue #137](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/137) for progress on a proper walkthrough.

For the canonical Neo4j-side setup, see the upstream add-on documentation:
[neo4j.com/docs/kerberos-add-on/current/](https://neo4j.com/docs/kerberos-add-on/current/).

## Authorization and RBAC

Manage Neo4j users, roles, and privileges declaratively via the `Neo4jUser`, `Neo4jRole`, and `Neo4jRoleBinding` CRDs — see the [User & Role Management guide](user_role_management.md) for the full design (privileges live on `Neo4jRole`, never on users) and worked examples.

The operator's own Kubernetes RBAC is installed by the Helm chart / OLM bundle; you don't need to author it. See the [Installation guide](installation.md) for chart values that scope the operator to selected namespaces.

## Audit Logging

Neo4j Enterprise produces two compliance-relevant logs by default:

- `security.log` — authentication attempts (success + failure) and
  administration commands run against the `system` database.
- `query.log` — every query executed, with parameters and timing.

In Neo4j 5.x / 2025.x there is **no** separate `dbms.security.audit.*`
config block (those were 4.x keys; removed). What modern Neo4j calls
"audit logging" is the combination of `dbms.security.*` and
`db.logs.query.*` keys. The operator exposes the audit-relevant subset
as a typed `spec.audit` field so you don't have to hand-roll spec.config
entries — and so the secure-by-default value for query-literal
obfuscation is one flag away.

### Minimum-viable compliance setup

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  topology:
    servers: 3
  audit:
    enabled: true   # secure-by-default: query literals are redacted
```

With `audit.enabled: true` and no other audit fields, the operator
emits `db.logs.query.obfuscate_literals=true`. PII / passwords passed
as query literals are scrubbed from `query.log` before it lands on
disk. Authentication logging and parameter logging keep Neo4j's
upstream defaults (both on).

### Field reference

| Field | Neo4j config key | Default | What it does |
|---|---|---|---|
| `audit.enabled` | — | `false` | Master flag. When `true` AND `obfuscateQueryLiterals` is unset, the operator defaults the emitted obfuscation value to `true` (the secure default). |
| `audit.logSuccessfulAuthentication` | `dbms.security.log_successful_authentication` | Neo4j default (`true`) | When `false`, only FAILED logins appear in `security.log`. Useful in high-volume environments. |
| `audit.obfuscateQueryLiterals` | `db.logs.query.obfuscate_literals` | `false` (Neo4j default); `true` when `audit.enabled=true` and field is unset | Redact literal values in `query.log`. **Strongly recommended for PCI / HIPAA / GDPR**. Doesn't redact node labels, relationship types, or property keys. |
| `audit.parameterLogging` | `db.logs.query.parameter_logging_enabled` | Neo4j default (`true`) | Include parameter VALUES in `query.log`. Set `false` when parameter values themselves are sensitive (passwords passed as params). |

### Notes

- **PII in query parameters**: set `audit.parameterLogging: false` if parameter values themselves are sensitive (passwords passed as params). Query shapes remain logged.
- **High-volume successful logins**: `audit.logSuccessfulAuthentication: false` keeps failed logins (the security-relevant signal) while suppressing successful ones.
- **`spec.monitoring` overlap**: when both set `db.logs.query.obfuscate_literals`, `spec.audit` wins (audit emits last). Direct `spec.config` overrides still win over both.
- **Not typed-exposed yet**: log4j JSON format, file rotation tuning, `db.logs.query.transaction.enabled`, `max_parameter_length`, `obfuscate_errors`, HTTP/GC logs. Set these via `spec.config` directly; track [#128](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/128) for typed-field requests.

## Network Security

### Operator-Managed Network Policy (recommended)

Set `spec.networkPolicy.enabled: true` on `Neo4jEnterpriseCluster` or
`Neo4jEnterpriseStandalone` to have the operator emit a NetworkPolicy
that hardens the Pod's ingress surface — closing [Neo4j security
checklist](https://neo4j.com/docs/operations-manual/current/security/checklist/)
gap #2 (backup port exposure) without hand-rolled YAML:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  topology:
    servers: 3
  networkPolicy:
    enabled: true   # opt-in; disabled by default
```

The emitted policy:

| Port  | Allowed source                                     | Why |
|-------|----------------------------------------------------|-----|
| 7474  | any pod                                            | HTTP — application clients |
| 7473  | any pod                                            | HTTPS — application clients |
| 7687  | any pod                                            | Bolt — application clients |
| 6000  | pods labelled `neo4j.com/cluster: <cluster>`       | V2 discovery + tcp-tx — peers only |
| 7000  | pods labelled `neo4j.com/cluster: <cluster>`       | RAFT — peers only |
| 7688  | pods labelled `neo4j.com/cluster: <cluster>`       | Cluster routing — peers only |
| 6362  | operator-managed backup pods only                  | Backup — only Neo4jBackup-spawned Jobs |

Standalone deployments get the same shape minus the peer-only ports
(single-server, no RAFT).

**CNI prerequisite.** NetworkPolicy is *enforced* only on CNI plugins that
support it: Calico, Cilium, Antrea, Weave (and most managed offerings —
EKS with VPC-CNI + Calico add-on, GKE Dataplane V2, AKS Azure CNI).
**Flannel does not enforce NetworkPolicy** — on a flannel cluster the
operator still creates the resource but the rules are inert. Verify with
`kubectl get pods -n kube-system | grep -iE 'calico|cilium|antrea|weave'`.

**Opting in safely.** If you have non-operator workloads that currently
talk to Neo4j on a port not in the table above (e.g. a custom dashboard
hitting Prometheus metrics on 2004), enabling this policy will break them
on enforcing CNIs. Audit your traffic first; the hand-rolled
NetworkPolicy below remains a valid escape hatch.

### External access and the additive-policy trade-off

Kubernetes NetworkPolicies are **additive (OR)**: when two policies select
the same Pod, the rendered allow-list is the **union** of their ingress
rules. This has a counterintuitive consequence for the operator-emitted
policy:

> The operator's public-ports rule uses `From: nil` (allow from any
> source, including external clients via LoadBalancer/Ingress). You
> **cannot** tighten this to a CIDR allowlist by adding a second
> NetworkPolicy — the operator's permissive rule already permits any
> source on those ports, and additive union semantics mean your second
> policy's narrower CIDR is ignored.

If you need to restrict who can reach 7474/7473/7687 (e.g. only
corporate VPN ranges, only specific application namespaces), pick one of
the following approaches:

1. **Disable the operator policy and roll your own.** Set
   `spec.networkPolicy.enabled: false` and write the entire policy
   yourself. You take on the responsibility of allowing the cluster
   peer ports (6000/7000/7688/7689), the backup pods (6362), the
   Prometheus scrapers (2004), and probes (kubelet handled
   automatically on most CNIs; see below).

2. **Use a CNI-specific extension that supports deny rules.** Calico
   GlobalNetworkPolicy and Cilium CiliumNetworkPolicy both allow deny
   semantics that override standard K8s NetworkPolicy. With those you
   can layer a restrictive deny over the operator's permissive allow.

3. **Restrict at the Service / Ingress layer instead.** Use
   `spec.service.type: ClusterIP` (no LoadBalancer external IP), or an
   Ingress with `nginx.ingress.kubernetes.io/whitelist-source-range` or
   equivalent. NetworkPolicy operates on Pod-to-Pod traffic; external
   traffic going through a LoadBalancer/Ingress is more naturally
   shaped at those layers anyway.

### External access via LoadBalancer / Ingress

When `spec.networkPolicy.enabled: true`, external traffic via
LoadBalancer or Ingress **does** reach the Pod on 7474/7473/7687:

- The operator's public-ports rule has `From: nil`, which matches
  "any source including external clients" per the K8s NetworkPolicy
  spec.
- This holds whether `externalTrafficPolicy: Cluster` (kube-proxy
  SNATs to a node IP) or `Local` (preserve client IP) is set.
- The Ingress controller's Pod itself is the source from the Neo4j
  Pod's perspective; that Pod matches the open `From: nil` selector.

So enabling the operator's policy does NOT break LoadBalancer or Ingress
exposure — it only adds the protections on 6362 (backup) and the peer
ports.

### Kubelet probes and the policy

The Neo4j Pod's readiness/liveness probes are HTTP on port 7474.
Kubelet — the source of probe traffic — is **not** a Pod and is
**not** subject to NetworkPolicy in conformant CNIs:

| CNI | Kubelet probe handling |
|---|---|
| Calico | Auto-allowed (host endpoint detection) |
| Cilium | Auto-allowed (host-firewall policy in CiliumNetworkPolicy, not standard NetworkPolicy) |
| Antrea | Auto-allowed |
| Weave | Auto-allowed |

The operator's policy doesn't include an explicit kubelet carve-out
because it isn't needed on conformant CNIs. If you use a CNI where
probes fail after enabling the policy, add an `ipBlock` rule for the
node CIDR on port 7474. This is a CNI bug, not an operator one.

### Hand-Rolled Network Policy (escape hatch)

If you need a custom policy shape — for example, restricting client
ports to specific namespaces, or adding egress rules — write a
NetworkPolicy directly. The operator's policy uses
`app.kubernetes.io/component: network-policy` as a label, so your custom
policy doesn't collide.

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

### Service Mesh Integration

If you're running Istio/Linkerd, target the operator-managed `Service`s (e.g. `{cluster}-client.{ns}.svc.cluster.local`) with your usual `DestinationRule` / `PeerAuthentication` to enforce mTLS at the mesh layer. Nothing operator-specific is required.

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

## Metrics Scraping and Security

The operator's metrics surface is intentionally narrow. Default
behavior:

| Subsystem | Operator default | Why |
|---|---|---|
| Prometheus endpoint (port 2004) | `enabled=true` when `spec.monitoring.enabled=true`, off otherwise | The sanctioned scrape path. Bound to `0.0.0.0` for in-cluster scrape. |
| JMX MBeans | **`enabled=false` unconditionally** | Neo4j upstream defaults JMX ON, exposing an unauthenticated management surface inside the Pod. Operator overrides because Prometheus is sufficient and JMX is redundant. |
| CSV file export | **`enabled=false` unconditionally** | Neo4j upstream defaults CSV ON, writing per-metric files into the Pod's ephemeral filesystem. Useless in K8s — files disappear on restart. |
| Graphite | off (Neo4j default) | Operator doesn't enable; users who need Graphite can opt in via `spec.config`. |

Users who genuinely need JMX or CSV can re-enable them through
`spec.config["server.metrics.jmx.enabled"]: "true"` — the user's
`spec.config` is appended last in the rendered conf and wins over the
operator-emitted defaults.

> **Important security caveat from the Neo4j ops manual**: *"You should
> never expose the Prometheus endpoint directly to the Internet."* The
> operator binds 2004 to `0.0.0.0` because that's required for in-cluster
> scraping, but the endpoint is unauthenticated. Keep it behind:
>
> - The cluster's ClusterIP Service (not exposed externally by default).
> - `spec.networkPolicy.enabled: true` — this opens 2004 to any pod in
>   the cluster's namespace but blocks external traffic.
> - For tighter scoping (e.g. only allow Prometheus operator's scraper
>   pod), write a custom NetworkPolicy alongside the operator's; the
>   operator uses a unique `app.kubernetes.io/component: network-policy`
>   label so the two don't collide.

#### Scraping the cluster

```yaml
# Example Prometheus scrape config — point at the cluster's ClusterIP
# Service. The operator already adds the standard
# prometheus.io/{scrape,port,path} annotations.
scrape_configs:
  - job_name: neo4j-prod
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: [neo4j-prod]
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: "true"
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        target_label: __address__
        regex: (.+)
        replacement: $1:2004
```

#### What's NOT exposed as typed fields

The following Neo4j metrics knobs are reachable only via `spec.config`:

- `server.metrics.filter` — comma-separated globbing pattern to subset
  the emitted metrics (e.g. `*check_point*,neo4j.page_cache.*`).
- `server.metrics.prefix` — override the default `neo4j.` prefix on
  every metric name. Useful for multi-cluster Prometheus federation.

These are advanced tuning. The defaults work for the standard
Prometheus + Grafana stack.

## Compliance and Governance

For GDPR / PCI DSS / HIPAA setups, combine:

- **TLS** — `spec.tls.mode: cert-manager` with `strictPeerValidation: true` (default). The operator emits the canonical strict cluster SSL policy (`trust_all=false`, `client_auth=REQUIRE`, `verify_hostname=true`, `tls_versions=TLSv1.3,TLSv1.2`).
- **Audit** — `spec.audit.enabled: true` (see the [Audit Logging](#audit-logging) section above for the typed-field reference).
- **Authentication** — set `dbms.security.auth_minimum_password_length` and `dbms.security.auth_cache_ttl` via `spec.config`, or use OIDC/LDAP (see [Authentication Configuration](#authentication-configuration)).
- **Retention** — `db.transaction.logs.rotation.retention_policy` and `db.logs.query.*` via `spec.config`. Backup retention is governed by `Neo4jBackup.spec.retention`.

> **TLS version pinning is not user-configurable.** The operator owns the `dbms.ssl.policy.*` surface end-to-end (the validator rejects any `dbms.ssl.policy.*` key in `spec.config`). The cluster policy is emitted with `tls_versions=TLSv1.3,TLSv1.2`; bolt/https policies inherit Neo4j defaults. For TLS 1.3-only or custom cipher allowlists, file an issue — there is no override today.

## Conformance Policies (Kyverno)

A starter pack of [Kyverno](https://kyverno.io/) ClusterPolicies that
audit Neo4j CRs against this operator's recommended production posture
(enterprise image, TLS enabled, monitoring on, no `runAsNonRoot=false`
override, explicit resource limits) is included at
[`examples/security/policies/`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/tree/main/examples/security/policies).
All policies ship in `Audit` mode and match only user-facing CRs, so
they never block operator reconciliation. See the directory's README
for install, the Audit → Enforce migration path, and the list of
bundled example CRs that intentionally trip Audit warnings.

## Production Checklist

Operator-specific posture for production deployments:

- [ ] `spec.tls.mode: cert-manager` with `strictPeerValidation: true` (default)
- [ ] Enterprise image (`neo4j:X-enterprise`, never `neo4j:X`)
- [ ] `spec.audit.enabled: true`
- [ ] `spec.monitoring.enabled: true` + Prometheus scrape (see [Prometheus & Grafana](guides/prometheus-grafana-setup.md))
- [ ] `spec.networkPolicy.enabled: true` if your CNI enforces (Calico/Cilium/Antrea/Weave)
- [ ] Admin credentials sourced from External Secrets Operator or Vault, not literal Secret YAML
- [ ] Backup CRs with retention configured + restore tested at least once
- [ ] Audit Kyverno policies under `examples/security/policies/` deployed in `Audit` mode

## Troubleshooting Security Issues

### Common Security Problems

1. **Certificate Issues**:
   ```bash
   # Substitute <cluster-name> with the name of your Neo4jEnterpriseCluster
   # (or Neo4jEnterpriseStandalone) — the operator emits the Secret as
   # `<cluster-name>-tls-secret`.

   # Check certificate status
   kubectl get certificates
   # The Certificate resource is named `<cluster-name>-tls` and produces
   # the Secret `<cluster-name>-tls-secret`. Don't conflate the two.
   kubectl describe certificate <cluster-name>-tls

   # Verify certificate content. Note: `grep tls.crt | base64 -d` is
   # broken because grep returns the field name + colon + value, not the
   # value alone, so base64 refuses to decode. Use jsonpath to extract
   # the base64 value directly. Keep the jsonpath in single quotes so
   # the shell doesn't reinterpret the `\.` escape:
   #   single-quote form: -o jsonpath='{.data.tls\.crt}'
   #   double-quote form: -o jsonpath="{.data.tls\\.crt}"  (escape the backslash)
   kubectl get secret <cluster-name>-tls-secret \
     -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
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
- [Configuration Best Practices](configuration.md#best-practices-for-specconfig)
- [TLS Configuration Guide](tls_configuration.md)
- [Backup & Restore](guides/backup_restore.md)
- [Split-Brain Recovery](troubleshooting/split-brain-recovery.md)
