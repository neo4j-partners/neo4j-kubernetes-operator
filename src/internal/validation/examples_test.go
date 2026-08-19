package validation

import (
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// Decodes every shipped example strictly and runs the Go-side validation over it. A field name that
// does not exist on the API types fails here rather than being silently dropped by the apiserver.
// ponytail: does not evaluate the CEL rules — that needs an envtest apiserver, which the suites in
// tests/ cover.
func TestExamplesDecodeAndValidate(t *testing.T) {
	paths, err := filepath.Glob("../../../examples/*/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no example manifests found")
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal(raw, &kind); err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if kind.Kind != "Neo4j" {
			continue // supporting manifests: Secret, ResourceQuota, PersistentVolumeClaim
		}
		var cr neo4jv1beta1.Neo4j
		if err := yaml.UnmarshalStrict(raw, &cr); err != nil {
			t.Errorf("%s: decode: %v", p, err)
			continue
		}
		if err := ValidateNeo4j(&cr); err != nil {
			t.Errorf("%s: validate: %v", p, err)
		}
	}
}
