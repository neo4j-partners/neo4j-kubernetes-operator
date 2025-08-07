package resources_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildConfigMapForEnterprise_NoSingleRaftEnabled(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
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
		cluster           *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectedBootstrap string
		expectedJoining   string
	}{
		{
			name: "two_node_cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "two-node",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			expectedBootstrap: "TOTAL_SERVERS=2",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=1",
		},
		{
			name: "three_node_cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "three-node",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			expectedBootstrap: "TOTAL_SERVERS=3",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=1",
		},
		{
			name: "version_specific_discovery_5x",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-5x",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise", // Neo4j 5.x should use specific parameters
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			expectedBootstrap: "dbms.cluster.discovery.version=V2_ONLY",
			expectedJoining:   "dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery",
		},
		{
			name: "version_specific_discovery_2025",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-2025",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "2025.01-enterprise", // Neo4j 2025.x should use different parameters
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			expectedBootstrap: "dbms.kubernetes.discovery.service_port_name=tcp-discovery",
			expectedJoining:   "dbms.kubernetes.discovery.service_port_name=tcp-discovery",
		},
		{
			name: "critical_v2_only_discovery_fix",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v2-only-test",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise", // Must use V2_ONLY mode
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			expectedBootstrap: "dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery",
			expectedJoining:   "dbms.cluster.discovery.version=V2_ONLY",
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

			// For multi-server clusters, verify unified bootstrap approach
			if tt.cluster.Spec.Topology.Servers > 1 {
				// Verify unified bootstrap approach
				assert.Contains(t, startupScript, "Using unified bootstrap discovery approach",
					"multi-server clusters should use unified bootstrap approach")

				// Verify minimum servers logic - always 1 for cluster formation
				assert.Contains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=1",
					"should use fixed minimum of 1 for server bootstrap")

				// Verify Kubernetes discovery configuration
				expectedK8sDiscoveryConfig := []string{
					"dbms.cluster.discovery.resolver_type=K8S",
					fmt.Sprintf("dbms.kubernetes.label_selector=neo4j.com/cluster=%s", tt.cluster.Name),
				}

				// Add version-specific service port name
				// CRITICAL: Must use tcp-discovery for V2_ONLY mode (both 5.26.x and 2025.x)
				if tt.cluster.Spec.Image.Tag != "" && strings.HasPrefix(tt.cluster.Spec.Image.Tag, "2025") {
					expectedK8sDiscoveryConfig = append(expectedK8sDiscoveryConfig, "dbms.kubernetes.discovery.service_port_name=tcp-discovery")
				} else {
					expectedK8sDiscoveryConfig = append(expectedK8sDiscoveryConfig, "dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery")
				}

				for _, config := range expectedK8sDiscoveryConfig {
					assert.Contains(t, startupScript, config,
						"startup script should contain Kubernetes discovery configuration")
				}

				// Verify automatic server enabling is configured
				assert.Contains(t, startupScript, "initial.dbms.automatically_enable_free_servers=true",
					"multi-node clusters should enable automatic server joining")

				// Verify that old static endpoint configuration is NOT present
				assert.NotContains(t, startupScript, "dbms.cluster.discovery.v2.endpoints=",
					"startup script should not contain static endpoint configuration")
			}
		})
	}
}

// TestV2OnlyDiscoveryConfiguration tests the critical fix for Neo4j V2_ONLY discovery configuration
// This test ensures that the discovery configuration uses tcp-discovery port (5000) instead of tcp-tx port (6000)
// for V2_ONLY mode, which is essential for cluster formation in Neo4j 5.26+ and 2025.x
func TestV2OnlyDiscoveryConfiguration(t *testing.T) {
	tests := []struct {
		name                  string
		imageTag              string
		expectedPortName      string
		expectedV2OnlyPresent bool
		expectedParameterName string
	}{
		{
			name:                  "neo4j_5_26_uses_tcp_discovery",
			imageTag:              "5.26-enterprise",
			expectedPortName:      "tcp-discovery",
			expectedV2OnlyPresent: true,
			expectedParameterName: "dbms.kubernetes.discovery.v2.service_port_name",
		},
		{
			name:                  "neo4j_5_27_uses_tcp_discovery",
			imageTag:              "5.27-enterprise",
			expectedPortName:      "tcp-discovery",
			expectedV2OnlyPresent: true,
			expectedParameterName: "dbms.kubernetes.discovery.v2.service_port_name",
		},
		{
			name:                  "neo4j_2025_uses_tcp_discovery",
			imageTag:              "2025.02.0-enterprise",
			expectedPortName:      "tcp-discovery",
			expectedV2OnlyPresent: false, // V2_ONLY is default in 2025.x
			expectedParameterName: "dbms.kubernetes.discovery.service_port_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  tt.imageTag,
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
				},
			}

			configMap := resources.BuildConfigMapForEnterprise(cluster)
			startupScript := configMap.Data["startup.sh"]

			// Verify the correct service port name is used
			expectedConfig := fmt.Sprintf("%s=%s", tt.expectedParameterName, tt.expectedPortName)
			assert.Contains(t, startupScript, expectedConfig,
				"startup script should use tcp-discovery port for V2_ONLY mode")

			// Verify V2_ONLY setting is correct
			if tt.expectedV2OnlyPresent {
				assert.Contains(t, startupScript, "dbms.cluster.discovery.version=V2_ONLY",
					"5.26+ should explicitly set V2_ONLY mode")
			} else {
				assert.NotContains(t, startupScript, "dbms.cluster.discovery.version=V2_ONLY",
					"2025.x should not set V2_ONLY (it's default)")
			}

			// Verify tcp-tx port is NOT used (the bug we fixed)
			assert.NotContains(t, startupScript, "service_port_name=tcp-tx",
				"should not use tcp-tx port for V2_ONLY mode")
			assert.NotContains(t, startupScript, "service_port_name=discovery",
				"should not use legacy discovery port name")

			// Verify cluster port is correctly referenced in advertised address
			assert.Contains(t, startupScript, "server.cluster.advertised_address=${HOSTNAME_FQDN}:5000",
				"cluster communication should use port 5000")
		})
	}
}

func TestBuildConfigMapForEnterprise_HealthScript(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
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
