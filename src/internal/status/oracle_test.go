package status

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

func TestErrorOracleDocumented(t *testing.T) {
	docPath := filepath.Join(moduleRoot(t), "docs", "user-guide", "05-reference", "errors.md")
	body, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read oracle doc: %v", err)
	}
	text := string(body)
	for _, e := range ErrorOracle {
		if !strings.Contains(text, e.Reason) {
			t.Errorf("user-guide errors.md missing reason %q (%s)", e.Reason, e.Condition)
		}
	}
}

// A mapped reason must exist in the oracle, otherwise e2e asserts on it while users find it
// documented nowhere.
func TestPipelineErrorReasonIsCatalogued(t *testing.T) {
	cases := map[error]string{
		fmt.Errorf("wrapped: %w", rendersecrets.ErrNotMountable):     ReasonSecretNotMountable,
		fmt.Errorf("wrapped: %w", rendersecrets.ErrAuthNotDelegated): ReasonSecretNotDelegated,
		errors.New("unrelated pipeline failure"):                     ReasonReconcileFailed,
	}
	for err, want := range cases {
		got := PipelineErrorReason(err)
		if got != want {
			t.Errorf("PipelineErrorReason(%v) = %q, want %q", err, got, want)
		}
		if !oracleHasReason(ConditionError, got) {
			t.Errorf("reason %q missing from ErrorOracle for condition %s", got, ConditionError)
		}
	}
}

func oracleHasReason(condition, reason string) bool {
	for _, e := range ErrorOracle {
		if e.Condition == condition && e.Reason == reason {
			return true
		}
	}
	return false
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
