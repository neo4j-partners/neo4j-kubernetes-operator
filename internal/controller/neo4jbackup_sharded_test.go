/*
Copyright 2025 Priyo Lahiri.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

func ptrBool(b bool) *bool { return &b }

func newShardedTestReconciler(t *testing.T, objs ...runtime.Object) *Neo4jBackupReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add clientgo scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add neo4j scheme: %v", err)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackup{}, &neo4jv1beta1.Neo4jShardedDatabase{}).
		Build()
	return &Neo4jBackupReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(16),
	}
}

// TestEffectiveRemoteAddressResolution pins the defaulting matrix for the
// --remote-address-resolution flag. Explicit user values always win; the
// default-true behavior only fires for kind=ShardedDatabase on a Neo4j version
// that supports the flag (2025.09+).
func TestEffectiveRemoteAddressResolution(t *testing.T) {
	v202509, _ := neo4j.ParseVersion("2025.09.0-enterprise")
	v202508, _ := neo4j.ParseVersion("2025.08.0-enterprise")
	v526, _ := neo4j.ParseVersion("5.26.0-enterprise")

	cases := []struct {
		name               string
		kind               string
		options            *neo4jv1beta1.BackupOptions
		version            *neo4j.Version
		clusterHasSharding bool
		want               bool
	}{
		{
			name:    "ShardedDatabase + 2025.09 + nil opts → defaults true",
			kind:    neo4jv1beta1.BackupTargetKindShardedDatabase,
			options: nil,
			version: v202509,
			want:    true,
		},
		{
			name:    "ShardedDatabase + 2025.09 + field unset → defaults true",
			kind:    neo4jv1beta1.BackupTargetKindShardedDatabase,
			options: &neo4jv1beta1.BackupOptions{},
			version: v202509,
			want:    true,
		},
		{
			name:    "ShardedDatabase + 2025.08 → false (version too old)",
			kind:    neo4jv1beta1.BackupTargetKindShardedDatabase,
			options: nil,
			version: v202508,
			want:    false,
		},
		{
			name:    "ShardedDatabase + 5.26 (semver) → false",
			kind:    neo4jv1beta1.BackupTargetKindShardedDatabase,
			options: nil,
			version: v526,
			want:    false,
		},
		{
			name:    "ShardedDatabase + explicit false → false (user wins)",
			kind:    neo4jv1beta1.BackupTargetKindShardedDatabase,
			options: &neo4jv1beta1.BackupOptions{RemoteAddressResolution: ptrBool(false)},
			version: v202509,
			want:    false,
		},
		{
			name:    "ShardedDatabase + explicit true → true",
			kind:    neo4jv1beta1.BackupTargetKindShardedDatabase,
			options: &neo4jv1beta1.BackupOptions{RemoteAddressResolution: ptrBool(true)},
			version: v202509,
			want:    true,
		},
		{
			name:    "Cluster kind + nil opts + non-sharding cluster → false",
			kind:    neo4jv1beta1.BackupTargetKindCluster,
			options: nil,
			version: v202509,
			want:    false,
		},
		{
			name:               "Cluster kind (all-DB) + nil opts + SHARDING cluster → true (glob hits shards)",
			kind:               neo4jv1beta1.BackupTargetKindCluster,
			options:            nil,
			version:            v202509,
			clusterHasSharding: true,
			want:               true,
		},
		{
			name:               "Cluster kind (all-DB) + SHARDING cluster + explicit false → false (user wins)",
			kind:               neo4jv1beta1.BackupTargetKindCluster,
			options:            &neo4jv1beta1.BackupOptions{RemoteAddressResolution: ptrBool(false)},
			version:            v202509,
			clusterHasSharding: true,
			want:               false,
		},
		{
			name:               "Cluster kind (all-DB) + SHARDING cluster + version too old → false",
			kind:               neo4jv1beta1.BackupTargetKindCluster,
			options:            nil,
			version:            v202508,
			clusterHasSharding: true,
			want:               false,
		},
		{
			name:    "Cluster kind + explicit true → true (user opt-in honored)",
			kind:    neo4jv1beta1.BackupTargetKindCluster,
			options: &neo4jv1beta1.BackupOptions{RemoteAddressResolution: ptrBool(true)},
			version: v202509,
			want:    true,
		},
		{
			name:    "Database kind + nil opts → false (no defaulting for non-sharded)",
			kind:    neo4jv1beta1.BackupTargetKindDatabase,
			options: nil,
			version: v202509,
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backup := &neo4jv1beta1.Neo4jBackup{
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target:  neo4jv1beta1.BackupTarget{Kind: tc.kind, Name: "x"},
					Options: tc.options,
				},
			}
			got := effectiveRemoteAddressResolution(backup, tc.version, tc.clusterHasSharding)
			if got != tc.want {
				t.Errorf("effectiveRemoteAddressResolution() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExpectedShardArtifactsForBackup_Matrix pins the per-shard manifest
// builder used by the backup controller to stamp BackupRun.ShardArtifacts.
// Names are derived from Neo4jShardedDatabase.spec.propertySharding alone —
// neo4j-admin output is not parsed in Phase 3.
func TestExpectedShardArtifactsForBackup_Matrix(t *testing.T) {
	sdb2 := &neo4jv1beta1.Neo4jShardedDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
			Name:       "products",
			ClusterRef: "ec",
			PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
				PropertyShards: 2,
				GraphShard:     neo4jv1beta1.DatabaseTopology{Primaries: 1},
				PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{
					Replicas: 1,
				},
			},
		},
	}
	sdb5 := &neo4jv1beta1.Neo4jShardedDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
			Name:       "orders",
			ClusterRef: "ec",
			PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
				PropertyShards: 5,
				GraphShard:     neo4jv1beta1.DatabaseTopology{Primaries: 1},
				PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{
					Replicas: 1,
				},
			},
		},
	}

	mkBackup := func(kind, name string) *neo4jv1beta1.Neo4jBackup {
		return &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-backup", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{Kind: kind, Name: name, ClusterRef: "ec"},
			},
		}
	}

	t.Run("ShardedDatabase with 2 property shards → g000 + p000 + p001", func(t *testing.T) {
		r := newShardedTestReconciler(t, sdb2)
		got := r.expectedShardArtifactsForBackup(context.Background(),
			mkBackup(neo4jv1beta1.BackupTargetKindShardedDatabase, "products"))
		want := []string{"products-g000", "products-p000", "products-p001"}
		gotNames := make([]string, 0, len(got))
		for _, a := range got {
			gotNames = append(gotNames, a.ShardName)
		}
		if !reflect.DeepEqual(gotNames, want) {
			t.Errorf("ShardNames = %v, want %v", gotNames, want)
		}
	})

	t.Run("ShardedDatabase with 5 property shards → g000 + p000..p004", func(t *testing.T) {
		r := newShardedTestReconciler(t, sdb5)
		got := r.expectedShardArtifactsForBackup(context.Background(),
			mkBackup(neo4jv1beta1.BackupTargetKindShardedDatabase, "orders"))
		if len(got) != 6 {
			t.Fatalf("expected 6 artifacts (1 graph + 5 property), got %d: %+v", len(got), got)
		}
		if got[0].ShardName != "orders-g000" {
			t.Errorf("got[0] = %q, want orders-g000", got[0].ShardName)
		}
		// Spot check the last property shard uses the 3-digit pattern.
		if got[5].ShardName != "orders-p004" {
			t.Errorf("got[5] = %q, want orders-p004", got[5].ShardName)
		}
	})

	t.Run("Cluster-kind backup → nil", func(t *testing.T) {
		r := newShardedTestReconciler(t, sdb2)
		got := r.expectedShardArtifactsForBackup(context.Background(),
			mkBackup(neo4jv1beta1.BackupTargetKindCluster, "products"))
		if got != nil {
			t.Errorf("expected nil for Cluster kind, got %+v", got)
		}
	})

	t.Run("Sharded DB CR missing → nil (non-fatal)", func(t *testing.T) {
		r := newShardedTestReconciler(t) // no objects
		got := r.expectedShardArtifactsForBackup(context.Background(),
			mkBackup(neo4jv1beta1.BackupTargetKindShardedDatabase, "products"))
		if got != nil {
			t.Errorf("expected nil when sharded DB CR missing, got %+v", got)
		}
	})
}

// TestUpdateShardedDBLastBackup_Matrix pins the reverse-lookup that wires
// Neo4jShardedDatabase.status.lastBackup from a completed Neo4jBackup run.
// Phase 3 observability — Failed runs and non-sharded kinds must be no-ops.
func TestUpdateShardedDBLastBackup_Matrix(t *testing.T) {
	now := metav1.Now()
	succeededRun := neo4jv1beta1.BackupRun{
		RunID:          "uid-1",
		Status:         "Succeeded",
		BackupsPath:    "/backup/products-backup",
		CompletionTime: &now,
	}
	failedRun := neo4jv1beta1.BackupRun{
		RunID:  "uid-2",
		Status: "Failed",
	}

	shardedDB := func() *neo4jv1beta1.Neo4jShardedDatabase {
		return &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
			Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{Name: "products", ClusterRef: "ec"},
		}
	}

	backup := func(kind string) *neo4jv1beta1.Neo4jBackup {
		return &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "products-backup", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{Kind: kind, Name: "products", ClusterRef: "ec"},
			},
		}
	}

	t.Run("Succeeded sharded run → lastBackup populated", func(t *testing.T) {
		sdb := shardedDB()
		r := newShardedTestReconciler(t, sdb)
		r.updateShardedDBLastBackup(context.Background(), backup(neo4jv1beta1.BackupTargetKindShardedDatabase), succeededRun)

		updated := &neo4jv1beta1.Neo4jShardedDatabase{}
		if err := r.Get(context.Background(), types.NamespacedName{Name: "products", Namespace: "default"}, updated); err != nil {
			t.Fatalf("get sharded DB: %v", err)
		}
		if updated.Status.LastBackup == nil {
			t.Fatalf("expected LastBackup to be populated, got nil")
		}
		if updated.Status.LastBackup.RunID != "uid-1" {
			t.Errorf("RunID = %q, want uid-1", updated.Status.LastBackup.RunID)
		}
		if updated.Status.LastBackup.BackupsPath != "/backup/products-backup" {
			t.Errorf("BackupsPath = %q, want /backup/products-backup", updated.Status.LastBackup.BackupsPath)
		}
		if updated.Status.LastBackup.BackupRef != "products-backup" {
			t.Errorf("BackupRef = %q, want products-backup", updated.Status.LastBackup.BackupRef)
		}
	})

	t.Run("Failed run → no-op (lastBackup untouched)", func(t *testing.T) {
		sdb := shardedDB()
		r := newShardedTestReconciler(t, sdb)
		r.updateShardedDBLastBackup(context.Background(), backup(neo4jv1beta1.BackupTargetKindShardedDatabase), failedRun)

		updated := &neo4jv1beta1.Neo4jShardedDatabase{}
		_ = r.Get(context.Background(), types.NamespacedName{Name: "products", Namespace: "default"}, updated)
		if updated.Status.LastBackup != nil {
			t.Errorf("expected LastBackup nil on Failed run, got %+v", updated.Status.LastBackup)
		}
	})

	t.Run("Cluster-kind backup → no-op", func(t *testing.T) {
		sdb := shardedDB()
		r := newShardedTestReconciler(t, sdb)
		r.updateShardedDBLastBackup(context.Background(), backup(neo4jv1beta1.BackupTargetKindCluster), succeededRun)

		updated := &neo4jv1beta1.Neo4jShardedDatabase{}
		_ = r.Get(context.Background(), types.NamespacedName{Name: "products", Namespace: "default"}, updated)
		if updated.Status.LastBackup != nil {
			t.Errorf("expected LastBackup nil for Cluster-kind backup, got %+v", updated.Status.LastBackup)
		}
	})

	t.Run("Sharded DB CR missing → swallowed silently", func(t *testing.T) {
		r := newShardedTestReconciler(t) // no objects
		// MUST NOT panic or return an error — the helper logs and moves on.
		r.updateShardedDBLastBackup(context.Background(), backup(neo4jv1beta1.BackupTargetKindShardedDatabase), succeededRun)
	})
}

// TestBuildBackupCommand_ShardedDatabase_EmitsFlagsWithNilOptions guards
// against the regression where the --remote-address-resolution emission was
// gated on Spec.Options != nil. A sharded backup CR with only target + storage
// set MUST still get the flag injected because the ShardedDatabase + 2025.09+
// default fires from effectiveRemoteAddressResolution regardless of Options.
// Caught by the integration test in property_sharding_backup_test.go on first
// run.
func TestBuildBackupCommand_ShardedDatabase_EmitsFlagsWithNilOptions(t *testing.T) {
	r := newShardedTestReconciler(t)
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			AcceptLicenseAgreement: "eval",
			Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "2025.12-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "bk", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target: neo4jv1beta1.BackupTarget{
				Kind:       neo4jv1beta1.BackupTargetKindShardedDatabase,
				Name:       "products",
				ClusterRef: "ec",
			},
			Storage: neo4jv1beta1.StorageLocation{
				Type: "pvc",
				PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
			},
			// Options DELIBERATELY left nil — this is what catches the bug.
		},
	}

	cmd, err := r.buildBackupCommand(context.Background(), backup, cluster)
	if err != nil {
		t.Fatalf("buildBackupCommand: %v", err)
	}
	if !strings.Contains(cmd, "--remote-address-resolution=true") {
		t.Errorf("expected --remote-address-resolution=true in cmd, got: %q", cmd)
	}
	if !strings.Contains(cmd, `"products*"`) {
		t.Errorf("expected quoted glob \"products*\" in cmd, got: %q", cmd)
	}
}

// TestShardedShardNamePattern pins the per-shard regex used to filter
// SHOW DATABASES output during glob-safety checks.
func TestShardedShardNamePattern(t *testing.T) {
	pat := shardedShardNamePattern("products")
	matches := []string{
		"products-g000",
		"products-p000",
		"products-p001",
		"products-p999",
	}
	for _, m := range matches {
		if !pat.MatchString(m) {
			t.Errorf("expected pattern to match %q", m)
		}
	}
	rejects := []string{
		"products",           // virtual DB (handled separately by the caller)
		"products-g00",       // too few digits
		"products-g0000",     // too many digits
		"products-g000-",     // trailing dash
		"products-x000",      // wrong shard prefix
		"productsales",       // unrelated DB starting with same prefix — the POISONING case
		"productsales-g000",  // same prefix, different family
		"PRODUCTS-G000",      // case-sensitive: Neo4j DB names are case-preserving
		"products-G000",      // mixed case in shard prefix
		"products-g000.test", // extra suffix
	}
	for _, r := range rejects {
		if pat.MatchString(r) {
			t.Errorf("expected pattern NOT to match %q", r)
		}
	}

	// Regex-special char in logical name must be escaped (QuoteMeta) so a
	// name like "v1.2" doesn't widen the match. Without QuoteMeta the dot
	// would match any char.
	patDot := shardedShardNamePattern("v1.2")
	if patDot.MatchString("v1X2-g000") {
		t.Errorf("regex-special char in logical name was not escaped: %q matched %q", "v1.2", "v1X2-g000")
	}
	if !patDot.MatchString("v1.2-g000") {
		t.Errorf("expected literal pattern to match its own name")
	}
}

func TestShardedPreflightStatic(t *testing.T) {
	clusterReady := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			AcceptLicenseAgreement: "eval",
			Image:                  neo4jv1beta1.ImageSpec{Tag: "2025.12.0-enterprise"},
			PropertySharding:       &neo4jv1beta1.PropertyShardingSpec{Enabled: true},
		},
	}
	clusterNoSharding := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			AcceptLicenseAgreement: "eval",
			Image:                  neo4jv1beta1.ImageSpec{Tag: "2025.12.0-enterprise"},
		},
	}
	clusterOldVersion := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			AcceptLicenseAgreement: "eval",
			Image:                  neo4jv1beta1.ImageSpec{Tag: "2025.11.0-enterprise"},
			PropertySharding:       &neo4jv1beta1.PropertyShardingSpec{Enabled: true},
		},
	}

	shardedDBReady := &neo4jv1beta1.Neo4jShardedDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{Name: "products", ClusterRef: "ec"},
		Status:     neo4jv1beta1.Neo4jShardedDatabaseStatus{ShardingReady: ptrBool(true)},
	}
	shardedDBNotReady := &neo4jv1beta1.Neo4jShardedDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{Name: "products", ClusterRef: "ec"},
	}
	shardedDBOtherCluster := &neo4jv1beta1.Neo4jShardedDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{Name: "products", ClusterRef: "different-cluster"},
		Status:     neo4jv1beta1.Neo4jShardedDatabaseStatus{ShardingReady: ptrBool(true)},
	}

	backup := func() *neo4jv1beta1.Neo4jBackup {
		return &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "bk", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind:       neo4jv1beta1.BackupTargetKindShardedDatabase,
					Name:       "products",
					ClusterRef: "ec",
				},
			},
		}
	}

	type tc struct {
		name        string
		objs        []runtime.Object
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
		mutate      func(*neo4jv1beta1.Neo4jBackup)
		wantAction  preflightAction
		wantErrSub  string
		wantWaitSub string
	}
	cases := []tc{
		{
			name:       "non-sharded kind short-circuits",
			cluster:    clusterReady,
			mutate:     func(b *neo4jv1beta1.Neo4jBackup) { b.Spec.Target.Kind = neo4jv1beta1.BackupTargetKindCluster },
			wantAction: preflightContinue,
		},
		{
			name:       "happy path",
			objs:       []runtime.Object{shardedDBReady},
			cluster:    clusterReady,
			wantAction: preflightContinue,
		},
		{
			name:       "cluster sharding disabled → Fail",
			objs:       []runtime.Object{shardedDBReady},
			cluster:    clusterNoSharding,
			wantAction: preflightFail,
			wantErrSub: "property sharding enabled",
		},
		{
			name:       "cluster version too old → Fail",
			objs:       []runtime.Object{shardedDBReady},
			cluster:    clusterOldVersion,
			wantAction: preflightFail,
			wantErrSub: "below the 2025.12 minimum",
		},
		{
			name:       "sharded DB CR missing → Fail",
			objs:       nil,
			cluster:    clusterReady,
			wantAction: preflightFail,
			wantErrSub: `Neo4jShardedDatabase "products" not found`,
		},
		{
			name:        "sharded DB CR not Ready → Wait",
			objs:        []runtime.Object{shardedDBNotReady},
			cluster:     clusterReady,
			wantAction:  preflightWait,
			wantWaitSub: "not yet Ready",
		},
		{
			name:       "sharded DB CR cluster mismatch → Fail",
			objs:       []runtime.Object{shardedDBOtherCluster},
			cluster:    clusterReady,
			wantAction: preflightFail,
			wantErrSub: `references cluster "different-cluster"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newShardedTestReconciler(t, c.objs...)
			b := backup()
			if c.mutate != nil {
				c.mutate(b)
			}
			action, waitMsg, err := r.shardedPreflightStatic(context.Background(), b, c.cluster)
			if action != c.wantAction {
				t.Fatalf("action=%v, want %v (err=%v, waitMsg=%q)", action, c.wantAction, err, waitMsg)
			}
			if c.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrSub) {
					t.Errorf("err %v does not contain %q", err, c.wantErrSub)
				}
			}
			if c.wantWaitSub != "" {
				if !strings.Contains(waitMsg, c.wantWaitSub) {
					t.Errorf("waitMsg %q does not contain %q", waitMsg, c.wantWaitSub)
				}
			}
		})
	}
}

// Pins the CR-name vs logical-name contract (fresh-eyes journey, v1.12.1):
// target.name references the Neo4jShardedDatabase CR; the neo4j-admin glob,
// the glob-safety pattern, and the artifact prefixes use the LOGICAL name
// from that CR's spec.name. A glob built from the CR name matched zero
// databases while preflight passed — the Job then failed at neo4j-admin.
func TestShardedLogicalNameForBackup_ResolvesSpecName(t *testing.T) {
	shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "basic-sharded-db", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
			ClusterRef: "ec",
			Name:       "products", // logical name differs from the CR name
			PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
				PropertyShards: 2,
			},
		},
	}
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "bk", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target: neo4jv1beta1.BackupTarget{
				Kind:       neo4jv1beta1.BackupTargetKindShardedDatabase,
				Name:       "basic-sharded-db",
				ClusterRef: "ec",
			},
			Storage: neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "p"}},
		},
	}
	r := newShardedTestReconciler(t, shardedDB, backup)
	ctx := context.Background()

	if got := r.shardedLogicalNameForBackup(ctx, backup); got != "products" {
		t.Fatalf("logical name = %q, want products (the CR's spec.name)", got)
	}

	// The backup command must glob the LOGICAL name.
	cmd, err := r.buildBackupCommand(ctx, backup, fieldFindingsCluster("2026.04-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand: %v", err)
	}
	if !strings.Contains(cmd, `"products*"`) {
		t.Fatalf("backup command must glob the logical name, got %q", cmd)
	}
	if strings.Contains(cmd, "basic-sharded-db*") {
		t.Fatalf("backup command must NOT glob the CR name, got %q", cmd)
	}

	// Artifact stubs carry the logical prefix.
	arts := r.expectedShardArtifactsForBackup(ctx, backup)
	if len(arts) != 3 || arts[0].ShardName != "products-g000" || arts[2].ShardName != "products-p001" {
		t.Fatalf("artifacts must use the logical prefix, got %+v", arts)
	}

	// CR missing: fall back to target.name (the historical equal-names case).
	orphan := backup.DeepCopy()
	orphan.Spec.Target.Name = "gone"
	if got := r.shardedLogicalNameForBackup(ctx, orphan); got != "gone" {
		t.Fatalf("missing CR fallback = %q, want gone", got)
	}
}
