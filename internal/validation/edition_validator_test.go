package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestNewEditionValidator(t *testing.T) {
	validator := NewEditionValidator()
	assert.NotNil(t, validator)
}

func TestEditionValidator_Validate(t *testing.T) {
	// Since Edition field has been removed (operator only supports enterprise),
	// validation always returns no errors
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				Size:      "10Gi",
				ClassName: "standard",
			},
		},
	}

	validator := NewEditionValidator()
	errors := validator.Validate(cluster)

	assert.Empty(t, errors, "Edition validation should always pass since operator only supports enterprise")
}
