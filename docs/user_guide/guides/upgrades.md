# Upgrades

This guide explains how to upgrade your Neo4j Enterprise clusters.

## Rolling Upgrades

The operator supports rolling upgrades to minimize downtime. To upgrade your cluster, you can simply update the `spec.image.tag` field in the `Neo4jEnterpriseCluster` resource.

## Upgrade Strategy

You can configure the upgrade strategy using the `spec.upgradeStrategy` field. The operator supports two strategies:

*   `RollingUpgrade`: Upgrades the cluster one pod at a time.
*   `Recreate`: Deletes the old cluster and creates a new one.
