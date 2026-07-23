package workload

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOfflineMaintenanceMode(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Maintenance: &neo4jv1beta1.MaintenanceSpec{OfflineMode: true},
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
	sts := StandaloneStatefulSet(render.StandaloneContext(neo4j))
	c := sts.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 3 || c.Command[0] != "bash" || !strings.Contains(c.Command[2], "offline maintenance mode") {
		t.Fatalf("command = %#v", c.Command)
	}
	if c.StartupProbe != nil || c.LivenessProbe != nil {
		t.Fatal("startup/liveness must be cleared in offline mode")
	}
	if c.ReadinessProbe == nil {
		t.Fatal("readiness kept so pod stays NotReady / out of Service endpoints")
	}
	if sts.Spec.Template.Spec.TerminationGracePeriodSeconds == nil ||
		*sts.Spec.Template.Spec.TerminationGracePeriodSeconds != 0 {
		t.Fatalf("terminationGracePeriodSeconds = %v", sts.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}
}

func TestOfflineMaintenanceModeOff(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
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
	sts := StandaloneStatefulSet(render.StandaloneContext(neo4j))
	c := sts.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 0 {
		t.Fatalf("command should be empty for normal mode, got %#v", c.Command)
	}
	if c.StartupProbe == nil || c.LivenessProbe == nil || c.ReadinessProbe == nil {
		t.Fatal("expected default probes when offline mode is off")
	}
}
