package plugins

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestNEO4JPluginsEnv(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{name: "empty", ids: nil, want: ""},
		{name: "apoc", ids: []string{"apoc"}, want: `["apoc"]`},
		{name: "gds maps to graph-data-science", ids: []string{"gds"}, want: `["graph-data-science"]`},
		{name: "multiple sorted", ids: []string{"gds", "apoc"}, want: `["apoc","graph-data-science"]`},
		{name: "dedupe", ids: []string{"apoc", "apoc"}, want: `["apoc"]`},
		{name: "unknown id omitted", ids: []string{"apoc", "not-a-plugin"}, want: `["apoc"]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NEO4JPluginsEnv(tc.ids); got != tc.want {
				t.Fatalf("NEO4JPluginsEnv() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssigned(t *testing.T) {
	if !Assigned([]string{"apoc", "gds"}, "apoc") {
		t.Fatal("expected apoc assigned")
	}
	if Assigned([]string{"apoc"}, "gds") {
		t.Fatal("expected gds not assigned")
	}
}

func TestValidateRejectsUnknownPlugin(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{Plugins: []string{"apoc-extended"}},
	}
	err := Validate(neo4j)
	if err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsVersionPin(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Plugins: []string{"apoc"},
			PluginDefinitions: map[string]neo4jv1beta1.PluginDefinitionSpec{
				"apoc": {Version: "5.26.0"},
			},
		},
	}
	err := Validate(neo4j)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("got %v", err)
	}
}

func TestSkipNetworkFetchExistingVolume(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Plugins: &neo4jv1beta1.AuxiliaryVolumeSpec{Mode: neo4jv1beta1.VolumeModeExisting},
				},
			},
		},
	}
	if !SkipNetworkFetch(neo4j) {
		t.Fatal("Existing plugins volume should skip NEO4J_PLUGINS fetch")
	}
	neo4j.Spec.Storage.Volumes.Plugins.Mode = neo4jv1beta1.VolumeModeShare
	if SkipNetworkFetch(neo4j) {
		t.Fatal("Share still downloads on first start")
	}
}
