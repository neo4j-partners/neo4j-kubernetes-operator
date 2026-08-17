package imagepolicy

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestValidateDefaultAllowsOfficialNeo4j(t *testing.T) {
	SetAllowedRepositories("")
	neo4j := &neo4jv1beta1.Neo4j{}
	if err := Validate(neo4j); err != nil {
		t.Fatal(err)
	}
	neo4j.Spec.Image = &neo4jv1beta1.ImageSpec{Repository: "docker.io/neo4j"}
	if err := Validate(neo4j); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsForeignRegistry(t *testing.T) {
	SetAllowedRepositories("")
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Image: &neo4jv1beta1.ImageSpec{Repository: "attacker.example.com/neo4j-but-worse"},
		},
	}
	err := Validate(neo4j)
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateAllowsConfiguredMirror(t *testing.T) {
	SetAllowedRepositories("neo4j,myacr.azurecr.io/neo4j")
	defer SetAllowedRepositories("")
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Image: &neo4jv1beta1.ImageSpec{Repository: "myacr.azurecr.io/neo4j"},
		},
	}
	if err := Validate(neo4j); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsNeo4jPrefixSpoof(t *testing.T) {
	SetAllowedRepositories("neo4j")
	defer SetAllowedRepositories("")
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Image: &neo4jv1beta1.ImageSpec{Repository: "neo4j.attacker.com/evil"},
		},
	}
	if err := Validate(neo4j); err == nil {
		t.Fatal("expected reject for neo4j prefix spoof")
	}
}

func TestValidateDigest(t *testing.T) {
	SetAllowedRepositories("")
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Image: &neo4jv1beta1.ImageSpec{
				Repository: "neo4j",
				Digest:     "sha256:not-hex",
			},
		},
	}
	if err := Validate(neo4j); err == nil {
		t.Fatal("expected bad digest reject")
	}
	neo4j.Spec.Image.Digest = "sha256:" + strings.Repeat("a", 64)
	if err := Validate(neo4j); err != nil {
		t.Fatal(err)
	}
}

func TestAllowAllStar(t *testing.T) {
	SetAllowedRepositories("*")
	defer SetAllowedRepositories("")
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Image: &neo4jv1beta1.ImageSpec{Repository: "anywhere.io/x"},
		},
	}
	if err := Validate(neo4j); err != nil {
		t.Fatal(err)
	}
}
