package formation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
)

// genLeafPEM makes a throwaway self-signed cert and returns its PEM plus the fingerprint the
// reconciler computes (hex SHA-256 of the DER), so a fake ServedLeaf can mimic a member serving it.
func genLeafPEM(t *testing.T) (pemBytes []byte, fingerprint string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "test"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), certFingerprint(cert)
}

// reloadTestCR is a 3-primary cluster with cert-manager bolt TLS and trust.reload on.
func reloadTestCR() *neo4jv1.Neo4j {
	n := testClusterCR(3)
	n.Spec.Trust = &neo4jv1.TrustSpec{
		Enabled: true,
		Reload:  &neo4jv1.TrustReloadSpec{Enabled: true},
		CertManager: &neo4jv1.CertManagerSpec{
			Enabled:   true,
			IssuerRef: &neo4jv1.IssuerRef{Kind: "ClusterIssuer", Name: "corp-ca"},
		},
		Certificates: &neo4jv1.TrustCertificatesSpec{
			Bolt: &neo4jv1.TLSPolicySpec{SecretName: "prod-bolt-tls"},
		},
	}
	return n
}

func newReloadReconciler(t *testing.T, neo4j *neo4jv1.Neo4j, certPEM []byte) (*TLSReloadReconciler, *reloadSpy) {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = neo4jv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-bolt-tls", Namespace: "default"},
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": []byte("k")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	spy := &reloadSpy{}
	r := &TLSReloadReconciler{
		Client: c,
		Connect: func(_ context.Context, _ *neo4jv1.Neo4j, uri string) (intneo4j.Admin, error) {
			spy.record(uri)
			return &fakeAdmin{}, nil
		},
		ServedLeaf: func(_ context.Context, addr string) (string, error) { return spy.serve(addr) },
	}
	return r, spy
}

type reloadSpy struct {
	mu       sync.Mutex
	reloaded []string
	served   string // fingerprint every member currently serves
	failPort string // addr suffix (e.g. ":7473") whose served read returns an error
}

func (s *reloadSpy) record(uri string) { s.mu.Lock(); defer s.mu.Unlock(); s.reloaded = append(s.reloaded, uri) }
func (s *reloadSpy) serve(addr string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failPort != "" && strings.HasSuffix(addr, s.failPort) {
		return "", fmt.Errorf("connection refused")
	}
	return s.served, nil
}
func (s *reloadSpy) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.reloaded) }

func TestTLSReloadDisabledIsNoOp(t *testing.T) {
	certPEM, _ := genLeafPEM(t)
	neo4j := reloadTestCR()
	neo4j.Spec.Trust.Reload.Enabled = false
	r, spy := newReloadReconciler(t, neo4j, certPEM)

	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil || out.Result.RequeueAfter != 0 {
		t.Fatalf("reload off must be a no-op, got %+v", out)
	}
	if spy.count() != 0 {
		t.Fatalf("reload off dialed %d members", spy.count())
	}
}

func TestTLSReloadTriggersThenConverges(t *testing.T) {
	certPEM, wantFP := genLeafPEM(t)
	neo4j := reloadTestCR()
	r, spy := newReloadReconciler(t, neo4j, certPEM)

	// Pass 1: members still serve the old leaf → reloadTLS on all 3, requeue, checksum NOT set.
	spy.served = "old-fingerprint"
	out := r.Reconcile(t.Context(), neo4j)
	if out.Result.RequeueAfter == 0 {
		t.Fatal("stale members must requeue until they serve the new leaf")
	}
	if spy.count() != 3 {
		t.Fatalf("reloadTLS called on %d members, want 3 (one per primary)", spy.count())
	}
	if neo4j.Status.TLSReloadChecksum != "" {
		t.Fatal("checksum must not advance while members are still stale")
	}

	// Pass 2: kubelet propagated + reload took effect, every member now serves the new leaf.
	spy.served = wantFP
	out = r.Reconcile(t.Context(), neo4j)
	if out.Err != nil || out.Result.RequeueAfter != 0 {
		t.Fatalf("converged pass must be Done, got %+v", out)
	}
	if spy.count() != 3 {
		t.Fatalf("converged pass must not re-reload (still %d, want 3)", spy.count())
	}
	if neo4j.Status.TLSReloadChecksum == "" {
		t.Fatal("checksum must advance once every member serves the new leaf")
	}

	// Pass 3: nothing changed → gated out entirely, no dials.
	out = r.Reconcile(t.Context(), neo4j)
	if out.Err != nil || out.Result.RequeueAfter != 0 {
		t.Fatalf("steady state must be Done, got %+v", out)
	}
	if spy.count() != 3 {
		t.Fatalf("steady state re-dialed members (now %d, want 3)", spy.count())
	}
}

// A secondary pool member whose https port is unreachable must still converge on the bolt read —
// a port we cannot read must not wedge the loop (the cl-analytics-0 symptom).
func TestTLSReloadUnreadablePortDoesNotWedge(t *testing.T) {
	certPEM, wantFP := genLeafPEM(t)
	neo4j := reloadTestCR()
	neo4j.Spec.Trust.Certificates.HTTPS = &neo4jv1.TLSPolicySpec{SecretName: "prod-bolt-tls"}
	r, spy := newReloadReconciler(t, neo4j, certPEM)

	spy.served = wantFP     // every reachable port already serves the new leaf
	spy.failPort = ":7473"  // but https is refused on every member

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil || out.Result.RequeueAfter != 0 {
		t.Fatalf("a refused https read must not wedge convergence, got %+v", out)
	}
	if neo4j.Status.TLSReloadChecksum == "" {
		t.Fatal("must converge on the reachable bolt read when https is unreadable")
	}
	if spy.count() != 0 {
		t.Fatalf("already-current members must not be reloaded (got %d)", spy.count())
	}
}
