# Aura Fleet Management Examples

These examples show how to integrate Neo4j deployments managed by the operator with
[Neo4j Aura Fleet Management](https://neo4j.com/docs/aura/fleet-management/), which lets
you monitor all your Neo4j instances from a single Aura console view.

## Quick start

```bash
# 1. Generate a token in the Aura console and create the Secret
kubectl create secret generic aura-fleet-token \
  --from-literal=token='<your-token-from-aura-console>'

# 2. Create the admin credentials Secret (if not already present)
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=admin123

# 3. Deploy a cluster with Fleet Management enabled
kubectl apply -f cluster-with-fleet-management.yaml

# 4. Watch the cluster reach Ready and register
kubectl get neo4jenterprisecluster my-cluster -w
kubectl get events --field-selector reason=AuraFleetManagementRegistered
```

## Examples

| File | Description |
|---|---|
| `cluster-with-fleet-management.yaml` | 3-server cluster with Fleet Management |
| `standalone-with-fleet-management.yaml` | Single-node standalone with Fleet Management |

## Notes

- The fleet-management plugin is **pre-bundled** in every Neo4j Enterprise image — no internet access is needed.
- Token registration is **idempotent**: the operator re-registers on every reconcile loop while `status.auraFleetManagement.registered` is `false`.
- To **force re-registration** after rotating a token: `kubectl patch neo4jenterprisecluster my-cluster --type=merge --subresource=status -p '{"status":{"auraFleetManagement":{"registered":false}}}'`

## Full documentation

See [Aura Fleet Management Guide](../../docs/user_guide/aura_fleet_management.md) for:
- Complete setup walkthrough
- Token rotation instructions
- Troubleshooting reference
