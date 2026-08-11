package shared

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestApplyRefusesAdoptUnowned(t *testing.T) {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-worker", Namespace: "team", UID: "cr-uid"},
	}
	foreign := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-worker", Namespace: "team"},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j, foreign).Build()

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "payments-worker", Namespace: "team"}}
	err := Apply(t.Context(), c, s, neo4j, sa, func() error {
		sa.Labels = map[string]string{"app.kubernetes.io/managed-by": "neo4j-operator"}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to adopt") {
		t.Fatalf("got %v", err)
	}
	var still corev1.ServiceAccount
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(foreign), &still); err != nil {
		t.Fatal(err)
	}
	if len(still.OwnerReferences) != 0 {
		t.Fatalf("foreign SA must stay unowned, got %#v", still.OwnerReferences)
	}
}

func TestApplyUpdatesOwned(t *testing.T) {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default", UID: "cr-uid"},
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"}}
	if err := controllerutil.SetControllerReference(neo4j, sa, s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j, sa).Build()

	target := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"}}
	err := Apply(t.Context(), c, s, neo4j, target, func() error {
		target.Labels = map[string]string{"k": "v"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got corev1.ServiceAccount
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(target), &got); err != nil {
		t.Fatal(err)
	}
	if got.Labels["k"] != "v" {
		t.Fatalf("labels = %#v", got.Labels)
	}
}

func TestApplyCreatesMissing(t *testing.T) {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default", UID: "cr-uid"},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j).Build()

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"}}
	if err := Apply(t.Context(), c, s, neo4j, sa, func() error {
		sa.Labels = map[string]string{"ok": "1"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var got corev1.ServiceAccount
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(sa), &got); err != nil {
		t.Fatal(err)
	}
	if !metav1.IsControlledBy(&got, neo4j) {
		t.Fatal("expected owner ref")
	}
}
