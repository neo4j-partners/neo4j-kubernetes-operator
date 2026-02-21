# Cypher Query Diagnostics (Operator-Side) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enhance the QueryMonitor to periodically run `SHOW SERVERS` and `SHOW DATABASES` against a Ready cluster and surface the results in `status.diagnostics`, `status.conditions`, and matching Prometheus metrics — eliminating the need for users to `exec` into pods to inspect cluster state.

**Architecture:** Add a `CollectDiagnostics(ctx, cluster, neo4jClient)` method to the existing `QueryMonitor` type. The cluster reconciler calls it whenever `QueryMonitoring.Enabled=true` AND `status.phase=Ready`. Results are written into a new `status.diagnostics` sub-struct and two new Kubernetes conditions (`ServersHealthy`, `DatabasesHealthy`). All diagnostics collection is non-blocking and non-fatal — failure to collect never prevents the cluster from reaching Ready.

**Tech Stack:** Go 1.24, controller-runtime v0.21, `internal/neo4j.Client` (existing Bolt client), `api/v1alpha1` CRD types, `internal/metrics` (Prometheus).

---

## Codebase Quick-Reference

| Symbol | File |
|---|---|
| `QueryMonitor` struct + `ReconcileQueryMonitoring` | `internal/controller/neo4jenterprisecluster_controller.go:1340–1500` |
| `Neo4jEnterpriseClusterStatus` | `api/v1alpha1/neo4jenterprisecluster_types.go:563–604` |
| `conditions.go` constants | `internal/controller/conditions.go` |
| `neo4jclient.GetServerList` | `internal/neo4j/client.go:1639` — returns `[]ServerInfo{Name,Address,State,Health,Hosting}` |
| `neo4jclient.GetDatabases` | `internal/neo4j/client.go:555` — returns `[]DatabaseInfo{Name,Status,Default,Home,Role,RequestedStatus}` |
| `r.createNeo4jClient` | `internal/controller/neo4jenterprisecluster_controller.go:~1155` |
| QueryMonitor call site in `Reconcile()` | `internal/controller/neo4jenterprisecluster_controller.go:~409–414` |
| `internal/metrics/metrics.go` | cluster metrics (ClusterMetrics, etc.) |
| `docs/user_guide/guides/monitoring.md` | Monitoring user guide |
| `docs/user_guide/configuration.md` | Main configuration reference |
| `docs/developer_guide/architecture.md` | Architecture developer guide |

---

## Task 1: Add Diagnostic Status Types to the API

**Files:**
- Modify: `api/v1alpha1/neo4jenterprisecluster_types.go`
- Run: `make generate && make manifests`

### Step 1: Read the existing status struct

Read `api/v1alpha1/neo4jenterprisecluster_types.go` lines 563–610 to understand the current `Neo4jEnterpriseClusterStatus` and the structs that appear after it (so you know where to insert the new types).

### Step 2: Add new types after the existing status types

Find the end of the `Neo4jEnterpriseClusterStatus` struct block and add the following new types. Insert them BEFORE the closing of the existing types section (typically before the `+kubebuilder:object:root=true` marker for the next type).

```go
// ClusterDiagnosticsStatus holds the most recent live diagnostics collected from
// the Neo4j cluster via Cypher queries. Populated only when spec.queryMonitoring.enabled=true
// and the cluster is in Ready phase.
type ClusterDiagnosticsStatus struct {
	// Servers lists the most recently observed state of each server in the cluster.
	// +optional
	Servers []ServerDiagnosticInfo `json:"servers,omitempty"`

	// Databases lists the most recently observed state of each database.
	// +optional
	Databases []DatabaseDiagnosticInfo `json:"databases,omitempty"`

	// LastCollected is the timestamp of the most recent successful diagnostics collection.
	// +optional
	LastCollected *metav1.Time `json:"lastCollected,omitempty"`

	// CollectionError holds the last error message if diagnostics collection failed.
	// Empty when collection succeeds.
	// +optional
	CollectionError string `json:"collectionError,omitempty"`
}

// ServerDiagnosticInfo holds the observed state of a single Neo4j server.
type ServerDiagnosticInfo struct {
	// Name is the server's display name (from SHOW SERVERS).
	Name string `json:"name"`

	// Address is the Bolt address of the server.
	Address string `json:"address"`

	// State is the server lifecycle state (e.g. "Enabled", "Cordoned", "Deallocating").
	State string `json:"state"`

	// Health is the server health status (e.g. "Available", "Unavailable").
	Health string `json:"health"`

	// HostingDatabases is the number of databases hosted by this server.
	HostingDatabases int `json:"hostingDatabases"`
}

// DatabaseDiagnosticInfo holds the observed state of a single Neo4j database.
type DatabaseDiagnosticInfo struct {
	// Name is the database name.
	Name string `json:"name"`

	// Status is the current operational status (e.g. "online", "offline", "quarantined").
	Status string `json:"status"`

	// RequestedStatus is the desired operational status.
	RequestedStatus string `json:"requestedStatus"`

	// Role is the database role on the most recently contacted server (e.g. "primary", "secondary").
	Role string `json:"role"`

	// Default indicates whether this is the default database.
	Default bool `json:"default,omitempty"`
}
```

