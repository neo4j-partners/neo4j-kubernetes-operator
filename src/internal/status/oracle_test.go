package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestErrorOracleDocumented(t *testing.T) {
	docPath := filepath.Join(moduleRoot(t), "docs", "03-user-documentation", "reference", "error-overview.md")
	body, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read oracle doc: %v", err)
	}
	text := string(body)
	for _, e := range ErrorOracle {
		if !strings.Contains(text, e.Reason) {
			t.Errorf("error-overview.md missing reason %q (%s)", e.Reason, e.Condition)
		}
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
