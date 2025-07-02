#!/bin/bash
# Script to test webhook functionality in development

set -euo pipefail

NAMESPACE=${NAMESPACE:-neo4j-operator-system}
WEBHOOK_SERVICE="neo4j-operator-webhook-service"

echo "=== Neo4j Operator Webhook Testing Script ==="

# Check if cert-manager is installed
echo "1. Checking cert-manager..."
if ! kubectl get ns cert-manager &>/dev/null; then
    echo "❌ cert-manager not found. Installing..."
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
    echo "⏳ Waiting for cert-manager to be ready..."
    kubectl wait --for=condition=available deployment/cert-manager -n cert-manager --timeout=300s
    kubectl wait --for=condition=available deployment/cert-manager-webhook -n cert-manager --timeout=300s
    kubectl wait --for=condition=available deployment/cert-manager-cainjector -n cert-manager --timeout=300s
else
    echo "✅ cert-manager is installed"
fi

# Check webhook certificate
echo -e "\n2. Checking webhook certificate..."
if kubectl get secret webhook-server-cert -n $NAMESPACE &>/dev/null; then
    echo "✅ Webhook certificate exists"
    echo "Certificate details:"
    kubectl get secret webhook-server-cert -n $NAMESPACE -o jsonpath='{.data.tls\.crt}' | \
        base64 -d | openssl x509 -text -noout | grep -E "(Subject:|DNS:|Not Before:|Not After:)"
else
    echo "❌ Webhook certificate not found"
fi

# Check webhook service
echo -e "\n3. Checking webhook service..."
if kubectl get service $WEBHOOK_SERVICE -n $NAMESPACE &>/dev/null; then
    echo "✅ Webhook service exists"
    kubectl get service $WEBHOOK_SERVICE -n $NAMESPACE
else
    echo "❌ Webhook service not found"
fi

# Check webhook configurations
echo -e "\n4. Checking webhook configurations..."
echo "Validating webhooks:"
kubectl get validatingwebhookconfigurations | grep neo4j || echo "❌ No validating webhooks found"
echo "Mutating webhooks:"
kubectl get mutatingwebhookconfigurations | grep neo4j || echo "❌ No mutating webhooks found"

# Test webhook validation
echo -e "\n5. Testing webhook validation..."
echo "Creating test resources..."

# Test 1: Valid resource (should succeed)
cat <<EOF | kubectl apply -f - --dry-run=server &>/dev/null && echo "✅ Valid resource accepted" || echo "❌ Valid resource rejected"
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: test-valid-cluster
  namespace: default
spec:
  imageRef: "neo4j:5.26.0-enterprise"
  acceptLicenseAgreement: "yes"
  authConfigRef:
    name: test-auth
  primaryNode:
    storage:
      size: "10Gi"
EOF

# Test 2: Invalid resource (should fail)
cat <<EOF | kubectl apply -f - --dry-run=server &>/dev/null && echo "❌ Invalid resource accepted (should have been rejected)" || echo "✅ Invalid resource rejected"
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: test-invalid-cluster
  namespace: default
spec:
  imageRef: "neo4j:5.26.0-enterprise"
  # Missing acceptLicenseAgreement and authConfigRef
  primaryNode:
    storage:
      size: "-10Gi"  # Invalid negative size
EOF

# Test webhook connectivity
echo -e "\n6. Testing webhook connectivity..."
if command -v curl &>/dev/null; then
    # Create a test pod with curl
    kubectl run webhook-test --image=curlimages/curl --rm -i --restart=Never -- \
        curl -k -s -o /dev/null -w "%{http_code}" \
        https://$WEBHOOK_SERVICE.$NAMESPACE.svc:443/healthz | \
        grep -q "200" && echo "✅ Webhook endpoint reachable" || echo "❌ Webhook endpoint not reachable"
else
    echo "⚠️  Skipping connectivity test (curl not available)"
fi

# Check operator logs for webhook errors
echo -e "\n7. Checking operator logs for webhook issues..."
echo "Recent webhook-related logs:"
kubectl logs -n $NAMESPACE deployment/neo4j-operator-controller-manager --tail=20 | grep -i webhook || echo "No webhook logs found"

# Summary
echo -e "\n=== Testing Summary ==="
echo "If all checks passed (✅), your webhooks are properly configured."
echo "If any checks failed (❌), review the configuration and ensure:"
echo "  - cert-manager is properly installed"
echo "  - The operator is deployed with webhook support"
echo "  - Certificates are properly generated"
echo "  - Webhook service is accessible"

# Debugging commands
echo -e "\n=== Useful debugging commands ==="
echo "# Watch certificate status:"
echo "kubectl describe certificate serving-cert -n $NAMESPACE"
echo ""
echo "# Check webhook endpoints:"
echo "kubectl describe validatingwebhookconfiguration neo4j-operator-validating-webhook-configuration"
echo ""
echo "# View operator logs:"
echo "kubectl logs -n $NAMESPACE deployment/neo4j-operator-controller-manager -f"
echo ""
echo "# Test webhook with port-forward:"
echo "kubectl port-forward -n $NAMESPACE service/$WEBHOOK_SERVICE 9443:443"
