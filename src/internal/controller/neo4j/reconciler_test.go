package neo4j

import (
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/trust"
)

func TestStandalonePersistenceStep(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
	out := persistence.New(nil).Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("persistence step failed: %v", out.Err)
	}
}

func TestTrustNoopWhenDisabled(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{}
	out := trust.New(nil, nil).Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("trust step failed: %v", out.Err)
	}
	if out.Result != (shared.StepResult{}.Result) {
		t.Fatalf("unexpected requeue")
	}
}

func TestNormalizeMaxConcurrentReconciles(t *testing.T) {
	got, err := NormalizeMaxConcurrentReconciles(0)
	if err != nil || got != DefaultMaxConcurrentReconciles {
		t.Fatalf("default: got %d %v", got, err)
	}
	got, err = NormalizeMaxConcurrentReconciles(8)
	if err != nil || got != 8 {
		t.Fatalf("in range: got %d %v", got, err)
	}
	if _, err := NormalizeMaxConcurrentReconciles(MaxConcurrentReconcilesLimit + 1); err == nil {
		t.Fatal("expected reject above cap")
	}
}
