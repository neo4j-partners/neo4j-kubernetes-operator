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

### Upgrade phases and resumability

The upgrade runs as a requeue-driven state machine whose state is persisted in
`status.upgradeStatus`, so you can follow it with `kubectl` and it survives
operator restarts:

| Phase | What's happening |
|---|---|
| `Staging` | Pre-upgrade health check, version-compatibility check, StatefulSet frozen (partition = replica count), new image applied to the template |
| `Rolling` | Pods restart one at a time, highest ordinal first; after each pod, Kubernetes readiness **and** Neo4j cluster membership (`SHOW SERVERS`) are verified before the partition is lowered |
| `Stabilizing` | All pods updated; the operator waits for consistently stable cluster health and consensus |
| `Verifying` | Every server's reported version is checked against the target before the upgrade is declared done |
| `Completed` / `Failed` / `Paused` | Terminal. `Paused` means a health check failed with `autoPauseOnFailure: true` — the cluster is left as-is for you to investigate |

Two status fields drive the machine and are useful when watching a long
upgrade: `currentPartition` (which StatefulSet partition step the rollout is
on — counts down toward 0) and `stepStartTime` (when the current step began;
each per-pod step is bounded by `upgradeTimeout`/`healthCheckTimeout`).

**Resumability.** Because the phase and partition are persisted in status, an
operator restart (or leader change) mid-upgrade resumes exactly where it left
off — already-rolled pods are not rolled again. Upgrades that were interrupted
under an older operator version (`phase: InProgress`) are adopted and resumed
automatically.

**Changing the target mid-upgrade.** If you patch `spec.image.tag` again while
an upgrade is running, the state machine retargets to the new image: pods not
yet rolled go straight to the new version, and pods already rolled to the
intermediate version are rolled again. You don't need to wait for the first
upgrade to finish.

**Version verification across SemVer/CalVer.** The `Verifying` phase compares
each server's `SHOW SERVERS` version against the image tag, normalizing
Neo4j's kernel alias (the `2025.01.x` CalVer releases report kernel `5.27.x` —
both are accepted as the same version).

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
