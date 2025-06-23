# Cert-Manager and External Secrets Operator Integration Guide

This guide explains how to use the Neo4j Enterprise Operator with cert-manager for automatic certificate management and External Secrets Operator (ESO) for secure secret management.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Cert-Manager Integration](#cert-manager-integration)
- [External Secrets Operator Integration](#external-secrets-operator-integration)
- [Configuration Examples](#configuration-examples)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Prerequisites

### Cert-Manager

Install cert-manager in your cluster:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.1/cert-manager.yaml
kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s
```

### External Secrets Operator

Install External Secrets Operator:

```bash
helm repo add external-secrets https://charts.external-secrets.io
helm install external-secrets external-secrets/external-secrets -n external-secrets-system --create-namespace
```

## Cert-Manager Integration

The Neo4j operator provides enhanced cert-manager integration for automatic TLS certificate management.

### Basic Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-cluster
spec:
  # ... other configuration ...

  tls:
    mode: cert-manager
    issuerRef:
      name: ca-issuer
      kind: ClusterIssuer
```

### Advanced Certificate Configuration

```yaml
tls:
  mode: cert-manager
  issuerRef:
    name: ca-issuer
    kind: ClusterIssuer
    group: cert-manager.io  # Optional, defaults to cert-manager.io

  # Certificate lifecycle management
  duration: "8760h"      # 1 year (default: cert-manager default)
  renewBefore: "720h"    # 30 days before expiry (default: cert-manager default)

  # Certificate subject fields
  subject:
    organizations:
      - "Your Organization"
    countries:
      - "US"
    organizationalUnits:
      - "Database Team"
    localities:
      - "San Francisco"
    provinces:
      - "California"

  # Certificate usage (defaults to standard Neo4j usages)
  usages:
    - "digital signature"
    - "key encipherment"
    - "server auth"
    - "client auth"
```

### Certificate Features

- **Automatic Certificate Provisioning**: Certificates are automatically created and managed by cert-manager
- **DNS Names**: Includes all necessary DNS names for cluster communication (services, pods, ingress)
- **Lifecycle Management**: Configurable certificate duration and renewal timing
- **Subject Customization**: Full control over certificate subject fields
- **Usage Specification**: Define specific certificate key usages

## External Secrets Operator Integration

ESO integration allows you to securely manage secrets from external systems like HashiCorp Vault, AWS Secrets Manager, Azure Key Vault, and more.

### TLS Secrets from External Store

```yaml
tls:
  mode: cert-manager
  issuerRef:
    name: ca-issuer
    kind: ClusterIssuer

  # External Secrets configuration for TLS certificates
  externalSecrets:
    enabled: true
    refreshInterval: "15m"  # How often to sync secrets
    secretStoreRef:
      name: vault-backend
      kind: SecretStore     # or ClusterSecretStore
    data:
      - secretKey: tls.crt
        remoteRef:
          key: neo4j/tls
          property: certificate
      - secretKey: tls.key
        remoteRef:
          key: neo4j/tls
          property: private-key
      - secretKey: ca.crt
        remoteRef:
          key: neo4j/tls
          property: ca-certificate
```

### Authentication Secrets from External Store

```yaml
auth:
  provider: native

  # External Secrets configuration for auth secrets
  externalSecrets:
    enabled: true
    refreshInterval: "1h"
    secretStoreRef:
      name: vault-backend
      kind: SecretStore
    data:
      - secretKey: password
        remoteRef:
          key: neo4j/auth
          property: admin-password
      - secretKey: username
        remoteRef:
          key: neo4j/auth
          property: admin-username
      - secretKey: neo4j-password
        remoteRef:
          key: neo4j/auth
          property: neo4j-password
```

### Supported Secret Stores

The operator supports all ESO-compatible secret stores:

- **HashiCorp Vault**
- **AWS Secrets Manager**
- **Azure Key Vault**
- **Google Secret Manager**
- **Kubernetes Secrets**
- **And many more...**

## Configuration Examples

### Example 1: Cert-Manager with Let's Encrypt

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-letsencrypt
spec:
  edition: enterprise
  image:
    repo: neo4j
    tag: "5.26-enterprise"

  topology:
    primaries: 3
    secondaries: 2

  storage:
    className: standard
    size: 10Gi

  tls:
    mode: cert-manager
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer
    duration: "2160h"  # 90 days
    renewBefore: "360h"  # 15 days before expiry

  service:
    type: ClusterIP
    ingress:
      enabled: true
      className: nginx
      host: neo4j.yourdomain.com
      tlsSecretName: neo4j-ingress-tls
      annotations:
        cert-manager.io/cluster-issuer: "letsencrypt-prod"
```

### Example 2: HashiCorp Vault Integration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-vault
spec:
  edition: enterprise
  image:
    repo: neo4j
    tag: "5.26-enterprise"

  topology:
    primaries: 3
    secondaries: 2

  storage:
    className: standard
    size: 10Gi

  tls:
    mode: cert-manager
    issuerRef:
      name: ca-issuer
      kind: ClusterIssuer
    externalSecrets:
      enabled: true
      refreshInterval: "15m"
      secretStoreRef:
        name: vault-backend
        kind: SecretStore
      data:
        - secretKey: tls.crt
          remoteRef:
            key: neo4j/tls
            property: certificate
        - secretKey: tls.key
          remoteRef:
            key: neo4j/tls
            property: private-key

  auth:
    provider: native
    passwordPolicy:
      minLength: 16
      requireUppercase: true
      requireLowercase: true
      requireNumbers: true
      requireSpecialChars: true
    externalSecrets:
      enabled: true
      refreshInterval: "1h"
      secretStoreRef:
        name: vault-backend
        kind: SecretStore
      data:
        - secretKey: password
          remoteRef:
            key: neo4j/auth
            property: admin-password

---
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: vault-backend
spec:
  provider:
    vault:
      server: "https://vault.yourdomain.com"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "neo4j-role"
          serviceAccountRef:
            name: "neo4j-vault-sa"
```

### Example 3: AWS Secrets Manager

```yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: aws-secrets-manager
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-west-2
      auth:
        jwt:
          serviceAccountRef:
            name: neo4j-aws-sa

---
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-aws
spec:
  # ... basic configuration ...

  auth:
    provider: native
    externalSecrets:
      enabled: true
      secretStoreRef:
        name: aws-secrets-manager
        kind: SecretStore
      data:
        - secretKey: password
          remoteRef:
            key: "neo4j-admin-password"
```

## Best Practices

### Security

1. **Use Strong Issuers**: Always use production-ready certificate issuers
2. **Rotate Secrets**: Configure appropriate refresh intervals for external secrets
3. **RBAC**: Implement proper RBAC for service accounts accessing secret stores
4. **Network Policies**: Restrict network access to secret stores and cert-manager

### Certificate Management

1. **Certificate Lifecycle**: Set appropriate duration and renewal timing
2. **Subject Fields**: Include relevant organization information in certificates
3. **DNS Names**: Ensure all necessary DNS names are included (handled automatically)
4. **Monitoring**: Monitor certificate expiry and renewal

### External Secrets

1. **Refresh Intervals**: Balance security and performance when setting refresh intervals
2. **Secret Versioning**: Use versioned secrets when supported by your secret store
3. **Fallback**: Have fallback mechanisms for when external secret stores are unavailable
4. **Audit**: Enable audit logging for secret access

## Troubleshooting

### Certificate Issues

```bash
# Check certificate status
kubectl get certificates -n <namespace>
kubectl describe certificate <certificate-name> -n <namespace>

# Check certificate events
kubectl get events --field-selector involvedObject.kind=Certificate

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager
```

### External Secrets Issues

```bash
# Check ExternalSecret status
kubectl get externalsecrets -n <namespace>
kubectl describe externalsecret <externalsecret-name> -n <namespace>

# Check SecretStore status
kubectl get secretstore -n <namespace>
kubectl describe secretstore <secretstore-name> -n <namespace>

# Check ESO controller logs
kubectl logs -n external-secrets-system deployment/external-secrets
```

### Common Issues and Solutions

#### Certificate Not Ready

- **Issue**: Certificate remains in "Pending" state
- **Solution**: Check issuer configuration and DNS propagation
- **Debug**: `kubectl describe certificate <name>` and check Events

#### External Secret Sync Failure

- **Issue**: ExternalSecret shows sync errors
- **Solution**: Verify SecretStore configuration and authentication
- **Debug**: Check ESO controller logs and SecretStore status

#### TLS Connection Issues

- **Issue**: Neo4j pods fail to start with TLS errors
- **Solution**: Verify certificate includes all required DNS names
- **Debug**: Check pod logs and certificate DNS names

## Advanced Configuration

### Multi-Region Setup

For multi-region deployments, consider:

- Regional certificate issuers
- Regional secret stores
- Cross-region secret replication

### High Availability

For HA setups:

- Multiple certificate issuers
- Backup secret stores
- Automated failover mechanisms

### Integration with Service Mesh

When using service mesh (Istio, Linkerd):

- Configure mesh-specific certificate requirements
- Consider sidecar proxy certificate needs
- Implement proper mTLS policies

## Monitoring and Observability

### Metrics to Monitor

- Certificate expiry dates
- External secret sync status
- Secret rotation frequency
- TLS handshake failures

### Alerting

Set up alerts for:

- Certificate expiry warnings
- External secret sync failures
- TLS connection failures
- Authentication failures

This integration provides enterprise-grade security for your Neo4j clusters with automated certificate management and secure secret handling.

## Enterprise License Management

### Storing Enterprise License with External Secrets

For production deployments, store your Neo4j Enterprise license using External Secrets Operator:

```yaml
# License stored in external secret store
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: vault-backend
  namespace: neo4j
spec:
  provider:
    vault:
      server: "https://vault.company.com:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "neo4j-operator"
---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: neo4j-enterprise-license
  namespace: neo4j
spec:
  refreshInterval: 24h
  secretStoreRef:
    name: vault-backend
    kind: SecretStore
  target:
    name: neo4j-enterprise-license
    creationPolicy: Owner
  data:
  - secretKey: license
    remoteRef:
      key: neo4j/enterprise
      property: license
```

### License Monitoring and Alerts

Set up monitoring for license expiration:

```yaml
# Prometheus alert for license expiration
- alert: Neo4jLicenseExpiring
  expr: neo4j_enterprise_license_days_remaining < 30
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: "Neo4j Enterprise license expiring in {{ $value }} days"
    description: "The Neo4j Enterprise license will expire soon. Renew before expiration to avoid service disruption."
```

### License Validation Script

Create a script to validate license status:

```bash
#!/bin/bash
# validate-license.sh - Check Neo4j Enterprise license status

NEO4J_POD=$(kubectl get pods -l app.kubernetes.io/name=neo4j -o jsonpath='{.items[0].metadata.name}')
NEO4J_PASSWORD=$(kubectl get secret neo4j-auth -o jsonpath='{.data.password}' | base64 -d)

# Check license status
kubectl exec $NEO4J_POD -- cypher-shell -u neo4j -p "$NEO4J_PASSWORD" \
  "CALL dbms.components() YIELD name, versions, edition RETURN name, versions, edition" | \
  grep -i enterprise || {
    echo "ERROR: Neo4j Enterprise license not found or invalid"
    exit 1
  }

echo "✅ Neo4j Enterprise license is valid"
```

## Conclusion

This integration provides enterprise-grade security for your Neo4j clusters with automated certificate management, secure secret handling, and comprehensive license management for production environments.
