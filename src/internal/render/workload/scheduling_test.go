package workload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestApplyScheduling(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Scheduling: &neo4jv1beta1.SchedulingSpec{
				NodeSelector:      map[string]string{"disk": "ssd"},
				PriorityClassName: "high-prio",
				Tolerations: []corev1.Toleration{{
					Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "neo4j", Effect: corev1.TaintEffectNoSchedule,
				}},
				Affinity: &neo4jv1beta1.SchedulingAffinitySpec{PodAntiAffinity: "hard"},
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": "dev"}},
				}},
			},
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
	spec := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec
	if spec.NodeSelector["disk"] != "ssd" {
		t.Fatalf("nodeSelector = %#v", spec.NodeSelector)
	}
	if spec.PriorityClassName != "high-prio" {
		t.Fatalf("priorityClassName = %q", spec.PriorityClassName)
	}
	if len(spec.Tolerations) != 1 || spec.Tolerations[0].Key != "dedicated" {
		t.Fatalf("tolerations = %#v", spec.Tolerations)
	}
	if len(spec.TopologySpreadConstraints) != 1 {
		t.Fatalf("topologySpread = %#v", spec.TopologySpreadConstraints)
	}
	if spec.TopologySpreadConstraints[0].LabelSelector == nil ||
		spec.TopologySpreadConstraints[0].LabelSelector.MatchLabels[render.LabelInstance] != "dev" {
		t.Fatalf("spread labelSelector should default to pool selectors, got %#v",
			spec.TopologySpreadConstraints[0].LabelSelector)
	}
	req := spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(req) != 1 || req[0].TopologyKey != "kubernetes.io/hostname" {
		t.Fatalf("hard anti-affinity = %#v", spec.Affinity)
	}
	if req[0].LabelSelector.MatchLabels[render.LabelInstance] != "dev" {
		t.Fatalf("anti-affinity labels = %#v", req[0].LabelSelector.MatchLabels)
	}
	if spec.TerminationGracePeriodSeconds == nil || *spec.TerminationGracePeriodSeconds != defaultTerminationGracePeriodSeconds {
		t.Fatalf("terminationGracePeriodSeconds = %v, want %d", spec.TerminationGracePeriodSeconds, defaultTerminationGracePeriodSeconds)
	}
}

func TestApplySchedulingSoftAntiAffinity(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Scheduling: &neo4jv1beta1.SchedulingSpec{
				Affinity: &neo4jv1beta1.SchedulingAffinitySpec{PodAntiAffinity: "soft"},
			},
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
	pref := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
	if len(pref) != 1 || pref[0].Weight != 100 {
		t.Fatalf("soft anti-affinity = %#v", pref)
	}
}

func TestTopologySpreadDefaultLabelSelector(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Scheduling: &neo4jv1beta1.SchedulingSpec{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
					MaxSkew:           1,
					TopologyKey:       "kubernetes.io/hostname",
					WhenUnsatisfiable: corev1.ScheduleAnyway,
					// LabelSelector omitted — operator fills pool SelectorLabels.
				}},
			},
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
	tsc := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.TopologySpreadConstraints
	if len(tsc) != 1 || tsc[0].LabelSelector == nil {
		t.Fatalf("expected default labelSelector, got %#v", tsc)
	}
	if tsc[0].LabelSelector.MatchLabels[render.LabelInstance] != "dev" ||
		tsc[0].LabelSelector.MatchLabels[render.LabelPool] == "" {
		t.Fatalf("labelSelector = %#v", tsc[0].LabelSelector.MatchLabels)
	}
}

func TestTerminationGracePeriodOverride(t *testing.T) {
	grace := int64(120)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Scheduling: &neo4jv1beta1.SchedulingSpec{
				TerminationGracePeriodSeconds: &grace,
			},
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
	got := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.TerminationGracePeriodSeconds
	if got == nil || *got != 120 {
		t.Fatalf("terminationGracePeriodSeconds = %v", got)
	}
}

func TestTerminationGracePeriodDefaultWithoutScheduling(t *testing.T) {
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
	got := StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.TerminationGracePeriodSeconds
	if got == nil || *got != defaultTerminationGracePeriodSeconds {
		t.Fatalf("terminationGracePeriodSeconds = %v", got)
	}
}
