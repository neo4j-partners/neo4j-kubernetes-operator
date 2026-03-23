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

func newStorageTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1alpha1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)
	return scheme
}

func newTestReconciler(scheme *runtime.Scheme, objs ...runtime.Object) *Neo4jEnterpriseClusterReconciler {
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()
	return &Neo4jEnterpriseClusterReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}
}

func makePVC(name, namespace, stsName, volumeName, storageClass, size string, ordinal int, withLabels bool) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      volumeName + "-" + stsName + "-" + itoa(ordinal),
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
			"app.kubernetes.io/name":     "neo4j",
			"app.kubernetes.io/instance": name,
		}
	}
	return pvc
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

func makeStorageClass(name string, allowExpansion bool) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Provisioner:          "kubernetes.io/test",
		AllowVolumeExpansion: &allowExpansion,
	}
}

func makeStatefulSet(name, namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-uid"),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(3)),
		},
	}
}

func TestFindPVCsForStatefulSet_LabelBased(t *testing.T) {
	scheme := newStorageTestScheme()
	pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, true)
	pvc1 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 1, true)
	pvc2 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 2, true)
	// Unrelated PVC
	unrelated := makePVC("other-cluster", "default", "other-cluster-server", "data", "ssd", "50Gi", 0, true)
	unrelated.Labels["app.kubernetes.io/instance"] = "other-cluster"

	r := newTestReconciler(scheme, pvc0, pvc1, pvc2, unrelated)

	pvcs, err := r.findPVCsForStatefulSet(context.Background(), "default", "my-cluster-server", "data")
	require.NoError(t, err)
	assert.Len(t, pvcs, 3)
}

func TestFindPVCsForStatefulSet_NamePrefixFallback(t *testing.T) {
	scheme := newStorageTestScheme()
	// Legacy PVCs without labels
	pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, false)
	pvc1 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 1, false)

	r := newTestReconciler(scheme, pvc0, pvc1)

	pvcs, err := r.findPVCsForStatefulSet(context.Background(), "default", "my-cluster-server", "data")
	require.NoError(t, err)
	assert.Len(t, pvcs, 2)
}

func TestFindPVCsForStatefulSet_PrefixCollisionProtection(t *testing.T) {
	scheme := newStorageTestScheme()
	// PVC for "my-cluster" (no labels, fallback mode)
	pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, false)
	// PVC for "my-cluster-extended" — should NOT be matched when looking for "my-cluster-server"
	pvcOther := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-my-cluster-server-extended-0",
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

	r := newTestReconciler(scheme, pvc0, pvcOther)

	pvcs, err := r.findPVCsForStatefulSet(context.Background(), "default", "my-cluster-server", "data")
	require.NoError(t, err)
	assert.Len(t, pvcs, 1)
	assert.Equal(t, "data-my-cluster-server-0", pvcs[0].Name)
}

