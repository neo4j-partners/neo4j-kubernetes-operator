package trust

import (
	"context"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
)

// Reconciler is a no-op when trust is disabled (Slice 1 Standalone default).
type Reconciler struct{}

func New() *Reconciler { return &Reconciler{} }

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	if neo4j.Spec.Trust != nil && neo4j.Spec.Trust.Enabled {
		return shared.Failed(errTrustNotImplemented)
	}
	return shared.Done()
}
