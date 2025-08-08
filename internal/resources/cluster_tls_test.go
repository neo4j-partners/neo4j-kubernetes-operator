package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildConfigMapForEnterprise_TLSConfiguration(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 5, // 3 + 2 total servers
			},
			TLS: &neo4jv1alpha1.TLSSpec{
				Mode: "cert-manager",
				IssuerRef: &neo4jv1alpha1.IssuerRef{
					Name: "ca-cluster-issuer",
					Kind: "ClusterIssuer",
				},
			},
			Storage: neo4jv1alpha1.StorageSpec{
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

	// Test Cluster SSL policy - CRITICAL for TLS cluster formation
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.enabled=true", "should enable cluster SSL policy")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.trust_all=true", "CRITICAL: should set trust_all=true for cluster SSL")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.base_directory=/ssl", "should set cluster base directory")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.private_key=tls.key", "should set cluster private key")
	assert.Contains(t, neo4jConf, "dbms.ssl.policy.cluster.public_certificate=tls.crt", "should set cluster certificate")

	// Test connector configuration
	assert.Contains(t, neo4jConf, "server.https.enabled=true", "should enable HTTPS")
	assert.Contains(t, neo4jConf, "server.bolt.tls_level=OPTIONAL", "should set Bolt TLS level to OPTIONAL")

	// Test startup script for parallel pod management compatibility
	startupScript := configMap.Data["startup.sh"]
	assert.Contains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=1", "should use fixed minimum of 1 for server bootstrap")
	assert.Contains(t, startupScript, "This works reliably even with TLS enabled", "should have TLS cluster formation comment")
}

func TestBuildStatefulSetForEnterprise_TLSClusterFormation(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-parallel-test",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 5, // 3 + 2 total servers
			},
			TLS: &neo4jv1alpha1.TLSSpec{
				Mode: "cert-manager",
				IssuerRef: &neo4jv1alpha1.IssuerRef{
					Name: "ca-cluster-issuer",
					Kind: "ClusterIssuer",
				},
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "10Gi",
			},
		},
	}

	// Test that TLS clusters use parallel pod management
	serverSts := resources.BuildServerStatefulSetForEnterprise(cluster)
	assert.Equal(t, serverSts.Spec.PodManagementPolicy, appsv1.ParallelPodManagement,
		"TLS clusters must use ParallelPodManagement for reliable formation")
}
