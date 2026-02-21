# Kubernetes Events Reference

The Neo4j operator emits structured Kubernetes events for all material state
transitions. Unlike pod logs, events persist in the cluster (default 1 hour TTL),
are queryable by reason, and are consumed by monitoring pipelines, GitOps tools,
and alerting systems.

## Viewing Events

```bash
# All events for a specific cluster
kubectl get events --field-selector involvedObject.name=<cluster-name>

# Filter by event reason
kubectl get events --field-selector reason=ClusterFormationStarted

# Watch events in real time
kubectl get events -w

# All Neo4j operator events across namespaces
kubectl get events -A --field-selector involvedObject.apiVersion=neo4j.neo4j.com/v1alpha1
```

## Event Reasons Reference

### Cluster Lifecycle

| Reason | Type | Description |
|---|---|---|
| `ClusterFormationStarted` | Normal | Cluster formation has begun (first time entering Forming phase) |
| `ClusterFormationFailed` | Warning | Cluster formation verification failed |
| `ClusterReady` | Normal | Cluster has reached Ready phase |
| `ValidationFailed` | Warning | Cluster spec validation failed |
| `TopologyWarning` | Warning | Topology validation produced warnings |
| `TopologyPlacementCalculated` | Normal | Topology placement constraints calculated successfully |
| `TopologyPlacementFailed` | Warning | Topology placement constraint calculation failed |
| `PropertyShardingValidationFailed` | Warning | Property sharding configuration validation failed |
| `ServerRoleValidationFailed` | Warning | Server role hint validation failed |
| `RouteAPINotFound` | Warning | OpenShift Route API not available in cluster |
| `MCPApocMissing` | Warning | MCP server requires APOC plugin which is not installed |
| `ReconcileFailed` | Warning | Reconciliation loop encountered an unrecoverable error |

### Rolling Upgrades

| Reason | Type | Description |
|---|---|---|
| `UpgradeStarted` | Normal | Rolling upgrade initiated |
| `UpgradeCompleted` | Normal | Rolling upgrade finished successfully |
| `UpgradePaused` | Normal | Upgrade paused (e.g., due to unhealthy pods) |
| `UpgradeFailed` | Warning | Upgrade failed |
| `UpgradeRolledBack` | Warning | Upgrade rolled back to previous version |

### Backups and Restores

| Reason | Type | Description |
|---|---|---|
| `BackupScheduled` | Normal | Backup CronJob created |
| `BackupStarted` | Normal | Backup job has started |
| `BackupCompleted` | Normal | Backup job completed successfully |
| `BackupFailed` | Warning | Backup job failed |
| `RestoreStarted` | Normal | Restore operation has started |
| `RestoreCompleted` | Normal | Restore operation completed |
| `RestoreFailed` | Warning | Restore operation failed |
| `DatabaseCreateFailed` | Warning | Database creation failed during a restore operation |

### Databases

| Reason | Type | Description |
|---|---|---|
| `DatabaseReady` | Normal | Database is created and online |
| `DatabaseDeleted` | Normal | Database was dropped |
| `DatabaseCreatedFromSeed` | Normal | Database created from a seed URI |
| `CreationFailed` | Warning | Database creation failed |
| `DeletionFailed` | Warning | Database deletion failed |
| `DataImported` | Normal | Initial data imported successfully |
| `DataImportFailed` | Warning | Initial data import failed |
| `DataSeeded` | Normal | Database seeded from URI |
| `ValidationWarning` | Warning | Database spec produced validation warnings |
| `ClusterNotFound` | Warning | Referenced cluster or standalone not found |
| `ClusterNotReady` | Warning | Referenced cluster is not yet Ready |
| `ConnectionFailed` | Warning | Could not connect to Neo4j via Bolt |
| `ClientCreationFailed` | Warning | Failed to create Neo4j Bolt client for the cluster |

### Plugins

| Reason | Type | Description |
|---|---|---|
| `PluginInstalled` | Normal | Plugin successfully installed |
| `PluginInstallFailed` | Warning | Plugin installation failed |
| `PluginEnabled` | Normal | Plugin enabled on cluster |
| `PluginDisabled` | Normal | Plugin disabled on cluster |

### Split-Brain Detection

| Reason | Type | Description |
|---|---|---|
| `SplitBrainDetected` | Warning | Split-brain condition detected in the cluster |
| `SplitBrainRepaired` | Normal | Split-brain condition repaired automatically |
| `SplitBrainRepairFailed` | Warning | Automatic split-brain repair failed |

### Aura Fleet Management

| Reason | Type | Description |
|---|---|---|
| `AuraFleetManagementRegistered` | Normal | Successfully registered with Aura Fleet Management |
| `AuraFleetManagementFailed` | Warning | Aura Fleet Management registration or operation failed |
| `AuraFleetManagementPluginPatchFailed` | Warning | Failed to patch the fleet-management plugin onto the StatefulSet |

### Sharded Databases

| Reason | Type | Description |
|---|---|---|
| `ShardedDatabaseReady` | Normal | Sharded database is created and all shards are online |

## Using Events in Alerting

Events can drive Alertmanager rules via the `kube-state-metrics` `kube_event_*` metrics, or you can use the [Kubernetes Event Exporter](https://github.com/resmoio/kubernetes-event-exporter) to forward events to external systems.

Example: alert on any `BackupFailed` event:

```yaml
# Using kubernetes-event-exporter config
route:
  routes:
    - match:
        reason: "BackupFailed"
      receivers:
        - slack
```

Example Alertmanager rule via kube-state-metrics:

```yaml
- alert: Neo4jBackupFailed
  expr: |
    kube_event_unique_events_total{
      reason="BackupFailed",
      namespace=~"neo4j-.*"
    } > 0
  for: 0m
  labels:
    severity: critical
  annotations:
    summary: "Neo4j backup failed in {{ $labels.namespace }}"
```

## Event Retention

Kubernetes events are stored in etcd and deleted after a configurable TTL (default: 1 hour). For long-term event retention, deploy the [Kubernetes Event Exporter](https://github.com/resmoio/kubernetes-event-exporter) or use a log aggregation tool (Loki, Elasticsearch) that captures event logs from the API server.

To check the current event TTL on your cluster:

```bash
kubectl get pods -n kube-system -l component=kube-apiserver -o jsonpath='{.items[0].spec.containers[0].command}' | tr ',' '\n' | grep event-ttl
```
