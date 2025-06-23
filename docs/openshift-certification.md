# OpenShift Certification Guide

This guide covers the Red Hat OpenShift certification process for the Neo4j Enterprise Operator.

## Overview

The Neo4j Enterprise Operator has been prepared for Red Hat OpenShift certification with the following components:

- **UBI-based container image** using Red Hat Universal Base Image
- **OLM bundle** for Operator Lifecycle Manager integration
- **Security contexts** compatible with OpenShift's restricted-v2 SCC
- **Comprehensive testing** including scorecard and preflight checks

## Prerequisites

### Tools Required

1. **Docker** or **Podman** for container builds
2. **operator-sdk** v1.34.1+ for OLM operations
3. **opm** (Operator Package Manager) for catalog operations
4. **oc** (OpenShift CLI) for deployment
5. **preflight** for Red Hat certification testing
6. **trivy** for security scanning (optional but recommended)

### Installation Commands

```bash
# Install operator-sdk
make operator-sdk

# Install opm
make opm

# Install preflight (for certification testing)
curl -L https://github.com/redhat-openshift-ecosystem/openshift-preflight/releases/latest/download/preflight-linux-amd64 -o preflight
chmod +x preflight
sudo mv preflight /usr/local/bin/

# Install trivy (for security scanning)
curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin
```

## OpenShift Compatibility Features

### 1. UBI-Based Container Image

The operator uses Red Hat Universal Base Image (UBI) for OpenShift certification:

- **Base Image**: `registry.access.redhat.com/ubi9/ubi-minimal:latest`
- **Build Image**: `registry.access.redhat.com/ubi9/go-toolset:latest`
- **Security**: Non-root user (UID 1001) with group 0 compatibility
- **Labels**: OpenShift-specific metadata and version compatibility

### 2. Security Context Constraints (SCC)

Compatible with OpenShift's `restricted-v2` SCC:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1001
  runAsGroup: 0
  fsGroup: 0
  seccompProfile:
    type: RuntimeDefault
```

Container-level security:

```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 1001
  runAsGroup: 0
  capabilities:
    drop: [ALL]
  seccompProfile:
    type: RuntimeDefault
```

### 3. Multi-Architecture Support

Supports both amd64 and arm64 architectures as required by OpenShift:

```yaml
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
          - key: kubernetes.io/arch
            operator: In
            values: [amd64, arm64]
```

## Certification Process

### Step 1: Build UBI-Based Images

```bash
# Build UBI-based operator image
make -f Makefile.openshift docker-build-ubi

# Build OLM bundle
make -f Makefile.openshift bundle-build
```

### Step 2: Validate Bundle

```bash
# Validate the OLM bundle
make -f Makefile.openshift bundle-validate
```

### Step 3: Run Certification Tests

```bash
# Run scorecard tests
make -f Makefile.openshift scorecard

# Run preflight certification tests
make -f Makefile.openshift preflight

# Run security scans
make -f Makefile.openshift security-scan
```

### Step 4: Push Images to Registry

```bash
# Push to your registry (replace with your registry)
export REGISTRY=quay.io/your-org
export VERSION=1.0.0

# Push UBI image
docker tag quay.io/neo4j/neo4j-operator:ubi-${VERSION} ${REGISTRY}/neo4j-operator:ubi-${VERSION}
docker push ${REGISTRY}/neo4j-operator:ubi-${VERSION}

# Push bundle
docker tag quay.io/neo4j/neo4j-operator-bundle:${VERSION} ${REGISTRY}/neo4j-operator-bundle:${VERSION}
docker push ${REGISTRY}/neo4j-operator-bundle:${VERSION}
```

## Testing on OpenShift

### Local Testing with CodeReady Containers (CRC)

1. **Install CRC**:

   ```bash
   # Download from https://developers.redhat.com/products/codeready-containers
   crc setup
   crc start
   ```

2. **Deploy Operator**:

   ```bash
   # Login to OpenShift
   oc login -u developer -p developer https://api.crc.testing:6443

   # Deploy using OLM
   make -f Makefile.openshift openshift-deploy
   ```

### Testing with OpenShift Cluster

1. **Create Catalog Source**:

   ```bash
   oc apply -f - <<EOF
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: neo4j-operator-catalog
     namespace: openshift-marketplace
   spec:
     sourceType: grpc
     image: quay.io/neo4j/neo4j-operator-catalog:1.0.0
     displayName: Neo4j Operator Catalog
     publisher: Neo4j Labs
   EOF
   ```

2. **Install via OperatorHub**:
   - Navigate to OpenShift Console
   - Go to Operators → OperatorHub
   - Search for "Neo4j"
   - Install the operator

## Certification Checklist

### Technical Requirements

- [x] **UBI-based container image**
- [x] **Non-root user with UID > 1000**
- [x] **Restricted SCC compatibility**
- [x] **Multi-architecture support (amd64, arm64)**
- [x] **Read-only root filesystem**
- [x] **Security context constraints compliance**
- [x] **OLM bundle with proper metadata**
- [x] **Scorecard tests passing**
- [x] **Health and readiness probes**
- [x] **Resource limits and requests**

### Documentation Requirements

- [x] **Installation guide**
- [x] **Configuration examples**
- [x] **Troubleshooting guide**
- [x] **Security documentation**
- [x] **API reference**

### Testing Requirements

- [x] **Unit tests with >80% coverage**
- [x] **Integration tests**
- [x] **End-to-end tests**
- [x] **Security scanning**
- [x] **Performance testing**

## Red Hat Partner Connect Submission

### Required Artifacts

1. **Container Images**:
   - UBI-based operator image
   - OLM bundle image
   - Catalog image (optional)

2. **Documentation**:
   - Installation guide
   - User guide
   - API documentation
   - Security guide

3. **Test Results**:
   - Scorecard test results
   - Preflight test results
   - Security scan results

### Submission Process

1. **Create Partner Connect Account**:
   - Visit <https://connect.redhat.com/>
   - Register as a technology partner

2. **Submit Product**:
   - Create new product listing
   - Upload container images
   - Provide documentation
   - Submit test results

3. **Certification Review**:
   - Red Hat reviews submission
   - Address any feedback
   - Complete certification process

## 🔒 Security Configuration

### OpenShift Security Context Constraints (SCC)

The operator is designed to work with OpenShift's restricted SCC:

```yaml
# Security context for operator pod
apiVersion: v1
kind: Pod
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1001
    runAsGroup: 0
    fsGroup: 0
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: manager
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      runAsNonRoot: true
      runAsUser: 1001
      capabilities:
        drop:
        - ALL
