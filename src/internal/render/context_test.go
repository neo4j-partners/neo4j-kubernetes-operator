package render

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStandaloneContextNaming(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
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
	if ctx.ImageRef() != "neo4j:2026.05.0-enterprise" {
		t.Fatalf("ImageRef = %q, want neo4j:2026.05.0-enterprise", ctx.ImageRef())
	}
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		name     string
		edition  neo4jv1beta1.Edition
		version  string
		repo     string
		wantRef  string
	}{
		{
			name:    "enterprise appends suffix",
			edition: neo4jv1beta1.EditionEnterprise,
			version: "2026.05.0",
			wantRef: "neo4j:2026.05.0-enterprise",
		},
		{
			name:    "enterprise does not double suffix",
			edition: neo4jv1beta1.EditionEnterprise,
			version: "2026.05.0-enterprise",
			wantRef: "neo4j:2026.05.0-enterprise",
		},
		{
			name:    "custom repository",
			edition: neo4jv1beta1.EditionEnterprise,
			version: "2026.05.0",
			repo:    "registry.example.com/neo4j",
			wantRef: "registry.example.com/neo4j:2026.05.0-enterprise",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := neo4jv1beta1.Neo4jSpec{
				Edition: tt.edition,
				Version: tt.version,
			}
			if tt.repo != "" {
				spec.Image = &neo4jv1beta1.ImageSpec{Repository: tt.repo}
			}
			ctx := StandaloneContext(&neo4jv1beta1.Neo4j{
				ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
				Spec:       spec,
			})
			if got := ctx.ImageRef(); got != tt.wantRef {
				t.Fatalf("ImageRef() = %q, want %q", got, tt.wantRef)
			}
		})
	}
}

func TestImageTag(t *testing.T) {
	if got := imageTag("2026.05.0", neo4jv1beta1.EditionEnterprise); got != "2026.05.0-enterprise" {
		t.Fatalf("imageTag enterprise = %q", got)
	}
	if got := imageTag("2026.05.0-enterprise", neo4jv1beta1.EditionEnterprise); got != "2026.05.0-enterprise" {
		t.Fatalf("imageTag no double suffix = %q", got)
	}
}
