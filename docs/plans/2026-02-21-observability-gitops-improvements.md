# Observability & GitOps Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire up structured Kubernetes Events, activate existing Prometheus metrics, add ArgoCD/Flux health checks, standardize status conditions, and fix image pull secret and Helm chart versioning gaps.

**Architecture:** All controllers already have an `EventRecorder` and `internal/metrics/metrics.go` already defines metric types — the work is primarily _wiring_ existing infrastructure consistently, then adding the small gaps (ArgoCD Lua scripts, condition constants, pull secrets, Helm versioning).

**Tech Stack:** Go 1.24, controller-runtime v0.21, prometheus/client_golang v1.22, Helm 3, GitHub Actions, Kubernetes Events API.

---

## Codebase Quick-Reference

| Symbol | File |
|---|---|
| `BuildPodSpecForEnterprise` | `internal/resources/cluster.go:1200` |
| `buildStatefulSetForEnterprise` | `internal/resources/cluster.go:198` |
| `Neo4jEnterpriseClusterSpec.Image.PullSecrets` | `api/v1alpha1/neo4jenterprisecluster_types.go:108` |
| `ClusterMetrics` | `internal/metrics/metrics.go:305` |
| `BackupMetrics` | `internal/metrics/metrics.go:389` |
| `ReconcileMetrics` | `internal/metrics/metrics.go:256` |
| Cluster reconciler `Reconcile()` | `internal/controller/neo4jenterprisecluster_controller.go` |
| Backup reconciler `Reconcile()` | `internal/controller/neo4jbackup_controller.go` |
| Chart.yaml | `charts/neo4j-operator/Chart.yaml` |
| release.yml Helm packaging | `.github/workflows/release.yml:146-151` |

---

## Task 1: Event Reason Constants (2.1)

**Purpose:** Replace all string-literal event reasons across controllers with typed constants. This prevents typos, enables `kubectl get events --field-selector reason=X`, and gives ArgoCD/monitoring tools a stable surface to key off.

**Files:**
- Create: `internal/controller/events.go`
- Modify: `internal/controller/neo4jenterprisecluster_controller.go`
- Modify: `internal/controller/neo4jenterprisestandalone_controller.go`
- Modify: `internal/controller/neo4jdatabase_controller.go`
- Modify: `internal/controller/neo4jbackup_controller.go`
- Modify: `internal/controller/neo4jrestore_controller.go`
- Modify: `internal/controller/plugin_controller.go`
- Modify: `internal/controller/neo4jshardeddatabase_controller.go`

### Step 1: Create `internal/controller/events.go`

```go
// Package controller provides event reason constants for all Neo4j operator controllers.
package controller

// Cluster formation events
const (
    EventReasonClusterFormationStarted  = "ClusterFormationStarted"
    EventReasonClusterFormationFailed   = "ClusterFormationFailed"
    EventReasonClusterReady             = "ClusterReady"
    EventReasonTopologyWarning          = "TopologyWarning"
    EventReasonValidationFailed         = "ValidationFailed"
    EventReasonTopologyPlacementFailed  = "TopologyPlacementFailed"
    EventReasonTopologyPlacementCalc    = "TopologyPlacementCalculated"
    EventReasonPropertyShardingFailed   = "PropertyShardingValidationFailed"
    EventReasonServerRoleFailed         = "ServerRoleValidationFailed"
    EventReasonRouteAPINotFound         = "RouteAPINotFound"
    EventReasonMCPApocMissing           = "MCPApocMissing"
    EventReasonReconcileFailed          = "ReconcileFailed"
)

// Rolling upgrade events
const (
    EventReasonUpgradeStarted   = "UpgradeStarted"
    EventReasonUpgradeCompleted = "UpgradeCompleted"
    EventReasonUpgradePaused    = "UpgradePaused"
    EventReasonUpgradeFailed    = "UpgradeFailed"
    EventReasonUpgradeRolledBack = "UpgradeRolledBack"
)

// Backup and restore events
const (
    EventReasonBackupScheduled  = "BackupScheduled"
    EventReasonBackupStarted    = "BackupStarted"
    EventReasonBackupCompleted  = "BackupCompleted"
    EventReasonBackupFailed     = "BackupFailed"
    EventReasonRestoreStarted   = "RestoreStarted"
    EventReasonRestoreCompleted = "RestoreCompleted"
    EventReasonRestoreFailed    = "RestoreFailed"
    EventReasonDatabaseCreateFailed = "DatabaseCreateFailed"
)

// Database events
const (
    EventReasonClusterNotFound      = "ClusterNotFound"
    EventReasonDatabaseReady        = "DatabaseReady"
    EventReasonDatabaseDeleted      = "DatabaseDeleted"
    EventReasonDatabaseCreatedSeed  = "DatabaseCreatedFromSeed"
    EventReasonCreationFailed       = "CreationFailed"
    EventReasonDeletionFailed       = "DeletionFailed"
    EventReasonDataImported         = "DataImported"
    EventReasonDataImportFailed     = "DataImportFailed"
    EventReasonDataSeeded           = "DataSeeded"
    EventReasonValidationWarning    = "ValidationWarning"
)

// Plugin events
const (
    EventReasonPluginInstalled     = "PluginInstalled"
    EventReasonPluginInstallFailed = "PluginInstallFailed"
    EventReasonPluginEnabled       = "PluginEnabled"
    EventReasonPluginDisabled      = "PluginDisabled"
)

// Split-brain events
const (
    EventReasonSplitBrainDetected    = "SplitBrainDetected"
    EventReasonSplitBrainRepaired    = "SplitBrainRepaired"
    EventReasonSplitBrainRepairFailed = "SplitBrainRepairFailed"
)

// Aura Fleet Management events
const (
    EventReasonAuraFleetFailed              = "AuraFleetManagementFailed"
    EventReasonAuraFleetPluginPatchFailed   = "AuraFleetManagementPluginPatchFailed"
    EventReasonAuraFleetRegistered          = "AuraFleetManagementRegistered"
)

// Sharded database events
const (
    EventReasonShardedDatabaseReady  = "ShardedDatabaseReady"
    EventReasonClusterNotReady       = "ClusterNotReady"
    EventReasonClientCreationFailed  = "ClientCreationFailed"
)
```

