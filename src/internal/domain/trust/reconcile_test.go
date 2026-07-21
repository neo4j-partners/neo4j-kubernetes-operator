package trust

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestReconcileRequiresSecretKeys(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-cert"},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		neo4j,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bolt-key", Namespace: "default"},
			Data:       map[string][]byte{"wrong.key": []byte("x")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bolt-cert", Namespace: "default"},
			Data:       map[string][]byte{"public.crt": []byte("cert")},
		},
	).Build()

	out := New(c).Reconcile(t.Context(), neo4j)
	if out.Err == nil || !strings.Contains(out.Err.Error(), "private.key") {
		t.Fatalf("expected missing private.key error, got %v", out.Err)
	}

	var key corev1.Secret
	if err := c.Get(t.Context(), types.NamespacedName{Name: "bolt-key", Namespace: "default"}, &key); err != nil {
		t.Fatal(err)
	}
	key.Data = map[string][]byte{"private.key": []byte("key")}
	if err := c.Update(t.Context(), &key); err != nil {
		t.Fatal(err)
	}

	out = New(c).Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("expected success, got %v", out.Err)
	}
}
