package connectivity

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	serviceMonitorGroup   = "monitoring.coreos.com"
	serviceMonitorVersion = "v1"
	serviceMonitorKind    = "ServiceMonitor"

	defaultServiceMonitorInterval = "30s"
	defaultServiceMonitorPath     = "/metrics"
	defaultServiceMonitorPort     = "tcp-prometheus"
)

// ServiceMonitorGVK is the Prometheus Operator ServiceMonitor type.
var ServiceMonitorGVK = schema.GroupVersionKind{
	Group:   serviceMonitorGroup,
	Version: serviceMonitorVersion,
	Kind:    serviceMonitorKind,
}

// ServiceMonitorEnabled is true when features.monitoring.serviceMonitor.enabled.
func ServiceMonitorEnabled(ctx render.Context) bool {
	m := ctx.Neo4j.Spec.Features
	return m != nil && m.Monitoring != nil && m.Monitoring.ServiceMonitor != nil &&
		m.Monitoring.ServiceMonitor.Enabled
}

// ServiceMonitorName is the owned ServiceMonitor object name (Helm parity).
func ServiceMonitorName(ctx render.Context) string {
	return ctx.Neo4j.Name + "-servicemonitor"
}

// ServiceMonitor builds a Prometheus Operator ServiceMonitor scraping the admin Service
// metrics port (BDR-010). Uses unstructured to avoid a prometheus-operator Go dependency.
func ServiceMonitor(ctx render.Context) *unstructured.Unstructured {
	sm := ctx.Neo4j.Spec.Features.Monitoring.ServiceMonitor

	port := defaultServiceMonitorPort
	if sm.Port != "" {
		port = sm.Port
	}
	path := defaultServiceMonitorPath
	if sm.Path != "" {
		path = sm.Path
	}
	interval := defaultServiceMonitorInterval
	if sm.Interval != "" {
		interval = sm.Interval
	}

	labels := ctx.CommonLabels("connectivity")
	for k, v := range sm.Labels {
		labels[k] = v
	}

	selector := defaultAdminSelector(ctx)
	if sm.Selector != nil {
		selector = labelSelectorToUnstructured(sm.Selector)
	}

	spec := map[string]interface{}{
		"endpoints": []interface{}{
			map[string]interface{}{
				"port":     port,
				"interval": interval,
				"path":     path,
			},
		},
		// Same namespace as the ServiceMonitor when namespaceSelector is omitted.
		"selector": selector,
	}
	if sm.JobLabel != "" {
		spec["jobLabel"] = sm.JobLabel
	}
	if len(sm.TargetLabels) > 0 {
		tls := make([]interface{}, len(sm.TargetLabels))
		for i, t := range sm.TargetLabels {
			tls[i] = t
		}
		spec["targetLabels"] = tls
	}
	// ponytail: CRD types NamespaceSelector as metav1.LabelSelector, but Prometheus uses
	// matchNames/any. Ignore the typed field; default same-namespace scrape is enough.
	// Upgrade: align OpenAPI with monitoring.coreos.com NamespaceSelector.

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(ServiceMonitorGVK)
	u.SetName(ServiceMonitorName(ctx))
	u.SetNamespace(ctx.Namespace())
	u.SetLabels(labels)
	_ = unstructured.SetNestedMap(u.Object, spec, "spec")
	return u
}

func defaultAdminSelector(ctx render.Context) map[string]interface{} {
	return map[string]interface{}{
		"matchLabels": map[string]interface{}{
			render.LabelInstance:    ctx.Neo4j.Name,
			render.LabelServiceRole: render.ServiceRoleAdmin,
		},
	}
}

func labelSelectorToUnstructured(sel *metav1.LabelSelector) map[string]interface{} {
	out := map[string]interface{}{}
	if len(sel.MatchLabels) > 0 {
		m := make(map[string]interface{}, len(sel.MatchLabels))
		for k, v := range sel.MatchLabels {
			m[k] = v
		}
		out["matchLabels"] = m
	}
	if len(sel.MatchExpressions) > 0 {
		exprs := make([]interface{}, 0, len(sel.MatchExpressions))
		for _, e := range sel.MatchExpressions {
			item := map[string]interface{}{
				"key":      e.Key,
				"operator": string(e.Operator),
			}
			if len(e.Values) > 0 {
				vals := make([]interface{}, len(e.Values))
				for i, v := range e.Values {
					vals[i] = v
				}
				item["values"] = vals
			}
			exprs = append(exprs, item)
		}
		out["matchExpressions"] = exprs
	}
	return out
}
