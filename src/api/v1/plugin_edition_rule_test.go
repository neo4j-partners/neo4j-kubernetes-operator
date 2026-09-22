package v1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"k8s.io/apiserver/pkg/cel/library"
	"sigs.k8s.io/yaml"
)

// The enterprise-plugin rule is the one CRD rule with a nested has() chain over three optional
// levels (storage → volumes → plugins → mode), which is where CEL is easiest to get wrong: a
// has() on a missing parent is an error, not false, and an erroring rule reads as a rejection.
// TestCELRulesCompile only compiles, so it would not notice. This evaluates the real generated
// rule against the specs users will write.
//
// Matters because the volume channel is how a free GDS build runs on community: over-refusing
// here blocks a legitimate deployment at apply time, with no workaround in the CRD.
func TestEnterprisePluginRuleEvaluation(t *testing.T) {
	rule := findRuleByMessage(t, "ship only in the enterprise image")
	env, err := cel.NewEnv(cel.Variable("self", cel.DynType), library.Quantity())
	if err != nil {
		t.Fatal(err)
	}
	ast, issues := env.Compile(rule)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatal(err)
	}

	existingPluginsVolume := map[string]any{
		"volumes": map[string]any{
			"plugins": map[string]any{
				"mode":     "Existing",
				"existing": map[string]any{"claimName": "my-jars"},
			},
		},
	}

	cases := map[string]struct {
		self map[string]any
		want bool
	}{
		"enterprise with gds": {
			self: map[string]any{"edition": "enterprise", "plugins": []any{"gds"}},
			want: true,
		},
		"community with no plugins at all": {
			self: map[string]any{"edition": "community"},
			want: true,
		},
		"community with apoc only": {
			self: map[string]any{"edition": "community", "plugins": []any{"apoc"}},
			want: true,
		},
		"community with apoc-extended, which is downloaded on both editions": {
			self: map[string]any{"edition": "community", "plugins": []any{"apoc-extended"}},
			want: true,
		},
		"community with gds and no storage block": {
			self: map[string]any{"edition": "community", "plugins": []any{"gds"}},
			want: false,
		},
		// The has() chain has to survive each level being absent, not just the leaf.
		"community with gds and a storage block holding no volumes": {
			self: map[string]any{"edition": "community", "plugins": []any{"gds"}, "storage": map[string]any{}},
			want: false,
		},
		"community with gds and volumes holding no plugins volume": {
			self: map[string]any{"edition": "community", "plugins": []any{"gds"},
				"storage": map[string]any{"volumes": map[string]any{}}},
			want: false,
		},
		"community with gds and a Share plugins volume, which supplies no JAR": {
			self: map[string]any{"edition": "community", "plugins": []any{"gds"},
				"storage": map[string]any{"volumes": map[string]any{"plugins": map[string]any{"mode": "Share"}}}},
			want: false,
		},
		"community with gds supplied on an Existing plugins volume": {
			self: map[string]any{"edition": "community", "plugins": []any{"gds"}, "storage": existingPluginsVolume},
			want: true,
		},
		"community with every enterprise plugin on an Existing plugins volume": {
			self: map[string]any{"edition": "community",
				"plugins": []any{"bloom", "fleet-management", "genai", "gds"},
				"storage": existingPluginsVolume},
			want: true,
		},
		"community with bloom and no volume": {
			self: map[string]any{"edition": "community", "plugins": []any{"bloom"}},
			want: false,
		},
	}

	for name, tc := range cases {
		out, _, err := prg.Eval(map[string]any{"self": tc.self})
		if err != nil {
			t.Errorf("%s: rule errored instead of deciding (%v) — an erroring rule rejects the apply", name, err)
			continue
		}
		got, ok := out.Value().(bool)
		if !ok {
			t.Errorf("%s: rule returned %T, want bool", name, out.Value())
			continue
		}
		if got != tc.want {
			verb := map[bool]string{true: "accept", false: "refuse"}
			t.Errorf("%s: rule would %s, want %s", name, verb[got], verb[tc.want])
		}
	}
}

// findRuleByMessage returns the generated rule whose message contains fragment, so the test reads
// what the apiserver will enforce rather than a copy that can drift from the marker.
func findRuleByMessage(t *testing.T, fragment string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(crdBasesDir, "neo4j.com_neo4js.yaml"))
	if err != nil {
		t.Fatalf("read CRD (run make manifests): %v", err)
	}
	var crd map[string]any
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatal(err)
	}
	var found string
	walkValidations(crd, func(rule, message string) {
		if strings.Contains(message, fragment) {
			found = rule
		}
	})
	if found == "" {
		t.Fatalf("no generated rule carries a message containing %q (run make manifests)", fragment)
	}
	return found
}

// walkValidations visits every x-kubernetes-validations entry anywhere in the schema, since the
// rule's depth is an implementation detail of where the marker sits.
func walkValidations(node any, visit func(rule, message string)) {
	switch n := node.(type) {
	case map[string]any:
		if raw, ok := n["x-kubernetes-validations"]; ok {
			if list, ok := raw.([]any); ok {
				for _, item := range list {
					entry, ok := item.(map[string]any)
					if !ok {
						continue
					}
					rule, _ := entry["rule"].(string)
					message, _ := entry["message"].(string)
					if rule != "" {
						visit(rule, message)
					}
				}
			}
		}
		for _, v := range n {
			walkValidations(v, visit)
		}
	case []any:
		for _, v := range n {
			walkValidations(v, visit)
		}
	}
}
