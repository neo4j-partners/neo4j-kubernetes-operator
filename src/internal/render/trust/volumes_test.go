package trust

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestTrustAnchorKeys(t *testing.T) {
	t.Run("cert-manager publishes ca.crt", func(t *testing.T) {
		neo4j := &neo4jv1.Neo4j{
			ObjectMeta: metav1.ObjectMeta{Name: "cl", Namespace: "default"},
			Spec: neo4jv1.Neo4jSpec{
				Trust: &neo4jv1.TrustSpec{
					Enabled:     true,
					CertManager: &neo4jv1.CertManagerSpec{Enabled: true, IssuerRef: &neo4jv1.IssuerRef{Name: "corp-ca"}},
					Certificates: &neo4jv1.TrustCertificatesSpec{
						Bolt: &neo4jv1.TLSPolicySpec{SecretName: "cl-bolt-tls"},
					},
				},
			},
		}
		got := TrustAnchorKeys(neo4j, "bolt")
		if len(got) != 1 || got[0] != (SecretKeyNeed{SecretName: "cl-bolt-tls", Key: "ca.crt"}) {
			t.Fatalf("got %#v, want [{cl-bolt-tls ca.crt}]", got)
		}
	})

	t.Run("self-signed BYO falls back to the leaf cert", func(t *testing.T) {
		got := TrustAnchorKeys(standaloneWithBoltTrust(), "bolt")
		// standaloneWithBoltTrust sets publicCertificate secret dev-bolt-cert, subPath public.crt.
		if len(got) != 1 || got[0] != (SecretKeyNeed{SecretName: "dev-bolt-cert", Key: "public.crt"}) {
			t.Fatalf("got %#v, want [{dev-bolt-cert public.crt}]", got)
		}
	})

	t.Run("BYO trustedCerts sources win over the leaf", func(t *testing.T) {
		neo4j := standaloneWithBoltTrust()
		neo4j.Spec.Trust.Certificates.Bolt.TrustedCerts = &neo4jv1.TLSTrustedCertsSpec{
			Sources: []corev1.VolumeProjection{{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{Name: "bolt-ca"},
					Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
				},
			}},
		}
		got := TrustAnchorKeys(neo4j, "bolt")
		if len(got) != 1 || got[0] != (SecretKeyNeed{SecretName: "bolt-ca", Key: "ca.crt"}) {
			t.Fatalf("got %#v, want [{bolt-ca ca.crt}]", got)
		}
	})

	t.Run("no policy material → nil", func(t *testing.T) {
		if got := TrustAnchorKeys(&neo4jv1.Neo4j{}, "bolt"); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
}

func TestAppendVolumesClusterBYO(t *testing.T) {
	neo4j := clusterWithTrust()
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	container := &corev1.Container{}
	podSpec := &corev1.PodSpec{}
	AppendVolumes(ctx, container, podSpec)

	if len(podSpec.Volumes) != 3 {
		t.Fatalf("volumes = %d, want 3 (cert,key,trusted)", len(podSpec.Volumes))
	}
	if len(container.VolumeMounts) != 3 {
		t.Fatalf("mounts = %d, want 3", len(container.VolumeMounts))
	}
	foundKey, foundCert, foundTrusted := false, false, false
	for _, m := range container.VolumeMounts {
		switch m.MountPath {
		case "/var/lib/neo4j/certificates/cluster/live-key":
			foundKey = true
			if m.SubPath != "" {
				t.Fatalf("key must be a directory mount (no subPath), got %q", m.SubPath)
			}
		case "/var/lib/neo4j/certificates/cluster/live-cert":
			foundCert = true
			if m.SubPath != "" {
				t.Fatalf("cert must be a directory mount (no subPath), got %q", m.SubPath)
			}
		case "/var/lib/neo4j/certificates/cluster/trusted/ca.crt":
			foundTrusted = true
			if m.SubPath != "ca.crt" {
				t.Fatalf("trusted ca.crt subPath = %q", m.SubPath)
			}
		}
	}
	if !foundKey || !foundCert || !foundTrusted {
		t.Fatalf("missing mounts key=%v cert=%v trusted=%v mounts=%v", foundKey, foundCert, foundTrusted, container.VolumeMounts)
	}
}

func TestAppendVolumesBoltAndHTTPS(t *testing.T) {
	neo4j := clusterWithTrustAndConnectors()
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	container := &corev1.Container{}
	podSpec := &corev1.PodSpec{}
	AppendVolumes(ctx, container, podSpec)

	// Leaf cert+key are directory mounts (no subPath) so kubelet refreshes them on a reissue.
	want := []string{
		"/var/lib/neo4j/certificates/https/live-cert",
		"/var/lib/neo4j/certificates/https/live-key",
		"/var/lib/neo4j/certificates/bolt/live-cert",
		"/var/lib/neo4j/certificates/bolt/live-key",
	}
	for _, p := range want {
		found := false
		for _, m := range container.VolumeMounts {
			if m.MountPath == p {
				found = true
				if m.SubPath != "" {
					t.Fatalf("mount %s must be a directory mount (no subPath), got %q", p, m.SubPath)
				}
				break
			}
		}
		if !found {
			t.Fatalf("missing mount %s in %#v", p, container.VolumeMounts)
		}
	}
}

func TestNeo4jConfKeysCluster(t *testing.T) {
	ctx := render.ContextForPool(clusterWithTrust(), render.PoolPrimary)
	keys := Neo4jConfKeys(ctx)
	if keys["dbms.ssl.policy.cluster.enabled"] != "true" {
		t.Fatalf("enabled = %q", keys["dbms.ssl.policy.cluster.enabled"])
	}
	if keys["dbms.ssl.policy.cluster.client_auth"] != "REQUIRE" {
		t.Fatalf("client_auth = %q", keys["dbms.ssl.policy.cluster.client_auth"])
	}
	if keys["internal.dbms.ssl.system.ignore_dot_files"] != "true" {
		t.Fatalf("ignore_dot_files = %q", keys["internal.dbms.ssl.system.ignore_dot_files"])
	}
	// public_certificate/private_key point at the live-directory mounts (split secrets here).
	if got := keys["dbms.ssl.policy.cluster.public_certificate"]; got != "/var/lib/neo4j/certificates/cluster/live-cert/public.crt" {
		t.Fatalf("public_certificate = %q", got)
	}
	if got := keys["dbms.ssl.policy.cluster.private_key"]; got != "/var/lib/neo4j/certificates/cluster/live-key/private.key" {
		t.Fatalf("private_key = %q", got)
	}
}

func TestNeo4jConfKeysBoltAndHTTPS(t *testing.T) {
	keys := Neo4jConfKeys(render.ContextForPool(clusterWithTrustAndConnectors(), render.PoolPrimary))
	if keys["dbms.ssl.policy.https.enabled"] != "true" {
		t.Fatalf("https enabled = %q", keys["dbms.ssl.policy.https.enabled"])
	}
	if keys["dbms.ssl.policy.bolt.enabled"] != "true" {
		t.Fatalf("bolt enabled = %q", keys["dbms.ssl.policy.bolt.enabled"])
	}
	if keys["dbms.ssl.policy.bolt.client_auth"] != "NONE" {
		t.Fatalf("bolt client_auth = %q", keys["dbms.ssl.policy.bolt.client_auth"])
	}
	if keys["server.bolt.tls_level"] != "REQUIRED" {
		t.Fatalf("tls_level = %q", keys["server.bolt.tls_level"])
	}
}

func TestValidateClusterShape(t *testing.T) {
	if err := ValidateClusterShape(clusterWithTrust()); err != nil {
		t.Fatal(err)
	}
	bad := clusterWithTrust()
	bad.Spec.Trust.Certificates.Cluster.PrivateKey = nil
	if err := ValidateClusterShape(bad); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestValidateStandaloneTrust(t *testing.T) {
	neo4j := standaloneWithBoltTrust()
	if err := Validate(neo4j); err != nil {
		t.Fatal(err)
	}
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	container := &corev1.Container{}
	podSpec := &corev1.PodSpec{}
	AppendVolumes(ctx, container, podSpec)
	foundBolt := false
	for _, m := range container.VolumeMounts {
		if m.MountPath == "/var/lib/neo4j/certificates/bolt/live-cert" {
			foundBolt = true
		}
		if strings.Contains(m.MountPath, "/certificates/cluster/") {
			t.Fatalf("standalone must not mount cluster certs: %s", m.MountPath)
		}
	}
	if !foundBolt {
		t.Fatal("expected bolt mount on standalone")
	}
	keys := Neo4jConfKeys(ctx)
	if keys["server.bolt.tls_level"] != "REQUIRED" {
		t.Fatalf("tls_level = %q", keys["server.bolt.tls_level"])
	}
	if _, ok := keys["dbms.ssl.policy.cluster.enabled"]; ok {
		t.Fatal("standalone must not enable cluster ssl policy")
	}

	withCluster := standaloneWithBoltTrust()
	withCluster.Spec.Trust.Certificates.Cluster = &neo4jv1.TLSPolicySpec{
		PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "cluster-key"},
		PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "cluster-cert"},
	}
	if err := ValidateClusterShape(withCluster); err == nil {
		t.Fatal("expected cluster policy rejected on standalone")
	}
}

