package persistence

import (
	"context"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// Reconciler validates storage spec; PVCs come from STS volumeClaimTemplates or Existing bindings.
type Reconciler struct{}

func New() *Reconciler { return &Reconciler{} }

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	if err := renderstorage.Validate(neo4j); err != nil {
		return shared.Failed(err)
	}
	return shared.Done()
}
