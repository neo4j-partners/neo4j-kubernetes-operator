#!/bin/bash

# Neo4j Kubernetes Operator Uninstall Script
# This script completely removes the Neo4j operator and all related resources

set -e

# Colors for better output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
print_step() {
    echo -e "${BLUE}📋 $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    print_error "kubectl is not installed or not in PATH"
    exit 1
fi

echo -e "${RED}🗑️  Neo4j Kubernetes Operator Uninstall Script${NC}"
echo ""
print_warning "This will completely remove the Neo4j operator and ALL Neo4j resources!"
print_warning "This action cannot be undone and will result in data loss!"
echo ""

# Safety check
read -p "Are you sure you want to continue? (type 'yes' to confirm): " confirm
if [[ $confirm != "yes" ]]; then
    echo "Uninstall cancelled."
    exit 0
fi

echo ""
print_step "Starting Neo4j Operator uninstall process..."

# Step 1: Clean up Neo4j custom resources first (before removing CRDs)
print_step "Cleaning up Neo4j custom resources..."

# Remove finalizers and delete Neo4j resources to prevent hanging
for resource_type in neo4jenterpriseclusters neo4jenterprisestandalones neo4jdatabases neo4jbackups neo4jrestores neo4jplugins neo4jshardeddatabases; do
    if kubectl get crd "$resource_type.neo4j.neo4j.com" &>/dev/null; then
        print_step "Cleaning up $resource_type resources..."

        # Get all resources of this type across all namespaces
        resources=$(kubectl get $resource_type -A --no-headers -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name 2>/dev/null || true)

        if [[ -n "$resources" ]]; then
            echo "$resources" | while read -r namespace name; do
                if [[ -n "$namespace" && -n "$name" ]]; then
                    echo "  Removing finalizers from $namespace/$name"
                    kubectl patch $resource_type "$name" -n "$namespace" --type='merge' -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
                    echo "  Deleting $namespace/$name"
                    kubectl delete $resource_type "$name" -n "$namespace" --ignore-not-found=true --timeout=30s || true
                fi
            done
        fi
    fi
done

# Step 2: Remove operator deployment
print_step "Removing operator deployment..."
kubectl delete deployment -n neo4j-operator-system neo4j-operator-controller-manager --ignore-not-found=true --timeout=60s

# Step 3: Remove operator namespace (this will delete remaining resources in the namespace)
print_step "Removing operator namespace..."
kubectl delete namespace neo4j-operator-system --ignore-not-found=true --timeout=120s

# Step 4: Remove Custom Resource Definitions
print_step "Removing Custom Resource Definitions..."
kubectl delete crd neo4jenterpriseclusters.neo4j.neo4j.com --ignore-not-found=true
kubectl delete crd neo4jenterprisestandalones.neo4j.neo4j.com --ignore-not-found=true
kubectl delete crd neo4jdatabases.neo4j.neo4j.com --ignore-not-found=true
kubectl delete crd neo4jbackups.neo4j.neo4j.com --ignore-not-found=true
kubectl delete crd neo4jrestores.neo4j.neo4j.com --ignore-not-found=true
kubectl delete crd neo4jplugins.neo4j.neo4j.com --ignore-not-found=true
kubectl delete crd neo4jshardeddatabases.neo4j.neo4j.com --ignore-not-found=true

# Step 5: Remove cluster roles and bindings (updated with correct names)
print_step "Removing cluster roles and bindings..."

# Main cluster roles
kubectl delete clusterrole manager-role --ignore-not-found=true
kubectl delete clusterrole neo4j-operator-manager-role --ignore-not-found=true

# Editor/viewer roles for each CRD
kubectl delete clusterrole neo4jbackup-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jbackup-viewer-role --ignore-not-found=true
kubectl delete clusterrole neo4jdatabase-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jdatabase-viewer-role --ignore-not-found=true
kubectl delete clusterrole neo4jenterprisecluster-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jenterprisecluster-viewer-role --ignore-not-found=true
kubectl delete clusterrole neo4jenterprisestandalone-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jenterprisestandalone-viewer-role --ignore-not-found=true
kubectl delete clusterrole neo4jplugin-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jplugin-viewer-role --ignore-not-found=true
kubectl delete clusterrole neo4jrestore-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jrestore-viewer-role --ignore-not-found=true
kubectl delete clusterrole neo4jshardeddatabase-editor-role --ignore-not-found=true
kubectl delete clusterrole neo4jshardeddatabase-viewer-role --ignore-not-found=true

# Cluster role bindings
kubectl delete clusterrolebinding manager-rolebinding --ignore-not-found=true
kubectl delete clusterrolebinding neo4j-operator-manager-rolebinding --ignore-not-found=true
kubectl delete clusterrolebinding neo4j-operator-manager-rolebinding-dev --ignore-not-found=true

# Step 6: Clean up any remaining resources with neo4j labels
print_step "Cleaning up remaining labeled resources..."
kubectl delete all -l app.kubernetes.io/name=neo4j-operator --all-namespaces --ignore-not-found=true 2>/dev/null || true

# Step 7: Clean up PVCs that might be left behind
print_step "Checking for remaining Neo4j PVCs..."
pvcs=$(kubectl get pvc -A -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{" "}{.spec.selector.matchLabels}{"\n"}{end}' 2>/dev/null | grep -E "(neo4j|cluster)" || true)
if [[ -n "$pvcs" ]]; then
    print_warning "Found Neo4j PVCs that may contain data:"
    echo "$pvcs"
    echo ""
    read -p "Do you want to delete these PVCs? This will permanently delete all Neo4j data! (type 'yes' to confirm): " delete_pvcs
    if [[ $delete_pvcs == "yes" ]]; then
        kubectl get pvc -A --no-headers -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name | grep -E "(neo4j|cluster)" | while read -r ns name; do
            kubectl delete pvc "$name" -n "$ns" --ignore-not-found=true
        done
        print_success "PVCs deleted"
    else
        print_warning "PVCs preserved - you may need to clean them up manually"
    fi
fi

# Step 8: Verification
print_step "Verifying uninstall..."
remaining_crds=$(kubectl get crd 2>/dev/null | grep neo4j | wc -l | tr -d ' ')
remaining_clusters=$(kubectl get neo4jenterprisecluster -A 2>/dev/null | wc -l | tr -d ' ' || echo "0")

if [[ "$remaining_crds" -eq 0 ]]; then
    print_success "All Neo4j CRDs removed"
else
    print_warning "Some Neo4j CRDs may still exist"
fi

if [[ "$remaining_clusters" -eq 0 ]]; then
    print_success "All Neo4j clusters removed"
else
    print_warning "Some Neo4j resources may still exist"
fi

echo ""
print_success "Neo4j Kubernetes Operator uninstall completed!"
print_step "Summary:"
echo "  - Operator deployment and namespace removed"
echo "  - All Custom Resource Definitions removed"
echo "  - All RBAC resources removed"
echo "  - Neo4j custom resources cleaned up"
echo ""
print_warning "Note: If you had Neo4j instances with persistent volumes, the data may still exist in PVCs"
print_warning "Run 'kubectl get pvc -A' to check for remaining persistent volume claims"
