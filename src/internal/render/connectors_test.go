package render

import (
	"testing"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClientExposeDefaultsAndFacade(t *testing.T) {
	httpFacade := int32(80)
	https := int32(7473)
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
			Connectivity: &neo4jv1.ConnectivitySpec{
				Listeners: &neo4jv1.ConnectivityListenersSpec{
					HTTPS: &https,
				},
				Service: &neo4jv1.ConnectivityServiceSpec{
					Ports: &neo4jv1.ServicePortsSpec{HTTP: &httpFacade},
				},
			},
		},
	}
	ctx := StandaloneContext(neo4j)
	expose := ctx.ClientExpose()
	if len(expose) != 2 || expose[0] != ConnectorBolt || expose[1] != ConnectorHTTP {
		t.Fatalf("default expose = %#v", expose)
	}
	if ctx.ServiceFacadePort(ConnectorHTTP) != 80 {
		t.Fatalf("http façade = %d", ctx.ServiceFacadePort(ConnectorHTTP))
	}
	if !ctx.HTTPSEnabled() || ctx.HTTPSPort() != 7473 {
		t.Fatalf("https enabled/port = %v/%d", ctx.HTTPSEnabled(), ctx.HTTPSPort())
	}
	if ctx.ShouldCreateAdminService() {
		t.Fatal("standalone without features should not require admin")
	}
}

func TestShouldCreateAdminServiceGates(t *testing.T) {
	standalone := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
		},
	}
	if StandaloneContext(standalone).ShouldCreateAdminService() {
		t.Fatal("unexpected admin for plain standalone")
	}

	withBackup := standalone.DeepCopy()
	withBackup.Spec.Features = &neo4jv1.FeaturesSpec{
		Backup: &neo4jv1.BackupFeatureSpec{Enabled: true},
	}
	if !StandaloneContext(withBackup).ShouldCreateAdminService() {
		t.Fatal("expected admin when backup enabled")
	}

	cluster := standalone.DeepCopy()
	cluster.Spec.Topology.Mode = neo4jv1.TopologyModeCluster
	cluster.Spec.Topology.Primaries = &neo4jv1.PrimariesSpec{Members: 3}
	if !ContextForPool(cluster, PoolPrimary).ShouldCreateAdminService() {
		t.Fatal("expected admin in cluster mode")
	}
}