### Step 2: Add missing events to cluster controller

Open `internal/controller/neo4jenterprisecluster_controller.go`.

**Add `ClusterFormationStarted`** — emitted once when the cluster first transitions to `Forming` phase.
Find the block around line 414 where `verifyNeo4jClusterFormation` is called and the result is `!clusterFormed`. Before the `updateClusterStatus(ctx, cluster, "Forming", ...)` call, emit:

```go
if cluster.Status.Phase != "Forming" && cluster.Status.Phase != "" {
    r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonClusterFormationStarted,
        "Neo4j cluster formation started")
}
```

**Add `ClusterFormationFailed`** — emitted when `verifyNeo4jClusterFormation` returns an error (not just `!clusterFormed`). In the `err != nil` branch after calling `verifyNeo4jClusterFormation`:

```go
r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonClusterFormationFailed,
    "Cluster formation check failed: %v", err)
```

**Add `UpgradeStarted`** — emitted at the top of `handleRollingUpgrade`, before `upgrader.ExecuteRollingUpgrade(...)`:

```go
r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonUpgradeStarted,
    "Rolling upgrade started: %s -> %s", cluster.Status.Version, cluster.Spec.Image.Tag)
```

**Add `UpgradeFailed`** — emitted in the `if err := upgrader.ExecuteRollingUpgrade(...)` error branch when `!AutoPauseOnFailure`:

```go
r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonUpgradeFailed,
    "Rolling upgrade failed: %v", err)
```

### Step 3: Replace all string literals with constants

For each file listed above, replace every `"ClusterReady"`, `"UpgradeCompleted"`, etc. string literal with the corresponding constant from `events.go`. Use exact search-replace for each:

Example (cluster controller):
- `"ClusterReady"` → `EventReasonClusterReady`
- `"UpgradeCompleted"` → `EventReasonUpgradeCompleted`
- `"UpgradePaused"` → `EventReasonUpgradePaused`
- `"SplitBrainDetected"` → `EventReasonSplitBrainDetected`
- etc.

### Step 4: Add plugin events to plugin controller

In `internal/controller/plugin_controller.go`, find the reconcile loop and add events at the key outcomes:

```go
// On successful install
r.Recorder.Eventf(&plugin, corev1.EventTypeNormal, EventReasonPluginInstalled,
    "Plugin %s version %s installed successfully", plugin.Spec.Name, plugin.Spec.Version)

// On install failure
r.Recorder.Eventf(&plugin, corev1.EventTypeWarning, EventReasonPluginInstallFailed,
    "Plugin %s installation failed: %v", plugin.Spec.Name, err)
```

### Step 5: Verify events compile

```bash
cd /Users/priyolahiri/Code/neo4j-kubernetes-operator
go build ./internal/controller/...
```

Expected: no errors.

### Step 6: Commit

