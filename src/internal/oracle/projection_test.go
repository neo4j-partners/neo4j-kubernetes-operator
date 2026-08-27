package oracle

import (
	"os"
	"path/filepath"
	"testing"
)

// The two projections are committed, so `make test` is enough to catch a catalog change that was
// not regenerated — nobody has to remember to run the audit target first.
func TestProjectionsAreUpToDate(t *testing.T) {
	root := moduleRoot(t)

	page, err := os.ReadFile(filepath.Join(root, DocPath))
	if err != nil {
		t.Fatalf("read %s: %v", DocPath, err)
	}
	rendered, err := RenderMarkdown(string(page))
	if err != nil {
		t.Fatalf("render %s: %v", DocPath, err)
	}
	if rendered != string(page) {
		t.Errorf("%s is stale — run `make errors` and commit the result", DocPath)
	}

	shell, err := os.ReadFile(filepath.Join(root, ShellPath))
	if err != nil {
		t.Fatalf("read %s: %v", ShellPath, err)
	}
	if RenderShell() != string(shell) {
		t.Errorf("%s is stale — run `make errors` and commit the result", ShellPath)
	}
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
