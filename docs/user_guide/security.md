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
apiVersion: neo4j.neo4j.com/v1alpha1
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
apiVersion: neo4j.neo4j.com/v1alpha1
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
    dbms.ssl.policy.cluster.trust_all: "false"   # Use proper certificate validation
```

## Authentication Configuration

### Native Authentication

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: secure-cluster
spec:
  auth:
    provider: native
    adminSecret: neo4j-admin-secret

    # Password policy configuration
    passwordPolicy:
      minimumLength: 12
      requireNumbers: true
      requireSpecialCharacters: true

  config:
    # Authentication settings
    dbms.security.auth_enabled: "true"
    dbms.security.auth_minimum_password_length: "12"

    # Session timeout
    dbms.security.auth_cache_ttl: "10m"
    dbms.security.auth_cache_max_capacity: "10000"
```

### LDAP Integration

```yaml
spec:
  auth:
    provider: ldap
    adminSecret: neo4j-admin-secret
    secretRef: ldap-config-secret  # Secret containing LDAP configuration

  config:
    # LDAP authentication settings
    dbms.security.realms: "ldap"
    dbms.security.ldap.authentication.enabled: "true"
    dbms.security.ldap.authorization.enabled: "true"

    # LDAP connection settings
    dbms.security.ldap.host: "ldaps://ldap.company.com:636"
    dbms.security.ldap.connection.timeout: "30s"
    dbms.security.ldap.authentication.cache.enabled: "true"
```

**LDAP Secret Configuration:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ldap-config-secret
type: Opaque
stringData:
  bindDN: "cn=neo4j-service,ou=services,dc=company,dc=com"
  bindPassword: "ldap-bind-password"
  userSearchBase: "ou=users,dc=company,dc=com"
  userSearchFilter: "(&(objectClass=person)(uid={0}))"
  groupSearchBase: "ou=groups,dc=company,dc=com"
  groupSearchFilter: "(&(objectClass=groupOfNames)(member={0}))"
```

### JWT Authentication

```yaml
spec:
  auth:
    provider: jwt
    adminSecret: neo4j-admin-secret
    secretRef: jwt-config-secret  # Secret containing JWT configuration

  config:
    # JWT authentication settings
    dbms.security.auth.jwt.enabled: "true"
    dbms.security.auth.jwt.jwks_uri: "https://auth.company.com/.well-known/jwks.json"
    dbms.security.auth.jwt.audience: "neo4j-kubernetes"
```

**JWT Secret Configuration:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: jwt-config-secret
type: Opaque
stringData:
  issuer: "https://auth.company.com"
  audience: "neo4j-kubernetes"
  claimsMapping: |
    {
      "username": "preferred_username",
      "roles": "groups"
    }
```

### Kerberos Authentication

```yaml
spec:
  auth:
    provider: kerberos
    adminSecret: neo4j-admin-secret
    secretRef: kerberos-config-secret  # Secret containing Kerberos configuration

  config:
    # Kerberos authentication settings
    dbms.security.auth.kerberos.enabled: "true"
    dbms.security.auth.kerberos.realm: "COMPANY.COM"
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
      port: 5000  # Discovery
    - protocol: TCP
      port: 6000  # Transaction
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
apiVersion: neo4j.neo4j.com/v1alpha1
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
  queryMonitoring:
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
    # Strong encryption requirements
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
- [Backup Security](guides/backup_restore.md#security-considerations)
- [Split-Brain Recovery](troubleshooting/split-brain-recovery.md)
