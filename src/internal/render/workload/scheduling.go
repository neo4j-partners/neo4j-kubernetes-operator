package workload

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	// defaultTerminationGracePeriodSeconds matches Helm podSpec.terminationGracePeriodSeconds.
	defaultTerminationGracePeriodSeconds int64 = 3600
)

// applyScheduling copies spec.scheduling onto the pod template (NEO-2-008).
// Always sets terminationGracePeriodSeconds (3600 default); offline mode overrides to 0 later.
func applyScheduling(ctx render.Context, podSpec *corev1.PodSpec) {
	grace := defaultTerminationGracePeriodSeconds
	s := ctx.Neo4j.Spec.Scheduling
	if s != nil && s.TerminationGracePeriodSeconds != nil {
		grace = *s.TerminationGracePeriodSeconds
	}
	podSpec.TerminationGracePeriodSeconds = &grace

	if s == nil {
		return
	}
	if len(s.NodeSelector) > 0 {
		podSpec.NodeSelector = s.NodeSelector
	}
	if len(s.Tolerations) > 0 {
		podSpec.Tolerations = s.Tolerations
	}
	if len(s.TopologySpreadConstraints) > 0 {
		podSpec.TopologySpreadConstraints = withDefaultSpreadSelectors(ctx, s.TopologySpreadConstraints)
	}
	if s.PriorityClassName != "" {
		podSpec.PriorityClassName = s.PriorityClassName
	}
	if aff := resolveAffinity(ctx, s.Affinity); aff != nil {
		podSpec.Affinity = aff
	}
}

// withDefaultSpreadSelectors copies constraints and fills a null labelSelector with
// this pool's SelectorLabels (otherwise kube warns and matches no pods).
func withDefaultSpreadSelectors(ctx render.Context, in []corev1.TopologySpreadConstraint) []corev1.TopologySpreadConstraint {
	out := make([]corev1.TopologySpreadConstraint, len(in))
	for i := range in {
		out[i] = *in[i].DeepCopy()
		if out[i].LabelSelector == nil {
			out[i].LabelSelector = &metav1.LabelSelector{MatchLabels: ctx.SelectorLabels()}
		}
	}
	return out
}

func resolveAffinity(ctx render.Context, aff *neo4jv1beta1.SchedulingAffinitySpec) *corev1.Affinity {
	if aff == nil {
		return nil
	}
	switch aff.PodAntiAffinity {
	case "hard":
		return antiAffinity(ctx, true)
	case "soft":
		return antiAffinity(ctx, false)
	case "custom":
		return aff.Custom
	default:
		return aff.Custom
	}
}

// antiAffinity spreads Neo4j instance pods across nodes (Helm podAntiAffinity parity).
func antiAffinity(ctx render.Context, hard bool) *corev1.Affinity {
	term := corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{MatchLabels: ctx.SelectorLabels()},
		TopologyKey:   "kubernetes.io/hostname",
	}
	if hard {
		return &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{term},
			},
		}
	}
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
				Weight:          100,
				PodAffinityTerm: term,
			}},
		},
	}
}
