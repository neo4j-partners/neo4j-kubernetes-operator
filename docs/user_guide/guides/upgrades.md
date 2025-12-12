# Upgrades

This guide explains how to upgrade your Neo4j Enterprise clusters.

## Rolling Upgrades

The operator supports rolling upgrades to minimize downtime. To upgrade your cluster, you can simply update the `spec.image.tag` field in the `Neo4jEnterpriseCluster` resource to a newer version.

The operator will then perform a rolling upgrade, one pod at a time, to ensure that the cluster remains available during the upgrade process.

Behind the scenes the operator manages a single StatefulSet named `<cluster>-server` and walks its ordinals with StatefulSet partitions so that pods roll one at a time (best effort keeping the current leader until the end). You can watch progress with:

```bash
kubectl rollout status statefulset/<cluster>-server -n <namespace>
```

## Upgrade Strategy

You can configure the upgrade strategy using the `spec.upgradeStrategy` field. The operator supports two strategies:

*   **`RollingUpgrade`**: This is the default strategy. It upgrades the cluster one pod at a time, ensuring that the cluster remains available during the upgrade.
*   **`Recreate`**: This strategy will delete the old cluster and create a new one. This is faster but will result in downtime.

For more details on the upgrade strategy, see the [API Reference](../../api_reference/neo4jenterprisecluster.md).
