package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildConfigMapForEnterprise_TLSConfiguration(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 5, // 3 + 2 total servers
			},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode: "cert-manager",
				IssuerRef: &neo4jv1beta1.IssuerRef{
					Name: "ca-cluster-issuer",
					Kind: "ClusterIssuer",
				},
			},
			Storage: neo4jv1beta1.StorageSpec{
				ClassName: "standard",
				Size:      "10Gi",
			},
		},
	}

	configMap := resources.BuildConfigMapForEnterprise(cluster)

	// Test that neo4j.conf contains TLS configuration
	neo4jConf := configMap.Data["neo4j.conf"]

	// Test HTTPS SSL policy
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.https.enabled=true", "should enable HTTPS SSL policy")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.https.base_directory=/ssl", "should set HTTPS base directory")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.https.private_key=tls.key", "should set HTTPS private key")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.https.public_certificate=tls.crt", "should set HTTPS certificate")

	// Test Bolt SSL policy
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.bolt.enabled=true", "should enable Bolt SSL policy")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.bolt.base_directory=/ssl", "should set Bolt base directory")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.bolt.private_key=tls.key", "should set Bolt private key")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.bolt.public_certificate=tls.crt", "should set Bolt certificate")

	// Test Cluster SSL policy — strictPeerValidation defaults to true, so
	// the operator emits trust_all=false + client_auth=REQUIRE +
	// verify_hostname=true to match Neo4j's canonical production posture.
	// The old "trust_all=true" assertion was a lock-in of debugging-only
	// config; see the strictPeerValidation=false test below for the
	// opt-out path.
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.enabled=true", "should enable cluster SSL policy")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=false", "default posture: peers validated against /ssl/trusted/ca.crt")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.client_auth=REQUIRE", "default posture: mutual TLS")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.verify_hostname=true", "default posture: explicit hostname verification")
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=true", "must not emit legacy trust_all=true by default")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.base_directory=/ssl", "should set cluster base directory")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.private_key=tls.key", "should set cluster private key")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.public_certificate=tls.crt", "should set cluster certificate")

	// Test connector configuration
	assert.Contains(t, neo4jConf, "server.https.enabled=true", "should enable HTTPS")
	assert.Contains(t, neo4jConf, "server.bolt.tls_level=REQUIRED", "should set Bolt TLS level to REQUIRED when TLS is enabled")

	// Test startup script for parallel pod management compatibility
	startupScript := configMap.Data["startup.sh"]
	assert.Contains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=${TOTAL_SERVERS}", "should use TOTAL_SERVERS as minimum to prevent split-brain")
	assert.Contains(t, startupScript, `BOOTSTRAP_STRATEGY="me"`, "should have me/other bootstrap strategy")
}

func TestBuildStatefulSetForEnterprise_TLSClusterFormation(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-parallel-test",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 5, // 3 + 2 total servers
			},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode: "cert-manager",
				IssuerRef: &neo4jv1beta1.IssuerRef{
					Name: "ca-cluster-issuer",
					Kind: "ClusterIssuer",
				},
			},
			Storage: neo4jv1beta1.StorageSpec{
				ClassName: "standard",
				Size:      "10Gi",
			},
		},
	}

	// Test that TLS clusters use parallel pod management
	serverStatefulSets := resources.BuildServerStatefulSetsForEnterprise(cluster)
	require.Len(t, serverStatefulSets, 5, "should create 5 StatefulSets for 5 servers")

	// Test the first StatefulSet as representative
	serverSts := serverStatefulSets[0]
	assert.Equal(t, serverSts.Spec.PodManagementPolicy, appsv1.ParallelPodManagement,
		"TLS clusters must use ParallelPodManagement for reliable formation")
}