### Step 3: Add `Diagnostics` field to `Neo4jEnterpriseClusterStatus`

Inside the `Neo4jEnterpriseClusterStatus` struct, after the `AuraFleetManagement` field, add:

```go
// Diagnostics holds the most recently collected live diagnostics from the cluster.
// Populated when spec.queryMonitoring.enabled=true and the cluster is Ready.
// +optional
Diagnostics *ClusterDiagnosticsStatus `json:"diagnostics,omitempty"`
```

### Step 4: Regenerate DeepCopy and CRD manifests

```bash
cd /Users/priyolahiri/Code/neo4j-kubernetes-operator
make generate
make manifests
```

Expected: `api/v1alpha1/zz_generated.deepcopy.go` updated with new types; `config/crd/bases/` CRD updated.

### Step 5: Verify build

```bash
go build ./...
```

Expected: no errors.

### Step 6: Commit

```bash
git add api/v1alpha1/neo4jenterprisecluster_types.go \
    api/v1alpha1/zz_generated.deepcopy.go \
    config/crd/bases/
git commit -m "feat(api): add ClusterDiagnosticsStatus with server and database diagnostic fields"
```

---

## Task 2: Add Diagnostic Condition Constants

**Files:**
- Modify: `internal/controller/conditions.go`

### Step 1: Add two new condition type constants

In `internal/controller/conditions.go`, in the `ConditionType*` const block (alongside `ConditionTypeReady`, `ConditionTypeAvailable`, etc.), add:

```go
// ConditionTypeServersHealthy indicates all Neo4j servers in the cluster
// are in Enabled state and Available health.
ConditionTypeServersHealthy = "ServersHealthy"

// ConditionTypeDatabasesHealthy indicates all expected databases are online
// with no unexpected status mismatches.
ConditionTypeDatabasesHealthy = "DatabasesHealthy"
```

### Step 2: Add reason constants

In the `ConditionReason*` const block, add:

```go
ConditionReasonAllServersHealthy    = "AllServersHealthy"
ConditionReasonServerDegraded       = "ServerDegraded"
ConditionReasonAllDatabasesOnline   = "AllDatabasesOnline"
ConditionReasonDatabaseOffline      = "DatabaseOffline"
ConditionReasonDiagnosticsUnavailable = "DiagnosticsUnavailable"
```

### Step 3: Add a generic `SetCondition` helper

The existing `SetReadyCondition` is specific to the `Ready` type. Add a more general helper for the new condition types:

```go
// SetNamedCondition upserts any condition type (not just "Ready") on a conditions slice.
// It preserves LastTransitionTime when status and reason are unchanged.
// Returns true if the condition changed.
func SetNamedCondition(conditions *[]metav1.Condition, condType string, generation int64, status metav1.ConditionStatus, reason, message string) bool {
	existing := findCondition(*conditions, condType)
	if existing != nil && existing.Status == status && existing.Reason == reason {
		existing.ObservedGeneration = generation
		existing.Message = message
		return false
	}
	newCond := metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	*conditions = upsertCondition(*conditions, newCond)
	return true
}
```

### Step 4: Build

```bash
go build ./internal/controller/...
```

Expected: no errors.

### Step 5: Commit

```bash
git add internal/controller/conditions.go
git commit -m "feat(conditions): add ServersHealthy and DatabasesHealthy condition types with helpers"
```

---

## Task 3: Implement `CollectDiagnostics` on QueryMonitor

**Files:**
- Modify: `internal/controller/neo4jenterprisecluster_controller.go`

### Step 1: Read the full QueryMonitor section

