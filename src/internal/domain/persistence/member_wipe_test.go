package persistence

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestOrdinalForSTS(t *testing.T) {
	ord, ok := ordinalForSTS("data-namespaced-primary-3", "namespaced-primary")
	if !ok || ord != 3 {
		t.Fatalf("got %d %v", ord, ok)
	}
	if _, ok := ordinalForSTS("data-other-primary-1", "namespaced-primary"); ok {
		t.Fatal("expected miss")
	}
}

func dynamicNeo4j(whenScaled neo4jv1beta1.VolumeClaimRetentionPolicyType) *neo4jv1beta1.Neo4j {
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "namespaced", Namespace: "restricted"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
	if whenScaled != "" {
		n.Spec.Storage.VolumeClaimRetention = &neo4jv1beta1.VolumeClaimRetentionPolicySpec{
			WhenScaled: whenScaled,
		}
	}
	return n
}

func TestWipeStaleMemberPVCsRequiresWhenScaledDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	labels := map[string]string{
		render.LabelInstance:  "namespaced",
		render.LabelComponent: "storage",
		render.LabelName:      render.AppNameValue,
		render.LabelManagedBy: render.ManagedByValue,
	}
	objs := []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-0", Namespace: "restricted", Labels: labels},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-3", Namespace: "restricted", Labels: labels},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-4", Namespace: "restricted", Labels: labels},
		},
	}

	// Default Retain — NEO-007: no wipe.
	cRetain := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	if err := WipeStaleMemberPVCs(t.Context(), cRetain, dynamicNeo4j(""), render.PoolPrimary, 3); err != nil {
		t.Fatal(err)
	}
	var leftRetain corev1.PersistentVolumeClaimList
	_ = cRetain.List(t.Context(), &leftRetain)
	if len(leftRetain.Items) != 3 {
		t.Fatalf("retain left = %d, want 3", len(leftRetain.Items))
	}

	// Explicit Delete — wipe ordinals >= 3.
	cDelete := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	if err := WipeStaleMemberPVCs(t.Context(), cDelete, dynamicNeo4j(neo4jv1beta1.VolumeClaimRetentionDelete), render.PoolPrimary, 3); err != nil {
		t.Fatal(err)
	}
	var leftDelete corev1.PersistentVolumeClaimList
	_ = cDelete.List(t.Context(), &leftDelete)
	if len(leftDelete.Items) != 1 || leftDelete.Items[0].Name != "data-namespaced-primary-0" {
		t.Fatalf("delete left = %#v", leftDelete.Items)
	}
}

func TestRecycleMemberStoreHealsUnderRetain(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	labels := map[string]string{
		render.LabelInstance:  "namespaced",
		render.LabelComponent: "storage",
		render.LabelName:      render.AppNameValue,
		render.LabelManagedBy: render.ManagedByValue,
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "namespaced-primary-3",
		Namespace: "restricted",
		Labels:    render.OperandInstanceLabels("namespaced"),
	}}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-3", Namespace: "restricted", Labels: labels},
	}

	// Heal recycle is allowed under default Retain (Dropped store cannot rejoin).
	cRetain := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod.DeepCopy(), pvc.DeepCopy()).Build()
	if err := RecycleMemberStore(t.Context(), cRetain, dynamicNeo4j(""), render.PoolPrimary, 3, pod.Name); err != nil {
		t.Fatal(err)
	}
	if err := cRetain.Get(t.Context(), types.NamespacedName{Name: pod.Name, Namespace: "restricted"}, &corev1.Pod{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pod should be deleted: %v", err)
	}
	if err := cRetain.Get(t.Context(), types.NamespacedName{Name: pvc.Name, Namespace: "restricted"}, &corev1.PersistentVolumeClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pvc should be deleted: %v", err)
	}
}
