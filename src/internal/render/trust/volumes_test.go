package trust

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
)

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
		case "/var/lib/neo4j/certificates/cluster/private.key":
			foundKey = true
			if m.SubPath != "private.key" {
				t.Fatalf("key subPath = %q", m.SubPath)
			}
		case "/var/lib/neo4j/certificates/cluster/public.crt":
			foundCert = true
		case "/var/lib/neo4j/certificates/cluster/trusted/ca.crt":
			foundTrusted = true
		}
	}
	if !foundKey || !foundCert || !foundTrusted {
		t.Fatalf("missing mounts key=%v cert=%v trusted=%v mounts=%v", foundKey, foundCert, foundTrusted, container.VolumeMounts)
	}
	for _, m := range container.VolumeMounts {
		if m.MountPath == "/var/lib/neo4j/certificates/cluster/trusted/ca.crt" && m.SubPath == "ca.crt" {
			return
		}
	}
	t.Fatal("expected trusted ca.crt subPath mount")
}

func TestAppendVolumesBoltAndHTTPS(t *testing.T) {
	neo4j := clusterWithTrustAndConnectors()
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	container := &corev1.Container{}
	podSpec := &corev1.PodSpec{}
	AppendVolumes(ctx, container, podSpec)

	want := map[string]string{
		"/var/lib/neo4j/certificates/https/public.crt": "public.crt",
		"/var/lib/neo4j/certificates/bolt/private.key":  "private.key",
		"/var/lib/neo4j/certificates/bolt/public.crt":   "public.crt",
	}
	for path, sub := range want {
		found := false
		for _, m := range container.VolumeMounts {
			if m.MountPath == path && m.SubPath == sub {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing mount %s subPath=%s in %#v", path, sub, container.VolumeMounts)
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

func TestValidateClusterBYOShape(t *testing.T) {
	if err := ValidateClusterBYOShape(clusterWithTrust()); err != nil {
		t.Fatal(err)
	}
	bad := clusterWithTrust()
	bad.Spec.Trust.Certificates.Cluster.PrivateKey = nil
	if err := ValidateClusterBYOShape(bad); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestValidateHTTPSBYOShapeRequiresBolt(t *testing.T) {
	httpsPort := int32(7473)
	neo4j := clusterWithTrust()
	neo4j.Spec.Connectivity = &neo4jv1beta1.ConnectivitySpec{
		Listeners: &neo4jv1beta1.ConnectivityListenersSpec{HTTPS: &httpsPort},
	}
	neo4j.Spec.Trust.Certificates.HTTPS = &neo4jv1beta1.TLSPolicySpec{
		PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-key"},
		PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-cert"},
	}
	if err := ValidateHTTPSBYOShape(neo4j); err == nil {
		t.Fatal("expected missing bolt certs error")
	}
	neo4j.Spec.Trust.Certificates.Bolt = &neo4jv1beta1.TLSPolicySpec{
		PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-key"},
		PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-cert"},
	}
	if err := ValidateHTTPSBYOShape(neo4j); err != nil {
		t.Fatal(err)
	}
}

func clusterWithTrust() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
			},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Cluster: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "cluster-key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "cluster-cert"},
						TrustedCerts: &neo4jv1beta1.TLSTrustedCertsSpec{
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

func clusterWithTrustAndConnectors() *neo4jv1beta1.Neo4j {
	neo4j := clusterWithTrust()
	httpsPort := int32(7473)
	neo4j.Spec.Connectivity = &neo4jv1beta1.ConnectivitySpec{
		Listeners: &neo4jv1beta1.ConnectivityListenersSpec{HTTPS: &httpsPort},
	}
	neo4j.Spec.Trust.Certificates.HTTPS = &neo4jv1beta1.TLSPolicySpec{
		PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-key", SubPath: "private.key"},
		PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-cert", SubPath: "public.crt"},
		ClientAuth:        "None",
	}
	neo4j.Spec.Trust.Certificates.Bolt = &neo4jv1beta1.TLSPolicySpec{
		PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-key", SubPath: "private.key"},
		PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-cert", SubPath: "public.crt"},
		ClientAuth:        "None",
	}
	return neo4j
}