Read `internal/controller/neo4jenterprisecluster_controller.go` from the `NewQueryMonitor` function (around line 1340) to the end of `setupAlertingRules` to understand the full structure.

### Step 2: Add the `CollectDiagnostics` method

Add the following method AFTER the existing `setupAlertingRules` function. It should appear before the next function or at the end of the file section:

```go
// CollectDiagnostics runs SHOW SERVERS and SHOW DATABASES against the cluster
// and writes the results into status.diagnostics and status.conditions.
// This is non-blocking — all errors are surfaced in status but do not fail
// the reconciliation loop.
func (qm *QueryMonitor) CollectDiagnostics(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, neo4jClient *neo4jclient.Client) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Collecting cluster diagnostics", "cluster", cluster.Name)

	diagnostics := &neo4jv1alpha1.ClusterDiagnosticsStatus{}

	// --- Collect server list ---
	servers, serverErr := neo4jClient.GetServerList(ctx)
	if serverErr != nil {
		logger.Error(serverErr, "Failed to collect SHOW SERVERS")
		diagnostics.CollectionError = fmt.Sprintf("SHOW SERVERS failed: %v", serverErr)
	} else {
		for _, s := range servers {
			diagnostics.Servers = append(diagnostics.Servers, neo4jv1alpha1.ServerDiagnosticInfo{
				Name:             s.Name,
				Address:          s.Address,
				State:            s.State,
				Health:           s.Health,
				HostingDatabases: len(s.Hosting),
			})
		}
	}

	// --- Collect database list ---
	databases, dbErr := neo4jClient.GetDatabases(ctx)
	if dbErr != nil {
		logger.Error(dbErr, "Failed to collect SHOW DATABASES")
		if diagnostics.CollectionError == "" {
			diagnostics.CollectionError = fmt.Sprintf("SHOW DATABASES failed: %v", dbErr)
		} else {
			diagnostics.CollectionError += fmt.Sprintf("; SHOW DATABASES failed: %v", dbErr)
		}
	} else {
		for _, d := range databases {
			diagnostics.Databases = append(diagnostics.Databases, neo4jv1alpha1.DatabaseDiagnosticInfo{
				Name:            d.Name,
				Status:          d.Status,
				RequestedStatus: d.RequestedStatus,
				Role:            d.Role,
				Default:         d.Default,
			})
		}
	}

	// Record collection timestamp
	now := metav1.Now()
	diagnostics.LastCollected = &now

	// --- Patch status with collected data ---
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
		if err := qm.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}

		latest.Status.Diagnostics = diagnostics

		// Update ServersHealthy condition
		qm.updateServersCondition(latest, servers, serverErr)

		// Update DatabasesHealthy condition
		qm.updateDatabasesCondition(latest, databases, dbErr)

		return qm.Status().Update(ctx, latest)
	})
}

// updateServersCondition sets the ServersHealthy condition based on SHOW SERVERS results.
func (qm *QueryMonitor) updateServersCondition(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, servers []neo4jclient.ServerInfo, collectErr error) {
	if collectErr != nil {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable,
			fmt.Sprintf("Could not collect server list: %v", collectErr))
		return
	}
	if len(servers) == 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable, "No servers returned by SHOW SERVERS")
		return
	}

	var degraded []string
	for _, s := range servers {
		if s.State != "Enabled" || s.Health != "Available" {
			degraded = append(degraded, fmt.Sprintf("%s (state=%s health=%s)", s.Name, s.State, s.Health))
		}
	}

	if len(degraded) > 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionFalse,
			ConditionReasonServerDegraded,
			fmt.Sprintf("%d server(s) unhealthy: %s", len(degraded), strings.Join(degraded, ", ")))
	} else {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionTrue,
			ConditionReasonAllServersHealthy,
			fmt.Sprintf("All %d servers are Enabled and Available", len(servers)))
	}
}

// updateDatabasesCondition sets the DatabasesHealthy condition based on SHOW DATABASES results.
func (qm *QueryMonitor) updateDatabasesCondition(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, databases []neo4jclient.DatabaseInfo, collectErr error) {
	if collectErr != nil {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable,
			fmt.Sprintf("Could not collect database list: %v", collectErr))
		return
	}
	if len(databases) == 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable, "No databases returned by SHOW DATABASES")
		return
	}

	// Skip system/internal databases for health purposes:
	// Only flag databases that are requested to be online but aren't.
	var offline []string
	userDBCount := 0
	for _, d := range databases {
		// skip the internal "system" database — it has special behavior
		if d.Name == "system" {
			continue
		}
		userDBCount++
		if d.RequestedStatus == "online" && d.Status != "online" {
			offline = append(offline, fmt.Sprintf("%s (status=%s)", d.Name, d.Status))
		}
	}

	if len(offline) > 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionFalse,
			ConditionReasonDatabaseOffline,
			fmt.Sprintf("%d database(s) not online: %s", len(offline), strings.Join(offline, ", ")))
	} else {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionTrue,
			ConditionReasonAllDatabasesOnline,
			fmt.Sprintf("All %d user database(s) are online", userDBCount))
	}
}
```

