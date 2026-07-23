package persistence

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestWipeOnUninstallRetainNoop(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
	}
	pending, err := WipeOnUninstall(t.Context(), fake.NewClientBuilder().Build(), neo4j)
	if err != nil || pending {
		t.Fatalf("retain wipe = pending=%v err=%v", pending, err)
	}
}

func TestWipeOnUninstallDeletesManagedPVCs(t *testing.T) {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				VolumeClaimRetention: &neo4jv1beta1.VolumeClaimRetentionPolicySpec{
					WhenDeleted: neo4jv1beta1.VolumeClaimRetentionDelete,
				},
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:     neo4jv1beta1.VolumeModeExisting,
						Existing: &neo4jv1beta1.ExistingVolumeSpec{ClaimName: "keep-me"},
					},
				},
			},
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dev-server",
			Namespace: "default",
			Labels:    map[string]string{render.LabelInstance: "dev"},
		},
	}
	dyn := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-dev-server-0",
			Namespace: "default",
			Labels: map[string]string{
				render.LabelInstance:  "dev",
				render.LabelComponent: "storage",
			},
		},
	}
	keep := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keep-me",
			Namespace: "default",
			Labels: map[string]string{
				render.LabelInstance:  "dev",
				render.LabelComponent: "storage",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j, sts, dyn, keep).Build()

	pending, err := WipeOnUninstall(t.Context(), c, neo4j)
	if err != nil {
		t.Fatalf("wipe sts: %v", err)
	}
	if !pending {
		t.Fatal("expected pending after STS delete")
	}

	pending, err = WipeOnUninstall(t.Context(), c, neo4j)
	if err != nil {
		t.Fatalf("wipe pvc: %v", err)
	}
	if !pending {
		t.Fatal("expected pending after PVC delete request")
	}

	pending, err = WipeOnUninstall(t.Context(), c, neo4j)
	if err != nil || pending {
		t.Fatalf("final wipe = pending=%v err=%v", pending, err)
	}

	var left corev1.PersistentVolumeClaimList
	if err := c.List(t.Context(), &left, client.InNamespace("default")); err != nil {
		t.Fatal(err)
	}
	if len(left.Items) != 1 || left.Items[0].Name != "keep-me" {
		t.Fatalf("expected only keep-me, got %#v", left.Items)
	}
}
