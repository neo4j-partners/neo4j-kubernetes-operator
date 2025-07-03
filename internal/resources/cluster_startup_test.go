package resources_test

import (
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
			name: "single_node_cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "single-node",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "Starting Neo4j Enterprise in single-node mode",
			expectedJoining:   "", // Should not have joining configuration for single node
		},
		{
			name: "three_node_cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "three-node",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
				},
			},
			expectedBootstrap: "dbms.cluster.minimum_initial_system_primaries_count=1",
			expectedJoining:   "dbms.cluster.minimum_initial_system_primaries_count=1",
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

			// For multi-node clusters, verify that both bootstrap and joining pods have the same minimum count setting
			if tt.cluster.Spec.Topology.Primaries > 1 {
				// Count occurrences of minimum_initial_system_primaries_count=1
				countOnes := strings.Count(startupScript, "dbms.cluster.minimum_initial_system_primaries_count=1")
				assert.Equal(t, 2, countOnes,
					"both bootstrap and joining configurations should set minimum_initial_system_primaries_count=1")

				// Ensure we don't have any settings with the total primary count (which would be wrong)
				wrongSetting := "dbms.cluster.minimum_initial_system_primaries_count=3"
				assert.NotContains(t, startupScript, wrongSetting,
					"startup script should not contain old cluster formation logic")

				// Verify all discovery endpoints are included
				expectedEndpoints := []string{
					"three-node-primary-0.three-node-headless.default.svc.cluster.local:6000",
					"three-node-primary-1.three-node-headless.default.svc.cluster.local:6000",
					"three-node-primary-2.three-node-headless.default.svc.cluster.local:6000",
				}

				for _, endpoint := range expectedEndpoints {
					assert.Contains(t, startupScript, endpoint,
						"startup script should contain all discovery endpoints")
				}
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

	// Verify health script checks both HTTP and Bolt ports
	assert.Contains(t, healthScript, "7474", "health script should check HTTP port")
	assert.Contains(t, healthScript, "7687", "health script should check Bolt port")
	assert.Contains(t, healthScript, "Neo4j is healthy", "health script should have success message")
}