**IMPORTANT — imports needed in the file**: Check existing imports in `neo4jenterprisecluster_controller.go`. You need:
- `"strings"` — likely already present
- `"k8s.io/client-go/util/retry"` — likely already present (used by `createOrUpdateResource`)
- `neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"` — likely already present
- `"sigs.k8s.io/controller-runtime/pkg/client"` — already present

Verify each is in the import block. Add any missing ones.

### Step 3: Build to verify no errors

```bash
go build ./internal/controller/...
```

Fix any issues (typically import or type name mismatches).

### Step 4: Commit (without wiring yet — we'll wire in next task)

```bash
git add internal/controller/neo4jenterprisecluster_controller.go
git commit -m "feat(diagnostics): add CollectDiagnostics method to QueryMonitor"
```

---

## Task 4: Wire `CollectDiagnostics` into the Cluster Reconciler

**Files:**
- Modify: `internal/controller/neo4jenterprisecluster_controller.go`

### Step 1: Find the QueryMonitor call site

Read the `Reconcile()` function around the block:
```go
if cluster.Spec.QueryMonitoring != nil && cluster.Spec.QueryMonitoring.Enabled {
    queryMonitor := NewQueryMonitor(r.Client, r.Scheme)
    if err := queryMonitor.ReconcileQueryMonitoring(ctx, cluster); err != nil {
```

### Step 2: Add `CollectDiagnostics` call after the existing QueryMonitoring block

After the closing `}` of the existing QueryMonitoring block, add:

```go
// Collect live diagnostics when QueryMonitoring is enabled and cluster is Ready.
// Diagnostics collection is non-fatal: failures are surfaced in status.diagnostics.collectionError.
if cluster.Spec.QueryMonitoring != nil && cluster.Spec.QueryMonitoring.Enabled &&
	cluster.Status.Phase == "Ready" {
	neo4jClient, clientErr := r.createNeo4jClient(ctx, cluster)
	if clientErr != nil {
		logger.V(1).Info("Skipping diagnostics: could not create Neo4j client", "error", clientErr)
	} else {
		defer neo4jClient.Close(ctx)
		diagMonitor := NewQueryMonitor(r.Client, r.Scheme)
		if diagErr := diagMonitor.CollectDiagnostics(ctx, cluster, neo4jClient); diagErr != nil {
			logger.Error(diagErr, "Failed to collect cluster diagnostics (non-fatal)")
		}
	}
}
```

**NOTE about `neo4jClient.Close(ctx)`**: The `neo4j.Driver` interface has `Close(ctx)`. Confirm the method name by checking how it's called in other places in the file. It may be `neo4jClient.Close(ctx)` or just `neo4jClient.Close()`. Use the same pattern as existing code.

### Step 3: Build

```bash
go build ./...
```

Expected: no errors.

### Step 4: Commit

```bash
git add internal/controller/neo4jenterprisecluster_controller.go
git commit -m "feat(diagnostics): wire CollectDiagnostics into cluster reconciler when Ready"
```

---

## Task 5: Unit Tests for CollectDiagnostics

