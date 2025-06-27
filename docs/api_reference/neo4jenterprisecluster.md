# Neo4jEnterpriseCluster

This document provides a reference for the `Neo4jEnterpriseCluster` Custom Resource Definition (CRD). This is the main resource for creating and managing Neo4j clusters.

For a high-level overview of how to use this resource, see the [Getting Started Guide](../user_guide/getting_started.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `image` | `ImageSpec` | The Neo4j Docker image to use. |
| `topology` | `TopologyConfiguration` | The number of primary and secondary replicas. |
| `storage` | `StorageSpec` | The storage configuration for the cluster. |
| `auth` | `AuthSpec` | The authentication provider and secret. |
| `license` | `LicenseSpec` | The Neo4j Enterprise license secret. |
| `resources` | `corev1.ResourceRequirements` | The CPU and memory resources for the Neo4j pods. |
| `backups` | `BackupsSpec` | The backup configuration. See the [Backup and Restore Guide](../user_guide/guides/backup_restore.md). |
| `monitoring` | `MonitoringSpec` | The monitoring configuration. See the [Monitoring Guide](../user_guide/guides/monitoring.md). |
| `plugins` | `[]PluginSpec` | The plugin configuration. |
| `autoScaling` | `AutoScalingSpec` | The autoscaling configuration. See the [Performance Guide](../user_guide/guides/performance.md). |
| `multiCluster` | `MultiClusterSpec` | The multi-cluster configuration. |
