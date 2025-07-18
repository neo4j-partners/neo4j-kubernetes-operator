# Split-Brain Recovery Quick Reference

## Quick Detection

```bash
# Check if all nodes see the same cluster
for i in 0 1 2; do
  echo "=== Primary-$i view ==="
  kubectl exec <cluster>-primary-$i -- cypher-shell -u neo4j -p <password> \
    "SHOW SERVERS" | grep -c "7687" | xargs echo "Sees nodes:"
done
```

## Recovery Options (Try in Order)

### 1. Quick Fix - Restart Minority Nodes
```bash
# Identify and restart nodes in smaller cluster
kubectl delete pod <cluster>-primary-1 <cluster>-secondary-1
kubectl wait --for=condition=ready pod -l neo4j.com/cluster=<cluster> --timeout=300s
```

### 2. Rolling Restart
```bash
# Restart all pods gracefully
kubectl rollout restart statefulset <cluster>-secondary
kubectl rollout restart statefulset <cluster>-primary
```

### 3. Full Reset (Causes Downtime)
```bash
# Scale down
kubectl scale statefulset <cluster>-secondary --replicas=0
kubectl scale statefulset <cluster>-primary --replicas=1

# Wait and scale back up
sleep 30
kubectl scale statefulset <cluster>-primary --replicas=3
kubectl scale statefulset <cluster>-secondary --replicas=2
```

## TLS Clusters - Special Handling

```bash
# Add delays between restarts for TLS
for pod in $(kubectl get pods -l neo4j.com/cluster=<cluster> -o name | grep -E "primary-[1-2]|secondary"); do
  kubectl delete $pod
  sleep 30  # Wait for TLS initialization
  kubectl wait --for=condition=ready $pod --timeout=300s
done
```

## Prevention Checklist

- [ ] Adequate resources allocated (CPU: 1+ cores, Memory: 2Gi+)
- [ ] Cluster formation timeouts increased for TLS
- [ ] Pod Disruption Budgets configured
- [ ] Regular health monitoring in place

## Emergency Contacts

- Check logs: `kubectl logs <pod-name> | grep -i error`
- Operator logs: `kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager`
- Full guide: `/docs/user_guide/troubleshooting/split-brain-recovery.md`