func TestValidateHTTPSShapeRequiresBolt(t *testing.T) {
	httpsPort := int32(7473)
	neo4j := clusterWithTrust()
	neo4j.Spec.Connectivity = &neo4jv1.ConnectivitySpec{
		Listeners: &neo4jv1.ConnectivityListenersSpec{HTTPS: &httpsPort},
	}
	neo4j.Spec.Trust.Certificates.HTTPS = &neo4jv1.TLSPolicySpec{
		PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "https-key"},
		PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "https-cert"},
	}
	if err := ValidateHTTPSShape(neo4j); err == nil {
		t.Fatal("expected missing bolt certs error")
	}
	neo4j.Spec.Trust.Certificates.Bolt = &neo4jv1.TLSPolicySpec{
		PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "bolt-key"},
		PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "bolt-cert"},
	}
	if err := ValidateHTTPSShape(neo4j); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredSecretKeys(t *testing.T) {
	keys := RequiredSecretKeys(clusterWithTrust())
	want := map[string]string{
		"cluster-key":  "private.key",
		"cluster-cert": "public.crt",
		"cluster-ca":   "ca.crt",
	}
	if len(keys) != len(want) {
		t.Fatalf("keys = %#v", keys)
	}
	for _, k := range keys {
		if want[k.SecretName] != k.Key {
			t.Fatalf("unexpected %#v", k)
		}
	}
	names := BYOSecretNames(clusterWithTrust())
	if len(names) != 3 {
		t.Fatalf("names = %#v", names)
	}
	if BoltTLSEnabled(clusterWithTrust()) {
		t.Fatal("cluster-only trust should not enable bolt TLS")
	}
	if !BoltTLSEnabled(standaloneWithBoltTrust()) {
		t.Fatal("expected bolt TLS on standalone bolt trust")
	}
}