```

### Secret Management

#### External Secrets Integration

```yaml
# Integration with External Secrets Operator
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: vault-backend
  namespace: neo4j
spec:
  provider:
    vault:
      server: "https://vault.company.com"
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
  name: neo4j-credentials
  namespace: neo4j
spec:
  refreshInterval: "1h"
  secretStoreRef:
    name: vault-backend
    kind: SecretStore
  target:
    name: neo4j-auth-secret
    creationPolicy: Owner
  data:
  - secretKey: username
    remoteRef:
      key: neo4j/credentials
      property: username
  - secretKey: password
    remoteRef:
      key: neo4j/credentials
      property: password
```

#### Enterprise License Management

```yaml
# Neo4j Enterprise License stored in Vault
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: neo4j-enterprise-license
  namespace: neo4j
spec:
  refreshInterval: "24h"
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

### Network Security

#### Network Policies

```yaml
# Restrict operator network access
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: neo4j-operator-netpol
  namespace: neo4j-operator-system
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: neo4j-operator
  policyTypes:
  - Ingress
  - Egress
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443  # Kubernetes API
    - protocol: TCP
      port: 7687 # Neo4j
  - to: []
    ports:
    - protocol: UDP
      port: 53   # DNS
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: openshift-monitoring
    ports:
    - protocol: TCP
      port: 8080 # Metrics
```

#### Service Mesh Integration

```yaml
# Istio Service Mesh configuration
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: neo4j-mtls
  namespace: neo4j
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  mtls:
    mode: STRICT
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: neo4j-access-control
  namespace: neo4j
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/neo4j/sa/neo4j-operator"]
  - to:
    - operation:
        methods: ["GET", "POST"]
        ports: ["7687", "7474"]
```

#### Advanced Network Policies

```yaml
# Comprehensive network policies for enterprise security
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: neo4j-enterprise-network-policy
  namespace: neo4j
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  policyTypes:
  - Ingress
  - Egress
  ingress:
  # Allow operator access
  - from:
    - namespaceSelector:
        matchLabels:
          name: neo4j-operator-system
    ports:
    - protocol: TCP
      port: 7687
    - protocol: TCP
      port: 7474
  # Allow cluster internal communication
  - from:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: neo4j
    ports:
    - protocol: TCP
      port: 5000
    - protocol: TCP
      port: 6000
    - protocol: TCP
      port: 7000
  # Allow monitoring
  - from:
    - namespaceSelector:
        matchLabels:
          name: openshift-monitoring
    ports:
    - protocol: TCP
      port: 2004
  egress:
  # Allow DNS resolution
  - to: []
    ports:
    - protocol: UDP
      port: 53
  # Allow backup to cloud storage
  - to: []
    ports:
    - protocol: TCP
      port: 443
  # Allow cluster communication
  - to:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: neo4j
```

### LDAP/Active Directory Integration

```yaml
# LDAP authentication configuration
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-enterprise-ldap
  namespace: neo4j
spec:
  auth:
    provider: ldap
    ldap:
      host: "ldap.company.com"
      port: 636
      useTLS: true
      userDNPattern: "cn={0},ou=users,dc=company,dc=com"
      userSearchBase: "ou=users,dc=company,dc=com"
      userSearchFilter: "(&(objectClass=person)(cn={0}))"
      groupSearchBase: "ou=groups,dc=company,dc=com"
      groupSearchFilter: "(&(objectClass=group)(member={0}))"
      groupToRoleMapping:
        "CN=Neo4j-Admins,OU=Groups,DC=company,DC=com": "admin"
        "CN=Neo4j-Users,OU=Groups,DC=company,DC=com": "reader"
    secretRef: neo4j-ldap-credentials
```