**Files:**
- Modify: `internal/controller/neo4jenterprisecluster_controller_test.go` (or create `internal/controller/diagnostics_test.go` if it's cleaner)

### Step 1: Read the existing controller test file

Read `internal/controller/neo4jenterprisecluster_controller_test.go` (or `internal/controller/suite_test.go`) to understand the test setup pattern and what mocking/faking infrastructure is available.

### Step 2: Write unit tests for `updateServersCondition`

The `updateServersCondition` and `updateDatabasesCondition` methods are pure logic (no Kubernetes API calls) — they can be unit tested directly.

Create `internal/controller/diagnostics_test.go`:

```go
package controller

import (
	"testing"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeCluster() *neo4jv1alpha1.Neo4jEnterpriseCluster {
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Generation: 1,
		},
	}
}

func findCond(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, condType string) *metav1.Condition {
	for i := range cluster.Status.Conditions {
		if cluster.Status.Conditions[i].Type == condType {
			return &cluster.Status.Conditions[i]
		}
	}
	return nil
}

func TestUpdateServersCondition_AllHealthy(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := makeCluster()
	servers := []neo4jclient.ServerInfo{
		{Name: "server-0", State: "Enabled", Health: "Available", Hosting: []string{"neo4j"}},
		{Name: "server-1", State: "Enabled", Health: "Available", Hosting: []string{"neo4j"}},
	}

	qm.updateServersCondition(cluster, servers, nil)

	cond := findCond(cluster, ConditionTypeServersHealthy)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ConditionReasonAllServersHealthy, cond.Reason)
}

func TestUpdateServersCondition_DegradedServer(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := makeCluster()
	servers := []neo4jclient.ServerInfo{
		{Name: "server-0", State: "Enabled", Health: "Available"},
		{Name: "server-1", State: "Cordoned", Health: "Available"},
	}

	qm.updateServersCondition(cluster, servers, nil)

	cond := findCond(cluster, ConditionTypeServersHealthy)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, ConditionReasonServerDegraded, cond.Reason)
	assert.Contains(t, cond.Message, "server-1")
}

func TestUpdateServersCondition_CollectionError(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := makeCluster()

	qm.updateServersCondition(cluster, nil, fmt.Errorf("bolt connection refused"))

	cond := findCond(cluster, ConditionTypeServersHealthy)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionUnknown, cond.Status)
	assert.Equal(t, ConditionReasonDiagnosticsUnavailable, cond.Reason)
}

func TestUpdateDatabasesCondition_AllOnline(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := makeCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "system", Status: "online", RequestedStatus: "online"},
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCond(cluster, ConditionTypeDatabasesHealthy)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, ConditionReasonAllDatabasesOnline, cond.Reason)
}

func TestUpdateDatabasesCondition_DatabaseOffline(t *testing.T) {
	qm := &QueryMonitor{}
	cluster := makeCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "system", Status: "online", RequestedStatus: "online"},
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
		{Name: "mydb", Status: "offline", RequestedStatus: "online"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCond(cluster, ConditionTypeDatabasesHealthy)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, ConditionReasonDatabaseOffline, cond.Reason)
	assert.Contains(t, cond.Message, "mydb")
}

func TestUpdateDatabasesCondition_SystemDatabaseSkipped(t *testing.T) {
	// system database offline should NOT trigger DatabaseOffline condition
	qm := &QueryMonitor{}
	cluster := makeCluster()
	databases := []neo4jclient.DatabaseInfo{
		{Name: "system", Status: "offline", RequestedStatus: "online"}, // system — should be skipped
		{Name: "neo4j", Status: "online", RequestedStatus: "online"},
	}

	qm.updateDatabasesCondition(cluster, databases, nil)

	cond := findCond(cluster, ConditionTypeDatabasesHealthy)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status) // system is skipped; only "neo4j" counts
}
```

**Note:** Add `"fmt"` to the import block if needed.

### Step 3: Run the tests

```bash
cd /Users/priyolahiri/Code/neo4j-kubernetes-operator
go test ./internal/controller/... -run TestUpdateServers -v
go test ./internal/controller/... -run TestUpdateDatabases -v
```

Expected: all PASS.

### Step 4: Run full unit tests

```bash
make test-unit
```

Expected: PASS.

### Step 5: Commit

```bash
git add internal/controller/diagnostics_test.go
git commit -m "test(diagnostics): add unit tests for server and database condition logic"
```

---

## Task 6: Add Prometheus Metrics for Diagnostics

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/controller/neo4jenterprisecluster_controller.go`

**Purpose:** Surface per-server health as a Prometheus gauge so Alertmanager rules can trigger on unhealthy servers.

### Step 1: Add a server health gauge to `internal/metrics/metrics.go`

Read `internal/metrics/metrics.go` to find the `ClusterMetrics` struct and its methods. After `RecordClusterPhase`, add a new var and method:

In the `var` block, add:
```go
serverHealth = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Subsystem: subsystem,
		Name:      "server_health",
		Help:      "Health of individual Neo4j servers (1 = Available/Enabled, 0 = degraded)",
	},
	[]string{LabelClusterName, LabelNamespace, "server_name", "server_address"},
)
```

Register it in `init()`:
```go
metrics.Registry.MustRegister(serverHealth)
```

Add a method on `ClusterMetrics`:
```go
// RecordServerHealth records the health of individual servers from SHOW SERVERS results.
func (m *ClusterMetrics) RecordServerHealth(servers []ServerHealth) {
	for _, s := range servers {
		v := 0.0
		if s.Enabled && s.Available {
			v = 1.0
		}
		serverHealth.WithLabelValues(m.clusterName, m.namespace, s.Name, s.Address).Set(v)
	}
}