// TestBuildConfigMapForEnterprise_TLSStrictPeerValidationOptOut covers the
// escape hatch for users whose external issuer doesn't populate ca.crt in
// the cert-manager Secret. With strictPeerValidation=false we revert to
// the legacy debugging-only posture (trust_all=true, client_auth=NONE).
func TestBuildConfigMapForEnterprise_TLSStrictPeerValidationOptOut(t *testing.T) {
	optOut := false
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-loose", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode:                 "cert-manager",
				IssuerRef:            &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
				StrictPeerValidation: &optOut,
			},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	neo4jConf := resources.BuildConfigMapForEnterprise(cluster).Data["neo4j.conf"]

	// Legacy posture: opt-out flips back to trust_all=true + client_auth=NONE.
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=true")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.client_auth=NONE")
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=false")
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.cluster.verify_hostname=true",
		"opt-out should not emit verify_hostname — leave Neo4j to its version default")
}

// TestBuildConfigMap_SSLPolicyKeysInSpecConfigAreDropped is the
// defence-in-depth test for the validator-side rejection of
// dbms.ssl.policy.* / server.bolt.tls_level / server.directories.
// certificates in spec.config. Even if a CR slips past the validator
// (e.g. a future custom admission controller bypasses our
// reconcile-time validation), the rendered neo4j.conf must NOT contain
// user values for these keys — because server.config.strict_validation.
// enabled=false elsewhere lets Neo4j silently honour a duplicate-key
// override and downgrade the strict cluster SSL posture.
func TestBuildConfigMap_SSLPolicyKeysInSpecConfigAreDropped(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "evil", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode:      "cert-manager",
				IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
			},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
			Config: map[string]string{
				// Hostile overrides that would silently downgrade the
				// strict default if the merge path didn't filter them.
				"dbms.ssl.policy.cluster.trust_all":       "true",
				"dbms.ssl.policy.cluster.client_auth":     "NONE",
				"dbms.ssl.policy.cluster.verify_hostname": "false",
				"dbms.ssl.policy.bolt.client_auth":        "REQUIRE",
				"server.bolt.tls_level":                   "OPTIONAL",
				"server.directories.certificates":         "/etc/neo4j/certs",
				// A legitimate key to verify the rest of spec.config still merges.
				"db.logs.query.enabled": "INFO",
			},
		},
	}

	neo4jConf := resources.BuildConfigMapForEnterprise(cluster).Data["neo4j.conf"]

	// The operator's strict defaults must be the values present in the
	// rendered config.
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=false")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.client_auth=REQUIRE")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.verify_hostname=true")
	assert.Contains(t, neo4jConf, "server.bolt.tls_level=REQUIRED")

	// The hostile spec.config values must NOT appear as standalone lines.
	// (Each is filtered out at merge time even though strict_validation is
	// disabled.)
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=true")
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.cluster.client_auth=NONE")
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.cluster.verify_hostname=false")
	assert.NotContains(t, neo4jConf, "dbms.ssl.policy.bolt.client_auth=REQUIRE")
	assert.NotContains(t, neo4jConf, "server.bolt.tls_level=OPTIONAL")
	assert.NotContains(t, neo4jConf, "server.directories.certificates=/etc/neo4j/certs")

	// Legitimate spec.config keys still pass through.
	assert.Contains(t, neo4jConf, "db.logs.query.enabled=INFO")
}

// TestBuildCertificate_UsagesAlwaysIncludeServerAndClientAuth is the
// defence-in-depth lock-in for the user-supplied-usages override. The
// validator already rejects a spec.tls.usages list missing either
// server-auth or client-auth, but if a CR slips past validation, the
// builder must still ensure both EKUs land on the issued cert —
// otherwise Neo4j's runtime would fail mutual TLS handshakes on
// cluster links under strict peer validation.
func TestBuildCertificate_UsagesAlwaysIncludeServerAndClientAuth(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-cust", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode:      "cert-manager",
				IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
				// User explicitly OMITS server-auth and client-auth.
				// (In practice this CR would be rejected by the validator,
				// but the builder is the second line of defence.)
				Usages: []string{"digital signature", "key encipherment"},
			},
		},
	}

	cert := resources.BuildCertificateForEnterprise(cluster)
	require.NotNil(t, cert)

	have := map[string]bool{}
	for _, u := range cert.Spec.Usages {
		have[string(u)] = true
	}
	assert.True(t, have["server auth"],
		"builder must inject server-auth EKU even when user override omits it; got %v", cert.Spec.Usages)
	assert.True(t, have["client auth"],
		"builder must inject client-auth EKU even when user override omits it; got %v", cert.Spec.Usages)
	// User's other usages must still be honoured.
	assert.True(t, have["digital signature"])
	assert.True(t, have["key encipherment"])

	// Idempotent: a user list that ALREADY includes both EKUs shouldn't
	// produce duplicates.
	cluster.Spec.TLS.Usages = []string{"server auth", "client auth", "digital signature"}
	cert = resources.BuildCertificateForEnterprise(cluster)
	seen := map[string]int{}
	for _, u := range cert.Spec.Usages {
		seen[string(u)]++
	}
	assert.Equal(t, 1, seen["server auth"], "no duplicate server auth: %v", cert.Spec.Usages)
	assert.Equal(t, 1, seen["client auth"], "no duplicate client auth: %v", cert.Spec.Usages)
}

