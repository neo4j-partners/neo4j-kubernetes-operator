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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func newDatabaseReconcilerForResolve(t *testing.T, objs ...client.Object) *Neo4jDatabaseReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &Neo4jDatabaseReconciler{Client: c, Scheme: scheme}
}

// TestResolveDatabaseHost covers the shared cluster-or-standalone resolution
// used by both Reconcile and handleDeletion. The standalone case is the
// regression that caused Neo4jDatabase deletion to silently skip DropDatabase
// on standalone hosts (handleDeletion used to look up only clusters).
func TestResolveDatabaseHost(t *testing.T) {
	ctx := context.Background()
	ns := "default"
	db := func(ref string) *neo4jv1beta1.Neo4jDatabase {
		return &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: ns},
			Spec:       neo4jv1beta1.Neo4jDatabaseSpec{Name: "mydb", ClusterRef: ref},
		}
	}

	t.Run("resolves a cluster", func(t *testing.T) {
		cl := &neo4jv1beta1.Neo4jEnterpriseCluster{ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: ns}}
		r := newDatabaseReconcilerForResolve(t, cl)
		cluster, standalone, isStandalone, found, err := r.resolveDatabaseHost(ctx, db("c1"))
		require.NoError(t, err)
		assert.True(t, found)
		assert.False(t, isStandalone)
		assert.NotNil(t, cluster)
		assert.Nil(t, standalone)
	})

	t.Run("falls back to a standalone (deletion regression)", func(t *testing.T) {
		sa := &neo4jv1beta1.Neo4jEnterpriseStandalone{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns}}
		r := newDatabaseReconcilerForResolve(t, sa)
		cluster, standalone, isStandalone, found, err := r.resolveDatabaseHost(ctx, db("s1"))
		require.NoError(t, err)
		assert.True(t, found)
		assert.True(t, isStandalone)
		assert.Nil(t, cluster)
		assert.NotNil(t, standalone)
	})

	t.Run("neither cluster nor standalone → not found, no error", func(t *testing.T) {
		r := newDatabaseReconcilerForResolve(t)
		_, _, _, found, err := r.resolveDatabaseHost(ctx, db("ghost"))
		require.NoError(t, err)
		assert.False(t, found)
	})
}
