package resources_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildConfigMapForEnterprise_NoSingleRaftEnabled(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 2,
			},
		},
	}

	configMap := resources.BuildConfigMapForEnterprise(cluster)
	assert.NotNil(t, configMap)

	// Check that single RAFT is NOT enabled
	neo4jConf := configMap.Data["neo4j.conf"]
	assert.NotContains(t, neo4jConf, "internal.dbms.single_raft_enabled=true",
		"neo4j.conf should NOT contain internal.dbms.single_raft_enabled")
}

func TestBuildConfigMapForEnterprise_ClusterFormation(t *testing.T) {
	tests := []struct {
		name              string
		cluster           *neo4jv1beta1.Neo4jEnterpriseCluster
		expectedBootstrap string
		expectedJoining   string
	}{
		{
			name: "two_node_cluster",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "two-node",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			expectedBootstrap: "TOTAL_SERVERS=2",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=${TOTAL_SERVERS}",
		},
		{
			name: "three_node_cluster",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "three-node",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			expectedBootstrap: "TOTAL_SERVERS=3",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=${TOTAL_SERVERS}",
		},
		{
			name: "list_discovery_5x",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-5x",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			expectedBootstrap: "dbms.cluster.discovery.resolver_type=LIST",
			expectedJoining:   "test-5x-server-0.test-5x-headless.default.svc.cluster.local:6000",
		},
		{
			name: "list_discovery_2025",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-2025",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  "2025.01-enterprise",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			// 2025.x: still needs resolver_type=LIST, but uses dbms.cluster.endpoints (renamed)
			expectedBootstrap: "dbms.cluster.discovery.resolver_type=LIST",
			expectedJoining:   "test-2025-server-0.test-2025-headless.default.svc.cluster.local:6000",
		},
		{
			name: "list_discovery_no_k8s_clusterip",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v2-only-test",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			// LIST discovery is used; K8S ClusterIP discovery is NOT used
			expectedBootstrap: "dbms.cluster.discovery.resolver_type=LIST",
			expectedJoining:   "dbms.cluster.discovery.v2.endpoints=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configMap := resources.BuildConfigMapForEnterprise(tt.cluster)

			// Check that ConfigMap is created
			assert.NotNil(t, configMap)
			assert.Equal(t, tt.cluster.Name+"-config", configMap.Name)
			assert.Equal(t, tt.cluster.Namespace, configMap.Namespace)

			// Check startup script content
			startupScript, exists := configMap.Data["startup.sh"]
			assert.True(t, exists, "startup.sh should exist in ConfigMap")
			assert.NotEmpty(t, startupScript, "startup.sh should not be empty")

			if tt.expectedBootstrap != "" {
				assert.Contains(t, startupScript, tt.expectedBootstrap,
					"startup script should contain expected bootstrap configuration")
			}

			if tt.expectedJoining != "" {
				assert.Contains(t, startupScript, tt.expectedJoining,
					"startup script should contain expected joining configuration")
			}

			// Verify Neo4j configuration exists
			_, configExists := configMap.Data["neo4j.conf"]
			assert.True(t, configExists, "neo4j.conf should exist in ConfigMap")

			// For multi-server clusters, verify version-specific discovery configuration
			if tt.cluster.Spec.Topology.Servers > 1 {
				tag := tt.cluster.Spec.Image.Tag
				isCalver := len(tag) >= 4 && tag[:4] == "2025"

				// All versions: pod FQDNs must appear in the endpoints list
				for i := int32(0); i < tt.cluster.Spec.Topology.Servers; i++ {
					expectedFQDN := fmt.Sprintf("%s-server-%d.%s-headless.%s.svc.cluster.local:6000",
						tt.cluster.Name, i, tt.cluster.Name, tt.cluster.Namespace)
					assert.Contains(t, startupScript, expectedFQDN,
						"startup script should contain FQDN for pod %d", i)
				}

				// All versions: auto-enable free servers so all nodes join automatically
				assert.Contains(t, startupScript, "initial.dbms.automatically_enable_free_servers=true",
					"multi-node clusters should enable automatic server joining")

				// All versions: K8S ClusterIP discovery must NOT be used (causes split-brain)
				assert.NotContains(t, startupScript, "dbms.cluster.discovery.resolver_type=K8S",
					"startup script must NOT use K8S ClusterIP discovery (causes split-brain)")

				// All versions: resolver_type=LIST is required per Neo4j docs for both 5.x and 2025.x
				// https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/
				assert.Contains(t, startupScript, "dbms.cluster.discovery.resolver_type=LIST",
					"startup script should use LIST resolver type (required in both 5.x and 2025.x)")

				if isCalver {
					// CalVer 2025.x+: uses dbms.cluster.endpoints (renamed from v2.endpoints).
					// No V2_ONLY flag needed (V2 is the only protocol in 2025.x).
					assert.Contains(t, startupScript, "dbms.cluster.endpoints=",
						"2025.x startup script should set dbms.cluster.endpoints for LIST discovery")
					assert.NotContains(t, startupScript, "dbms.cluster.discovery.version=V2_ONLY",
						"2025.x startup script should NOT set V2_ONLY (it is the only/default protocol)")
					assert.NotContains(t, startupScript, "dbms.cluster.discovery.v2.endpoints=",
						"2025.x startup script should NOT use the renamed 5.x endpoint setting")
				} else {
					// SemVer 5.26.x: must explicitly enable V2_ONLY and use v2.endpoints
					assert.Contains(t, startupScript, "dbms.cluster.discovery.version=V2_ONLY",
						"5.x startup script must explicitly set V2_ONLY discovery mode")
					assert.Contains(t, startupScript, "dbms.cluster.discovery.v2.endpoints=",
						"5.x startup script should use dbms.cluster.discovery.v2.endpoints")

					// 5.x: ME/OTHER bootstrap strategy hint prevents race during simultaneous start
					assert.Contains(t, startupScript, `BOOTSTRAP_STRATEGY="me"`,
						"startup script should set 'me' for server-0")
					assert.Contains(t, startupScript, `BOOTSTRAP_STRATEGY="other"`,
						"startup script should set 'other' for non-zero servers")
					assert.Contains(t, startupScript, `internal.dbms.cluster.discovery.system_bootstrapping_strategy=${BOOTSTRAP_STRATEGY}`,
						"5.x startup script should use internal bootstrapping strategy hint")
				}

				// All versions: minimum_initial_system_primaries_count prevents premature solo bootstrap
				assert.Contains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=${TOTAL_SERVERS}",
					"should use TOTAL_SERVERS shell variable to prevent premature solo bootstrapping")
				assert.NotContains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=1",
					"must NOT hardcode 1 as minimum - that causes split-brain")
			}
		})
	}
}

