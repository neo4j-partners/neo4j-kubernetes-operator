# Upgrades

This guide explains how to upgrade your Neo4j Enterprise clusters.

## Rolling Upgrades

The operator supports rolling upgrades to minimize downtime. To upgrade your cluster, update the `spec.image.tag` field in the `Neo4jEnterpriseCluster` resource to a newer version:

```bash
kubectl patch neo4jenterprisecluster <name> \
  --type=merge \
  -p '{"spec":{"image":{"tag":"2025.01.0-enterprise"}}}'
```

The operator performs a safe, ordered rolling upgrade:

1. **Freezes all pods** — sets the StatefulSet partition to block any automatic restarts.
2. **Restarts pods highest-ordinal-first** — pods `N-1`, `N-2`, … `1` are restarted in order. Kubernetes StatefulSet partition-based rolling always restarts pods from highest ordinal to lowest, so pod `0` is naturally the last to restart.
3. **Verifies cluster membership after each pod** — after Kubernetes reports the pod Ready, the operator also queries `SHOW SERVERS` to confirm the server is `Enabled`/`Available` in the Neo4j cluster before moving to the next pod. This closes the gap where a pod can pass the Kubernetes readiness probe while still joining the cluster.
4. **Rolls pod 0 last** — the operator attempts to detect which pod holds the primary role for the `system` database (via `SHOW DATABASES`) and logs a warning if it is not ordinal 0. Ordinal 0 is always the last pod rolled regardless, so the system-database primary is preserved as long as possible.
5. **Waits for cluster stabilisation** — after all pods are updated the operator waits for the cluster health and consensus state to be consistently stable before marking the upgrade complete.

Watch progress with:

```bash
# StatefulSet rollout progress
kubectl rollout status statefulset/<cluster>-server -n <namespace>

# Operator upgrade status
kubectl get neo4jenterprisecluster <name> -o jsonpath='{.status.upgradeStatus}'

# Operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f | grep -i upgrade
```

### First upgrade after operator deployment

If `status.version` is not yet set on the cluster resource (e.g. immediately after the operator is first deployed against an existing cluster), the version-compatibility check is skipped and the upgrade proceeds. Downgrade protection is applied on all subsequent upgrades.

## Upgrade Strategy

Configure the upgrade strategy with `spec.upgradeStrategy`:

| Strategy | Description |
|---|---|
| `RollingUpgrade` (default) | Restarts one pod at a time; cluster stays available throughout |
| `Recreate` | Deletes and recreates all pods; faster but causes downtime |

```yaml
spec:
  upgradeStrategy:
    strategy: RollingUpgrade
    preUpgradeHealthCheck: true   # validate cluster health before upgrading (default true)
    autoPauseOnFailure: true      # pause the upgrade for manual intervention if a check fails (default true)
    upgradeTimeout: 30m           # per-pod Kubernetes readiness timeout (default 30m)
    healthCheckTimeout: 5m        # per-pod Neo4j cluster-membership timeout (default 5m)
    stabilizationTimeout: 3m      # post-upgrade cluster-stabilization wait (default 3m)
    postUpgradeHealthCheck: true  # validate cluster health after upgrade (default true)
```

When `preUpgradeHealthCheck` is enabled, the operator verifies connectivity to Neo4j before changing the image. If the check fails and `autoPauseOnFailure` is `true`, the upgrade is blocked until you intervene rather than proceeding against an unhealthy deployment.

For the full field reference see the [API Reference](../../api_reference/neo4jenterprisecluster.md).

## Supported Upgrade Paths

| From | To | Supported |
|---|---|---|
| SemVer 5.26.x | SemVer 5.26.y (patch only) | ✅ |
| SemVer 5.26.x | CalVer 2025.y / 2026.y | ✅ (only 5.26.x — the last SemVer LTS — may cross to CalVer) |
| CalVer 2025.x | CalVer 2025.y / 2026.y (newer minor, patch, or year) | ✅ |
| CalVer 2025.x | SemVer 5.y | ❌ (downgrade) |
| Any | earlier version | ❌ (downgrade) |
