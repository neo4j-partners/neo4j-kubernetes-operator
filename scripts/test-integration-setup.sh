#!/bin/bash

# Test script to verify integration test setup
set -e

echo "🧪 Testing Integration Test Setup"
echo "=================================="

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl is not installed or not in PATH"
    exit 1
fi

# Check if cluster is accessible
echo "🔍 Checking cluster connectivity..."
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ Cannot connect to Kubernetes cluster"
    exit 1
fi
echo "✅ Cluster connectivity verified"

# Check if cert-manager is deployed
echo "🔍 Checking cert-manager deployment..."
if ! kubectl get namespace cert-manager &> /dev/null; then
    echo "⚠️  Cert-manager namespace not found"
    echo "   This will be deployed automatically during test setup"
else
    if kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=30s &> /dev/null; then
        echo "✅ Cert-manager is ready"
    else
        echo "⚠️  Cert-manager is deployed but not ready"
        echo "   This will be fixed during test setup"
    fi
fi

# Check if operator is deployed
echo "🔍 Checking operator deployment..."
if ! kubectl get namespace neo4j-operator-system &> /dev/null; then
    echo "⚠️  Operator namespace not found"
    echo "   This will be created during test setup"
else
    if kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager -n neo4j-operator-system --timeout=30s &> /dev/null; then
        echo "✅ Operator is ready"
    else
        echo "⚠️  Operator is deployed but not ready"
        echo "   This will be fixed during test setup"
    fi
fi

# Check if CRDs are installed
echo "🔍 Checking CRDs..."
required_crds=(
    "neo4jenterpriseclusters.neo4j.neo4j.com"
    "neo4jdatabases.neo4j.neo4j.com"
    "neo4jbackups.neo4j.neo4j.com"
    "neo4jrestores.neo4j.neo4j.com"
    "neo4jroles.neo4j.neo4j.com"
    "neo4jgrants.neo4j.neo4j.com"
    "neo4jusers.neo4j.neo4j.com"
    "neo4jplugins.neo4j.neo4j.com"
)

missing_crds=()
for crd in "${required_crds[@]}"; do
    if ! kubectl get crd "$crd" &> /dev/null; then
        missing_crds+=("$crd")
    fi
done

if [ ${#missing_crds[@]} -eq 0 ]; then
    echo "✅ All required CRDs are installed"
else
    echo "⚠️  Missing CRDs: ${missing_crds[*]}"
    echo "   These will be installed during test setup"
fi

# Check if test configuration exists
echo "🔍 Checking test configuration..."
if [ -f "config/test-with-webhooks/kustomization.yaml" ]; then
    echo "✅ Test configuration with webhooks found"
else
    echo "❌ Test configuration with webhooks not found"
    echo "   Expected: config/test-with-webhooks/kustomization.yaml"
    exit 1
fi

# Check if Go is available
echo "🔍 Checking Go installation..."
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed or not in PATH"
    exit 1
fi
echo "✅ Go is available"

# Check if test dependencies are available
echo "🔍 Checking test dependencies..."
if [ -f "go.mod" ]; then
    echo "✅ Go module found"
else
    echo "❌ Go module not found"
    exit 1
fi

echo ""
echo "🎯 Setup Verification Summary"
echo "============================="
echo "✅ Cluster connectivity"
echo "✅ Test configuration"
echo "✅ Go environment"
echo ""
echo "The integration tests will automatically:"
echo "  • Deploy/verify cert-manager"
echo "  • Deploy operator with webhooks and TLS"
echo "  • Install missing CRDs"
echo "  • Verify all components are working"
echo ""
echo "🚀 Ready to run integration tests!"
echo "   Use: go test ./test/integration/... -v -timeout=10m"
