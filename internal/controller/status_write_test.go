/*
Copyright 2025.

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// TestUpdateClusterStatusWithVersion_PersistsAfterPriorStatusWrite pins the
// #207 fix: the rolling-upgrade completion path does TWO status writes in one
// reconcile. The first (updateClusterStatus) refetches + updates a fresh copy,
// leaving the in-memory `cluster` at a stale resourceVersion. The old code then
// did `cluster.Status.Version = tag; r.Status().Update(ctx, cluster)` on that
// stale object — a guaranteed 409 that was logged and swallowed, so the version
// bump was lost. Folding the version into updateClusterStatusWithVersion (its
// own refetch + RetryOnConflict) makes it land regardless.
func TestUpdateClusterStatusWithVersion_PersistsAfterPriorStatusWrite(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", Generation: 3},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Tag: "2025.04.0-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
		},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &Neo4jEnterpriseClusterReconciler{Client: fc}

	// First write leaves the in-memory `cluster` resourceVersion stale.
	r.updateClusterStatus(context.Background(), cluster, "Ready", "ready")
	// Version write must still land despite the stale in-memory object.
	r.updateClusterStatusWithVersion(context.Background(), cluster, "Ready", "Rolling upgrade completed successfully", cluster.Spec.Image.Tag)

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "c", Namespace: "default"}, latest))
	assert.Equal(t, "2025.04.0-enterprise", latest.Status.Version,
		"status.version must reflect spec.image.tag after a successful upgrade (#207)")
	assert.Equal(t, "Ready", latest.Status.Phase)
	assert.Equal(t, "Rolling upgrade completed successfully", latest.Status.Message)
}

// TestUpdateClusterStatus_NoVersionWrite confirms the plain (no-version) path is
// unchanged: it never touches status.version.
func TestUpdateClusterStatus_NoVersionWrite(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", Generation: 1},
		Spec:       neo4jv1beta1.Neo4jEnterpriseClusterSpec{Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3}},
		Status:     neo4jv1beta1.Neo4jEnterpriseClusterStatus{Version: "preexisting"},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &Neo4jEnterpriseClusterReconciler{Client: fc}

	r.updateClusterStatus(context.Background(), cluster, "Forming", "forming")

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "c", Namespace: "default"}, latest))
	assert.Equal(t, "preexisting", latest.Status.Version, "plain updateClusterStatus must not alter status.version")
	assert.Equal(t, "Forming", latest.Status.Phase)
}

// TestSetFleetManagementStatus_Cluster_RefetchesAndPersists pins the cluster
// fleet status fix: the write goes to the refetched object (not the stale
// reconcile-start one) and is mirrored back in memory.
func TestSetFleetManagementStatus_Cluster_RefetchesAndPersists(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &Neo4jEnterpriseClusterReconciler{Client: fc}

	require.NoError(t, r.setFleetManagementStatus(context.Background(), cluster, true, "registered"))

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "c", Namespace: "default"}, latest))
	require.NotNil(t, latest.Status.AuraFleetManagement)
	assert.True(t, latest.Status.AuraFleetManagement.Registered)
	assert.Equal(t, "registered", latest.Status.AuraFleetManagement.Message)
	assert.NotNil(t, latest.Status.AuraFleetManagement.LastRegistrationTime)
	// In-memory object mirrored.
	require.NotNil(t, cluster.Status.AuraFleetManagement)
	assert.True(t, cluster.Status.AuraFleetManagement.Registered)
}

// TestSetFailedStatus_Standalone pins the failure-path fix: a refetched write
// with phase=Failed, Ready=false, and ObservedGeneration set (it was previously
// dropped, and one path used an event-reason constant as the phase).
func TestSetFailedStatus_Standalone(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	sa := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default", Generation: 7},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).WithStatusSubresource(sa).Build()
	r := &Neo4jEnterpriseStandaloneReconciler{Client: fc}

	r.setFailedStatus(context.Background(), sa, "boom")

	latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "s", Namespace: "default"}, latest))
	assert.Equal(t, "Failed", latest.Status.Phase)
	assert.Equal(t, "boom", latest.Status.Message)
	assert.False(t, latest.Status.Ready)
	assert.Equal(t, int64(7), latest.Status.ObservedGeneration,
		"ObservedGeneration must track the reconciled generation")
	// In-memory mirror.
	assert.Equal(t, "Failed", sa.Status.Phase)
}

// TestSetFailedStatus_Standalone_StaleGeneration pins that the failure is
// stamped against the generation we actually reconciled, not the newer
// generation a concurrent spec edit left in the store. Otherwise a failure
// derived from generation N would mark generation N+1 as observed and suppress
// the re-reconcile the new spec deserves.
func TestSetFailedStatus_Standalone_StaleGeneration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	// The object in the store has advanced to generation 8 (spec changed
	// mid-reconcile).
	stored := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default", Generation: 8},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stored).WithStatusSubresource(stored).Build()
	r := &Neo4jEnterpriseStandaloneReconciler{Client: fc}

	// We reconciled generation 7.
	reconcileStart := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default", Generation: 7},
	}
	r.setFailedStatus(context.Background(), reconcileStart, "boom")

	latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "s", Namespace: "default"}, latest))
	assert.Equal(t, int64(8), latest.Generation, "store still holds the newer generation")
	assert.Equal(t, int64(7), latest.Status.ObservedGeneration,
		"must stamp the reconciled generation (7), not the stored latest (8)")
	require.NotEmpty(t, latest.Status.Conditions)
	assert.Equal(t, int64(7), latest.Status.Conditions[0].ObservedGeneration,
		"the Ready condition's observedGeneration must also track the reconciled generation")
}