// ServerHealth is a lightweight struct for metric recording.
type ServerHealth struct {
	Name      string
	Address   string
	Enabled   bool
	Available bool
}
```

### Step 2: Call `RecordServerHealth` from `CollectDiagnostics`

In `CollectDiagnostics` in `neo4jenterprisecluster_controller.go`, after collecting `servers`, add:

```go
// Record per-server health metrics
if serverErr == nil {
	clusterM := metrics.NewClusterMetrics(cluster.Name, cluster.Namespace)
	serverHealthData := make([]metrics.ServerHealth, 0, len(servers))
	for _, s := range servers {
		serverHealthData = append(serverHealthData, metrics.ServerHealth{
			Name:      s.Name,
			Address:   s.Address,
			Enabled:   s.State == "Enabled",
			Available: s.Health == "Available",
		})
	}
	clusterM.RecordServerHealth(serverHealthData)
}
```

### Step 3: Build and test

```bash
go build ./...
make test-unit
```

Expected: both PASS.

### Step 4: Commit

```bash
git add internal/metrics/metrics.go \
    internal/controller/neo4jenterprisecluster_controller.go
git commit -m "feat(metrics): add per-server health gauge from SHOW SERVERS diagnostics"
```

---

## Task 7: Update Documentation

**Files:**
- Modify: `docs/user_guide/guides/monitoring.md`
- Modify: `docs/user_guide/configuration.md`
- Modify: `docs/developer_guide/architecture.md`

### Step 1: Read current documentation

Read all three files fully before editing:
- `docs/user_guide/guides/monitoring.md`
- `docs/user_guide/configuration.md` (focus on queryMonitoring section)
- `docs/developer_guide/architecture.md`

### Step 2: Update `docs/user_guide/guides/monitoring.md`

The monitoring guide currently describes how to enable `spec.queryMonitoring`. Add a new section after the existing content covering:

```markdown
## Live Cluster Diagnostics

When `spec.queryMonitoring.enabled: true` and the cluster is in `Ready` phase, the
operator periodically collects live diagnostics from the cluster and surfaces them in
`status.diagnostics` and `status.conditions`.

### Viewing Diagnostics

```bash
# Quick overview of all servers
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.diagnostics.servers}' | jq .

# Check when diagnostics were last collected
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.diagnostics.lastCollected}'

# View any collection errors
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.diagnostics.collectionError}'
```

### Diagnostic Status Fields

| Field | Description |
|---|---|
| `status.diagnostics.servers[]` | One entry per server: name, address, state, health, hostingDatabases |
| `status.diagnostics.databases[]` | One entry per database: name, status, requestedStatus, role, default |
| `status.diagnostics.lastCollected` | Timestamp of the most recent successful collection |
| `status.diagnostics.collectionError` | Error message if the last collection failed; empty on success |

### Diagnostic Conditions

The operator sets two standard Kubernetes conditions on the cluster resource:

| Condition | True When | False When |
|---|---|---|
| `ServersHealthy` | All servers are `state=Enabled` AND `health=Available` | Any server is Cordoned, Deallocating, or Unavailable |
| `DatabasesHealthy` | All user databases are `online` | Any user database is `offline` or `quarantined` while `requestedStatus=online` |

Both conditions are `Unknown` when diagnostics cannot be collected (e.g., cluster not yet reachable).

#### Example: Alert on server degradation

```bash
# Watch for ServersHealthy=False
kubectl get neo4jenterprisecluster <name> \
  -o jsonpath='{.status.conditions[?(@.type=="ServersHealthy")]}'
```

### Prometheus Metrics

When diagnostics are enabled, the operator exposes a per-server health gauge:

| Metric | Type | Description |
|---|---|---|
| `neo4j_operator_server_health{cluster_name, namespace, server_name, server_address}` | Gauge | `1` = server is Enabled + Available; `0` = degraded |