```bash
git add internal/controller/events.go internal/controller/neo4jenterprisecluster_controller.go \
    internal/controller/neo4jenterprisestandalone_controller.go \
    internal/controller/neo4jdatabase_controller.go \
    internal/controller/neo4jbackup_controller.go \
    internal/controller/neo4jrestore_controller.go \
    internal/controller/plugin_controller.go \
    internal/controller/neo4jshardeddatabase_controller.go
git commit -m "feat(events): add event reason constants and missing lifecycle events"
```

---

## Task 2: Wire Prometheus Metrics Into Controllers (2.2)

**Context:** `internal/metrics/metrics.go` already defines `ClusterMetrics`, `BackupMetrics`, and `ReconcileMetrics`. None of them are called from the cluster or backup controllers yet. The goal is to call them at the right points.

**Files:**
- Modify: `internal/controller/neo4jenterprisecluster_controller.go`
- Modify: `internal/controller/neo4jbackup_controller.go`
- Create: (no new files — all metric types already exist)

### Step 1: Add `ClusterMetrics` field to cluster reconciler struct

In `internal/controller/neo4jenterprisecluster_controller.go`, find the `Neo4jEnterpriseClusterReconciler` struct (around line 56). It already has `Recorder record.EventRecorder`. Do NOT add a field — instead, create metrics instances inline during reconcile because cluster name/namespace come from the object being reconciled.

### Step 2: Record cluster health and replica counts at reconcile end

At the point in `Reconcile()` where status is updated to `"Ready"` (just after `r.updateClusterStatus(ctx, cluster, "Ready", ...)`), add:

```go
clusterMetrics := metrics.NewClusterMetrics(cluster.Name, cluster.Namespace)
clusterMetrics.RecordClusterHealth(true)
// Servers field is the desired count; use Status.Replicas if available, fallback to Spec
clusterMetrics.RecordClusterReplicas(cluster.Spec.Topology.Servers, 0)
```

At the point in `Reconcile()` where status is updated to `"Failed"` or `"Forming"`, add:

```go
clusterMetrics := metrics.NewClusterMetrics(cluster.Name, cluster.Namespace)
clusterMetrics.RecordClusterHealth(false)
```

**Import** `"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/metrics"` if not already present.

### Step 3: Add cluster phase gauge metric

In `internal/metrics/metrics.go`, add a new GaugeVec for cluster phase after the `clusterHealthy` definition (around line 86):

```go
clusterPhase = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Subsystem: subsystem,
        Name:      "cluster_phase",
        Help:      "Current phase of the Neo4j cluster (1 = active phase, 0 = not in this phase). Labels: cluster_name, namespace, phase.",
    },
    []string{LabelClusterName, LabelNamespace, LabelPhase},
)
```

Register it in the `init()` function alongside the others:

```go
metrics.Registry.MustRegister(clusterPhase)
```

Add a method on `ClusterMetrics`:

```go
// RecordClusterPhase records the cluster phase as a labelled gauge.
// It sets 1.0 for the active phase and 0.0 for all others (Pending/Forming/Ready/Failed/Degraded).
func (m *ClusterMetrics) RecordClusterPhase(phase string) {
    for _, p := range []string{"Pending", "Forming", "Ready", "Failed", "Degraded", "Upgrading"} {
        v := 0.0
        if p == phase {
            v = 1.0
        }
        clusterPhase.WithLabelValues(m.clusterName, m.namespace, p).Set(v)
    }
}
```

Call it in the cluster reconciler alongside `RecordClusterHealth`.

### Step 4: Wire `ReconcileMetrics` into cluster reconciler

At the very top of `Reconcile()`, after fetching the cluster object, add:

```go
reconcileStart := time.Now()
reconcileMetrics := metrics.NewReconcileMetrics(cluster.Name, cluster.Namespace)
defer func() {
    success := cluster.Status.Phase != "Failed"
    reconcileMetrics.RecordReconcile(ctx, "cluster", time.Since(reconcileStart), success)
}()
```

**Import** `"time"` if not already present (it almost certainly is).

### Step 5: Wire `BackupMetrics` into backup controller

In `internal/controller/neo4jbackup_controller.go`, find the backup execution logic around line 237. At backup start:

```go
backupStart := time.Now()
backupMetrics := metrics.NewBackupMetrics(backup.Name, backup.Namespace)
```

After the backup completes successfully (around line 247):

```go
backupMetrics.RecordBackup(ctx, true, time.Since(backupStart), 0)
```

After a backup failure (around line 258):

```go
backupMetrics.RecordBackup(ctx, false, time.Since(backupStart), 0)
```

