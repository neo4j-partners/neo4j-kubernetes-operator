package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
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
	// Anything the image's own registry does not name: the entrypoint exits 1 on it before Neo4j
	// starts, so the operator has to refuse it first (NEO-013).
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{Plugins: []string{"graph-algorithms"}},
	}
	err := Validate(neo4j)
	if err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Fatalf("got %v", err)
	}
}

// The catalog has to name the same plugins as /startup/neo4j-plugins.json, because an id the
// image does not know is a crash and an id it knows but we omit is a plugin users cannot ask for.
func TestCatalogCoversTheImageRegistry(t *testing.T) {
	want := map[string]string{
		"apoc":             "apoc",
		"apoc-extended":    "apoc-extended",
		"gds":              "graph-data-science",
		"bloom":            "bloom",
		"genai":            "genai",
		"fleet-management": "fleet-management",
	}
	for id, imageName := range want {
		if !Known(id) {
			t.Errorf("catalog id %q is missing", id)
			continue
		}
		if got := ImageName(id); got != imageName {
			t.Errorf("ImageName(%q) = %q, want %q", id, got, imageName)
		}
	}
	if got := len(CatalogIDs()); got != len(want) {
		t.Errorf("catalog holds %d ids, want %d: %v", got, len(want), CatalogIDs())
	}
}

// Only apoc is bundled in both images, and only apoc-extended is bundled in neither. The
// difference is what decides whether an install needs egress, so it is worth pinning per id.
func TestPluginSources(t *testing.T) {
	cases := map[string]Source{
		"apoc":             BundledAnyEdition,
		"apoc-extended":    AlwaysDownloaded,
		"gds":              BundledEnterprise,
		"bloom":            BundledEnterprise,
		"genai":            BundledEnterprise,
		"fleet-management": BundledEnterprise,
	}
	for id, want := range cases {
		if got := SourceOf(id); got != want {
			t.Errorf("SourceOf(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestValidateRejectsEnterprisePluginOnCommunity(t *testing.T) {
	for _, id := range EnterpriseOnlyIDs() {
		neo4j := &neo4jv1.Neo4j{
			Spec: neo4jv1.Neo4jSpec{Edition: neo4jv1.EditionCommunity, Plugins: []string{id}},
		}
		err := Validate(neo4j)
		if err == nil {
			t.Errorf("%s on community must be refused: the entrypoint would fall through to a download that cannot succeed, without failing", id)
			continue
		}
		if !strings.Contains(err.Error(), "enterprise") {
			t.Errorf("%s: message must name the edition, got %v", id, err)
		}
	}
}

// apoc-extended is downloaded on both editions, so community is not a reason to refuse it.
func TestValidateAllowsDownloadedPluginOnCommunity(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{Edition: neo4jv1.EditionCommunity, Plugins: []string{"apoc", "apoc-extended"}},
	}
	if err := Validate(neo4j); err != nil {
		t.Fatalf("apoc and apoc-extended are legal on community: %v", err)
	}
}

// The CEL rule on Neo4jSpec has to spell the enterprise-only ids literally, since CEL cannot read
// the Go catalog. This is the only thing holding the two lists together: adding a bundled
// enterprise plugin here without touching the rule would leave it accepted at admission and
// refused later in the pipeline, which reports far worse than a rejected apply.
func TestCELRuleListsEveryEnterpriseOnlyID(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..",
		"config", "crd", "bases", "neo4j.com_neo4js.yaml"))
	if err != nil {
		t.Skipf("generated CRD not available: %v", err)
	}
	// controller-gen wraps long rule strings, so compare on collapsed whitespace.
	crd := strings.Join(strings.Fields(string(data)), " ")
	for _, id := range EnterpriseOnlyIDs() {
		if !strings.Contains(crd, "p != '"+id+"'") {
			t.Errorf("CEL rule does not exclude %q on community — add it to the rule in api/v1/neo4j_types.go", id)
		}
	}
	for _, id := range CatalogIDs() {
		if SourceOf(id) == BundledEnterprise {
			continue
		}
		if strings.Contains(crd, "p != '"+id+"'") {
			t.Errorf("CEL rule excludes %q on community, but it is not enterprise-only", id)
		}
	}
}

func TestValidateAllowsEnterprisePluginOnEnterprise(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{Edition: neo4jv1.EditionEnterprise, Plugins: []string{"gds", "bloom", "genai"}},
	}
	if err := Validate(neo4j); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

func TestValidateRejectsVersionPin(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{
			Plugins: []string{"apoc"},
			PluginDefinitions: map[string]neo4jv1.PluginDefinitionSpec{
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
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{
			Storage: &neo4jv1.StorageSpec{
				Volumes: &neo4jv1.VolumesSpec{
					Plugins: &neo4jv1.AuxiliaryVolumeSpec{Mode: neo4jv1.VolumeModeExisting},
				},
			},
		},
	}
	if !SkipNetworkFetch(neo4j) {
		t.Fatal("Existing plugins volume should skip NEO4J_PLUGINS fetch")
	}
	neo4j.Spec.Storage.Volumes.Plugins.Mode = neo4jv1.VolumeModeShare
	if SkipNetworkFetch(neo4j) {
		t.Fatal("Share still downloads on first start")
	}
}
