#!/bin/bash
# cleanup-test-resources.sh - Clean up test resources to free disk space

set -euo pipefail

echo "🧹 Cleaning up test resources..."

# Clean up completed jobs older than 1 hour
echo "Removing old completed jobs..."
kubectl get jobs --all-namespaces -o json | \
  jq -r '.items[] |
    select(.status.succeeded > 0 or .status.failed > 0) |
    select(.status.completionTime != null) |
    select(now - (.status.completionTime | fromdateiso8601) > 3600) |
    "\(.metadata.namespace) \(.metadata.name)"' | \
  while read -r namespace name; do
    echo "  Deleting job $name in namespace $namespace"
    kubectl delete job "$name" -n "$namespace" --ignore-not-found=true
  done

# Clean up failed pods
echo "Removing failed pods..."
kubectl get pods --all-namespaces --field-selector=status.phase=Failed -o json | \
  jq -r '.items[] | "\(.metadata.namespace) \(.metadata.name)"' | \
  while read -r namespace name; do
    echo "  Deleting failed pod $name in namespace $namespace"
    kubectl delete pod "$name" -n "$namespace" --ignore-not-found=true
  done

# Clean up evicted pods
echo "Removing evicted pods..."
kubectl get pods --all-namespaces -o json | \
  jq -r '.items[] |
    select(.status.reason == "Evicted") |
    "\(.metadata.namespace) \(.metadata.name)"' | \
  while read -r namespace name; do
    echo "  Deleting evicted pod $name in namespace $namespace"
    kubectl delete pod "$name" -n "$namespace" --ignore-not-found=true
  done

# Clean up orphaned PVCs (not bound to any pod)
echo "Checking for orphaned PVCs..."
kubectl get pvc --all-namespaces -o json | \
  jq -r '.items[] |
    select(.status.phase == "Bound") |
    "\(.metadata.namespace) \(.metadata.name)"' | \
  while read -r namespace name; do
    # Check if PVC is actually in use
    in_use=$(kubectl get pods -n "$namespace" -o json | \
      jq --arg pvc "$name" -r '.items[].spec.volumes[]? |
        select(.persistentVolumeClaim.claimName == $pvc) |
        .persistentVolumeClaim.claimName' | wc -l)

    if [ "$in_use" -eq 0 ]; then
      echo "  PVC $name in namespace $namespace is not in use"
      # Uncomment to actually delete orphaned PVCs
      # kubectl delete pvc "$name" -n "$namespace"
    fi
  done

# Show disk usage by namespace
echo -e "\n📊 Disk usage by namespace:"
kubectl get pv -o json | \
  jq -r '.items[] |
    select(.status.phase == "Bound") |
    "\(.spec.claimRef.namespace) \(.spec.claimRef.name) \(.spec.capacity.storage)"' | \
  awk '{ns[$1]+=$3} END {for (n in ns) print n ": " ns[n]}' | \
  sort -k2 -nr

# Show pod resource usage
echo -e "\n📊 Pod resource usage:"
kubectl top pods --all-namespaces --sort-by=memory 2>/dev/null | head -20 || \
  echo "  Metrics server not available"

# Clean up Docker system (if running in Kind)
if command -v docker &> /dev/null && docker info &> /dev/null; then
  echo -e "\n🐳 Docker cleanup:"
  echo "  Current disk usage:"
  docker system df

  # Clean up dangling images and volumes
  docker image prune -f
  docker volume prune -f

  echo -e "\n  After cleanup:"
  docker system df
fi

echo -e "\n✅ Cleanup complete!"

# Show current disk usage on nodes
echo -e "\n💾 Node disk usage:"
kubectl get nodes -o json | \
  jq -r '.items[].metadata.name' | \
  while read -r node; do
    echo "Node: $node"
    kubectl describe node "$node" | grep -A5 "Allocated resources:" || true
  done
