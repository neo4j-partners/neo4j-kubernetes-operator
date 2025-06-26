#!/bin/bash

# Neo4j Operator Integration Tests with Webhooks Enabled
# This script runs integration tests with webhooks enabled using cert-manager

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Check for ginkgo CLI
if ! command -v ginkgo &> /dev/null; then
    echo -e "${YELLOW}Ginkgo CLI not found. Installing...${NC}"
    go install github.com/onsi/ginkgo/v2/ginkgo@latest
    export PATH="$PATH:$(go env GOPATH)/bin"
fi

echo -e "${BLUE}🚀 Running Neo4j Operator Integration Tests with Webhooks${NC}"
echo

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl is not installed or not in PATH${NC}"
    exit 1
fi

# Check if cert-manager is installed
echo -e "${YELLOW}🔍 Checking cert-manager installation...${NC}"
if ! kubectl get pods -n cert-manager &> /dev/null; then
    echo -e "${RED}❌ cert-manager is not installed${NC}"
    echo -e "${YELLOW}Installing cert-manager...${NC}"

    # Install cert-manager
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

    # Wait for cert-manager to be ready
    echo -e "${YELLOW}Waiting for cert-manager to be ready...${NC}"
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s
    echo -e "${GREEN}✅ cert-manager is ready${NC}"
else
    echo -e "${GREEN}✅ cert-manager is already installed${NC}"
fi

# Deploy the operator with webhooks enabled
echo -e "${YELLOW}📦 Deploying Neo4j Operator with webhooks...${NC}"
kubectl apply -k config/test-with-webhooks/

# Apply the Issuer first and wait for it to be ready
echo -e "${YELLOW}🔐 Applying webhook certificate issuer...${NC}"
kubectl apply -f config/certmanager/issuer.yaml

# Wait for the Issuer to be ready
echo -e "${YELLOW}⏳ Waiting for certificate issuer to be ready...${NC}"
kubectl wait --for=condition=ready issuer/neo4j-operator-selfsigned-issuer -n neo4j-operator-system --timeout=60s

# Apply the Certificate after the Issuer is ready
echo -e "${YELLOW}🔐 Applying webhook certificate...${NC}"
kubectl apply -f config/certmanager/certificate.yaml

# Wait for the certificate to be ready
echo -e "${YELLOW}⏳ Waiting for webhook certificate to be ready...${NC}"
kubectl wait --for=condition=ready certificate/serving-cert -n neo4j-operator-system --timeout=300s

# Wait for the operator to be ready
echo -e "${YELLOW}⏳ Waiting for operator to be ready...${NC}"
kubectl wait --for=condition=available deployment/controller-manager -n neo4j-operator-system --timeout=300s

# Wait for the actual tls.crt file to exist (race condition fix)
WEBHOOK_CERT_PATH="/var/folders/8_/z8fx9g411bdc0n0fzsw545l80000gp/T/k8s-webhook-server/serving-certs/tls.crt"
CERT_SECRET_NAME="webhook-server-cert"
CERT_NAMESPACE="neo4j-operator-system"

# Find the secret name from the webhook deployment if not default
SECRET_NAME=$(kubectl get secret -n $CERT_NAMESPACE | grep serving-cert | awk '{print $1}')
if [ -z "$SECRET_NAME" ]; then
  SECRET_NAME=$CERT_SECRET_NAME
fi

# Wait for the secret to have the tls.crt data
for i in {1..60}; do
  kubectl get secret "$SECRET_NAME" -n "$CERT_NAMESPACE" -o jsonpath='{.data.tls\.crt}' 2>/dev/null | base64 --decode > /tmp/tls.crt 2>/dev/null
  if [ -s /tmp/tls.crt ]; then
    echo -e "${GREEN}✅ Webhook certificate is present${NC}"
    break
  fi
  echo -e "${YELLOW}Waiting for webhook tls.crt to be created... ($i/60)${NC}"
  sleep 2
  if [ $i -eq 60 ]; then
    echo -e "${RED}❌ Timed out waiting for webhook tls.crt${NC}"
    exit 1
  fi
done

echo -e "${GREEN}✅ Operator with webhooks is ready${NC}"

# Run integration tests with webhooks enabled
echo -e "${YELLOW}🧪 Running integration tests with webhooks (Ginkgo CLI)...${NC}"
export ENABLE_WEBHOOKS=true
export TEST_MODE=true

cd "$PROJECT_ROOT/test/integration"
ginkgo -p --fail-fast --timeout=15m

cd "$PROJECT_ROOT"
echo -e "${GREEN}✅ Integration tests with webhooks completed${NC}"

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kubectl delete -k config/test-with-webhooks/ --ignore-not-found=true

echo -e "${GREEN}🎉 All done!${NC}"
