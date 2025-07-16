# Neo4jEnterpriseCluster

This document provides a reference for the `Neo4jEnterpriseCluster` Custom Resource Definition (CRD). This resource is used for creating and managing Neo4j Enterprise clusters with high availability.

**Important**: `Neo4jEnterpriseCluster` now requires a minimum cluster topology of either:
- **1 primary + 1 secondary** (minimum cluster configuration)
- **2 or more primaries** (with any number of secondaries)

For single-node deployments, use [`Neo4jEnterpriseStandalone`](neo4jenterprisestandalone.md) instead.

For a high-level overview of how to use this resource, see the [Getting Started Guide](../user_guide/getting_started.md).

## Spec

| Field | Type | Description |
|---|---|---|
| `image` | `ImageSpec` | The Neo4j Docker image to use. |
| `topology` | `TopologyConfiguration` | The number of primary and secondary replicas. **Minimum**: 1 primary + 1 secondary OR 2+ primaries. |
| `storage` | `StorageSpec` | The storage configuration for the cluster. |
| `auth` | `AuthSpec` | The authentication provider and secret. |
| `license` | `LicenseSpec` | The Neo4j Enterprise license secret. |
| `resources` | `corev1.ResourceRequirements` | The CPU and memory resources for the Neo4j pods. |
| `backups` | `BackupsSpec` | The backup configuration. See the [Backup and Restore Guide](../user_guide/guides/backup_restore.md). |
| `monitoring` | `MonitoringSpec` | The monitoring configuration. See the [Monitoring Guide](../user_guide/guides/monitoring.md). |
| `plugins` | `[]PluginSpec` | The plugin configuration. |
| `autoScaling` | `AutoScalingSpec` | The autoscaling configuration. See the [Performance Guide](../user_guide/guides/performance.md). |
| `multiCluster` | `MultiClusterSpec` | The multi-cluster configuration. |
| `config` | `map[string]string` | Custom Neo4j configuration. See [Configuration Best Practices](../user_guide/guides/configuration_best_practices.md). |

## Configuration

The `config` field allows you to specify custom Neo4j configuration settings. For Neo4j 5.26+, ensure you use the correct, non-deprecated settings:

### Example Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  topology:
    primaries: 3
    secondaries: 1
  config:
    # Memory settings (use server.* prefix for 5.26+)
    server.memory.heap.initial_size: "4G"
    server.memory.heap.max_size: "8G"
    server.memory.pagecache.size: "4G"

    # Query logging
    dbms.logs.query.enabled: "INFO"
    dbms.logs.query.threshold: "500ms"

    # Security
    dbms.security.procedures.unrestricted: "gds.*,apoc.*"
```

**Note**: The operator automatically configures clustering and discovery settings. You should not manually set:
- `dbms.cluster.discovery.resolver_type` (set to "K8S")
- `dbms.cluster.discovery.version` (set to "V2_ONLY" for 5.26+)
- Any Kubernetes discovery endpoints

For a complete guide on configuration best practices, see the [Configuration Best Practices Guide](../user_guide/guides/configuration_best_practices.md).
