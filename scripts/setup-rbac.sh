#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🔐 Neo4j Operator RBAC Setup"
echo "============================"

# Check if user has permissions to create ClusterRoles
echo -n "Checking RBAC permissions... "
if kubectl auth can-i create clusterroles --all-namespaces &>/dev/null; then
    echo -e "${GREEN}✓${NC}"
    echo "You have sufficient permissions to deploy the operator."
    exit 0
fi

echo -e "${YELLOW}⚠${NC}"
echo ""
echo "You don't have ClusterRole creation permissions. Attempting to set up RBAC..."

# Get current user
CURRENT_USER=$(kubectl auth whoami 2>/dev/null | grep Username | cut -d: -f2 | tr -d ' ' || echo "")
if [ -z "$CURRENT_USER" ]; then
    # Fallback to gcloud for GKE
    CURRENT_USER=$(gcloud config get-value account 2>/dev/null || echo "")
fi

if [ -z "$CURRENT_USER" ]; then
    echo -e "${RED}Error: Could not determine current user${NC}"
    echo "Please set KUBE_USER environment variable with your username"
    exit 1
fi

echo "Current user: $CURRENT_USER"

# Try to create a ClusterRoleBinding for the current user
echo -n "Attempting to create cluster-admin binding... "
if kubectl create clusterrolebinding neo4j-operator-admin-binding \
    --clusterrole=cluster-admin \
    --user="$CURRENT_USER" \
    --dry-run=client -o yaml | kubectl apply -f - 2>/dev/null; then
    echo -e "${GREEN}✓${NC}"
    echo "Successfully created cluster-admin binding for $CURRENT_USER"
else
    echo -e "${RED}✗${NC}"
    echo ""
    echo "Could not create cluster-admin binding. Please ask your cluster administrator to run:"
    echo ""
    echo "  kubectl create clusterrolebinding neo4j-operator-admin-binding \\"
    echo "    --clusterrole=cluster-admin \\"
    echo "    --user=$CURRENT_USER"
    echo ""
    echo "Alternatively, for GKE clusters, ensure you have the following IAM permissions:"
    echo "  - container.clusterRoles.create"
    echo "  - container.clusterRoleBindings.create"
    echo ""
    exit 1
fi

echo ""
echo -e "${GREEN}✓ RBAC setup complete!${NC}"
echo "You can now run 'make deploy-dev-registry' or 'make deploy-prod-registry'"