### Step 6: Add split-brain counter metric

In `internal/metrics/metrics.go`, add after the other cluster metrics:

```go
splitBrainDetected = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Subsystem: subsystem,
        Name:      "split_brain_detected_total",
        Help:      "Total number of split-brain scenarios detected",
    },
    []string{LabelClusterName, LabelNamespace},
)
```

Register it in `init()`. Add a function:

```go
// RecordSplitBrainDetected increments the split-brain detection counter.
func RecordSplitBrainDetected(clusterName, namespace string) {
    splitBrainDetected.WithLabelValues(clusterName, namespace).Inc()
}
```

In `internal/controller/neo4jenterprisecluster_controller.go`, find the `SplitBrainDetected` event emission (around line 1198) and add:

```go
metrics.RecordSplitBrainDetected(cluster.Name, cluster.Namespace)
```

### Step 7: Verify build

```bash
cd /Users/priyolahiri/Code/neo4j-kubernetes-operator
go build ./...
```

Expected: no errors.

### Step 8: Run unit tests

```bash
make test-unit
```

Expected: PASS.

### Step 9: Commit

```bash
git add internal/metrics/metrics.go \
    internal/controller/neo4jenterprisecluster_controller.go \
    internal/controller/neo4jbackup_controller.go
git commit -m "feat(metrics): wire cluster health, phase, reconcile, backup, and split-brain metrics"
```

---

## Task 3: ArgoCD & Flux Health Checks (2.3)

**Context:** ArgoCD does not understand custom status fields. Without a health check, a `Neo4jEnterpriseCluster` with `status.phase=Ready` still shows `Progressing` in ArgoCD. The fix is a Lua health check ConfigMap that ArgoCD reads, telling it how to interpret `status.phase` for each CRD group.

**Files:**
- Create: `docs/gitops/argocd-health-checks.yaml`
- Create: `docs/gitops/README.md` (single-file, very brief)

### Step 1: Create ArgoCD health check ConfigMap

ArgoCD reads custom health checks from `argocd-cm` ConfigMap in the `argocd` namespace. The key format is `resource.customizations.health.<group>_<kind>`.

Create `docs/gitops/argocd-health-checks.yaml`:

```yaml
# Apply this to your ArgoCD namespace to enable health checks for Neo4j operator resources.
# Usage: kubectl patch configmap argocd-cm -n argocd --patch-file docs/gitops/argocd-health-checks.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
  namespace: argocd
data:
  resource.customizations.health.neo4j.neo4j.com_Neo4jEnterpriseCluster: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for cluster status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Ready" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Neo4j cluster is ready"
    elseif phase == "Failed" or phase == "Degraded" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Neo4j cluster is in a failed state"
    elseif phase == "Upgrading" or phase == "Forming" or phase == "Creating" or phase == "Pending" then
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Neo4j cluster is " .. phase)
    else
      hs.status = "Progressing"
      hs.message = "Neo4j cluster phase: " .. phase
    end
    return hs

  resource.customizations.health.neo4j.neo4j.com_Neo4jEnterpriseStandalone: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for standalone status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Ready" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Neo4j standalone is ready"
    elseif phase == "Failed" or phase == "Degraded" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Neo4j standalone is in a failed state"
    else
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Neo4j standalone is " .. phase)
    end
    return hs

  resource.customizations.health.neo4j.neo4j.com_Neo4jDatabase: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for database status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Ready" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Database is ready"
    elseif phase == "Failed" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Database creation failed"
    else
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Database is " .. phase)
    end
    return hs

  resource.customizations.health.neo4j.neo4j.com_Neo4jBackup: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for backup status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Succeeded" or phase == "Ready" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Backup completed"
    elseif phase == "Failed" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Backup failed"
    else
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Backup is " .. phase)
    end
    return hs

  resource.customizations.health.neo4j.neo4j.com_Neo4jRestore: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for restore status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Completed" or phase == "Ready" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Restore completed"
    elseif phase == "Failed" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Restore failed"
    else
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Restore is " .. phase)
    end
    return hs

  resource.customizations.health.neo4j.neo4j.com_Neo4jPlugin: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for plugin status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Ready" or phase == "Installed" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Plugin is installed and ready"
    elseif phase == "Failed" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Plugin installation failed"
    else
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Plugin is " .. phase)
    end
    return hs

  resource.customizations.health.neo4j.neo4j.com_Neo4jShardedDatabase: |
    hs = {}
    if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
      hs.status = "Progressing"
      hs.message = "Waiting for sharded database status"
      return hs
    end
    phase = obj.status.phase
    if phase == "Ready" then
      hs.status = "Healthy"
      hs.message = obj.status.message or "Sharded database is ready"
    elseif phase == "Failed" then
      hs.status = "Degraded"
      hs.message = obj.status.message or "Sharded database failed"
    else
      hs.status = "Progressing"
      hs.message = obj.status.message or ("Sharded database is " .. phase)
    end
    return hs
```