// TestBuildStatefulSet_TLSVolume_OptOutFlatMount covers the opt-out path:
// when strictPeerValidation=false, the Secret must mount FLAT (no items[])
// so issuers that don't populate ca.crt still produce a working Pod. With
// items[] in play, a missing ca.crt key causes the kubelet to refuse the
// volume mount (KeyToPath has no per-item optional flag).
func TestBuildStatefulSet_TLSVolume_OptOutFlatMount(t *testing.T) {
	optOut := false
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-loose-vol", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode:                 "cert-manager",
				IssuerRef:            &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
				StrictPeerValidation: &optOut,
			},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	sts := resources.BuildServerStatefulSetsForEnterprise(cluster)[0]

	var certsVol *corev1.Volume
	for i := range sts.Spec.Template.Spec.Volumes {
		v := &sts.Spec.Template.Spec.Volumes[i]
		if v.Name == "certs" {
			certsVol = v
			break
		}
	}
	require.NotNil(t, certsVol, "certs volume must be present when TLS enabled")
	require.NotNil(t, certsVol.Secret, "certs volume must be backed by a Secret")
	require.Equal(t, "tls-loose-vol-tls-secret", certsVol.Secret.SecretName)

	// Critical: when opting out, Items MUST be empty so a missing ca.crt
	// key in the Secret doesn't cause the kubelet to refuse the mount.
	require.Empty(t, certsVol.Secret.Items,
		"strictPeerValidation=false must emit a flat Secret mount; Items list found: %+v",
		certsVol.Secret.Items)
}

// TestBuildStatefulSet_TLSVolumeProjectsCAToTrustedDir locks in the Secret
// items projection that places ca.crt at /ssl/trusted/ca.crt — the path
// Neo4j's cluster SSL policy reads when trust_all=false. Without the
// projection, strict peer validation would have no trust anchors and
// every peer connection would fail.
func TestBuildStatefulSet_TLSVolumeProjectsCAToTrustedDir(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-vol", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode:      "cert-manager",
				IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
			},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	sts := resources.BuildServerStatefulSetsForEnterprise(cluster)[0]

	var certsVol *corev1.Volume
	for i := range sts.Spec.Template.Spec.Volumes {
		v := &sts.Spec.Template.Spec.Volumes[i]
		if v.Name == "certs" {
			certsVol = v
			break
		}
	}
	require.NotNil(t, certsVol, "certs volume must be present when TLS enabled")
	require.NotNil(t, certsVol.Secret, "certs volume must be backed by a Secret")
	require.Equal(t, "tls-vol-tls-secret", certsVol.Secret.SecretName)

	// Items must include the trusted/ca.crt projection.
	paths := map[string]string{}
	for _, item := range certsVol.Secret.Items {
		paths[item.Key+"->"+item.Path] = item.Path
	}
	require.Contains(t, paths, "ca.crt->trusted/ca.crt",
		"Secret projection must put ca.crt at /ssl/trusted/ca.crt for cluster SSL trust anchors")
	require.Contains(t, paths, "tls.crt->tls.crt", "tls.crt projected at root")
	require.Contains(t, paths, "tls.key->tls.key", "tls.key projected at root")
}
