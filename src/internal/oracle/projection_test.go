package oracle

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The three projections are committed, so `make test` is enough to catch a catalog change that
// was not regenerated — nobody has to remember to run the audit target first.
func TestProjectionsAreUpToDate(t *testing.T) {
	root := moduleRoot(t)

	for _, page := range []struct {
		rel    string
		render func(string) (string, error)
	}{
		{DocPath, RenderMarkdown},
		{StatusDocPath, RenderStatusMarkdown},
	} {
		body, err := os.ReadFile(filepath.Join(root, page.rel))
		if err != nil {
			t.Fatalf("read %s: %v", page.rel, err)
		}
		rendered, err := page.render(string(body))
		if err != nil {
			t.Fatalf("render %s: %v", page.rel, err)
		}
		if rendered != string(body) {
			t.Errorf("%s is stale — run `make errors` and commit the result", page.rel)
		}
	}

	shell, err := os.ReadFile(filepath.Join(root, ShellPath))
	if err != nil {
		t.Fatalf("read %s: %v", ShellPath, err)
	}
	if RenderShell() != string(shell) {
		t.Errorf("%s is stale — run `make errors` and commit the result", ShellPath)
	}
}

// The heading of the hand-written table that faces the generated one in the status contract.
const plannedHeading = "### Planned for a later version"

var plannedRow = regexp.MustCompile("(?m)^\\|\\s*`([A-Za-z]+)`\\s*\\|")

// The generated table cannot advertise a condition the operator does not write, but the table of
// planned ones next to it is written by hand and would happily keep listing a condition after
// somebody implemented it — the page would then promise it twice, once as a contract and once as
// an intention. Keeping the two sets disjoint means promoting a condition has to move its row.
func TestPlannedConditionsAreNotCatalogued(t *testing.T) {
	page, err := os.ReadFile(filepath.Join(moduleRoot(t), StatusDocPath))
	if err != nil {
		t.Fatalf("read %s: %v", StatusDocPath, err)
	}
	section := plannedConditionSection(string(page))
	if section == "" {
		t.Fatalf("%s: no %q section — if it was renamed, update plannedHeading so the check keeps "+
			"guarding the planned table", StatusDocPath, plannedHeading)
	}

	catalogued := make(map[string]bool, len(conditions))
	for _, c := range conditions {
		catalogued[c.String()] = true
	}
	for _, row := range plannedRow.FindAllStringSubmatch(section, -1) {
		if catalogued[row[1]] {
			t.Errorf("%s: %s is catalogued in internal/oracle, so the operator writes it — remove its "+
				"row from %q, where it now contradicts the generated table above",
				StatusDocPath, row[1], plannedHeading)
		}
	}
}

// plannedConditionSection returns the planned table, from its heading to the horizontal rule that
// closes the Conditions section.
func plannedConditionSection(page string) string {
	start := strings.Index(page, plannedHeading)
	if start < 0 {
		return ""
	}
	rest := page[start+len(plannedHeading):]
	if end := strings.Index(rest, "\n---"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
