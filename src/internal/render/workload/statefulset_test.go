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
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
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
	env := sts.Spec.Template.Spec.Containers[0].Env
	if len(env) < 2 {
		t.Fatalf("expected license and auth env vars, got %d", len(env))
	}
	if env[0].Name != "NEO4J_ACCEPT_LICENSE_AGREEMENT" || env[0].Value != "yes" {
		t.Fatalf("license env = %#v", env[0])
	}
}
