package serverconfig

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterNeo4jConfInjected(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{
					Members: 3,
				},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				ClusterDomain: "cluster.local",
			},
		},
	}
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	for key, want := range map[string]string{
		"server.default_listen_address":              "0.0.0.0",
		"server.http.listen_address":                 ":7474",
		"server.http.enabled":                        "true",
		"server.bolt.listen_address":                 ":7687",
		"initial.dbms.default_primaries_count":       "3",
		"dbms.cluster.discovery.resolver_type":       "K8S",
		"dbms.kubernetes.discovery.service_port_name": "tcp-tx",
		"dbms.kubernetes.label_selector":             "app.kubernetes.io/name=neo4j,app.kubernetes.io/instance=prod,neo4j.com/service=internals",
		"dbms.routing.enabled":                       "true",
		"server.bolt.advertised_address":             "$(bash -c 'echo ${SERVICE_NEO4J}')",
		"server.cluster.raft.advertised_address":     "$(bash -c 'echo ${SERVICE_NEO4J_INTERNALS}')",
	} {
		if data[key] != want {
			t.Fatalf("primary config key %q = %q, want %q", key, data[key], want)
		}
	}

	analyticsData := ConfigMap(render.ContextForPool(neo4j, render.PoolAnalytics)).Data
	if analyticsData["server.cluster.system_database_mode"] != "SECONDARY" {
		t.Fatalf("analytics config missing SECONDARY mode: %#v", analyticsData)
	}
	if analyticsData["initial.server.mode_constraint"] != "SECONDARY" {
		t.Fatalf("analytics config missing mode_constraint: %#v", analyticsData)
	}
}

func TestReadPoolCannotBootstrapAsPrimary(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 1},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Analytics: &neo4jv1beta1.SecondaryPoolSpec{Members: 1, Plugins: []string{"gds"}},
					Read:      &neo4jv1beta1.SecondaryPoolSpec{Members: 1, Plugins: []string{"apoc"}},
				},
			},
		},
	}
	read := ConfigMap(render.ContextForPool(neo4j, render.PoolRead)).Data
	if read["server.cluster.system_database_mode"] != "SECONDARY" ||
		read["initial.server.mode_constraint"] != "SECONDARY" {
		t.Fatalf("read pool must be SECONDARY: %#v", read)
	}
	primary := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if primary["server.cluster.system_database_mode"] == "SECONDARY" {
		t.Fatalf("primary must not force SECONDARY system mode: %#v", primary)
	}
	if primary["initial.server.mode_constraint"] == "SECONDARY" {
		t.Fatalf("primary must not have SECONDARY mode_constraint: %#v", primary)
	}
}

func TestStandaloneNeo4jConfInjectedK8sDefaults(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	for key, want := range map[string]string{
		"server.default_listen_address": "0.0.0.0",
		"server.http.listen_address":    ":7474",
		"server.http.enabled":           "true",
	} {
		if data[key] != want {
			t.Fatalf("standalone config key %q = %q, want %q", key, data[key], want)
		}
	}
	if _, ok := data["dbms.cluster.discovery.resolver_type"]; ok {
		t.Fatalf("standalone config must not contain cluster keys: %#v", data)
	}
}

func TestListenerConfKeysHTTPSBackupMetrics(t *testing.T) {
	https := int32(7473)
	backup := int32(6362)
	metrics := int32(2004)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Features: &neo4jv1beta1.FeaturesSpec{
				Backup: &neo4jv1beta1.BackupFeatureSpec{Enabled: true},
				Monitoring: &neo4jv1beta1.MonitoringFeaturesSpec{
					Prometheus: &neo4jv1beta1.PrometheusMonitoringSpec{Enabled: true},
				},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{
					HTTPS:   &https,
					Backup:  &backup,
					Metrics: &metrics,
				},
			},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	for key, want := range map[string]string{
		"server.https.listen_address":            ":7473",
		"server.https.enabled":                   "true",
		"server.backup.listen_address":           "0.0.0.0:6362",
		"server.backup.enabled":                  "true",
		"server.metrics.prometheus.enabled":      "true",
		"server.metrics.prometheus.endpoint":     "localhost:2004",
	} {
		if data[key] != want {
			t.Fatalf("config key %q = %q, want %q", key, data[key], want)
		}
	}
}

func TestNeo4jConfDataHasNoNeo4jConfBlobKey(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	for k := range ConfigMap(render.StandaloneContext(neo4j)).Data {
		if strings.Contains(k, "\n") || k == "neo4j.conf" {
			t.Fatalf("unexpected config key %q", k)
		}
	}
}
