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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

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
// with phase=Failed, Ready=false, and ObservedGeneration set to the current
// generation (it was previously dropped, and one path used an event-reason
// constant as the phase).
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
	assert.Equal(t, latest.Generation, latest.Status.ObservedGeneration,
		"ObservedGeneration must track the object's generation")
	// In-memory mirror.
	assert.Equal(t, "Failed", sa.Status.Phase)
}
