package connectivity

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderconn "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/connectivity"
)

func (r *Reconciler) reconcileServiceMonitor(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	renderCtx := render.ClientServiceContext(neo4j)

	if !renderconn.ServiceMonitorEnabled(renderCtx) {
		return r.deleteServiceMonitorIfPresent(ctx, neo4j)
	}

	if !renderCtx.PrometheusFeatureEnabled() || !renderCtx.MetricsListenerEnabled() {
		return shared.Failed(fmt.Errorf(
			"features.monitoring.serviceMonitor.enabled requires features.monitoring.prometheus.enabled and connectivity.listeners.metrics",
		))
	}

	desired := renderconn.ServiceMonitor(renderCtx)
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(renderconn.ServiceMonitorGVK)
	sm.SetName(desired.GetName())
	sm.SetNamespace(desired.GetNamespace())

	if err := shared.Apply(ctx, r.Client, r.Scheme, neo4j, sm, func() error {
		sm.SetLabels(desired.GetLabels())
		spec, found, err := unstructured.NestedMap(desired.Object, "spec")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("ServiceMonitor spec missing")
		}
		return unstructured.SetNestedMap(sm.Object, spec, "spec")
	}); err != nil {
		if meta.IsNoMatchError(err) {
			// MON-001: Prometheus Operator CRD not installed — skip without failing reconcile.
			return shared.Done()
		}
		return shared.Failed(fmt.Errorf("apply ServiceMonitor: %w", err))
	}
	return shared.Done()
}

func (r *Reconciler) deleteServiceMonitorIfPresent(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	renderCtx := render.ClientServiceContext(neo4j)
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(renderconn.ServiceMonitorGVK)
	key := types.NamespacedName{
		Name:      renderconn.ServiceMonitorName(renderCtx),
		Namespace: neo4j.Namespace,
	}
	if err := r.Client.Get(ctx, key, u); err != nil {
		if meta.IsNoMatchError(err) || client.IgnoreNotFound(err) == nil {
			return shared.Done()
		}
		return shared.Failed(fmt.Errorf("get ServiceMonitor for delete: %w", err))
	}
	if err := r.Client.Delete(ctx, u); err != nil && client.IgnoreNotFound(err) != nil {
		if meta.IsNoMatchError(err) {
			return shared.Done()
		}
		return shared.Failed(fmt.Errorf("delete ServiceMonitor: %w", err))
	}
	return shared.Done()
}
