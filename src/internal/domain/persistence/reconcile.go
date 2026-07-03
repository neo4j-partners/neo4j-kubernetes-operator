package persistence

import (
	"context"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
)

// Reconciler validates storage spec; Dynamic PVCs are provisioned via STS volumeClaimTemplates.
type Reconciler struct{}

func New() *Reconciler { return &Reconciler{} }

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	if neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil {
		return shared.Failed(errMissingStorage)
	}
	if neo4j.Spec.Storage.Volumes.Data.Mode != neo4jv1beta1.VolumeModeDynamic {
		return shared.Failed(errUnsupportedVolumeMode)
	}
	return shared.Done()
}
