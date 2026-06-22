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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// Pins the #227 URL-escaping fix: type=storage restores feed user-supplied
// spec.source.backupPath into the proxy URL — reserved URL characters must
// be percent-escaped per path segment, with '/' separators preserved.
func TestPvcSeedProxyURL_EscapesUserControlledSegments(t *testing.T) {
	cases := []struct {
		name        string
		backupsPath string
		filename    string
		want        string
	}{
		{
			name:        "plain path untouched",
			backupsPath: "inventory-backup",
			filename:    "inventory-2026-06-08T01-18-06.backup",
			want:        "http://backup-seed-proxy-r.ns.svc.cluster.local:8080/inventory-backup/inventory-2026-06-08T01-18-06.backup",
		},
		{
			name:        "space, percent and hash escaped; slashes preserved",
			backupsPath: "my backups/100%",
			filename:    "db#1.backup",
			want:        "http://backup-seed-proxy-r.ns.svc.cluster.local:8080/my%20backups/100%25/db%231.backup",
		},
		{
			name:        "question mark cannot start a query string",
			backupsPath: "dir",
			filename:    "a?b.backup",
			want:        "http://backup-seed-proxy-r.ns.svc.cluster.local:8080/dir/a%3Fb.backup",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pvcSeedProxyURL("r", "ns", tc.backupsPath, tc.filename))
		})
	}
}

// Pins the #227 seed-proxy wait anchor: the first stamp wins (the deadline
// must not slide on requeues), clearing is idempotent, and a fresh attempt
// can't inherit a stale anchor.
func TestSeedProxyWaitStarted_AnchorLifecycle(t *testing.T) {
	restore := restoreWithBackupRef("r1", "default", "nightly")
	r := newResolvedSourceReconciler(t, restore)
	ctx := context.Background()

	first, err := r.markSeedProxyWaitStarted(ctx, restore)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), first, time.Minute)

	// Second call preserves the original anchor.
	again, err := r.markSeedProxyWaitStarted(ctx, restore)
	require.NoError(t, err)
	assert.True(t, again.Equal(first), "anchor must not slide on requeue")

	persisted := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(restore), persisted))
	assert.Contains(t, persisted.Annotations, AnnotationSeedProxyWaitStarted)

	require.NoError(t, r.clearSeedProxyWaitStarted(ctx, restore))
	require.NoError(t, r.clearSeedProxyWaitStarted(ctx, restore)) // idempotent

	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(restore), persisted))
	assert.NotContains(t, persisted.Annotations, AnnotationSeedProxyWaitStarted)
	assert.NotContains(t, restore.Annotations, AnnotationSeedProxyWaitStarted)
}

// Bugbot PR #265 (medium): a persistently failing annotation write must not
// reopen the unbounded proxy wait — the anchor falls back to
// status.startTime, and only a restore with no anchor at all skips expiry.
func TestSeedProxyWaitStart_FallsBackToStartTimeOnPersistFailure(t *testing.T) {
	restore := restoreWithBackupRef("r1", "default", "nightly")
	started := metav1.NewTime(time.Now().Add(-10 * time.Minute).Truncate(time.Second))
	restore.Status.StartTime = &started

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	failingClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(restore).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				return errors.New("injected: annotation write always fails")
			},
		}).
		Build()
	r := &Neo4jRestoreReconciler{Client: failingClient, Scheme: scheme, RequeueAfter: time.Second}

	anchor, have := r.seedProxyWaitStart(context.Background(), restore)
	require.True(t, have, "StartTime fallback must provide an anchor")
	assert.True(t, anchor.Equal(started.Time), "fallback anchor must be the persisted status.startTime")

	// The anchor must NOT slide: startRestore resets the IN-MEMORY StartTime
	// to "now" on every Pending requeue — the fallback reads the PERSISTED
	// value (Bugbot round 2).
	slid := metav1.Now()
	restore.Status.StartTime = &slid
	anchor, have = r.seedProxyWaitStart(context.Background(), restore)
	require.True(t, have)
	assert.True(t, anchor.Equal(started.Time), "fallback anchor must ignore the slid in-memory StartTime")

	// Happy path unaffected: with a working client the stamp is the anchor.
	rOK := newResolvedSourceReconciler(t, restoreWithBackupRef("r2", "default", "nightly"))
	r2 := restoreWithBackupRef("r2", "default", "nightly")
	anchor, have = rOK.seedProxyWaitStart(context.Background(), r2)
	require.True(t, have)
	assert.WithinDuration(t, time.Now(), anchor, time.Minute)
}