This metric can drive Alertmanager rules:

```yaml
# Example PrometheusRule
- alert: Neo4jServerDegraded
  expr: neo4j_operator_server_health == 0
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Neo4j server {{ $labels.server_name }} is degraded"
```

### Disabling Diagnostics

Diagnostics are collected only when `spec.queryMonitoring.enabled: true`. To disable:

```yaml
spec:
  queryMonitoring:
    enabled: false
```
```

### Step 3: Update `docs/user_guide/configuration.md`

Find the `spec.queryMonitoring` section. After the existing fields table, add a note:

```markdown
> **Live Diagnostics**: When `enabled: true` and the cluster is `Ready`, the operator
> automatically collects `SHOW SERVERS` and `SHOW DATABASES` results and writes them
> to `status.diagnostics`. See the [Monitoring Guide](guides/monitoring.md) for details.
```

### Step 4: Update `docs/developer_guide/architecture.md`

Find the section describing controllers or the QueryMonitor. Add or expand a "QueryMonitor / Diagnostics" section:

```markdown
### QueryMonitor and Live Diagnostics

The `QueryMonitor` type (defined inline in `neo4jenterprisecluster_controller.go`) handles
two responsibilities:

1. **Infrastructure setup** (`ReconcileQueryMonitoring`): Creates the metrics `Service`,
   `ServiceMonitor`, and `PrometheusRule` Kubernetes resources. Runs on every reconcile.

2. **Live diagnostics** (`CollectDiagnostics`): Runs `SHOW SERVERS` and `SHOW DATABASES`
   via the Bolt client and writes results to `status.diagnostics`. Only runs when the
   cluster is in `Ready` phase. Sets `ServersHealthy` and `DatabasesHealthy` conditions.
   Non-fatal: diagnostics failures are surfaced in `status.diagnostics.collectionError`
   but never fail the reconciliation loop.

Both methods use the standard `retry.RetryOnConflict` pattern for Kubernetes status writes.
The Bolt client is created once per reconcile and passed to `CollectDiagnostics`; it is
not shared with other reconcile logic to keep concerns isolated.
```

### Step 5: Verify documentation is accurate

Double-check the field names you documented match the actual Go struct fields defined in Task 1.

### Step 6: Commit

```bash
git add docs/user_guide/guides/monitoring.md \
    docs/user_guide/configuration.md \
    docs/developer_guide/architecture.md
git commit -m "docs: document cluster diagnostics feature in monitoring guide, configuration, and architecture"
```

---

## Task 8: Final Verification

### Step 1: Full build

```bash
go build ./...
```

Expected: no errors.

### Step 2: Full unit tests

```bash
make test-unit
```

Expected: PASS (including new diagnostics tests).

### Step 3: Verify CRD schema

```bash
kubectl explain --recursive neo4jenterprisecluster.status.diagnostics 2>/dev/null || \
  cat config/crd/bases/neo4j.neo4j.com_neo4jenterpriseclusters.yaml | grep -A30 "diagnostics:"
```

Expected: `servers`, `databases`, `lastCollected`, `collectionError` fields are present in the CRD schema.

### Step 4: Helm lint

```bash
helm lint charts/neo4j-operator
```

Expected: no errors.

### Step 5: Final commit log

```bash
git log --oneline feature/observability-gitops-improvements ^main
```

Expected: 13–15 commits total (9 from previous work + 6 new ones from this feature).

---

## Summary of Changes

| Task | Files | Commit |
|---|---|---|
| 1 - API types | `neo4jenterprisecluster_types.go`, `zz_generated.deepcopy.go`, CRD YAML | `feat(api): add ClusterDiagnosticsStatus` |
| 2 - Condition constants | `conditions.go` | `feat(conditions): add ServersHealthy and DatabasesHealthy` |
| 3 - CollectDiagnostics | `neo4jenterprisecluster_controller.go` | `feat(diagnostics): add CollectDiagnostics method` |
| 4 - Wire into reconciler | `neo4jenterprisecluster_controller.go` | `feat(diagnostics): wire CollectDiagnostics into reconciler` |
| 5 - Unit tests | `diagnostics_test.go` | `test(diagnostics): add unit tests` |
| 6 - Prometheus metric | `metrics.go` + controller | `feat(metrics): add per-server health gauge` |
| 7 - Documentation | 3 docs files | `docs: document cluster diagnostics feature` |