func TestRequiredSecretKeysCustomSubPath(t *testing.T) {
	neo4j := standaloneWithBoltTrust()
	neo4j.Spec.Trust.Certificates.Bolt.PrivateKey.SubPath = "tls.key"
	neo4j.Spec.Trust.Certificates.Bolt.PublicCertificate.SubPath = "tls.crt"
	keys := RequiredSecretKeys(neo4j)
	got := map[string]string{}
	for _, k := range keys {
		got[k.SecretName] = k.Key
	}
	if got["dev-bolt-key"] != "tls.key" || got["dev-bolt-cert"] != "tls.crt" {
		t.Fatalf("got %#v", got)
	}
}

func clusterWithTrust() *neo4jv1.Neo4j {
	return &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode:      neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{Members: 3},
			},
			Trust: &neo4jv1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1.TrustCertificatesSpec{
					Cluster: &neo4jv1.TLSPolicySpec{
						PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "cluster-key"},
						PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "cluster-cert"},
						TrustedCerts: &neo4jv1.TLSTrustedCertsSpec{
							Sources: []corev1.VolumeProjection{{
								Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{Name: "cluster-ca"},
									Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
								},
							}},
						},
					},
				},
			},
		},
	}
}

func standaloneWithBoltTrust() *neo4jv1.Neo4j {
	return &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
			Trust: &neo4jv1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1.TrustCertificatesSpec{
					Bolt: &neo4jv1.TLSPolicySpec{
						PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "dev-bolt-key", SubPath: "private.key"},
						PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "dev-bolt-cert", SubPath: "public.crt"},
						ClientAuth:        "None",
					},
				},
			},
		},
	}
}

func clusterWithTrustAndConnectors() *neo4jv1.Neo4j {
	neo4j := clusterWithTrust()
	httpsPort := int32(7473)
	neo4j.Spec.Connectivity = &neo4jv1.ConnectivitySpec{
		Listeners: &neo4jv1.ConnectivityListenersSpec{HTTPS: &httpsPort},
	}
	neo4j.Spec.Trust.Certificates.HTTPS = &neo4jv1.TLSPolicySpec{
		PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "https-key", SubPath: "private.key"},
		PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "https-cert", SubPath: "public.crt"},
		ClientAuth:        "None",
	}
	neo4j.Spec.Trust.Certificates.Bolt = &neo4jv1.TLSPolicySpec{
		PrivateKey:        &neo4jv1.TLSSecretKeyRef{SecretName: "bolt-key", SubPath: "private.key"},
		PublicCertificate: &neo4jv1.TLSSecretKeyRef{SecretName: "bolt-cert", SubPath: "public.crt"},
		ClientAuth:        "None",
	}
	return neo4j
}
