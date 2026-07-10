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
	conf := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data["neo4j.conf"]
	for _, want := range []string{
		"initial.dbms.default_primaries_count=3",
		"dbms.kubernetes.discovery.type=K8S",
		"dbms.kubernetes.service_name=prod-internals",
		"dbms.kubernetes.namespace=default",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("primary neo4j.conf missing %q:\n%s", want, conf)
		}
	}

	analyticsConf := ConfigMap(render.ContextForPool(neo4j, render.PoolAnalytics)).Data["neo4j.conf"]
	if !strings.Contains(analyticsConf, "server.cluster.system_database_mode=SECONDARY") {
		t.Fatalf("analytics neo4j.conf missing SECONDARY mode:\n%s", analyticsConf)
	}
}
