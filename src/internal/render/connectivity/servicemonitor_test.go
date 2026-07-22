package connectivity

import (
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func int32Ptr(v int32) *int32 { return &v }

func TestServiceMonitorDefaults(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "demo"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Features: &neo4jv1beta1.FeaturesSpec{
				Monitoring: &neo4jv1beta1.MonitoringFeaturesSpec{
					Prometheus:     &neo4jv1beta1.PrometheusMonitoringSpec{Enabled: true},
					ServiceMonitor: &neo4jv1beta1.ServiceMonitorSpec{Enabled: true},
				},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{Metrics: int32Ptr(2004)},
			},
		},
	}
	ctx := render.ClientServiceContext(neo4j)
	if !ServiceMonitorEnabled(ctx) {
		t.Fatal("expected ServiceMonitor enabled")
	}
	sm := ServiceMonitor(ctx)
	if sm.GetName() != "dev-servicemonitor" {
		t.Fatalf("name = %q", sm.GetName())
	}
	if sm.GetNamespace() != "demo" {
		t.Fatalf("namespace = %q", sm.GetNamespace())
	}
	if sm.GroupVersionKind() != ServiceMonitorGVK {
		t.Fatalf("gvk = %v", sm.GroupVersionKind())
	}
	eps, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil || !found || len(eps) != 1 {
		t.Fatalf("endpoints: found=%v err=%v len=%d", found, err, len(eps))
	}
	ep := eps[0].(map[string]interface{})
	if ep["port"] != "tcp-prometheus" {
		t.Fatalf("port = %v", ep["port"])
	}
	if ep["path"] != "/metrics" {
		t.Fatalf("path = %v", ep["path"])
	}
	if ep["interval"] != "30s" {
		t.Fatalf("interval = %v", ep["interval"])
	}
	ml, _, _ := unstructured.NestedStringMap(sm.Object, "spec", "selector", "matchLabels")
	if ml[render.LabelInstance] != "dev" || ml[render.LabelServiceRole] != render.ServiceRoleAdmin {
		t.Fatalf("selector matchLabels = %#v", ml)
	}
}

func TestServiceMonitorOverrides(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Features: &neo4jv1beta1.FeaturesSpec{
				Monitoring: &neo4jv1beta1.MonitoringFeaturesSpec{
					ServiceMonitor: &neo4jv1beta1.ServiceMonitorSpec{
						Enabled:  true,
						Port:     "custom-metrics",
						Path:     "/prom",
						Interval: "15s",
						JobLabel: "neo4j",
						Labels:   map[string]string{"team": "platform"},
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "neo4j"}},
						TargetLabels: []string{"app.kubernetes.io/instance"},
					},
				},
			},
		},
	}
	sm := ServiceMonitor(render.ClientServiceContext(neo4j))
	if sm.GetLabels()["team"] != "platform" {
		t.Fatalf("labels = %#v", sm.GetLabels())
	}
	eps, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	ep := eps[0].(map[string]interface{})
	if ep["port"] != "custom-metrics" || ep["path"] != "/prom" || ep["interval"] != "15s" {
		t.Fatalf("endpoint = %#v", ep)
	}
	job, _, _ := unstructured.NestedString(sm.Object, "spec", "jobLabel")
	if job != "neo4j" {
		t.Fatalf("jobLabel = %q", job)
	}
	ml, _, _ := unstructured.NestedStringMap(sm.Object, "spec", "selector", "matchLabels")
	if ml["app"] != "neo4j" {
		t.Fatalf("selector = %#v", ml)
	}
}

func TestServiceMonitorDisabled(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jSpec{Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone}},
	}
	if ServiceMonitorEnabled(render.ClientServiceContext(neo4j)) {
		t.Fatal("expected disabled")
	}
}
