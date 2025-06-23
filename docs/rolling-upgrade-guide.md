# Neo4j Rolling Upgrade Guide

This document describes the intelligent rolling upgrade functionality implemented for the Neo4j Enterprise Operator for Kubernetes.

## Overview

The Neo4j Operator now supports intelligent online rolling upgrades for minor and patch releases, ensuring zero-downtime upgrades while maintaining cluster consistency and data integrity.

## Features

### 🚀 Intelligent Upgrade Orchestration
- **Leader-aware upgrades**: Preserves cluster leadership during upgrades
- **Sequential upgrade phases**: Secondaries → Non-leader primaries → Leader
- **Automatic rollback**: On failure with configurable auto-pause
- **Health validation**: Pre and post-upgrade cluster health checks

### ⚙️ Configurable Upgrade Strategy
- **Rolling vs Recreate**: Choose between online rolling upgrades or full recreation
- **Customizable timeouts**: Configure upgrade, health check, and stabilization timeouts
- **Failure handling**: Auto-pause on failure or continue with best effort

### 📊 Comprehensive Monitoring
- **Progress tracking**: Real-time upgrade progress with detailed status
- **Metrics collection**: Prometheus metrics for upgrade operations
- **Event logging**: Kubernetes events for upgrade lifecycle

## Configuration

### Upgrade Strategy Specification

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"

  upgradeStrategy:
    # Strategy type: RollingUpgrade (default) or Recreate
    strategy: RollingUpgrade

    # Enable cluster health validation before starting upgrade
    preUpgradeHealthCheck: true

    # Enable health validation after upgrade completion
    postUpgradeHealthCheck: true

    # Maximum unavailable replicas during upgrade (default: 1)
    maxUnavailableDuringUpgrade: 1

    # Timeout for entire upgrade process (default: 30m)
    upgradeTimeout: "30m"

    # Timeout for health checks (default: 5m)
    healthCheckTimeout: "5m"

    # Cluster stabilization timeout (default: 3m)
    stabilizationTimeout: "3m"

    # Pause upgrade on failure for manual intervention (default: true)
    autoPauseOnFailure: true
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `strategy` | string | `RollingUpgrade` | Upgrade strategy: `RollingUpgrade` or `Recreate` |
| `preUpgradeHealthCheck` | bool | `true` | Validate cluster health before upgrade |
| `postUpgradeHealthCheck` | bool | `true` | Validate cluster health after upgrade |
| `maxUnavailableDuringUpgrade` | int | `1` | Max replicas unavailable during upgrade |
| `upgradeTimeout` | duration | `30m` | Total upgrade timeout |
| `healthCheckTimeout` | duration | `5m` | Individual health check timeout |
| `stabilizationTimeout` | duration | `3m` | Cluster stabilization wait time |
| `autoPauseOnFailure` | bool | `true` | Pause on failure for manual intervention |

## Upgrade Process

### Phase 1: Pre-Upgrade Validation
1. **Cluster Health Check**: Verify all nodes are healthy and reachable
2. **Version Compatibility**: Validate upgrade path (minor/patch only)
3. **Resource Availability**: Ensure sufficient cluster resources
4. **Consensus Verification**: Confirm cluster has proper consensus

### Phase 2: Secondary Nodes Upgrade
1. **Update StatefulSet**: Modify secondary StatefulSet with new image
2. **Rolling Update**: Kubernetes performs rolling update of secondaries
3. **Health Monitoring**: Wait for all secondary nodes to become ready
4. **Cluster Stabilization**: Ensure cluster maintains quorum

### Phase 3: Primary Nodes Upgrade (Leader-Aware)
1. **Leader Detection**: Identify current cluster leader
2. **Non-Leader Update**: Use StatefulSet partitioning to upgrade non-leaders first
3. **Health Validation**: Verify cluster stability after non-leader upgrades
4. **Leader Upgrade**: Finally upgrade the leader node
5. **Leader Election**: Wait for new leader election if needed

### Phase 4: Post-Upgrade Validation
1. **Health Verification**: Comprehensive cluster health check
2. **Version Confirmation**: Verify all nodes running target version
3. **Consensus Check**: Ensure cluster consensus is maintained
4. **Status Update**: Mark upgrade as complete

## Monitoring Upgrade Progress

### Status Fields

The cluster status includes detailed upgrade progress information:

```yaml
status:
  phase: Ready
  version: "5.27-enterprise"
  lastUpgradeTime: "2024-01-15T10:30:00Z"

  upgradeStatus:
    phase: Completed
    startTime: "2024-01-15T10:00:00Z"
    completionTime: "2024-01-15T10:30:00Z"
    currentStep: "Rolling upgrade completed successfully"
    previousVersion: "5.26-enterprise"
    targetVersion: "5.27-enterprise"

    progress:
      total: 5
      upgraded: 5
      inProgress: 0
      pending: 0

      primaries:
        total: 3
        upgraded: 3
        pending: 0
        currentLeader: "my-cluster-primary-2"

      secondaries:
        total: 2
        upgraded: 2
        pending: 0
```

### Upgrade Phases

| Phase | Description |
|-------|-------------|
| `Pending` | Upgrade queued but not started |
| `InProgress` | Upgrade actively running |
| `Paused` | Upgrade paused due to failure (if `autoPauseOnFailure: true`) |
| `Completed` | Upgrade completed successfully |
| `Failed` | Upgrade failed and cannot continue |

### Kubernetes Events

The operator emits events during upgrade process:

```bash
# View upgrade events
kubectl get events --field-selector involvedObject.name=my-cluster

# Example events:
# Normal   UpgradeStarted     Rolling upgrade started from 5.26-enterprise to 5.27-enterprise
# Normal   SecondariesUpgraded Secondary nodes upgraded successfully
# Normal   PrimariesUpgraded   Primary nodes upgraded successfully
# Normal   UpgradeCompleted    Rolling upgrade completed successfully
# Warning  UpgradePaused      Upgrade paused due to health check failure
```

## Triggering Upgrades

### Simple Image Tag Update

```bash
# Trigger upgrade by updating image tag
kubectl patch neo4jenterprisecluster my-cluster \
  --type='merge' \
  --patch='{"spec":{"image":{"tag":"5.27-enterprise"}}}'
```

### Declarative Update

```yaml
# Update the cluster specification
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  image:
    repo: neo4j
    tag: "5.27-enterprise"  # Changed from 5.26-enterprise
```

## Supported Upgrade Paths

### Version Support Matrix

| From Version | To Version | Support Level |
|--------------|------------|---------------|
| 5.26.x | 5.26.y (y > x) | ✅ Patch upgrades |
| 5.26.x | 5.27.x | ✅ Minor upgrades |
| 5.27.x | 5.28.x | ✅ Minor upgrades |
| 5.26+ | 2025.x.x | ✅ SemVer to CalVer upgrade |
| 2025.1.x | 2025.1.y (y > x) | ✅ Patch upgrades |
| 2025.1.x | 2025.2.x | ✅ Minor upgrades |
| 2025.x.x | 2026.x.x | ✅ Year upgrades |
| 5.x.x | 4.x.x | ❌ Downgrades not supported |
| 2025.x.x | 5.x.x | ❌ CalVer to SemVer not supported |
| 2025.x.x | 2024.x.x | ❌ Year downgrades not supported |

### Neo4j Versioning Schemes

Neo4j has transitioned from **Semantic Versioning (SemVer)** to **Calendar Versioning (CalVer)**:

#### **Legacy SemVer Format (5.x.x)**
- **Pattern**: `MAJOR.MINOR.PATCH` (e.g., 5.26.1, 5.27.0)
- **Used until**: Neo4j 5.x series
- **Support**: Neo4j 5.26+ is supported

#### **New CalVer Format (2025.x.x)**
- **Pattern**: `YEAR.MINOR.PATCH` (e.g., 2025.1.0, 2025.2.1)
- **Used from**: Neo4j 2025.1.0 onwards
- **Support**: All CalVer versions are supported

### Version Validation

The operator automatically validates upgrade paths and supports both versioning schemes:

#### **✅ Supported Upgrade Paths**
- **SemVer Patch upgrades**: 5.26.1 → 5.26.2
- **SemVer Minor upgrades**: 5.26.x → 5.27.x
- **CalVer Patch upgrades**: 2025.1.1 → 2025.1.2
- **CalVer Minor upgrades**: 2025.1.x → 2025.2.x
- **CalVer Year upgrades**: 2025.x.x → 2026.x.x
- **SemVer to CalVer**: 5.26+ → 2025.x.x

#### **❌ Blocked Operations**
- **Major SemVer upgrades**: 5.x.x → 6.x.x (not applicable)
- **Any downgrades**: Higher version → Lower version
- **CalVer to SemVer**: 2025.x.x → 5.x.x (backwards incompatible)
- **Unsupported SemVer**: 5.25.x → Any (requires 5.26+)

### Example Upgrade Scenarios

