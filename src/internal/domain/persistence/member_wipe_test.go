package persistence

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

func TestWipeStaleMemberPVCs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := &neo4jv1beta1.Neo4j{
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
	labels := map[string]string{
		render.LabelInstance:  "namespaced",
		render.LabelComponent: "storage",
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-0", Namespace: "restricted", Labels: labels},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-3", Namespace: "restricted", Labels: labels},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-namespaced-primary-4", Namespace: "restricted", Labels: labels},
		},
	).Build()

	if err := WipeStaleMemberPVCs(t.Context(), c, neo4j, render.PoolPrimary, 3); err != nil {
		t.Fatal(err)
	}

	var left corev1.PersistentVolumeClaimList
	_ = c.List(t.Context(), &left)
	if len(left.Items) != 1 || left.Items[0].Name != "data-namespaced-primary-0" {
		t.Fatalf("left = %#v", left.Items)
	}
}
