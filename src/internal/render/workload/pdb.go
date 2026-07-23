package workload

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// PDBEnabled is true when spec.podDisruptionBudget.enabled.
func PDBEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.PodDisruptionBudget != nil && neo4j.Spec.PodDisruptionBudget.Enabled
}

// PDBName is the owned PodDisruptionBudget name.
func PDBName(ctx render.Context) string {
	return ctx.Neo4j.Name + "-pdb"
}

// PodDisruptionBudget builds a policy/v1 PDB selecting all pods for this Neo4j
// instance (all pools). Opt-in via enabled; minAvailable defaults to 2 for
// Cluster (≥3 members) else 1 (NEO-2-008).
func PodDisruptionBudget(ctx render.Context) *policyv1.PodDisruptionBudget {
	minAvail := defaultPDBMinAvailable(ctx.Neo4j)
	if ctx.Neo4j.Spec.PodDisruptionBudget != nil && ctx.Neo4j.Spec.PodDisruptionBudget.MinAvailable != nil {
		minAvail = *ctx.Neo4j.Spec.PodDisruptionBudget.MinAvailable
	}
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PDBName(ctx),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("pdb"),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvail,
			Selector: &metav1.LabelSelector{
				MatchLabels: ctx.ClusterMemberSelectorLabels(),
			},
		},
	}
}

func defaultPDBMinAvailable(neo4j *neo4jv1beta1.Neo4j) intstr.IntOrString {
	if render.IsClusterMode(neo4j) && totalDesiredMembers(neo4j) >= 3 {
		return intstr.FromInt32(2)
	}
	return intstr.FromInt32(1)
}

func totalDesiredMembers(neo4j *neo4jv1beta1.Neo4j) int32 {
	var n int32
	for _, pool := range render.ActivePools(neo4j) {
		n += render.ContextForPool(neo4j, pool).PoolReplicas()
	}
	return n
}
