package serverconfig

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendercfg "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
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
	if err := rendercfg.ValidateConfig(neo4j); err != nil {
		return shared.Failed(err)
	}
	if err := rendercfg.ValidateLogging(neo4j); err != nil {
		return shared.Failed(err)
	}
	baseCtx := render.StandaloneContext(neo4j)
	if out := r.reconcileLoggingConfigMaps(ctx, neo4j, baseCtx); out.Err != nil {
		return out
	}
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

func (r *Reconciler) reconcileLoggingConfigMaps(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, baseCtx render.Context) shared.StepResult {
	if desired := rendercfg.ServerLogsConfigMap(baseCtx); desired != nil {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, cm, func() error {
			cm.Labels = desired.Labels
			cm.Data = desired.Data
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	} else if err := r.deleteConfigMapIfPresent(ctx, neo4j.Namespace, baseCtx.ServerLogsConfigMapName()); err != nil {
		return shared.Failed(err)
	}
	if desired := rendercfg.UserLogsConfigMap(baseCtx); desired != nil {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
		if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, cm, func() error {
			cm.Labels = desired.Labels
			cm.Data = desired.Data
			return nil
		}); err != nil {
			return shared.Failed(err)
		}
	} else if err := r.deleteConfigMapIfPresent(ctx, neo4j.Namespace, baseCtx.UserLogsConfigMapName()); err != nil {
		return shared.Failed(err)
	}
	return shared.Done()
}

func (r *Reconciler) deleteConfigMapIfPresent(ctx context.Context, namespace, name string) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := r.Client.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// OwnedTypes returns types watched via Owns().
func OwnedTypes() []client.Object {
	return []client.Object{
		&corev1.ConfigMap{},
	}
}
