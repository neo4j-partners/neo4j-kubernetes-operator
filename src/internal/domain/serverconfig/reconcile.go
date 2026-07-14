package serverconfig

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	rendercfg "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/serverconfig"
)

// Reconciler applies neo4j.conf ConfigMaps per workload pool (BDR-008).
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func New(c client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme}
}

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		desired := rendercfg.ConfigMap(ctxRender)
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, cm, func() error {
			cm.Labels = desired.Labels
			cm.Data = desired.Data
			return nil
		}); err != nil {
			return shared.Failed(err)
		}

		if apocDesired := rendercfg.ApocConfigMap(ctxRender); apocDesired != nil {
			apocCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: apocDesired.Name, Namespace: apocDesired.Namespace}}
			if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, apocCM, func() error {
				apocCM.Labels = apocDesired.Labels
				apocCM.Data = apocDesired.Data
				return nil
			}); err != nil {
				return shared.Failed(err)
			}
		}
	}
	return shared.Done()
}

// OwnedTypes returns types watched via Owns().
func OwnedTypes() []client.Object {
	return []client.Object{
		&corev1.ConfigMap{},
	}
}