// TestListDiscoveryConfiguration tests that LIST discovery is used with static pod FQDNs.
// LIST discovery provides one address per pod (via the headless service DNS) so that
// minimum_initial_system_primaries_count=N can be satisfied without relying on a ClusterIP VIP
// that resolves to a single address, which would cause split-brain.
func TestListDiscoveryConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		imageTag string
		servers  int32
	}{
		{
			name:     "neo4j_5_26_list_discovery",
			imageTag: "5.26-enterprise",
			servers:  2,
		},
		{
			name:     "neo4j_2025_01_0_list_discovery",
			imageTag: "2025.01.0-enterprise",
			servers:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  tt.imageTag,
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: tt.servers,
					},
				},
			}

			configMap := resources.BuildConfigMapForEnterprise(cluster)
			startupScript := configMap.Data["startup.sh"]

			isCalver := len(tt.imageTag) >= 4 && tt.imageTag[:4] == "2025"

			// All versions: each pod FQDN must appear in the endpoints list
			for i := int32(0); i < tt.servers; i++ {
				expectedFQDN := fmt.Sprintf("test-cluster-server-%d.test-cluster-headless.default.svc.cluster.local:6000", i)
				assert.Contains(t, startupScript, expectedFQDN,
					"startup script should contain FQDN for pod %d", i)
			}

			// All versions: K8S ClusterIP discovery must NOT be used (split-brain risk)
			assert.NotContains(t, startupScript, "dbms.cluster.discovery.resolver_type=K8S",
				"startup script must NOT use K8S ClusterIP discovery")

			// All versions: cluster communication port 6000 (tcp-tx) for advertised address
			assert.Contains(t, startupScript, "server.cluster.advertised_address=${HOSTNAME_FQDN}:6000",
				"cluster catchup communication should use port 6000")

			// All versions: resolver_type=LIST required (docs show it for both 5.x and 2025.x)
			// https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/
			assert.Contains(t, startupScript, "dbms.cluster.discovery.resolver_type=LIST",
				"startup script should use LIST resolver type (required for both 5.x and 2025.x)")

			if isCalver {
				// CalVer 2025.x+: dbms.cluster.endpoints (renamed from v2.endpoints), no V2_ONLY flag
				assert.Contains(t, startupScript, "dbms.cluster.endpoints=",
					"2025.x should use dbms.cluster.endpoints for LIST discovery")
				assert.NotContains(t, startupScript, "dbms.cluster.discovery.version=V2_ONLY",
					"2025.x should NOT set V2_ONLY (V2 is the only protocol)")
				assert.NotContains(t, startupScript, "dbms.cluster.discovery.v2.endpoints=",
					"2025.x should NOT use the old v2.endpoints setting name")
				// CalVer doesn't honour system_bootstrapping_strategy, so the
				// BOOTSTRAP_STRATEGY shell assignment must not be emitted —
				// otherwise it's dead work in every pod startup.
				assert.NotContains(t, startupScript, "BOOTSTRAP_STRATEGY=\"me\"",
					"CalVer startup script must NOT emit the SemVer-only BOOTSTRAP_STRATEGY assignment")
				assert.NotContains(t, startupScript, "BOOTSTRAP_STRATEGY=\"other\"",
					"CalVer startup script must NOT emit the SemVer-only BOOTSTRAP_STRATEGY assignment")
				assert.NotContains(t, startupScript, "internal.dbms.cluster.discovery.system_bootstrapping_strategy=",
					"CalVer does not honour internal.dbms.cluster.discovery.system_bootstrapping_strategy as a config directive")
			} else {
				// SemVer 5.26.x: explicit V2_ONLY and v2.endpoints required
				assert.Contains(t, startupScript, "dbms.cluster.discovery.version=V2_ONLY",
					"5.x startup script must explicitly set V2_ONLY discovery mode")
				assert.Contains(t, startupScript, "dbms.cluster.discovery.v2.endpoints=",
					"5.x startup script should use dbms.cluster.discovery.v2.endpoints")
				// SemVer must still emit the BOOTSTRAP_STRATEGY assignment so
				// the ${BOOTSTRAP_STRATEGY} substitution inside the discovery
				// config resolves to "me" / "other".
				assert.Contains(t, startupScript, "BOOTSTRAP_STRATEGY=\"me\"",
					"SemVer startup script must emit BOOTSTRAP_STRATEGY=me for server-0")
				assert.Contains(t, startupScript, "BOOTSTRAP_STRATEGY=\"other\"",
					"SemVer startup script must emit BOOTSTRAP_STRATEGY=other for non-zero indices")
				assert.Contains(t, startupScript, "system_bootstrapping_strategy=${BOOTSTRAP_STRATEGY}",
					"SemVer startup script must reference ${BOOTSTRAP_STRATEGY} in the discovery config")
			}
		})
	}
}

func TestBuildConfigMapForEnterprise_HealthScript(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}

	configMap := resources.BuildConfigMapForEnterprise(cluster)

	// Check health script content
	healthScript, exists := configMap.Data["health.sh"]
	assert.True(t, exists, "health.sh should exist in ConfigMap")
	assert.NotEmpty(t, healthScript, "health.sh should not be empty")

	// Verify health script checks HTTP port and has appropriate health messaging
	assert.Contains(t, healthScript, "7474", "health script should check HTTP port")
	assert.Contains(t, healthScript, "healthy", "health script should have success message")

	// Verify health script handles cluster formation process
	assert.Contains(t, healthScript, "cluster formation process",
		"health script should handle cluster formation waiting period")
	assert.Contains(t, healthScript, "cluster formation barrier",
		"health script should recognize cluster formation barrier logs")
}
