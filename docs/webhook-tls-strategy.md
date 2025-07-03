# Webhook SSL/TLS Development and Testing Strategy

## Overview
Webhooks require valid TLS certificates for secure communication with the Kubernetes API server. This document outlines strategies for different environments.

## Development Strategies

### 1. Local Development (Outside Cluster)
```bash
# Run without webhooks for rapid development
make dev-run ARGS="--enable-webhooks=false"

# Or use envtest with automatic cert generation
make test-webhooks
```

### 2. Local Kubernetes (Kind)
```yaml
# config/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
- ../default

patchesStrategicMerge:
- webhook-dev-patch.yaml

resources:
- self-signed-issuer.yaml
```

**Self-signed issuer for development:**
```yaml
# config/dev/self-signed-issuer.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-cluster-issuer
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: selfsigned-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: selfsigned-ca
  secretName: selfsigned-ca-secret
  issuerRef:
    name: selfsigned-cluster-issuer
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: dev-ca-issuer
spec:
  ca:
    secretName: selfsigned-ca-secret
```

### 3. Development Best Practices
- Use self-signed certificates for local development
- Automate cert-manager installation in dev clusters
- Use separate namespaces for testing different configurations
- Enable webhook debugging with verbose logging

## Testing Strategies

### 1. Unit Testing
```go
// Use envtest for webhook testing without real certificates
import (
    "sigs.k8s.io/controller-runtime/pkg/envtest"
)

testEnv = &envtest.Environment{
    CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
    ErrorIfCRDPathMissing: true,
    WebhookInstallOptions: envtest.WebhookInstallOptions{
        Paths: []string{filepath.Join("..", "..", "config", "webhook")},
    },
}
```

### 2. Integration Testing
```makefile
# Makefile additions for webhook testing
.PHONY: test-webhooks-integration
test-webhooks-integration: dev-cluster operator-setup
	kubectl apply -f config/samples/
	kubectl wait --for=condition=ready pod -l app=neo4j-operator --timeout=60s
	go test ./test/integration/webhooks/... -v

.PHONY: test-webhook-cert-rotation
test-webhook-cert-rotation: dev-cluster operator-setup
	# Test certificate rotation
	kubectl delete secret webhook-server-cert -n neo4j-operator-system
	kubectl wait --for=condition=ready certificate serving-cert -n neo4j-operator-system --timeout=120s
	# Verify webhook still works
	kubectl apply -f config/samples/invalid-cluster.yaml 2>&1 | grep -q "denied the request" && echo "Webhook validation working"
```

### 3. CI/CD Testing
```yaml
# .github/workflows/webhook-tests.yml
name: Webhook Tests
on: [push, pull_request]

jobs:
  webhook-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3

    - name: Create Kind cluster
      uses: helm/kind-action@v1.5.0
      with:
        config: test/kind-config.yaml

    - name: Install cert-manager
      run: |
        kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
        kubectl wait --for=condition=available --timeout=300s deployment/cert-manager -n cert-manager

    - name: Deploy operator with webhooks
      run: |
        make docker-build IMG=neo4j-operator:test
        kind load docker-image neo4j-operator:test
        make deploy-test-with-webhooks IMG=neo4j-operator:test

    - name: Test webhook validation
      run: |
        # Test invalid resource rejection
        ! kubectl apply -f test/fixtures/invalid-cluster.yaml
        # Test valid resource acceptance
        kubectl apply -f config/samples/neo4j_v1alpha1_neo4jenterprisecluster.yaml
```

## Environment-Specific Configurations

### Development Environment
```yaml
# config/dev/webhook-dev-patch.yaml
apiVersion: v1
kind: Service
metadata:
  name: webhook-service
  namespace: system
spec:
  type: NodePort  # Use NodePort for easier local access
  ports:
  - name: https
    port: 443
    targetPort: 9443
    nodePort: 30443  # Fixed port for consistent testing
```

### Staging Environment
```yaml
# config/staging/kustomization.yaml
bases:
- ../default

patchesStrategicMerge:
- webhook-staging-patch.yaml

configMapGenerator:
- name: webhook-config
  literals:
  - tls.minVersion=1.2
  - tls.cipherSuites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
```

### Production Considerations
For production, webhooks should use:
1. **Internal CA**: Organization's internal CA for private clusters
2. **Service Mesh**: Istio/Linkerd can handle mTLS automatically
3. **Cloud Provider CA**: Integrated certificate management from cloud providers

## Testing Checklist

### Functional Tests
- [ ] Webhook accepts valid resources
- [ ] Webhook rejects invalid resources with clear error messages
- [ ] Webhook mutations work correctly
- [ ] Webhook works after certificate rotation
- [ ] Webhook handles high load without timeout

### Security Tests
- [ ] TLS version >= 1.2
- [ ] Strong cipher suites only
- [ ] Certificate validation works
- [ ] No plaintext communication possible
- [ ] Proper RBAC for webhook configuration

### Certificate Tests
- [ ] Certificates generated with correct DNS names
- [ ] Certificate rotation before expiry
- [ ] CA bundle properly injected
- [ ] Webhook continues working during cert renewal

## Debugging Tools

### 1. Check Certificate Details
```bash
# Get webhook certificate
kubectl get secret webhook-server-cert -n neo4j-operator-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout

# Test webhook connectivity
kubectl run test-pod --image=curlimages/curl --rm -it -- \
  curl -k https://neo4j-operator-webhook-service.neo4j-operator-system.svc:443/healthz
```

### 2. Webhook Logs
```bash
# Enable debug logging
kubectl edit deployment neo4j-operator-controller-manager -n neo4j-operator-system
# Add to container args: --zap-log-level=debug

# View logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager -f | grep webhook
```

### 3. Test Webhook Locally
```bash
# Port-forward webhook service
kubectl port-forward -n neo4j-operator-system service/neo4j-operator-webhook-service 9443:443

# Test with curl
curl -k https://localhost:9443/validate-neo4j-neo4j-com-v1alpha1-neo4jenterprisecluster \
  -H "Content-Type: application/json" \
  -d @test/fixtures/admission-review.json
```

## Common Issues and Solutions

### Issue: Certificate CN doesn't match service name
**Solution**: Ensure certificate includes all required DNS names:
- `webhook-service`
- `webhook-service.{namespace}`
- `webhook-service.{namespace}.svc`
- `webhook-service.{namespace}.svc.cluster.local`

### Issue: Webhook timeout during testing
**Solution**: Increase timeout in webhook configuration:
```yaml
webhooks:
- name: vneo4jenterprisecluster.neo4j.com
  timeoutSeconds: 30  # Increase from default 10s
```

### Issue: cert-manager not ready in CI
**Solution**: Add proper wait conditions:
```bash
kubectl wait --for=condition=available deployment/cert-manager-webhook -n cert-manager --timeout=300s
kubectl wait --for=condition=ready issuer/selfsigned-issuer -n neo4j-operator-system --timeout=60s
```

## Summary

The key principle is to use **self-signed certificates** for development and testing, while reserving LetsEncrypt/ACME for production Neo4j clusters that need external access. Webhooks should always use cluster-internal certificates since they communicate within the cluster.
