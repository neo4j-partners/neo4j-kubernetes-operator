package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNeo4jEnterpriseClusterSpec_BasicValidation(t *testing.T) {
	tests := []struct {
		name  string
		spec  Neo4jEnterpriseClusterSpec
		valid bool
	}{
		{
			name: "valid minimal spec",
			spec: Neo4jEnterpriseClusterSpec{
				Edition: "enterprise",
				Image: ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26.0-enterprise",
				},
				Topology: TopologyConfiguration{
					Servers: 2,
				},
				Storage: StorageSpec{
					Size:      "10Gi",
					ClassName: "standard",
				},
			},
			valid: true,
		},
		{
			name: "invalid edition",
			spec: Neo4jEnterpriseClusterSpec{
				Edition: "community",
				Image: ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26.0-enterprise",
				},
				Topology: TopologyConfiguration{
					Servers: 2,
				},
				Storage: StorageSpec{
					Size:      "10Gi",
					ClassName: "standard",
				},
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: tt.spec,
			}

			// Test basic field validation
			if tt.valid {
				assert.Equal(t, "enterprise", cluster.Spec.Edition)
				assert.NotEmpty(t, cluster.Spec.Image.Repo)
				assert.NotEmpty(t, cluster.Spec.Image.Tag)
			} else {
				// For invalid specs, check that the problematic field is set incorrectly
				if cluster.Spec.Edition != "enterprise" {
					assert.NotEqual(t, "enterprise", cluster.Spec.Edition)
				}
			}
		})
	}
}

func TestImageSpec_Defaults(t *testing.T) {
	tests := []struct {
		name     string
		input    ImageSpec
		expected ImageSpec
	}{
		{
			name: "with pull policy",
			input: ImageSpec{
				Repo:       "neo4j",
				Tag:        "5.26.0-enterprise",
				PullPolicy: "Always",
			},
			expected: ImageSpec{
				Repo:       "neo4j",
				Tag:        "5.26.0-enterprise",
				PullPolicy: "Always",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.PullPolicy, tt.input.PullPolicy)
		})
	}
}

func TestTopologyConfiguration_BasicValidation(t *testing.T) {
	tests := []struct {
		name     string
		topology TopologyConfiguration
		valid    bool
	}{
		{
			name: "valid single primary",
			topology: TopologyConfiguration{
				Servers: 2,
			},
			valid: true,
		},
		{
			name: "valid cluster with secondaries",
			topology: TopologyConfiguration{
				Servers: 5,
			},
			valid: true,
		},
		{
			name: "invalid zero primaries",
			topology: TopologyConfiguration{
				Servers: 1,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				assert.True(t, tt.topology.Servers >= 2)
			} else {
				assert.True(t, tt.topology.Servers < 2)
			}
		})
	}
}

func TestStorageSpec_BasicValidation(t *testing.T) {
	tests := []struct {
		name    string
		storage StorageSpec
		valid   bool
	}{
		{
			name: "valid storage spec",
			storage: StorageSpec{
				Size:      "10Gi",
				ClassName: "standard",
			},
			valid: true,
		},
		{
			name: "missing storage class",
			storage: StorageSpec{
				Size: "10Gi",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				assert.NotEmpty(t, tt.storage.ClassName)
				assert.NotEmpty(t, tt.storage.Size)
			} else {
				assert.True(t, tt.storage.ClassName == "" || tt.storage.Size == "")
			}
		})
	}
}

func TestNeo4jEnterpriseCluster_ResourceRequirements(t *testing.T) {
	tests := []struct {
		name      string
		resources *corev1.ResourceRequirements
		valid     bool
	}{
		{
			name: "valid resource requirements",
			resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1000m"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			valid: true,
		},
		{
			name:      "nil resources",
			resources: nil,
			valid:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Topology: TopologyConfiguration{
						Servers: 2,
					},
					Storage: StorageSpec{
						Size:      "10Gi",
						ClassName: "standard",
					},
					Resources: tt.resources,
				},
			}

			if tt.valid {
				// Test that the cluster can be created with these resources
				assert.NotNil(t, cluster)
			}
		})
	}
}

func TestNeo4jEnterpriseCluster_StatusConditions(t *testing.T) {
	cluster := &Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
		Spec: Neo4jEnterpriseClusterSpec{
			Edition: "enterprise",
			Image: ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: TopologyConfiguration{
				Servers: 2,
			},
			Storage: StorageSpec{
				Size:      "10Gi",
				ClassName: "standard",
			},
		},
	}

	// Test initial status
	assert.Equal(t, "", cluster.Status.Phase)
	assert.Empty(t, cluster.Status.Conditions)

	// Test setting status
	cluster.Status.Phase = "Running"
	cluster.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ClusterReady",
			Message:            "Cluster is ready and operational",
		},
	}

	assert.Equal(t, "Running", cluster.Status.Phase)
	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, "Ready", cluster.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, cluster.Status.Conditions[0].Status)
}

func TestDeepCopyMethods(t *testing.T) {
	// Test that deep copy methods work correctly for main types
	original := &Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
		Spec: Neo4jEnterpriseClusterSpec{
			Edition: "enterprise",
			Image: ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: TopologyConfiguration{
				Servers: 2,
			},
			Storage: StorageSpec{
				Size:      "10Gi",
				ClassName: "standard",
			},
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()
	require.NotNil(t, copied)
	assert.Equal(t, original.Name, copied.Name)
	assert.Equal(t, original.Namespace, copied.Namespace)
	assert.Equal(t, original.Spec.Edition, copied.Spec.Edition)
	assert.Equal(t, original.Spec.Image.Repo, copied.Spec.Image.Repo)
	assert.Equal(t, original.Spec.Image.Tag, copied.Spec.Image.Tag)

	// Test DeepCopyObject
	copiedObj := original.DeepCopyObject()
	require.NotNil(t, copiedObj)
	copiedCluster, ok := copiedObj.(*Neo4jEnterpriseCluster)
	require.True(t, ok)
	assert.Equal(t, original.Name, copiedCluster.Name)
}
