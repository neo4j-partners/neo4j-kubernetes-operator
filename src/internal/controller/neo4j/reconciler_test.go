package neo4j

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/trust"
)

func TestStandalonePersistenceStep(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{Mode: neo4jv1beta1.VolumeModeDynamic},
				},
			},
		},
	}
	out := persistence.New().Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("persistence step failed: %v", out.Err)
	}
}

func TestTrustNoopWhenDisabled(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{}
	out := trust.New().Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("trust step failed: %v", out.Err)
	}
	if out.Result != (shared.StepResult{}.Result) {
		t.Fatalf("unexpected requeue")
	}
}
