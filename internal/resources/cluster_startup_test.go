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

func TestBuildConfigMapForEnterprise_ClusterFormation(t *testing.T) {
	tests := []struct {
		name              string
		cluster           *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectedBootstrap string
		expectedJoining   string
	}{
		{
			name: "single_primary_cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "single-primary",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise", // Test version-specific config
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "Starting Neo4j Enterprise in cluster mode",
			expectedJoining:   "", // Single-primary uses RAFT-enabled single mode
		},
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
						Primaries:   2,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "MIN_PRIMARIES=2",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=${MIN_PRIMARIES}",
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
						Primaries:   3,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "MIN_PRIMARIES=$((TOTAL_PRIMARIES / 2 + 1))",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=${MIN_PRIMARIES}",
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
						Primaries:   2,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "dbms.cluster.discovery.version=V2_ONLY",
			expectedJoining:   "dbms.kubernetes.discovery.v2.service_port_name=discovery",
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
						Primaries:   2,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "dbms.kubernetes.discovery.service_port_name=discovery",
			expectedJoining:   "dbms.kubernetes.discovery.service_port_name=discovery",
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

			// For single-node clusters, verify single RAFT mode
			if tt.cluster.Spec.Topology.Primaries == 1 && tt.cluster.Spec.Topology.Secondaries == 0 {
				assert.Contains(t, startupScript, "internal.dbms.single_raft_enabled=true",
					"single-node clusters should use single RAFT mode")
			}

			// For multi-node clusters, verify unified bootstrap approach
			if tt.cluster.Spec.Topology.Primaries > 1 {
				// Verify unified bootstrap approach
				assert.Contains(t, startupScript, "Using unified bootstrap discovery approach",
					"multi-node clusters should use unified bootstrap approach")

				// Verify minimum primaries logic based on cluster size
				if tt.cluster.Spec.Topology.Primaries == 2 {
					assert.Contains(t, startupScript, "MIN_PRIMARIES=2",
						"2-node clusters should require both nodes for bootstrap")
				} else if tt.cluster.Spec.Topology.Primaries >= 3 {
					expectedQuorum := fmt.Sprintf("MIN_PRIMARIES=$((TOTAL_PRIMARIES / 2 + 1))")
					assert.Contains(t, startupScript, expectedQuorum,
						"3+ node clusters should use quorum logic")
				}

				// Verify Kubernetes discovery configuration
				expectedK8sDiscoveryConfig := []string{
					"dbms.cluster.discovery.resolver_type=K8S",
					fmt.Sprintf("dbms.kubernetes.label_selector=neo4j.com/cluster=%s", tt.cluster.Name),
				}

				// Add version-specific service port name
				if tt.cluster.Spec.Image.Tag != "" && strings.HasPrefix(tt.cluster.Spec.Image.Tag, "2025") {
					expectedK8sDiscoveryConfig = append(expectedK8sDiscoveryConfig, "dbms.kubernetes.discovery.service_port_name=discovery")
				} else {
					expectedK8sDiscoveryConfig = append(expectedK8sDiscoveryConfig, "dbms.kubernetes.discovery.v2.service_port_name=discovery")
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

func TestBuildConfigMapForEnterprise_HealthScript(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Primaries: 3,
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
