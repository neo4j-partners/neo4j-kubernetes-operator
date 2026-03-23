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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

func newStandaloneTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1alpha1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)
	return scheme
}

func newStandaloneTestReconciler(scheme *runtime.Scheme, objs ...runtime.Object) *Neo4jEnterpriseStandaloneReconciler {
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()
	return &Neo4jEnterpriseStandaloneReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}
}

func makeStandalonePVC(name, namespace, storageClass, size string, ordinal int, withLabels bool) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("neo4j-data-%s-%d", name, ordinal),
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(size),
				},
			},
		},
	}
	if withLabels {
		pvc.Labels = map[string]string{
			"app": name,
		}
	}
	return pvc
}

func TestFindStandalonePVCs_LabelBased(t *testing.T) {
	scheme := newStandaloneTestScheme()
	pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, true)

	r := newStandaloneTestReconciler(scheme, pvc)
	pvcs, err := r.findStandalonePVCs(context.Background(), "default", "my-standalone", "neo4j-data")
	require.NoError(t, err)
	assert.Len(t, pvcs, 1)
	assert.Equal(t, "neo4j-data-my-standalone-0", pvcs[0].Name)
}

func TestFindStandalonePVCs_NamePrefixFallback(t *testing.T) {
	scheme := newStandaloneTestScheme()
	// Legacy PVC without labels
	pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, false)

	r := newStandaloneTestReconciler(scheme, pvc)
	pvcs, err := r.findStandalonePVCs(context.Background(), "default", "my-standalone", "neo4j-data")
	require.NoError(t, err)
	assert.Len(t, pvcs, 1)
}

func TestFindStandalonePVCs_PrefixCollisionProtection(t *testing.T) {
	scheme := newStandaloneTestScheme()
	pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, false)
	// Similar name but different standalone — should NOT match
	pvcOther := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "neo4j-data-my-standalone-extended-0",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi"),
				},
			},
		},
	}

	r := newStandaloneTestReconciler(scheme, pvc, pvcOther)
	pvcs, err := r.findStandalonePVCs(context.Background(), "default", "my-standalone", "neo4j-data")
	require.NoError(t, err)
	assert.Len(t, pvcs, 1)
	assert.Equal(t, "neo4j-data-my-standalone-0", pvcs[0].Name)
}

