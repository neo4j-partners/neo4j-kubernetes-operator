package workload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestApplyProbesDefaults(t *testing.T) {
	neo4j := standaloneBase()
	c := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.Containers[0]
	if c.StartupProbe == nil || c.StartupProbe.FailureThreshold != 1000 {
		t.Fatalf("startup default = %#v", c.StartupProbe)
	}
	if c.ReadinessProbe == nil || c.ReadinessProbe.FailureThreshold != 20 {
		t.Fatalf("readiness default = %#v", c.ReadinessProbe)
	}
	if c.LivenessProbe == nil || c.LivenessProbe.FailureThreshold != 40 {
		t.Fatalf("liveness default = %#v", c.LivenessProbe)
	}
	if c.StartupProbe.TCPSocket == nil || c.StartupProbe.TCPSocket.Port.IntVal != 7687 {
		t.Fatalf("startup tcp = %#v", c.StartupProbe.TCPSocket)
	}
}

func TestApplyProbesOverrides(t *testing.T) {
	neo4j := standaloneBase()
	neo4j.Spec.Probes = &neo4jv1beta1.ProbesSpec{
		Startup: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(7474)},
			},
			FailureThreshold: 50,
			PeriodSeconds:    10,
		},
		Readiness: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(7687)},
			},
			FailureThreshold: 5,
			PeriodSeconds:    3,
		},
	}
	c := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.Containers[0]
	if c.StartupProbe.HTTPGet == nil || c.StartupProbe.FailureThreshold != 50 {
		t.Fatalf("startup override = %#v", c.StartupProbe)
	}
	if c.ReadinessProbe.FailureThreshold != 5 || c.ReadinessProbe.PeriodSeconds != 3 {
		t.Fatalf("readiness override = %#v", c.ReadinessProbe)
	}
	// Liveness unset → default remains.
	if c.LivenessProbe.FailureThreshold != 40 {
		t.Fatalf("liveness should stay default, got %#v", c.LivenessProbe)
	}
}

func standaloneBase() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
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
}
