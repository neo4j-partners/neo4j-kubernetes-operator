package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestNeo4jPasswordRotationSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if _, err := scheme.New(GroupVersion.WithKind("Neo4jPasswordRotation")); err != nil {
		t.Fatalf("scheme.New Neo4jPasswordRotation: %v", err)
	}
	if _, err := scheme.New(GroupVersion.WithKind("Neo4jPasswordRotationList")); err != nil {
		t.Fatalf("scheme.New Neo4jPasswordRotationList: %v", err)
	}
}

// DeepCopy must produce an independent object: mutating the copy must not touch the original.
// The staged-Secret ref and phase are the fields the reconciler reads and writes, so they are the
// ones worth guarding against an accidental shallow copy.
func TestNeo4jPasswordRotationDeepCopy(t *testing.T) {
	orig := &Neo4jPasswordRotation{
		ObjectMeta: metav1.ObjectMeta{Name: "rotate-prod", Namespace: "default"},
		Spec: Neo4jPasswordRotationSpec{
			Neo4jRef:             Neo4jRef{Name: "prod"},
			NewPasswordSecretRef: PasswordSecretRef{Name: "prod-new-credential"},
		},
		Status: Neo4jPasswordRotationStatus{Phase: RunPhasePending},
	}
	cp := orig.DeepCopy()
	if cp == orig {
		t.Fatal("DeepCopy returned same pointer")
	}
	cp.Spec.NewPasswordSecretRef.Name = "changed"
	cp.Status.Phase = RunPhaseSucceeded
	if orig.Spec.NewPasswordSecretRef.Name != "prod-new-credential" {
		t.Fatal("mutating the copy's NewPasswordSecretRef leaked into the original")
	}
	if orig.Status.Phase != RunPhasePending {
		t.Fatal("mutating the copy's Phase leaked into the original")
	}
}
