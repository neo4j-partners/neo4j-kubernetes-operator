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
    dbms.ssl.policy.cluster.trust_all: "false"   # Validate peer certificates against the cluster truststore (mounted at /ssl/cluster/trusted/); set to "true" only for development
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
| `ldap.useStartTLS` | `dbms.security.ldap.use_starttls` | Upgrade a plain `ldap://` connection to TLS via STARTTLS after the initial connect. Do not use with `ldaps://`, which negotiates TLS at connect time. |
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

The operator supports one or more OIDC providers via the `spec.auth.oidc` map. Each key becomes the provider name in Neo4j's config (`dbms.security.oidc.<name>.*`).

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

When connecting to LDAPS servers or OIDC providers that use certificates signed by an internal CA (not publicly trusted), configure a custom JVM truststore:

```yaml
spec:
  auth:
    authenticationProviders: [ldap, native]
    ldap:
      host: "ldaps://ldap.internal.corp:636"
      # ... other LDAP config
    trustStore:
      name: corp-ca-cert             # Secret containing the CA certificate
      key: ca.crt                   # Key in the Secret (default: ca.crt)
```

Create the Secret with your CA certificate in PEM format:

```bash
kubectl create secret generic corp-ca-cert --from-file=ca.crt=/path/to/corporate-ca.pem
```

The operator automatically:
1. Mounts the CA certificate into the pod
2. Runs an init container that converts the PEM to a JKS truststore using `keytool`
3. Adds JVM flags (`-Djavax.net.ssl.trustStore=...`) so Neo4j trusts the internal CA

This works for both LDAPS connections and OIDC providers behind internal CAs.

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

### Auth Cache TTL

Control how long authentication results are cached:

```yaml
spec:
  auth:
    authCacheTTL: "5m"    # Maps to dbms.security.auth_cache_ttl
```

Short TTL = changes propagate faster. Long TTL = better performance. Setting to `"0"` disables caching.

### Kerberos Authentication

```yaml
spec:
  auth:
    authenticationProviders: [kerberos, native]
    authorizationProviders: [kerberos, native]
    adminSecret: neo4j-admin-secret
    kerberos:
      realm: "CORP.EXAMPLE.COM"
      servicePrincipal: "neo4j/neo4j-server.corp.example.com@CORP.EXAMPLE.COM"
      keytab:
        name: neo4j-keytab-secret
        key: keytab
```

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
      port: 6000  # Discovery (V2)
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
    # Strong encryption requirements (PCI DSS v4.0 requires TLS 1.2 minimum;
    # TLS 1.3 is strongly preferred. The settings below pin both Bolt and HTTPS
    # to TLS 1.2/1.3 and restrict ciphers to AEAD-only suites.)
    dbms.ssl.policy.bolt.tls_versions: "TLSv1.2,TLSv1.3"
    dbms.ssl.policy.https.tls_versions: "TLSv1.2,TLSv1.3"
    dbms.ssl.policy.bolt.ciphers: "TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256"
    dbms.ssl.policy.https.ciphers: "TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256"

    # Authentication and session management
    dbms.security.auth_minimum_password_length: "12"
    dbms.security.auth_cache_ttl: "15m"

    # Access logging
    db.logs.query.enabled: "INFO"
    dbms.logs.security.level: "INFO"
```

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
