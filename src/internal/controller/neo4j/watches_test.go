package neo4j

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

func TestMapSecretToNeo4jEnqueuesMountingCR(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	cr := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-cm", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				CertManager: &neo4jv1beta1.CertManagerSpec{
					Enabled:   true,
					IssuerRef: &neo4jv1beta1.IssuerRef{Name: "corp-ca"},
				},
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{SecretName: "prod-cm-bolt-tls"},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-cm-bolt-tls",
			Namespace: "default",
			Labels:    rendersecrets.WithMountableLabel(nil),
		},
	}
	other := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated",
			Namespace: "default",
			Labels:    rendersecrets.WithMountableLabel(nil),
		},
	}
	unlabeled := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-cm-bolt-tls", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, secret, other).Build()
	r := &Neo4jReconciler{Client: c}

	got := r.mapSecretToNeo4j(t.Context(), secret)
	if len(got) != 1 || got[0].Name != "prod-cm" {
		t.Fatalf("tls secret enqueue = %#v", got)
	}
	if reqs := r.mapSecretToNeo4j(t.Context(), other); len(reqs) != 0 {
		t.Fatalf("unrelated secret enqueue = %#v", reqs)
	}
	if reqs := r.mapSecretToNeo4j(t.Context(), unlabeled); len(reqs) != 0 {
		t.Fatalf("unlabeled secret enqueue = %#v", reqs)
	}
}
