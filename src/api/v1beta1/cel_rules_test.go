package v1beta1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/cel-go/cel"
	"sigs.k8s.io/yaml"
)

// crdBasesDir holds every generated manifest, so this test covers the rules the apiserver
// will actually compile rather than the markers in Go source.
const crdBasesDir = "../../../config/crd/bases"

// TestCELRulesCompile parses every x-kubernetes-validations rule in every generated CRD and
// compiles it. The apiserver rejects the whole CRD when any rule fails to compile, so an
// invalid rule is not a soft failure: it blocks install. Catching it here keeps that out of
// `kubectl apply`.
//
// ponytail: `self` is declared as Dyn rather than as the real schema type, so this catches
// syntax and macro misuse (for example has() on a comprehension variable, which is what
// broke TLS-002c and TLS-002d) but not type mismatches. Upgrade path: build a typed
// environment from the schema the way apiextensions does.
func TestCELRulesCompile(t *testing.T) {
	crds, err := filepath.Glob(filepath.Join(crdBasesDir, "neo4j.com_*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(crds) == 0 {
		t.Fatalf("no CRDs found in %s (run make manifests)", crdBasesDir)
	}

	env, err := cel.NewEnv(cel.Variable("self", cel.DynType), cel.Variable("oldSelf", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}

	total := 0
	for _, crdPath := range crds {
		raw, err := os.ReadFile(crdPath)
		if err != nil {
			t.Fatalf("read CRD %s (run make manifests): %v", crdPath, err)
		}
		var crd map[string]interface{}
		if err := yaml.Unmarshal(raw, &crd); err != nil {
			t.Fatalf("%s: %v", crdPath, err)
		}
		name := filepath.Base(crdPath)
		rules := collectRules(crd, name)
		for _, r := range rules {
			if _, issues := env.Compile(r.rule); issues != nil && issues.Err() != nil {
				t.Errorf("%s: rule does not compile: %v\n  rule: %s", r.path, issues.Err(), r.rule)
			}
		}
		total += len(rules)
	}
	if total == 0 {
		t.Fatal("no x-kubernetes-validations rules found; CRD generation changed shape")
	}
	t.Logf("compiled %d CEL rules across %d CRDs", total, len(crds))
}

type celRule struct {
	path string
	rule string
}

// collectRules walks the CRD looking for x-kubernetes-validations at any depth, since rules
// can be attached to nested schemas (TLSPolicySpec and TrustSpec both carry one).
func collectRules(node interface{}, path string) []celRule {
	var out []celRule
	switch n := node.(type) {
	case map[string]interface{}:
		if vs, ok := n["x-kubernetes-validations"].([]interface{}); ok {
			for i, v := range vs {
				m, ok := v.(map[string]interface{})
				if !ok {
					continue
				}
				if rule, ok := m["rule"].(string); ok {
					out = append(out, celRule{path: path + ".x-kubernetes-validations[" + itoa(i) + "]", rule: rule})
				}
			}
		}
		for k, v := range n {
			if k == "x-kubernetes-validations" {
				continue
			}
			out = append(out, collectRules(v, path+"."+k)...)
		}
	case []interface{}:
		for i, v := range n {
			out = append(out, collectRules(v, path+"["+itoa(i)+"]")...)
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
