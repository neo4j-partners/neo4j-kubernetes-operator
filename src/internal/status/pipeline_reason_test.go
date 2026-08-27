package status

import (
	"errors"
	"fmt"
	"testing"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

// A mapped reason must be catalogued under the Error condition, otherwise e2e asserts on it while
// users find it documented nowhere.
func TestPipelineErrorReasonIsCatalogued(t *testing.T) {
	cases := map[error]oracle.Reason{
		fmt.Errorf("wrapped: %w", rendersecrets.ErrNotMountable):      oracle.ReasonSecretNotMountable,
		fmt.Errorf("wrapped: %w", rendersecrets.ErrAuthNotDelegated):  oracle.ReasonSecretNotDelegated,
		fmt.Errorf("wrapped: %w", rendersecrets.ErrAuthValueRejected): oracle.ReasonAuthSecretInvalid,
		errors.New("unrelated pipeline failure"):                      oracle.ReasonReconcileFailed,
	}
	for err, want := range cases {
		got := PipelineErrorReason(err)
		if got != want {
			t.Errorf("PipelineErrorReason(%v) = %q, want %q", err, got, want)
		}
		if _, ok := oracle.Lookup(oracle.ConditionError, got); !ok {
			t.Errorf("reason %q is not catalogued under %s", got, oracle.ConditionError)
		}
	}
}
