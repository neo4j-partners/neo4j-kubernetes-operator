package formation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Every pass that dials Neo4j builds these options, and client-go budgets Events per object, so
// repeating the warning verbatim would spend the budget an actual report needs later.
func TestAdminConnectOptsWarnsOncePerGeneration(t *testing.T) {
	rec := record.NewFakeRecorder(10)
	r := &Reconciler{Recorder: rec}
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns", UID: "uid-1", Generation: 1},
		Spec: neo4jv1beta1.Neo4jSpec{
			Trust: &neo4jv1beta1.TrustSpec{InsecureAdminConnection: true},
		},
	}
	for range 4 {
		if _, err := r.adminConnectOpts(context.Background(), n); err != nil {
			t.Fatalf("adminConnectOpts: %v", err)
		}
	}
	events := 0
	for {
		select {
		case <-rec.Events:
			events++
			continue
		default:
		}
		break
	}
	if events != 1 {
		t.Fatalf("expected 1 InsecureAdminConnection event over 4 passes, got %d", events)
	}
}

func TestAdminConnectOptsFailClosed(t *testing.T) {
	rec := record.NewFakeRecorder(2)
	r := &Reconciler{Recorder: rec}
	n := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}}
	_, err := r.adminConnectOpts(context.Background(), n)
	if err == nil || !strings.Contains(err.Error(), "insecureAdminConnection") {
		t.Fatalf("expected fail-closed, got %v", err)
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, "AdminBoltTLSRequired") {
			t.Fatalf("event %q", e)
		}
	default:
		t.Fatal("expected Warning event")
	}
}

func TestAdminConnectOptsInsecureEmitsWarning(t *testing.T) {
	rec := record.NewFakeRecorder(2)
	r := &Reconciler{Recorder: rec}
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Trust: &neo4jv1beta1.TrustSpec{InsecureAdminConnection: true},
		},
	}
	opts, err := r.adminConnectOpts(context.Background(), n)
	if err != nil || !opts.AllowPlaintext || opts.RootCAs != nil {
		t.Fatalf("opts=%#v err=%v", opts, err)
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, corev1.EventTypeWarning) || !strings.Contains(e, "InsecureAdminConnection") {
			t.Fatalf("event %q", e)
		}
	default:
		t.Fatal("expected Warning event")
	}
}

func selfSignedPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "corp-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func certManagerBoltCR() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled:     true,
				CertManager: &neo4jv1beta1.CertManagerSpec{Enabled: true, IssuerRef: &neo4jv1beta1.IssuerRef{Name: "corp-ca"}},
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{SecretName: "dev-bolt-tls"},
				},
			},
		},
	}
}

// The operator's own bolt+s dial must trust the cert-manager-issued material. loadBoltRootCAs
// resolves through PolicyMaterial, so the cert-manager shape (one Secret, tls.crt) works with no
// BYO publicCertificate — this is the path ClusterFormed depends on.
func TestLoadBoltRootCAsTrustsCertManagerLeaf(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := certManagerBoltCR()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dev-bolt-tls", Namespace: "ns"},
		Data:       map[string][]byte{"tls.crt": selfSignedPEM(t), "tls.key": []byte("key")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	pool, err := loadBoltRootCAs(t.Context(), c, neo4j)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil || len(pool.Subjects()) == 0 { //nolint:staticcheck // Subjects ok for test assertion
		t.Fatal("expected the issued leaf in the trust pool")
	}
}

func TestLoadBoltRootCAsCertManagerSecretMissingKey(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := certManagerBoltCR()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dev-bolt-tls", Namespace: "ns"},
		Data:       map[string][]byte{"tls.key": []byte("key")}, // tls.crt not issued yet
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	if _, err := loadBoltRootCAs(t.Context(), c, neo4j); err == nil {
		t.Fatal("expected an error when tls.crt is absent")
	}
}
