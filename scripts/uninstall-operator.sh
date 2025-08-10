#!/bin/bash

# Neo4j Kubernetes Operator Uninstall Script
# This script completely removes the Neo4j operator and all related resources

set -e

echo "🗑️  Uninstalling Neo4j Kubernetes Operator..."

# Delete operator deployment
echo "Removing operator deployment..."
kubectl delete deployment -n neo4j-operator-system neo4j-operator-controller-manager 2>/dev/null || true

# Delete namespace (this will delete all resources in the namespace)
echo "Removing operator namespace..."
kubectl delete namespace neo4j-operator-system 2>/dev/null || true

# Delete CRDs
echo "Removing Custom Resource Definitions..."
kubectl delete crd neo4jenterpriseclusters.neo4j.neo4j.com 2>/dev/null || true
kubectl delete crd neo4jenterprisestandalones.neo4j.neo4j.com 2>/dev/null || true
kubectl delete crd neo4jdatabases.neo4j.neo4j.com 2>/dev/null || true
kubectl delete crd neo4jbackups.neo4j.neo4j.com 2>/dev/null || true
kubectl delete crd neo4jrestores.neo4j.neo4j.com 2>/dev/null || true
kubectl delete crd neo4jplugins.neo4j.neo4j.com 2>/dev/null || true

# Delete webhook configurations
echo "Removing webhook configurations..."
kubectl delete validatingwebhookconfiguration neo4j-operator-validating-webhook-configuration 2>/dev/null || true
kubectl delete mutatingwebhookconfiguration neo4j-operator-mutating-webhook-configuration 2>/dev/null || true

# Delete cluster roles and bindings
echo "Removing cluster roles and bindings..."
kubectl delete clusterrole neo4j-operator-manager-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-metrics-reader 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-metrics-auth-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-leader-election-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jbackup-editor-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jbackup-viewer-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jdatabase-editor-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jdatabase-viewer-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jenterprisecluster-editor-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jenterprisecluster-viewer-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jenterprisestandalone-editor-role 2>/dev/null || true
kubectl delete clusterrole neo4j-operator-neo4jenterprisestandalone-viewer-role 2>/dev/null || true

kubectl delete clusterrolebinding neo4j-operator-manager-rolebinding 2>/dev/null || true
kubectl delete clusterrolebinding neo4j-operator-leader-election-rolebinding 2>/dev/null || true
kubectl delete clusterrolebinding neo4j-operator-metrics-auth-rolebinding 2>/dev/null || true

# Clean up any remaining resources with neo4j labels
echo "Cleaning up remaining resources..."
kubectl delete all -l app.kubernetes.io/name=neo4j-operator --all-namespaces 2>/dev/null || true

echo "✅ Neo4j Kubernetes Operator uninstalled successfully!"