### RBAC Configuration

#### Minimal RBAC for Production

```yaml
# Minimal cluster role for operator
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: neo4j-operator-minimal
rules:
# Core resources
- apiGroups: [""]
  resources: ["pods", "services", "configmaps", "secrets", "persistentvolumeclaims"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["statefulsets", "deployments"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
# Neo4j CRDs
- apiGroups: ["neo4j.neo4j.com"]
  resources: ["*"]
  verbs: ["*"]
# Events
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
# Metrics
- apiGroups: [""]
  resources: ["endpoints"]
  verbs: ["get", "list", "watch"]
```

#### Namespace-Scoped RBAC

```yaml
# For multi-tenant deployments
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: neo4j-operator-tenant
  namespace: tenant-namespace
rules:
- apiGroups: [""]
  resources: ["pods", "services", "configmaps", "secrets", "persistentvolumeclaims"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["statefulsets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["neo4j.neo4j.com"]
  resources: ["neo4jenterpriseclusters"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### Image Security

#### Image Scanning with Quay

```yaml
# Quay security scan configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: quay-security-config
data:
  config.yaml: |
    FEATURE_SECURITY_SCANNER: true
    SECURITY_SCANNER_V4_ENDPOINT: https://quay.io
    SECURITY_SCANNER_V4_PSK: your-psk-here
    SECURITY_SCANNER_INDEXING_INTERVAL: 30
```

#### Container Image Signatures

```yaml
# Cosign signature verification
apiVersion: v1
kind: ConfigMap
metadata:
  name: cosign-public-key
data:
  cosign.pub: |
    -----BEGIN PUBLIC KEY-----
    MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
    -----END PUBLIC KEY-----
```

### Compliance and Auditing

#### OpenShift Compliance Operator

```yaml
# Compliance scan for Neo4j operator
apiVersion: compliance.openshift.io/v1alpha1
kind: ComplianceScan
metadata:
  name: neo4j-operator-scan
spec:
  profile: xccdf_org.ssgproject.content_profile_moderate
  content: ssg-ocp4-ds.xml
  nodeSelector:
    node-role.kubernetes.io/worker: ""
  scanType: Node
```

#### Audit Logging

```yaml
# Audit policy for Neo4j operator
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: Metadata
  namespaces: ["neo4j-operator-system"]
  resources:
  - group: "neo4j.neo4j.com"
    resources: ["*"]
  verbs: ["create", "update", "patch", "delete"]
- level: Request
  namespaces: ["neo4j-operator-system"]
  resources:
  - group: ""
    resources: ["secrets"]
  verbs: ["create", "update", "patch", "delete"]
```

## Troubleshooting

### Common Issues

#### 1. SCC Permission Denied

**Problem**: Pod fails with "permission denied" errors.

**Solution**: Ensure security context uses UID 1001 and group 0:

```yaml
securityContext:
  runAsUser: 1001
  runAsGroup: 0
```

#### 2. Bundle Validation Failures

**Problem**: Bundle validation fails with metadata errors.

**Solution**: Check bundle annotations and CSV metadata:

```bash
make -f Makefile.openshift bundle-validate
```

#### 3. Scorecard Test Failures

**Problem**: Scorecard tests fail due to missing descriptors.

**Solution**: Add proper spec and status descriptors to CSV.

#### 4. Network Policy Blocking Traffic

**Problem**: Operator cannot connect to Neo4j clusters.

**Solution**: Update network policies to allow required traffic:

```bash
# Check network policy logs
oc logs -n openshift-sdn -l app=sdn
```

#### 5. RBAC Permission Errors

**Problem**: Operator lacks permissions for certain operations.

**Solution**: Review and update RBAC permissions:

```bash
# Check operator permissions
oc auth can-i create statefulsets --as=system:serviceaccount:neo4j-operator-system:neo4j-operator
```

### Getting Help

- **OpenShift Documentation**: <https://docs.openshift.com/>
- **Operator SDK Documentation**: <https://sdk.operatorframework.io/>
- **Red Hat Partner Connect**: <https://connect.redhat.com/support>
- **Neo4j Community**: <https://community.neo4j.com/>

## Maintenance

### Updating for New OpenShift Versions

1. Update supported versions in annotations:

   ```yaml
   com.redhat.openshift.versions: "v4.10,v4.11,v4.12,v4.13,v4.14,v4.15,v4.16"
   ```

2. Test with new OpenShift version
3. Update documentation
4. Re-submit for certification if required

### Security Updates

1. Regularly update UBI base images
2. Run security scans on updated images
3. Address any security vulnerabilities
4. Update certification if needed

## Conclusion

The Neo4j Enterprise Operator is now ready for Red Hat OpenShift certification. Follow the steps in this guide to complete the certification process and make the operator available through Red Hat's certified operator catalog.
