// Package rbac holds cross-file consistency guards for the operator's RBAC. It has no runtime
// code — the manager Role rules are hand-maintained in two places (config/rbac/role.yaml for the
// kustomize/`make deploy` path and the Helm chart's managerRoleRules helper), and this test fails
// if they drift. That drift shipped a Helm operator that could not watch neo4jbackups/neo4jrestores
// (see the backup-restore round-trip work), which this guard would have caught.
package rbac

import (
	"os"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const (
	roleYAMLPath = "../../../config/rbac/role.yaml"
	helpersPath  = "../../../charts/neo4j-operator/templates/_helpers.tpl"
	// managerRoleRulesDefine opens the Helm named template whose body is pure `rules:` YAML.
	managerRoleRulesDefine = `{{- define "neo4j-operator.managerRoleRules" -}}`
)

// policyRule is the subset of an RBAC rule we compare.
type policyRule struct {
	APIGroups []string `json:"apiGroups"`
	Resources []string `json:"resources"`
	Verbs     []string `json:"verbs"`
}

type rulesDoc struct {
	Rules []policyRule `json:"rules"`
}

// TestManagerRoleRulesMatchHelmChart asserts config/rbac/role.yaml and the chart's
// managerRoleRules grant exactly the same (apiGroup, resource, verb) triples. Expanding to
// triples makes the comparison independent of how rules are grouped in each source.
func TestManagerRoleRulesMatchHelmChart(t *testing.T) {
	kustomize := tripleSet(t, readKustomizeRules(t))
	chart := tripleSet(t, readChartRules(t))

	if missing := diff(kustomize, chart); len(missing) > 0 {
		t.Errorf("permissions in config/rbac/role.yaml but MISSING from the Helm chart managerRoleRules\n"+
			"(a Helm-installed operator would be denied these):\n  %s", strings.Join(missing, "\n  "))
	}
	if extra := diff(chart, kustomize); len(extra) > 0 {
		t.Errorf("permissions in the Helm chart managerRoleRules but MISSING from config/rbac/role.yaml\n"+
			"(the kustomize/`make deploy` path would be denied these):\n  %s", strings.Join(extra, "\n  "))
	}
}

func readKustomizeRules(t *testing.T) []policyRule {
	t.Helper()
	raw, err := os.ReadFile(roleYAMLPath)
	if err != nil {
		t.Fatalf("read %s: %v", roleYAMLPath, err)
	}
	var doc rulesDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", roleYAMLPath, err)
	}
	if len(doc.Rules) == 0 {
		t.Fatalf("%s has no rules", roleYAMLPath)
	}
	return doc.Rules
}

// readChartRules extracts the managerRoleRules named-template body (pure `rules:` YAML with no
// template actions) and parses it. If someone adds a `{{ ... }}` action inside the body this will
// fail to parse — a signal to keep that helper templating-free.
func readChartRules(t *testing.T) []policyRule {
	t.Helper()
	raw, err := os.ReadFile(helpersPath)
	if err != nil {
		t.Fatalf("read %s: %v", helpersPath, err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == managerRoleRulesDefine {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s: could not find %s", helpersPath, managerRoleRulesDefine)
	}
	var body []string
	for _, l := range lines[start:] {
		if strings.TrimSpace(l) == "{{- end }}" {
			break
		}
		if strings.Contains(l, "{{") {
			t.Fatalf("managerRoleRules body contains a template action (%q); the drift guard "+
				"needs it to stay pure YAML", strings.TrimSpace(l))
		}
		body = append(body, l)
	}
	var doc rulesDoc
	if err := yaml.Unmarshal([]byte(strings.Join(body, "\n")), &doc); err != nil {
		t.Fatalf("parse managerRoleRules body: %v", err)
	}
	if len(doc.Rules) == 0 {
		t.Fatalf("managerRoleRules body has no rules")
	}
	return doc.Rules
}

// tripleSet expands every rule into its (apiGroup, resource, verb) combinations.
func tripleSet(t *testing.T, rules []policyRule) map[string]struct{} {
	t.Helper()
	out := map[string]struct{}{}
	for _, r := range rules {
		for _, g := range r.APIGroups {
			for _, res := range r.Resources {
				for _, v := range r.Verbs {
					out[g+"|"+res+"|"+v] = struct{}{}
				}
			}
		}
	}
	return out
}

// diff returns members of a not present in b, sorted.
func diff(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
