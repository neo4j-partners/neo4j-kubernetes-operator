package workload

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStandaloneStatefulSet(t *testing.T) {
	size := "10Gi"
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "graph-dev"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Version: "2026.05.0",
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode: neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: size},
					},
				},
			},
		},
	}
	sts := StandaloneStatefulSet(render.StandaloneContext(neo4j))
	if sts.Name != "dev-server" {
		t.Fatalf("sts name = %q", sts.Name)
	}
	if *sts.Spec.Replicas != 1 {
		t.Fatalf("replicas = %d", *sts.Spec.Replicas)
	}
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("expected one VCT")
	}
}
