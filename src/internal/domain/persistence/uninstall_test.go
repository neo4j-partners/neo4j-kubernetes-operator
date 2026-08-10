package persistence

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

func operandLabels(component string) map[string]string {
	m := render.OperandInstanceLabels("dev")
	m[render.LabelComponent] = component
	return m
}

func TestWipeOnUninstallDeletesManagedPVCs(t *testing.T) {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default", UID: "neo4j-uid"},
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
			Labels:    operandLabels("workload"),
		},
	}
	if err := controllerutil.SetControllerReference(neo4j, sts, s); err != nil {
		t.Fatal(err)
	}
	dyn := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-dev-server-0",
			Namespace: "default",
			Labels:    operandLabels("storage"),
		},
	}
	keep := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keep-me",
			Namespace: "default",
			Labels:    operandLabels("storage"),
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

func TestWipeOnUninstallSkipsForeignHelmSTS(t *testing.T) {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "neo4j-uid"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				VolumeClaimRetention: &neo4jv1beta1.VolumeClaimRetentionPolicySpec{
					WhenDeleted: neo4jv1beta1.VolumeClaimRetentionDelete,
				},
			},
		},
	}
	// Helm release also named "orders" — only shares app.kubernetes.io/instance.
	foreign := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orders",
			Namespace: "default",
			Labels: map[string]string{
				render.LabelInstance: "orders",
				render.LabelName:     "orders-app",
				render.LabelManagedBy: "Helm",
			},
		},
	}
	ours := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orders-primary",
			Namespace: "default",
			Labels:    render.OperandInstanceLabels("orders"),
		},
	}
	if err := controllerutil.SetControllerReference(neo4j, ours, s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j, foreign, ours).Build()

	pending, err := WipeOnUninstall(t.Context(), c, neo4j)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("expected pending while deleting our STS")
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(foreign), foreign); err != nil {
		t.Fatalf("foreign Helm STS must remain: %v", err)
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(ours), &appsv1.StatefulSet{}); err == nil {
		t.Fatal("expected our STS deleted")
	}
}
