package trust

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestCertificatesDisabledWithoutCertManager(t *testing.T) {
	if got := Certificates(clusterWithTrust(), nil); got != nil {
		t.Fatalf("BYO trust must not render Certificates, got %d", len(got))
	}
}

func TestCertificatesPerPolicy(t *testing.T) {
	neo4j := clusterWithCertManager()
	certs := Certificates(neo4j, map[string]string{"neo4j.com/mountable-by-operator": "true"})

	byPolicy := map[string]*unstructured.Unstructured{}
	for _, pc := range certs {
		byPolicy[pc.Policy] = pc.Object
	}
	if len(byPolicy) != 2 {
		t.Fatalf("policies = %v, want cluster and bolt", keysOf(byPolicy))
	}

	cluster, ok := byPolicy["cluster"]
	if !ok {
		t.Fatal("missing cluster Certificate")
	}
	if cluster.GetName() != "prod-cluster-tls" {
		t.Fatalf("name = %q", cluster.GetName())
	}
	if got := nestedString(t, cluster, "spec", "secretName"); got != "prod-cluster-tls-secret" {
		t.Fatalf("secretName = %q", got)
	}
	if got := nestedString(t, cluster, "spec", "issuerRef", "name"); got != "corp-ca" {
		t.Fatalf("issuerRef.name = %q", got)
	}
	// Kind defaults to ClusterIssuer, group is always cert-manager.io.
	if got := nestedString(t, cluster, "spec", "issuerRef", "kind"); got != "ClusterIssuer" {
		t.Fatalf("issuerRef.kind = %q", got)
	}
	if got := nestedString(t, cluster, "spec", "issuerRef", "group"); got != "cert-manager.io" {
		t.Fatalf("issuerRef.group = %q", got)
	}
	if got := nestedString(t, cluster, "spec", "secretTemplate", "labels", "neo4j.com/mountable-by-operator"); got != "true" {
		t.Fatalf("secretTemplate label = %q; issued Secret must carry the mount opt-in", got)
	}

	// Cluster members authenticate to each other, so the cert is used from both ends.
	if !hasString(nestedSlice(t, cluster, "spec", "usages"), "client auth") {
		t.Fatalf("cluster usages = %v, want client auth", nestedSlice(t, cluster, "spec", "usages"))
	}
	if hasString(nestedSlice(t, byPolicy["bolt"], "spec", "usages"), "client auth") {
		t.Fatal("bolt certificate is server-side only and must not request client auth")
	}
}

func TestCertificateClusterSANsAreMemberInternalsOnly(t *testing.T) {
	certs := Certificates(clusterWithCertManager(), nil)
	var cluster *unstructured.Unstructured
	for _, pc := range certs {
		if pc.Policy == "cluster" {
			cluster = pc.Object
		}
	}
	sans := nestedSlice(t, cluster, "spec", "dnsNames")

	// server.cluster.advertised_address resolves to SERVICE_NEO4J_INTERNALS.
	for _, want := range []string{
		"prod-primary-0-internals.default.svc.cluster.local",
		"prod-primary-1-internals.default.svc.cluster.local",
		"prod-primary-2-internals.default.svc.cluster.local",
	} {
		if !hasString(sans, want) {
			t.Fatalf("cluster SANs %v missing %q", sans, want)
		}
	}
	// BDR-006: caller-facing names never land on the cluster certificate.
	for _, unwanted := range []string{"neo4j.example.com", "bolt.prod.example.com", "prod.default.svc"} {
		if hasString(sans, unwanted) {
			t.Fatalf("cluster SANs must not include %q, got %v", unwanted, sans)
		}
	}
}