```bash
# SemVer patch upgrade
kubectl patch neo4jenterprisecluster my-cluster \
  --patch='{"spec":{"image":{"tag":"5.26.2-enterprise"}}}'

# SemVer minor upgrade
kubectl patch neo4jenterprisecluster my-cluster \
  --patch='{"spec":{"image":{"tag":"5.27.0-enterprise"}}}'

# SemVer to CalVer upgrade
kubectl patch neo4jenterprisecluster my-cluster \
  --patch='{"spec":{"image":{"tag":"2025.1.0-enterprise"}}}'

# CalVer minor upgrade
kubectl patch neo4jenterprisecluster my-cluster \
  --patch='{"spec":{"image":{"tag":"2025.2.0-enterprise"}}}'

# CalVer year upgrade
kubectl patch neo4jenterprisecluster my-cluster \
  --patch='{"spec":{"image":{"tag":"2026.1.0-enterprise"}}}'
```

## Error Handling and Recovery

### Automatic Failure Handling

When `autoPauseOnFailure: true`:
1. Upgrade pauses on first failure
2. Cluster status shows `Paused` phase
3. Administrator can investigate and fix issues
4. Resume upgrade by fixing the issue (cluster will auto-retry)

### Manual Recovery

```bash
# Check upgrade status
kubectl describe neo4jenterprisecluster my-cluster

# View upgrade logs
kubectl logs deployment/neo4j-operator-controller-manager -n neo4j-operator-system

# Force retry by updating a dummy annotation
kubectl annotate neo4jenterprisecluster my-cluster upgrade.neo4j.com/retry="$(date)"
```

### Common Issues and Solutions

| Issue | Symptom | Solution |
|-------|---------|----------|
| Health check timeout | Upgrade stuck in validation | Check Neo4j logs, verify network connectivity |
| Leader election failure | No leader after upgrade | Wait for election or restart problematic pod |
| Resource constraints | Pods fail to schedule | Ensure adequate cluster resources |
| Image pull failure | Upgrade fails at start | Verify image availability and pull secrets |

## Best Practices

### Pre-Upgrade Checklist
- [ ] Verify cluster health: `kubectl get neo4jenterprisecluster`
- [ ] Check resource availability: `kubectl top nodes`
- [ ] Backup critical data if needed
- [ ] Plan maintenance window (even for zero-downtime)
- [ ] Verify target image availability

### During Upgrade
- [ ] Monitor upgrade progress via cluster status
- [ ] Watch for Kubernetes events
- [ ] Monitor application connectivity
- [ ] Check operator logs for detailed progress

### Post-Upgrade
- [ ] Verify cluster health and consensus
- [ ] Test application connectivity
- [ ] Validate Neo4j version: `CALL dbms.components()`
- [ ] Check performance metrics

## Prometheus Metrics

The operator exposes upgrade-specific metrics:

```prometheus
# Total upgrade attempts
neo4j_operator_upgrade_total{cluster="my-cluster", result="success"}

# Upgrade duration by phase
neo4j_operator_upgrade_duration_seconds{cluster="my-cluster", phase="primaries"}

# Current upgrade status
neo4j_operator_upgrade_status{cluster="my-cluster", phase="completed"}
```

## Troubleshooting

### Enable Debug Logging

```yaml
# Add to operator deployment
env:
- name: LOG_LEVEL
  value: "debug"
```

### Common Commands

```bash
# Check cluster status
kubectl get neo4jenterprisecluster my-cluster -o yaml

# View detailed events
kubectl describe neo4jenterprisecluster my-cluster

# Check StatefulSet status
kubectl get statefulsets -l app.kubernetes.io/name=neo4j

# View pod status
kubectl get pods -l app.kubernetes.io/name=neo4j

# Access Neo4j logs
kubectl logs my-cluster-primary-0 -c neo4j

# Check operator logs
kubectl logs deployment/neo4j-operator-controller-manager -n neo4j-operator-system
```

## Security Considerations

### RBAC Requirements

The operator requires additional permissions for rolling upgrades:

```yaml
rules:
- apiGroups: ["apps"]
  resources: ["statefulsets"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
```

### Network Policies

Ensure operator can communicate with Neo4j pods:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: neo4j-operator-access
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: neo4j-operator-system
```

## Conclusion

The intelligent rolling upgrade feature provides:
- **Zero-downtime upgrades** for Neo4j clusters
- **Comprehensive validation** and error handling
- **Detailed monitoring** and observability
- **Flexible configuration** for different environments

This ensures production Neo4j clusters can be kept up-to-date safely and efficiently.