**Note on ArgoCD API version:** Modern ArgoCD (v2.6+) supports `resource.customizations.health.<group>_<kind>` as a top-level key in `argocd-cm`. Older versions require a nested YAML structure — document this in the README.

### Step 2: Create `docs/gitops/README.md`

Write a brief README explaining:
1. Apply the ArgoCD health check ConfigMap using `kubectl patch`.
2. For Flux: Flux uses the `Ready` condition from `status.conditions` automatically if the CRD uses `metav1.Condition` with type `Ready` — no extra config needed once Task 4 adds that condition.
3. Link to ArgoCD docs for custom health checks.

### Step 3: Add Prometheus ServiceMonitor to Helm chart

In `charts/neo4j-operator/templates/`, create `servicemonitor.yaml`:

```yaml
{{- if and .Values.metrics.enabled .Values.metrics.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "neo4j-operator.fullname" . }}
  labels:
    {{- include "neo4j-operator.labels" . | nindent 4 }}
    {{- with .Values.metrics.serviceMonitor.labels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
spec:
  selector:
    matchLabels:
      {{- include "neo4j-operator.selectorLabels" . | nindent 6 }}
  endpoints:
    - port: metrics
      path: /metrics
      interval: {{ .Values.metrics.serviceMonitor.interval | default "30s" }}
      scrapeTimeout: {{ .Values.metrics.serviceMonitor.scrapeTimeout | default "10s" }}
{{- end }}
```

Add to `values.yaml` under `metrics.serviceMonitor`:
```yaml
metrics:
  serviceMonitor:
    enabled: false
    interval: "30s"
    scrapeTimeout: "10s"
    labels: {}
```

### Step 4: Commit

```bash
git add docs/gitops/ charts/neo4j-operator/templates/servicemonitor.yaml charts/neo4j-operator/values.yaml
git commit -m "feat(gitops): add ArgoCD health checks and Prometheus ServiceMonitor for Helm"
```

---

## Task 4: Standardize Status Conditions (2.4)

**Context:** All 7 CRDs already have `Status.Conditions []metav1.Condition`. The problem is no constants exist for condition type names or reasons, so each controller uses ad-hoc strings. Flux's automatic health check requires a condition of type `Ready`. `ObservedGeneration` exists in some CRDs but is not consistently written.

**Files:**
- Create: `internal/controller/conditions.go`
- Modify: `internal/controller/neo4jenterprisecluster_controller.go`
- Modify: `internal/controller/neo4jenterprisestandalone_controller.go`
- Modify: `internal/controller/neo4jdatabase_controller.go`
- Modify: `internal/controller/neo4jbackup_controller.go`
- Modify: `internal/controller/neo4jrestore_controller.go`
- Modify: `internal/controller/plugin_controller.go`
- Modify: `internal/controller/neo4jshardeddatabase_controller.go`

### Step 1: Create `internal/controller/conditions.go`