func TestCertificateBoltSANsCoverClientAndMembers(t *testing.T) {
	certs := Certificates(clusterWithCertManager(), nil)
	var bolt *unstructured.Unstructured
	for _, pc := range certs {
		if pc.Policy == "bolt" {
			bolt = pc.Object
		}
	}
	sans := nestedSlice(t, bolt, "spec", "dnsNames")

	for _, want := range []string{
		// The operator's admin dial uses the short Service form (formation.ClientBoltURI).
		"prod.default.svc",
		"prod.default.svc.cluster.local",
		// server.bolt.advertised_address is the per-member client FQDN (SERVICE_NEO4J).
		"prod-primary-0.default.svc.cluster.local",
		"prod-primary-2.default.svc.cluster.local",
		// trust.certManager.dnsNames and ingress hosts apply to bolt/https.
		"bolt.prod.example.com",
		"neo4j.example.com",
	} {
		if !hasString(sans, want) {
			t.Fatalf("bolt SANs %v missing %q", sans, want)
		}
	}
}

func TestCertManagerMountsAndSecretKeys(t *testing.T) {
	neo4j := clusterWithCertManager()

	mat, ok := PolicyMaterial(neo4j, "bolt")
	if !ok {
		t.Fatal("expected bolt material from cert-manager shape")
	}
	if !mat.Provisioned {
		t.Fatal("cert-manager material must be marked Provisioned")
	}
	if mat.KeyPath != "tls.key" || mat.CertPath != "tls.crt" {
		t.Fatalf("cert-manager keys = %q/%q, want tls.key/tls.crt", mat.KeyPath, mat.CertPath)
	}

	// The issued Secret is operator-provisioned, so it must not be demanded up front by
	// the user-supplied Secret checks (EnsureMountable, TLSReady/SecretMissing).
	for _, n := range BYOSecretNames(neo4j) {
		if n == "prod-bolt-tls-secret" || n == "prod-cluster-tls-secret" {
			t.Fatalf("cert-manager target Secret %q must not be a required user Secret", n)
		}
	}
	if !hasSecretKey(RequiredSecretKeys(neo4j), "cluster-ca", "ca.crt") {
		t.Fatal("user-supplied trustedCerts keys are still required under cert-manager")
	}
	if !hasSecretKey(ProvisionedSecretKeys(neo4j), "prod-bolt-tls-secret", "tls.crt") {
		t.Fatalf("expected provisioned bolt key, got %#v", ProvisionedSecretKeys(neo4j))
	}
}

// cert-manager publishes ca.crt next to the leaf, so pointing trustedCerts at the issued
// Secret is natural. That Secret must not become an up-front user requirement, or the first
// reconcile deadlocks: it cannot exist until the operator has created the Certificate.
func TestTrustedCertsOnIssuedSecretAreNotUserRequired(t *testing.T) {
	neo4j := clusterWithCertManager()
	neo4j.Spec.Trust.Certificates.Cluster.TrustedCerts = &neo4jv1beta1.TLSTrustedCertsSpec{
		Sources: []corev1.VolumeProjection{{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: "prod-cluster-tls-secret"},
				Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
			},
		}},
	}

	for _, n := range BYOSecretNames(neo4j) {
		if n == "prod-cluster-tls-secret" {
			t.Fatal("issued Secret must not be required by EnsureMountable before issuance")
		}
	}
	if hasSecretKey(RequiredSecretKeys(neo4j), "prod-cluster-tls-secret", "ca.crt") {
		t.Fatal("issued ca.crt must not be a hard user requirement")
	}
	if !hasSecretKey(ProvisionedSecretKeys(neo4j), "prod-cluster-tls-secret", "ca.crt") {
		t.Fatalf("issued ca.crt must be awaited, got %#v", ProvisionedSecretKeys(neo4j))
	}
}

// A cert-manager-shaped CR used to pass CEL and then fail the reconciler because the
// BYO privateKey/publicCertificate pair was missing.
func TestValidateAcceptsCertManagerShape(t *testing.T) {
	if err := Validate(clusterWithCertManager()); err != nil {
		t.Fatalf("cert-manager shape must validate: %v", err)
	}
}

func TestValidateCertManagerRequiresSecretName(t *testing.T) {
	neo4j := clusterWithCertManager()
	neo4j.Spec.Trust.Certificates.Cluster.SecretName = ""
	err := Validate(neo4j)
	if err == nil {
		t.Fatal("expected TLS-002c error")
	}
	if !strings.Contains(err.Error(), "TLS-002c") {
		t.Fatalf("error should name TLS-002c, got %v", err)
	}
}

