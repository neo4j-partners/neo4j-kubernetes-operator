package shared

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// StepResult is returned by each domain reconcile step.
type StepResult struct {
	Result ctrl.Result
	Err    error
}

// Done returns a successful step result.
func Done() StepResult {
	return StepResult{Result: ctrl.Result{}}
}

// Requeue returns a step result that requeues after the given duration.
func Requeue(after time.Duration) StepResult {
	return StepResult{Result: ctrl.Result{RequeueAfter: after}}
}

// Failed returns a step result with an error.
func Failed(err error) StepResult {
	return StepResult{Err: err}
}

// Reconciler is implemented by each domain package in the ADR-003 pipeline.
type Reconciler interface {
	Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) StepResult
}