```go
package controller

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

// Standard condition types following the Kubernetes API conventions.
// Flux health checks use the "Ready" condition type automatically.
const (
    ConditionTypeAvailable   = "Available"
    ConditionTypeProgressing = "Progressing"
    ConditionTypeDegraded    = "Degraded"
    ConditionTypeReady       = "Ready"
)

// Reason constants for the Ready/Available condition.
const (
    ConditionReasonReady           = "ClusterReady"
    ConditionReasonForming         = "ClusterForming"
    ConditionReasonFailed          = "ReconciliationFailed"
    ConditionReasonUpgrading       = "UpgradeInProgress"
    ConditionReasonPending         = "Pending"
    ConditionReasonDatabaseReady   = "DatabaseReady"
    ConditionReasonDatabaseFailed  = "DatabaseCreationFailed"
    ConditionReasonBackupSucceeded = "BackupSucceeded"
    ConditionReasonBackupFailed    = "BackupFailed"
    ConditionReasonRestoreComplete = "RestoreCompleted"
    ConditionReasonRestoreFailed   = "RestoreFailed"
    ConditionReasonPluginInstalled = "PluginInstalled"
    ConditionReasonPluginFailed    = "PluginInstallFailed"
)

// SetReadyCondition sets the standard "Ready" condition on an object's conditions slice.
// Returns true if the condition changed.
func SetReadyCondition(conditions *[]metav1.Condition, generation int64, status metav1.ConditionStatus, reason, message string) bool {
    existing := findCondition(*conditions, ConditionTypeReady)
    newCond := metav1.Condition{
        Type:               ConditionTypeReady,
        Status:             status,
        ObservedGeneration: generation,
        LastTransitionTime: metav1.Now(),
        Reason:             reason,
        Message:            message,
    }
    if existing != nil && existing.Status == status && existing.Reason == reason {
        // Update ObservedGeneration and message without changing LastTransitionTime
        existing.ObservedGeneration = generation
        existing.Message = message
        return false
    }
    *conditions = upsertCondition(*conditions, newCond)
    return true
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
    for i := range conditions {
        if conditions[i].Type == condType {
            return &conditions[i]
        }
    }
    return nil
}

func upsertCondition(conditions []metav1.Condition, cond metav1.Condition) []metav1.Condition {
    for i := range conditions {
        if conditions[i].Type == cond.Type {
            conditions[i] = cond
            return conditions
        }
    }
    return append(conditions, cond)
}

// PhaseToConditionStatus maps a phase string to a metav1.ConditionStatus and ready-condition reason.
func PhaseToConditionStatus(phase string) (metav1.ConditionStatus, string) {
    switch phase {
    case "Ready":
        return metav1.ConditionTrue, ConditionReasonReady
    case "Failed", "Degraded":
        return metav1.ConditionFalse, ConditionReasonFailed
    default:
        return metav1.ConditionUnknown, ConditionReasonPending
    }
}

// HasObject is satisfied by any runtime.Object with status.conditions access.
// Used to keep condition helper functions generic.
var _ client.Object = (*metav1.ObjectMeta)(nil) // compile-time check
```

### Step 2: Call `SetReadyCondition` inside `updateClusterStatus`

In `neo4jenterprisecluster_controller.go`, find `updateClusterStatus`. After setting `latest.Status.Phase = phase`, add:

```go
condStatus, condReason := PhaseToConditionStatus(phase)
SetReadyCondition(&latest.Status.Conditions, latest.Generation, condStatus, condReason, message)
```

### Step 3: Ensure `ObservedGeneration` is set for all controllers

For each controller's status update path, ensure `status.ObservedGeneration = obj.Generation` is set before writing. For example in the database controller:

```go
database.Status.ObservedGeneration = database.Generation
```

Check each controller's status write and add this line wherever it's missing.

### Step 4: Verify build and tests

```bash
go build ./internal/controller/...
make test-unit
```

Expected: both pass.

### Step 5: Commit

```bash
git add internal/controller/conditions.go \
    internal/controller/neo4jenterprisecluster_controller.go \
    internal/controller/neo4jenterprisestandalone_controller.go \
    internal/controller/neo4jdatabase_controller.go \
    internal/controller/neo4jbackup_controller.go \
    internal/controller/neo4jrestore_controller.go \
    internal/controller/plugin_controller.go \
    internal/controller/neo4jshardeddatabase_controller.go
git commit -m "feat(conditions): standardize Ready condition type across all CRDs with ObservedGeneration"
```

---

## Task 5: Wire imagePullSecrets Into StatefulSet Builder (3.2)

**Context:** `Neo4jEnterpriseClusterSpec.Image.PullSecrets []string` is defined in the API type and in `zz_generated.deepcopy.go`, but `BuildPodSpecForEnterprise` in `internal/resources/cluster.go` does NOT set `podSpec.ImagePullSecrets`. The standalone type doesn't have the field at all. MCP already has a working implementation in `internal/resources/mcp.go:726-734` to copy from.

**Files:**
- Modify: `internal/resources/cluster.go`
- Modify: `api/v1alpha1/neo4jenterprisestandalone_types.go`
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (via `make generate`)
- Test: `internal/resources/cluster_test.go`

### Step 1: Add `imagePullSecrets` helper to `internal/resources/cluster.go`

Find the file around line 1463 where `BuildPodSpecForEnterprise` returns `podSpec`. Add a helper function at the bottom of the file:

```go
// clusterImagePullSecrets converts []string pull secret names to []corev1.LocalObjectReference.
func clusterImagePullSecrets(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) []corev1.LocalObjectReference {
    if cluster.Spec.Image == nil || len(cluster.Spec.Image.PullSecrets) == 0 {
        return nil
    }
    refs := make([]corev1.LocalObjectReference, 0, len(cluster.Spec.Image.PullSecrets))
    for _, name := range cluster.Spec.Image.PullSecrets {
        refs = append(refs, corev1.LocalObjectReference{Name: name})
    }
    return refs
}
```