func TestValidateCertManagerIncludeIngressHostsNeedsHost(t *testing.T) {
	neo4j := clusterWithCertManager()
	neo4j.Spec.Connectivity.Ingress.Rules = nil
	if err := Validate(neo4j); err == nil || !strings.Contains(err.Error(), "TLS-007") {
		t.Fatalf("expected TLS-007 error, got %v", err)
	}
}

func TestValidateCertManagerRequiresIssuer(t *testing.T) {
	neo4j := clusterWithCertManager()
	neo4j.Spec.Trust.CertManager.IssuerRef = nil
	if err := Validate(neo4j); err == nil || !strings.Contains(err.Error(), "TLS-001") {
		t.Fatalf("expected TLS-001 error, got %v", err)
	}
}

// TLS-004: Optional still accepts client certificates, so it needs a CA bundle too.
func TestClientAuthOptionalRequiresTrustedCerts(t *testing.T) {
	neo4j := standaloneWithBoltTrust()
	neo4j.Spec.Trust.Certificates.Bolt.ClientAuth = "Optional"
	err := Validate(neo4j)
	if err == nil || !strings.Contains(err.Error(), "TLS-004") {
		t.Fatalf("expected TLS-004 error for clientAuth Optional, got %v", err)
	}

	neo4j.Spec.Trust.Certificates.Bolt.TrustedCerts = &neo4jv1beta1.TLSTrustedCertsSpec{
		Sources: clusterWithTrust().Spec.Trust.Certificates.Cluster.TrustedCerts.Sources,
	}
	if err := Validate(neo4j); err != nil {
		t.Fatalf("Optional with trustedCerts must pass: %v", err)
	}
}

func clusterWithCertManager() *neo4jv1beta1.Neo4j {
	neo4j := clusterWithTrust()
	neo4j.Spec.Trust.CertManager = &neo4jv1beta1.CertManagerSpec{
		Enabled:             true,
		IssuerRef:           &neo4jv1beta1.IssuerRef{Name: "corp-ca"},
		IncludeIngressHosts: true,
		DNSNames:            []string{"bolt.prod.example.com"},
	}
	// cert-manager shape: target secretName only, no privateKey/publicCertificate.
	neo4j.Spec.Trust.Certificates.Cluster.PrivateKey = nil
	neo4j.Spec.Trust.Certificates.Cluster.PublicCertificate = nil
	neo4j.Spec.Trust.Certificates.Cluster.SecretName = "prod-cluster-tls-secret"
	neo4j.Spec.Trust.Certificates.Bolt = &neo4jv1beta1.TLSPolicySpec{SecretName: "prod-bolt-tls-secret"}
	neo4j.Spec.Connectivity = &neo4jv1beta1.ConnectivitySpec{
		Ingress: &neo4jv1beta1.IngressSpec{
			Enabled: true,
			Rules:   []neo4jv1beta1.IngressRuleSpec{{Host: "neo4j.example.com"}},
		},
	}
	return neo4j
}

func nestedString(t *testing.T, u *unstructured.Unstructured, fields ...string) string {
	t.Helper()
	v, found, err := unstructured.NestedString(u.Object, fields...)
	if err != nil || !found {
		t.Fatalf("field %v not found (err=%v)", fields, err)
	}
	return v
}

func nestedSlice(t *testing.T, u *unstructured.Unstructured, fields ...string) []string {
	t.Helper()
	v, found, err := unstructured.NestedStringSlice(u.Object, fields...)
	if err != nil || !found {
		t.Fatalf("field %v not found (err=%v)", fields, err)
	}
	return v
}

func hasString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func hasSecretKey(needs []SecretKeyNeed, secret, key string) bool {
	for _, n := range needs {
		if n.SecretName == secret && n.Key == key {
			return true
		}
	}
	return false
}

func keysOf(m map[string]*unstructured.Unstructured) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
