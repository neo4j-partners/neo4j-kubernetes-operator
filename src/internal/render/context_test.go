package render

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStandaloneContextNaming(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "graph-dev"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Version: "2026.05.0",
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	ctx := StandaloneContext(neo4j)

	if ctx.STSName() != "dev-server" {
		t.Fatalf("STSName = %q, want dev-server", ctx.STSName())
	}
	if ctx.ClientServiceName() != "dev" {
		t.Fatalf("ClientServiceName = %q, want dev", ctx.ClientServiceName())
	}
	if ctx.AuthSecretName() != "dev-auth" {
		t.Fatalf("AuthSecretName = %q, want dev-auth", ctx.AuthSecretName())
	}
	if ctx.ImageRef() != "neo4j:2026.05.0" {
		t.Fatalf("ImageRef = %q", ctx.ImageRef())
	}
}
