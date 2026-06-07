# Split-Brain Quick Reference

Fast lookup for detection + recovery commands. For full diagnosis and recovery, see [Split-Brain Recovery](../troubleshooting/split-brain-recovery.md).

## Detect

```bash
# Compare cluster view from each server — different lists mean split-brain
for i in 0 1 2; do
  echo "=== server-$i ==="
  kubectl exec <cluster>-server-$i -- cypher-shell -u neo4j -p <pw> \
    "SHOW SERVERS YIELD name, state ORDER BY name"
done
```

## Auto-recovery (operator-managed)

```bash
kubectl get events --field-selector reason=SplitBrainDetected -A
kubectl get events --field-selector reason=SplitBrainRepaired -A
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager -f \
  | grep -i splitbrain
```

The operator restarts orphaned pods automatically. Manual intervention is rare.

## Manual override (if auto-recovery fails)

```bash
kubectl delete pod <cluster>-server-N -n <namespace>
kubectl exec <cluster>-server-0 -- cypher-shell -u neo4j -p <pw> "SHOW SERVERS"
```

If the cluster doesn't reconverge, follow the [full recovery procedure](../troubleshooting/split-brain-recovery.md#repair-strategies).
