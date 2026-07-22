package connectivity_test

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/connectivity"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func int32Ptr(v int32) *int32 { return &v }

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func TestReconcileServiceMonitorRequiresPrometheusAndMetrics(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := connectivity.New(c, scheme)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Features: &neo4jv1beta1.FeaturesSpec{
				Monitoring: &neo4jv1beta1.MonitoringFeaturesSpec{
					ServiceMonitor: &neo4jv1beta1.ServiceMonitorSpec{Enabled: true},
				},
			},
		},
	}
	out := r.Reconcile(t.Context(), neo4j)
	if out.Err == nil {
		t.Fatal("expected error when prometheus/metrics missing")
	}
	if !strings.Contains(out.Err.Error(), "serviceMonitor.enabled requires") {
		t.Fatalf("unexpected error: %v", out.Err)
	}
}

func TestReconcileServiceMonitorSkipsWhenDisabled(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := connectivity.New(c, scheme)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
}

func TestReconcileServiceMonitorCreatesWhenEnabled(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := connectivity.New(c, scheme)

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
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
	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("reconcile: %v", out.Err)
	}

	sm := &unstructured.Unstructured{}
	sm.SetAPIVersion("monitoring.coreos.com/v1")
	sm.SetKind("ServiceMonitor")
	if err := c.Get(t.Context(), client.ObjectKey{Name: "dev-servicemonitor", Namespace: "default"}, sm); err != nil {
		t.Fatalf("get ServiceMonitor: %v", err)
	}
}
