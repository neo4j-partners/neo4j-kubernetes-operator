package status

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestBuildEndpointsPlainBolt(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	ep := buildEndpoints(render.ClientServiceContext(neo4j))
	if !strings.HasPrefix(ep.Bolt, "neo4j://") {
		t.Fatalf("bolt = %q", ep.Bolt)
	}
	if ep.Neo4j != ep.Bolt {
		t.Fatalf("neo4j = %q bolt = %q", ep.Neo4j, ep.Bolt)
	}
	if strings.Contains(ep.ConnectionExamples.PortForward, "bolt+s") {
		t.Fatalf("portForward = %q", ep.ConnectionExamples.PortForward)
	}
}

func TestBuildEndpointsBoltTLSAndHTTPS(t *testing.T) {
	https := int32(7473)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{HTTPS: &https},
				Service:   &neo4jv1beta1.ConnectivityServiceSpec{Expose: []string{"bolt", "http", "https"}},
			},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-cert"},
					},
					HTTPS: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-cert"},
					},
				},
			},
		},
	}
	ep := buildEndpoints(render.ClientServiceContext(neo4j))
	if !strings.HasPrefix(ep.Bolt, "neo4j+s://") {
		t.Fatalf("bolt = %q", ep.Bolt)
	}
	if ep.ConnectionExamples.Neo4jURI != ep.Bolt {
		t.Fatalf("neo4jURI = %q", ep.ConnectionExamples.Neo4jURI)
	}
	if !strings.Contains(ep.ConnectionExamples.PortForward, "bolt+s://127.0.0.1:") {
		t.Fatalf("portForward = %q", ep.ConnectionExamples.PortForward)
	}
	if ep.HTTPS != "https://prod.default.svc:7473" {
		t.Fatalf("https = %q", ep.HTTPS)
	}
}

func TestObservePoolStorageReadyPendingWithStorageClass(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	sc := "missing-sc"
	neo4j := standaloneWithDynamicSC("dev", "default", sc)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data-dev-server-0", Namespace: "default"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &sc},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build())
	ok, reason, msg := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 1)
	if ok || reason != oracle.ReasonPVCPending {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	if !strings.Contains(msg, `storageClassName="missing-sc"`) || !strings.Contains(msg, "data-dev-server-0") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestObservePoolStorageReadyPendingNoStorageClass(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := standaloneWithDynamicSC("dev", "default", "")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data-dev-server-0", Namespace: "default"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build())
	ok, reason, msg := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 1)
	if ok || reason != oracle.ReasonPVCPending {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	if !strings.Contains(msg, "default StorageClass") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestObservePoolStorageReadyBound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := standaloneWithDynamicSC("dev", "default", "standard")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data-dev-server-0", Namespace: "default"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build())
	ok, reason, msg := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 1)
	if !ok || reason != oracle.ReasonPVCBound || msg != "" {
		t.Fatalf("ok=%v reason=%q msg=%q", ok, reason, msg)
	}
}

func TestObservePoolStorageReadyPVCMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := standaloneWithDynamicSC("dev", "default", "standard")
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).Build())
	ok, reason, msg := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 1)
	if ok || reason != oracle.ReasonPVCPending {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	if !strings.Contains(msg, `waiting for PVC "data-dev-server-0"`) {
		t.Fatalf("msg = %q", msg)
	}
}

func standaloneCertManagerCR() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
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

func TestObserveTLSReadyDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jSpec{Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone}},
	}
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).Build())
	ok, reason, _ := w.observeTLSReady(t.Context(), neo4j)
	if !ok || reason != oracle.ReasonTrustDisabled {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

// Before cert-manager writes the Secret, TLSReady must read as pending issuance rather than
// a user-fixable SecretMissing — the operator provisioned it and is waiting.
func TestObserveTLSReadyCertManagerPending(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := standaloneCertManagerCR()
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).Build())
	ok, reason, msg := w.observeTLSReady(t.Context(), neo4j)
	if ok || reason != oracle.ReasonCertificatePending {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	if !strings.Contains(msg, "dev-bolt-tls") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestObserveTLSReadyCertManagerIssued(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	neo4j := standaloneCertManagerCR()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dev-bolt-tls", Namespace: "default"},
		Data:       map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")},
	}
	w := NewWriter(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build())
	ok, reason, _ := w.observeTLSReady(t.Context(), neo4j)
	if !ok || reason != oracle.ReasonSecretsPresent {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func standaloneWithDynamicSC(name, ns, sc string) *neo4jv1beta1.Neo4j {
	dyn := &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi", StorageClassName: sc}
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: dyn,
					},
				},
			},
		},
	}
}
