# Neo4jEnterpriseCluster

This document provides a reference for the `Neo4jEnterpriseCluster` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `image` | `ImageSpec` | The Neo4j Docker image to use. |
| `topology` | `TopologyConfiguration` | The number of primary and secondary replicas. |
| `storage` | `StorageSpec` | The storage configuration for the cluster. |
| `auth` | `AuthSpec` | The authentication provider and secret. |
| `license` | `LicenseSpec` | The Neo4j Enterprise license secret. |
| `resources` | `corev1.ResourceRequirements` | The CPU and memory resources for the Neo4j pods. |
| `backups` | `BackupsSpec` | The backup configuration. |
| `monitoring` | `MonitoringSpec` | The monitoring configuration. |
| `plugins` | `[]PluginSpec` | The plugin configuration. |
| `autoScaling` | `AutoScalingSpec` | The autoscaling configuration. |
| `multiCluster` | `MultiClusterSpec` | The multi-cluster configuration. |
