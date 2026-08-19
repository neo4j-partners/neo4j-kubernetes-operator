package v1beta1

import (
	"os"
	"testing"

	"github.com/google/cel-go/cel"
	"sigs.k8s.io/yaml"
)

// crdPath is the generated manifest, so this test covers the rules the apiserver will
// actually compile rather than the markers in Go source.
const crdPath = "../../../config/crd/bases/neo4j.com_neo4js.yaml"

// TestCELRulesCompile parses every x-kubernetes-validations rule in the CRD and compiles it.
// The apiserver rejects the whole CRD when any rule fails to compile, so an invalid rule is
// not a soft failure: it blocks install. Catching it here keeps that out of `kubectl apply`.
//
// ponytail: `self` is declared as Dyn rather than as the real schema type, so this catches
// syntax and macro misuse (for example has() on a comprehension variable, which is what
// broke TLS-002c and TLS-002d) but not type mismatches. Upgrade path: build a typed
// environment from the schema the way apiextensions does.
func TestCELRulesCompile(t *testing.T) {
	raw, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD (run make manifests): %v", err)
	}
	var crd map[string]interface{}
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatal(err)
	}

	env, err := cel.NewEnv(cel.Variable("self", cel.DynType), cel.Variable("oldSelf", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}

	rules := collectRules(crd, "")
	if len(rules) == 0 {
		t.Fatal("no x-kubernetes-validations rules found; CRD generation changed shape")
	}
	for _, r := range rules {
		if _, issues := env.Compile(r.rule); issues != nil && issues.Err() != nil {
			t.Errorf("%s: rule does not compile: %v\n  rule: %s", r.path, issues.Err(), r.rule)
		}
	}
	t.Logf("compiled %d CEL rules", len(rules))
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
