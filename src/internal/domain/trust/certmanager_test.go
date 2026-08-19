package trust

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

func certManagerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	gvk := rendertrust.CertificateGVK
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List",
	}, &unstructured.UnstructuredList{})
	return scheme
}

func standaloneCertManagerCR() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default", UID: types.UID("cr-uid")},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled:     true,
				CertManager: &neo4jv1beta1.CertManagerSpec{Enabled: true, IssuerRef: &neo4jv1beta1.IssuerRef{Name: "corp-ca"}},
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{SecretName: "dev-bolt-tls-secret"},
				},
			},
		},
	}
}

func issuedSecret(name string, labels map[string]string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
		},
	}
}

// First reconcile creates the Certificate and then waits: the Secret cannot exist yet, and
// that is pending issuance rather than a user error.
func TestCertManagerCreatesCertificateThenAwaitsSecret(t *testing.T) {
	scheme := certManagerScheme(t)
	neo4j := standaloneCertManagerCR()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(neo4j).Build()

	out := New(c, scheme).Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.Result.RequeueAfter == 0 {
		t.Fatal("expected a requeue while cert-manager has not issued the Secret")
	}

	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(rendertrust.CertificateGVK)
	key := types.NamespacedName{Name: "dev-bolt-tls", Namespace: "default"}
	if err := c.Get(t.Context(), key, cert); err != nil {
		t.Fatalf("expected Certificate %q: %v", key.Name, err)
	}
	if got, _, _ := unstructured.NestedString(cert.Object, "spec", "secretName"); got != "dev-bolt-tls-secret" {
		t.Fatalf("Certificate secretName = %q", got)
	}
	if len(cert.GetOwnerReferences()) == 0 {
		t.Fatal("Certificate must be owned by the Neo4j CR for GC")
	}
}

func TestCertManagerSucceedsOnceIssued(t *testing.T) {
	scheme := certManagerScheme(t)
	neo4j := standaloneCertManagerCR()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		neo4j,
		issuedSecret("dev-bolt-tls-secret", rendersecrets.WithMountableLabel(nil)),
	).Build()

	out := New(c, scheme).Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.Result.RequeueAfter != 0 {
		t.Fatalf("expected completion, got requeue after %s", out.Result.RequeueAfter)
	}
}

// The target secretName is CR-supplied, so it can name a pre-existing Secret belonging to
// something else. Without the mount opt-in label that Secret must not reach Neo4j pods.
func TestCertManagerRejectsSecretWithoutMountOptIn(t *testing.T) {
	scheme := certManagerScheme(t)
	neo4j := standaloneCertManagerCR()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		neo4j,
		issuedSecret("dev-bolt-tls-secret", nil),
	).Build()

	out := New(c, scheme).Reconcile(t.Context(), neo4j)
	if out.Err == nil {
		t.Fatal("expected refusal to mount a Secret without the opt-in label")
	}
	if !strings.Contains(out.Err.Error(), rendersecrets.MountableLabel) {
		t.Fatalf("error should name the required label, got %v", out.Err)
	}
}

// cert-manager can create the Secret before it finishes writing the key material. A present but
// empty data key is still pending issuance, so reconcile must requeue rather than mount nothing.
func TestCertManagerRequeuesOnPartialIssuance(t *testing.T) {
	scheme := certManagerScheme(t)
	neo4j := standaloneCertManagerCR()
	half := issuedSecret("dev-bolt-tls-secret", rendersecrets.WithMountableLabel(nil))
	half.Data["tls.crt"] = nil // key present, value not written yet
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(neo4j, half).Build()

	out := New(c, scheme).Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.Result.RequeueAfter == 0 {
		t.Fatal("expected a requeue while tls.crt is empty")
	}
}

func TestCertManagerPrunesCertificateWhenDisabled(t *testing.T) {
	scheme := certManagerScheme(t)
	neo4j := standaloneCertManagerCR()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		neo4j,
		issuedSecret("dev-bolt-tls-secret", rendersecrets.WithMountableLabel(nil)),
	).Build()

	if out := New(c, scheme).Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("setup reconcile failed: %v", out.Err)
	}

	// Turning cert-manager off must stop renewals, not leave a live Certificate behind.
	neo4j.Spec.Trust.CertManager.Enabled = false
	neo4j.Spec.Trust.Certificates.Bolt = &neo4jv1beta1.TLSPolicySpec{
		PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "dev-bolt-tls-secret"},
		PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "dev-bolt-tls-secret"},
	}
	byoSecret := issuedSecret("dev-bolt-tls-secret", rendersecrets.WithMountableLabel(nil))
	byoSecret.Data["private.key"] = []byte("key")
	byoSecret.Data["public.crt"] = []byte("cert")
	if err := c.Update(t.Context(), byoSecret); err != nil {
		t.Fatal(err)
	}

	if out := New(c, scheme).Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("reconcile after disabling cert-manager failed: %v", out.Err)
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(rendertrust.CertificateGVK)
	if err := c.List(t.Context(), list, client.InNamespace("default")); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected stale Certificates pruned, got %d", len(list.Items))
	}
}