func TestComparePVCSizes(t *testing.T) {
	scheme := newStorageTestScheme()
	pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, true)
	pvc1 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 1, true)

	r := newTestReconciler(scheme, pvc0, pvc1)

	// Same size — match
	state, err := r.comparePVCSizes(context.Background(), "default", "my-cluster-server", "data", resource.MustParse("100Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeMatch, state)

	// Larger desired — expand
	state, err = r.comparePVCSizes(context.Background(), "default", "my-cluster-server", "data", resource.MustParse("200Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeExpand, state)

	// Smaller desired — shrink
	state, err = r.comparePVCSizes(context.Background(), "default", "my-cluster-server", "data", resource.MustParse("50Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeShrink, state)

	// No PVCs — match (fresh cluster)
	r2 := newTestReconciler(scheme)
	state, err = r2.comparePVCSizes(context.Background(), "default", "new-cluster-server", "data", resource.MustParse("100Gi"))
	require.NoError(t, err)
	assert.Equal(t, pvcSizeMatch, state)
}

func TestValidateStorageClassExpandable(t *testing.T) {
	scheme := newStorageTestScheme()

	t.Run("expandable StorageClass", func(t *testing.T) {
		sc := makeStorageClass("fast-ssd", true)
		r := newTestReconciler(scheme, sc)

		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("fast-ssd"),
			},
		}
		err := r.validateStorageClassExpandable(context.Background(), pvc)
		assert.NoError(t, err)
	})

	t.Run("non-expandable StorageClass", func(t *testing.T) {
		sc := makeStorageClass("slow-hdd", false)
		r := newTestReconciler(scheme, sc)

		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("slow-hdd"),
			},
		}
		err := r.validateStorageClassExpandable(context.Background(), pvc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not allow volume expansion")
	})

	t.Run("missing StorageClass", func(t *testing.T) {
		r := newTestReconciler(scheme)

		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("nonexistent"),
			},
		}
		err := r.validateStorageClassExpandable(context.Background(), pvc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("nil StorageClassName", func(t *testing.T) {
		r := newTestReconciler(scheme)

		pvc := corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{},
		}
		err := r.validateStorageClassExpandable(context.Background(), pvc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no StorageClass set")
	})
}

func TestOrphanDeleteStatefulSet(t *testing.T) {
	scheme := newStorageTestScheme()

	t.Run("deletes existing StatefulSet", func(t *testing.T) {
		sts := makeStatefulSet("my-cluster-server", "default")
		r := newTestReconciler(scheme, sts)

		err := r.orphanDeleteStatefulSet(context.Background(), "default", "my-cluster-server")
		assert.NoError(t, err)

		// Verify StatefulSet is deleted
		got := &appsv1.StatefulSet{}
		err = r.Get(context.Background(), types.NamespacedName{Name: "my-cluster-server", Namespace: "default"}, got)
		assert.True(t, err != nil, "StatefulSet should be deleted")
	})

	t.Run("handles already-deleted StatefulSet", func(t *testing.T) {
		r := newTestReconciler(scheme)

		err := r.orphanDeleteStatefulSet(context.Background(), "default", "nonexistent")
		assert.NoError(t, err)
	})
}

func TestCheckStorageExpansionNeeded(t *testing.T) {
	scheme := newStorageTestScheme()

	t.Run("no expansion needed", func(t *testing.T) {
		pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, true)
		pvc1 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 1, true)

		r := newTestReconciler(scheme, pvc0, pvc1)

		cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "100Gi"},
			},
		}

		result, err := r.checkStorageExpansionNeeded(context.Background(), cluster)
		require.NoError(t, err)
		assert.False(t, result.needed)
		assert.False(t, result.dataExpansion)
		assert.False(t, result.backupExpansion)
	})

	t.Run("data expansion needed", func(t *testing.T) {
		pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, true)
		pvc1 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 1, true)

		r := newTestReconciler(scheme, pvc0, pvc1)

		cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "200Gi"},
			},
		}

		result, err := r.checkStorageExpansionNeeded(context.Background(), cluster)
		require.NoError(t, err)
		assert.True(t, result.needed)
		assert.True(t, result.dataExpansion)
		assert.False(t, result.backupExpansion)
	})

	t.Run("shrink detected", func(t *testing.T) {
		pvc0 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "200Gi", 0, true)
		pvc1 := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "200Gi", 1, true)

		r := newTestReconciler(scheme, pvc0, pvc1)

		cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "100Gi"},
			},
		}

		result, err := r.checkStorageExpansionNeeded(context.Background(), cluster)
		require.NoError(t, err)
		assert.True(t, result.shrinkDetected)
		assert.Contains(t, result.shrinkMessage, "smaller than existing")
		assert.False(t, result.needed)
	})

	t.Run("no PVCs yet (fresh cluster)", func(t *testing.T) {
		r := newTestReconciler(scheme)

		cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "new-cluster", Namespace: "default"},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Storage: neo4jv1alpha1.StorageSpec{ClassName: "ssd", Size: "100Gi"},
			},
		}

		result, err := r.checkStorageExpansionNeeded(context.Background(), cluster)
		require.NoError(t, err)
		assert.False(t, result.needed)
	})
}

func TestPatchPVCSize(t *testing.T) {
	scheme := newStorageTestScheme()
	pvc := makePVC("my-cluster", "default", "my-cluster-server", "data", "ssd", "100Gi", 0, true)
	r := newTestReconciler(scheme, pvc)

	desiredSize := resource.MustParse("200Gi")
	err := r.patchPVCSize(context.Background(), pvc, desiredSize)
	require.NoError(t, err)

	// Verify PVC was updated
	got := &corev1.PersistentVolumeClaim{}
	err = r.Get(context.Background(), types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, got)
	require.NoError(t, err)
	gotSize := got.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, desiredSize.String(), gotSize.String())
}