### Step 2: Wire it into `BuildPodSpecForEnterprise`

In `BuildPodSpecForEnterprise` (line ~1437 where `podSpec` is built), after the Affinity block (line ~1457) and before `return podSpec`, add:

```go
// Wire image pull secrets from cluster spec
if refs := clusterImagePullSecrets(cluster); len(refs) > 0 {
    podSpec.ImagePullSecrets = refs
}
```

### Step 3: Add `PullSecrets` to standalone image spec

In `api/v1alpha1/neo4jenterprisestandalone_types.go`, find the `ImageSpec` or equivalent image fields. Add:

```go
// PullSecrets is a list of Secret names in the same namespace to use for pulling the Neo4j image.
PullSecrets []string `json:"pullSecrets,omitempty"`
```

Check what struct holds the standalone image configuration — look for the `Image` field in `Neo4jEnterpriseStandaloneSpec`. Add `PullSecrets` to whichever inner struct it is.

### Step 4: Regenerate deepcopy

```bash
make generate
```

This regenerates `zz_generated.deepcopy.go` with the new `PullSecrets` field for standalone.

### Step 5: Wire pull secrets into the standalone StatefulSet builder

Find the standalone StatefulSet builder — either in `internal/resources/cluster.go` or `internal/controller/neo4jenterprisestandalone_controller.go` — where it builds the `PodSpec`. Add similar logic:

```go
if refs := standaloneImagePullSecrets(standalone); len(refs) > 0 {
    podSpec.ImagePullSecrets = refs
}
```

Add a corresponding `standaloneImagePullSecrets` helper.

### Step 6: Write a unit test in `internal/resources/cluster_test.go`

Add a test case to the existing `TestBuildPodSpecForEnterprise_*` test suite:

```go
func TestBuildPodSpecForEnterprise_WithPullSecrets(t *testing.T) {
    cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
        Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
            Topology: neo4jv1alpha1.TopologySpec{Servers: 3},
            Image: &neo4jv1alpha1.ImageSpec{
                Repository: "neo4j",
                Tag:        "5.26-enterprise",
                PullSecrets: []string{"my-registry-secret", "another-secret"},
            },
            Storage: neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "10Gi"},
        },
    }
    cluster.Name = "test-cluster"
    cluster.Namespace = "default"

    podSpec := BuildPodSpecForEnterprise(cluster, "server", "neo4j-admin-secret")

    require.Len(t, podSpec.ImagePullSecrets, 2)
    assert.Equal(t, "my-registry-secret", podSpec.ImagePullSecrets[0].Name)
    assert.Equal(t, "another-secret", podSpec.ImagePullSecrets[1].Name)
}
```

### Step 7: Run test to verify it passes

```bash
go test ./internal/resources/... -run TestBuildPodSpecForEnterprise_WithPullSecrets -v
```

Expected: PASS.

### Step 8: Regenerate CRDs

```bash
make manifests
```

This updates the CRD YAML in `config/crd/bases/` with the new standalone `pullSecrets` field.

### Step 9: Commit

```bash
git add internal/resources/cluster.go \
    api/v1alpha1/neo4jenterprisestandalone_types.go \
    api/v1alpha1/zz_generated.deepcopy.go \
    internal/resources/cluster_test.go \
    config/crd/bases/
git commit -m "feat(registry): wire imagePullSecrets into server StatefulSet and add standalone field"
```

---

## Task 6: Fix Helm Chart Versioning in Release Pipeline (3.3)

**Context:** `charts/neo4j-operator/Chart.yaml` has `version: 0.1.0` and `appVersion: "0.1.0"` hardcoded. The release workflow at `.github/workflows/release.yml:146-151` runs `helm package` without updating these values first, so every release ships a chart tagged `0.1.0` regardless of the git tag. Also, `artifacthub.io/changes` in `Chart.yaml` needs to be updated on each release.

**Files:**
- Modify: `.github/workflows/release.yml`
- Modify: `charts/neo4j-operator/Chart.yaml`

### Step 1: Read the exact helm packaging step in release.yml

Read `.github/workflows/release.yml` lines 140-160 to see the current `helm package` invocation and identify exactly where to insert the version update.

### Step 2: Add a `yq` version-update step before helm package

In `release.yml`, in the `build-and-push` job, **before** the `helm package` step, add:

```yaml
- name: Update Helm chart versions
  run: |
    CLEAN_TAG="${{ needs.determine-tag.outputs.clean_tag }}"
    yq e ".version = \"${CLEAN_TAG}\"" -i charts/neo4j-operator/Chart.yaml
    yq e ".appVersion = \"${CLEAN_TAG}\"" -i charts/neo4j-operator/Chart.yaml
    echo "Updated Chart.yaml to version ${CLEAN_TAG}"
    cat charts/neo4j-operator/Chart.yaml
```

`yq` is available on GitHub Actions runners. Alternatively use `sed`:

```yaml
- name: Update Helm chart versions
  run: |
    CLEAN_TAG="${{ needs.determine-tag.outputs.clean_tag }}"
    sed -i "s/^version: .*/version: ${CLEAN_TAG}/" charts/neo4j-operator/Chart.yaml
    sed -i "s/^appVersion: .*/appVersion: \"${CLEAN_TAG}\"/" charts/neo4j-operator/Chart.yaml
```

### Step 3: Update `artifacthub.io/changes` in Chart.yaml automatically

Optionally, in the same step, update the changes annotation. A simple approach is to clear it to a placeholder:

```bash
yq e '.annotations["artifacthub.io/changes"] = "- kind: changed\n  description: \"Release ${CLEAN_TAG}\""' \
  -i charts/neo4j-operator/Chart.yaml
```

Or leave this as a manual step for the release author and just ensure version/appVersion are correct.

### Step 4: Verify the Helm chart lints cleanly

In release.yml, after the version update step and before `helm package`, add:

```yaml
- name: Lint Helm chart
  run: helm lint charts/neo4j-operator
```

This catches Chart.yaml syntax errors before the push.

### Step 5: Check ArtifactHub metadata is complete

In `charts/neo4j-operator/Chart.yaml`, verify these annotations exist (they already do per earlier grep):
- `artifacthub.io/operator: "true"` ✓
- `artifacthub.io/operatorCapabilities: Full Lifecycle` ✓
- `artifacthub.io/license: Apache-2.0` ✓

Add if missing:
```yaml
artifacthub.io/maintainers: |
  - name: Neo4j Partners
    email: operator@neo4j.com
artifacthub.io/links: |
  - name: Documentation
    url: https://github.com/neo4j-partners/neo4j-kubernetes-operator/tree/main/docs
  - name: Support
    url: https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues
```

### Step 6: Test the Makefile helm-lint target

```bash
make helm-lint
```

Expected: PASS (no errors in chart template).

### Step 7: Commit

```bash
git add .github/workflows/release.yml charts/neo4j-operator/Chart.yaml
git commit -m "fix(helm): auto-update chart version/appVersion from git tag in release pipeline"
```

---

## Task 7: Final Verification

### Step 1: Full unit test run

```bash
make test-unit
```

Expected: all tests PASS.

### Step 2: Full lint run

```bash
make lint
```

Expected: no new lint errors. If there are minor errors introduced, fix them before committing.

### Step 3: Build check

```bash
make build
```

Expected: binary builds cleanly.

### Step 4: Verify CRDs are up-to-date

```bash
make manifests
git diff config/crd/
```

Expected: only the standalone `pullSecrets` field addition. If any other diffs appear, investigate.

### Step 5: Delete the backup artifact

```bash
rm internal/controller/plugin_controller.go.backup
git add -u internal/controller/plugin_controller.go.backup
git commit -m "chore: remove stale plugin_controller.go.backup artifact"
```

### Step 6: Final commit and push

```bash
git log --oneline feature/observability-gitops-improvements ^main
git push -u origin feature/observability-gitops-improvements
```

---

## Summary of Changes

| Task | Files Changed | Commit Message |
|---|---|---|
| 1 - Events | `events.go` + 7 controllers | `feat(events): add event reason constants and missing lifecycle events` |
| 2 - Metrics | `metrics.go` + cluster + backup controllers | `feat(metrics): wire cluster health, phase, reconcile, backup, split-brain metrics` |
| 3 - ArgoCD | `docs/gitops/` + Helm ServiceMonitor | `feat(gitops): add ArgoCD health checks and Prometheus ServiceMonitor` |
| 4 - Conditions | `conditions.go` + 7 controllers | `feat(conditions): standardize Ready condition type across all CRDs` |
| 5 - PullSecrets | `resources/cluster.go` + standalone types + test | `feat(registry): wire imagePullSecrets into server StatefulSet` |
| 6 - Helm versioning | `release.yml` + `Chart.yaml` | `fix(helm): auto-update chart version from git tag in release pipeline` |
| 7 - Cleanup | `plugin_controller.go.backup` | `chore: remove stale backup artifact` |
