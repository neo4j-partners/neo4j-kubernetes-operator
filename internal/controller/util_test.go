package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestStandaloneAsCluster(t *testing.T) {
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	standalone.Name = "my-standalone"
	standalone.Namespace = "prod"
	standalone.Spec.Image = neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"}
	standalone.Spec.Auth = &neo4jv1beta1.AuthSpec{AdminSecret: "admin-secret"}
	standalone.Status.Phase = "Ready"

	cluster := standaloneAsCluster(standalone)

	require.NotNil(t, cluster)
	assert.Equal(t, "my-standalone", cluster.Name)
	assert.Equal(t, "prod", cluster.Namespace)
	assert.Equal(t, "neo4j", cluster.Spec.Image.Repo)
	assert.Equal(t, "5.26.0-enterprise", cluster.Spec.Image.Tag)
	assert.Equal(t, "admin-secret", cluster.Spec.Auth.AdminSecret)
	assert.Equal(t, "Ready", cluster.Status.Phase)
	assert.Equal(t, int32(1), cluster.Spec.Topology.Servers, "standalone should always have Servers=1")
}
