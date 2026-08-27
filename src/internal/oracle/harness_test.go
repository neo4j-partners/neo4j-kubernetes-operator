package oracle

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The e2e harness is shell, so nothing stops it from waiting for a reason string the operator no
// longer emits — and the cost of that is a suite that times out in CI and blames the operator. The
// harness therefore states its expectations one way only: an uppercase variable whose name ends in
// _REASON, holding the reason as its default, passed to an oracle_ lookup. These tests hold that
// convention up, so a reason renamed in catalog.go fails `make test` with a file and a line.
var (
	// EXPECT_REASON=Foo, EXPECT_TLS_REASON="${TLS_READY_REASON:-Foo}", export EXPECT_REASON=Foo.
	reasonAssignment = regexp.MustCompile(`^\s*(?:export\s+)?([A-Z][A-Z0-9_]*_REASON)=(\S+)`)
	// The default of a ${OVERRIDE:-Default} expansion, which is where the literal usually sits.
	expansionDefault = regexp.MustCompile(`:-([A-Za-z][A-Za-z0-9]*)\}`)
	bareIdentifier   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	// A comparison against a literal, to catch a reason pinned outside the convention.
	literalComparison = regexp.MustCompile(`(==|!=)\s*"([A-Za-z][A-Za-z0-9]*)"`)
)

func TestHarnessPinsCataloguedReasons(t *testing.T) {
	catalogued := reasonNames()

	forEachHarnessScript(t, func(path string, lines []string) {
		rel := harnessRelPath(t, path)
		body := strings.Join(lines, "\n")

		for i, line := range lines {
			match := reasonAssignment.FindStringSubmatch(stripComment(line))
			if match == nil {
				continue
			}
			name, literal := match[1], reasonLiteral(match[2])
			if literal == "" {
				// Value comes from elsewhere with no default — whatever it holds is checked at
				// run time by the oracle_ lookup required below.
				continue
			}
			if !catalogued[literal] {
				t.Errorf("%s:%d: %s pins reason %q, which internal/oracle does not catalogue — "+
					"fix the expectation or declare the reason", rel, i+1, name, literal)
			}
			if strings.HasPrefix(rel, "tests/actions/") && !passedToOracleLookup(body, name) {
				t.Errorf("%s:%d: %s is never passed to oracle_require or oracle_has — add "+
					"`oracle_require <condition> \"${%s}\"` (use `event` for an Event-only reason) so a "+
					"rename fails immediately instead of after the wait", rel, i+1, name, name)
			}
		}
	})
}

// A literal compared against a reason read from the CR is the shape this convention replaces: it
// looks harmless and survives every gate until the suite runs.
func TestHarnessComparesNoReasonLiteral(t *testing.T) {
	catalogued := reasonNames()

	forEachHarnessScript(t, func(path string, lines []string) {
		rel := harnessRelPath(t, path)

		for i, line := range lines {
			code := stripComment(line)
			for _, match := range literalComparison.FindAllStringSubmatchIndex(code, -1) {
				literal := code[match[4]:match[5]]
				if !catalogued[literal] {
					continue
				}
				// Only a comparison whose left side reads a reason: `phase` and a pod's
				// waiting.reason share names with catalogued reasons without being ones.
				if !strings.Contains(strings.ToLower(code[:match[2]]), "reason") {
					continue
				}
				t.Errorf("%s:%d: reason %q is compared as a literal — hold it in an uppercase "+
					"*_REASON variable and check it with oracle_require, so a rename in "+
					"catalog.go is caught here", rel, i+1, literal)
			}
		}
	})
}

func reasonNames() map[string]bool {
	out := map[string]bool{}
	for _, e := range Entries() {
		out[e.Reason.String()] = true
	}
	return out
}

// reasonLiteral returns the reason a shell assignment pins, or "" when the value carries none.
func reasonLiteral(value string) string {
	if match := expansionDefault.FindStringSubmatch(value); match != nil {
		return match[1]
	}
	unquoted := strings.Trim(value, `"'`)
	if bareIdentifier.MatchString(unquoted) {
		return unquoted
	}
	return ""
}

func passedToOracleLookup(body, name string) bool {
	ref := regexp.QuoteMeta("${" + name)
	return regexp.MustCompile(`oracle_(?:require|has)\b[^\n]*` + ref).MatchString(body)
}

func stripComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return ""
	}
	if idx := strings.Index(line, " #"); idx >= 0 {
		return line[:idx]
	}
	return line
}

// forEachHarnessScript visits the hand-written shell of tests/, generated lookups excluded.
func forEachHarnessScript(t *testing.T, visit func(path string, lines []string)) {
	t.Helper()
	root := filepath.Join(moduleRoot(t), "tests")
	generated := filepath.Join(root, "lib", "oracle.sh")
	visited := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sh") || path == generated {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		visit(path, strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n"))
		visited++
		return nil
	})
	if err != nil {
		t.Fatalf("walk tests: %v", err)
	}
	if visited == 0 {
		t.Fatal("no harness script found under tests/ — has the layout moved?")
	}
}

func harnessRelPath(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(moduleRoot(t), path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