func TestCompareStandalonePVCSizes(t *testing.T) {
	scheme := newStandaloneTestScheme()
	pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, true)
	r := newStandaloneTestReconciler(scheme, pvc)

	// Match
	state, err := r.compareStandalonePVCSizes(context.Background(), "default", "my-standalone", "neo4j-data", resource.MustParse("100Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeMatch, state)

	// Expand
	state, err = r.compareStandalonePVCSizes(context.Background(), "default", "my-standalone", "neo4j-data", resource.MustParse("200Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeExpand, state)

	// Shrink
	state, err = r.compareStandalonePVCSizes(context.Background(), "default", "my-standalone", "neo4j-data", resource.MustParse("50Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeShrink, state)

	// No PVCs
	r2 := newStandaloneTestReconciler(scheme)
	state, err = r2.compareStandalonePVCSizes(context.Background(), "default", "new-standalone", "neo4j-data", resource.MustParse("100Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeMatch, state)
}

func TestValidateStandaloneStorageClassExpandable(t *testing.T) {
	scheme := newStandaloneTestScheme()

	t.Run("expandable", func(t *testing.T) {
		sc := makeStorageClass("fast-ssd", true)
		r := newStandaloneTestReconciler(scheme, sc)
		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To("fast-ssd")},
		}
		assert.NoError(t, r.validateStandaloneStorageClassExpandable(context.Background(), pvc))
	})

	t.Run("non-expandable", func(t *testing.T) {
		sc := makeStorageClass("slow-hdd", false)
		r := newStandaloneTestReconciler(scheme, sc)
		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To("slow-hdd")},
		}
		err := r.validateStandaloneStorageClassExpandable(context.Background(), pvc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not allow volume expansion")
	})
}

func TestOrphanDeleteStandaloneStatefulSet(t *testing.T) {
	scheme := newStandaloneTestScheme()

	t.Run("deletes existing", func(t *testing.T) {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-standalone",
				Namespace: "default",
				UID:       types.UID("test-uid"),
			},
			Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
		}
		r := newStandaloneTestReconciler(scheme, sts)
		assert.NoError(t, r.orphanDeleteStandaloneStatefulSet(context.Background(), "default", "my-standalone"))

		got := &appsv1.StatefulSet{}
		err := r.Get(context.Background(), types.NamespacedName{Name: "my-standalone", Namespace: "default"}, got)
		assert.True(t, err != nil, "StatefulSet should be deleted")
	})

	t.Run("handles already-deleted", func(t *testing.T) {
		r := newStandaloneTestReconciler(scheme)
		assert.NoError(t, r.orphanDeleteStandaloneStatefulSet(context.Background(), "default", "nonexistent"))
	})
}

func TestReconcileStandaloneStorageExpansion(t *testing.T) {
	scheme := newStandaloneTestScheme()

	t.Run("no expansion needed", func(t *testing.T) {
		pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, true)
		r := newStandaloneTestReconciler(scheme, pvc)

		standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "100Gi"},
			},
		}

		requeue, err := r.reconcileStandaloneStorageExpansion(context.Background(), standalone)
		require.NoError(t, err)
		assert.False(t, requeue)
	})

	t.Run("shrink rejected", func(t *testing.T) {
		pvc := makeStandalonePVC("my-standalone", "default", "ssd", "200Gi", 0, true)
		r := newStandaloneTestReconciler(scheme, pvc)

		standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "100Gi"},
			},
		}

		requeue, err := r.reconcileStandaloneStorageExpansion(context.Background(), standalone)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shrink not supported")
		assert.False(t, requeue)
	})

	t.Run("expansion with expandable StorageClass", func(t *testing.T) {
		pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, true)
		sc := makeStorageClass("ssd", true)
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-standalone",
				Namespace: "default",
				UID:       types.UID("test-uid"),
			},
			Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
		}
		r := newStandaloneTestReconciler(scheme, pvc, sc, sts)

		standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "200Gi"},
			},
		}

		requeue, err := r.reconcileStandaloneStorageExpansion(context.Background(), standalone)
		require.NoError(t, err)
		assert.True(t, requeue)

		// Verify PVC was patched
		got := &corev1.PersistentVolumeClaim{}
		err = r.Get(context.Background(), types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, got)
		require.NoError(t, err)
		gotSize := got.Spec.Resources.Requests[corev1.ResourceStorage]
		assert.Equal(t, "200Gi", gotSize.String())

		// Verify StatefulSet was deleted
		gotSts := &appsv1.StatefulSet{}
		err = r.Get(context.Background(), types.NamespacedName{Name: "my-standalone", Namespace: "default"}, gotSts)
		assert.True(t, err != nil, "StatefulSet should be orphan-deleted")
	})

	t.Run("no PVCs yet (fresh deployment)", func(t *testing.T) {
		r := newStandaloneTestReconciler(scheme)

		standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "new-standalone", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "100Gi"},
			},
		}

		requeue, err := r.reconcileStandaloneStorageExpansion(context.Background(), standalone)
		require.NoError(t, err)
		assert.False(t, requeue)
	})
}

func TestPatchStandalonePVCSize(t *testing.T) {
	scheme := newStandaloneTestScheme()
	pvc := makeStandalonePVC("my-standalone", "default", "ssd", "100Gi", 0, true)
	r := newStandaloneTestReconciler(scheme, pvc)

	desiredSize := resource.MustParse("200Gi")
	err := r.patchStandalonePVCSize(context.Background(), pvc, desiredSize)
	require.NoError(t, err)

	got := &corev1.PersistentVolumeClaim{}
	err = r.Get(context.Background(), types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, got)
	require.NoError(t, err)
	gotSize := got.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "200Gi", gotSize.String())
}
