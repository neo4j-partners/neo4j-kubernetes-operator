package status

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestBuildEndpointsPlainBolt(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	ep := buildEndpoints(render.ClientServiceContext(neo4j))
	if !strings.HasPrefix(ep.Bolt, "neo4j://") {
		t.Fatalf("bolt = %q", ep.Bolt)
	}
	if ep.Neo4j != ep.Bolt {
		t.Fatalf("neo4j = %q bolt = %q", ep.Neo4j, ep.Bolt)
	}
	if strings.Contains(ep.ConnectionExamples.PortForward, "bolt+s") {
		t.Fatalf("portForward = %q", ep.ConnectionExamples.PortForward)
	}
}

func TestBuildEndpointsBoltTLSAndHTTPS(t *testing.T) {
	https := int32(7473)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{HTTPS: &https},
				Service:   &neo4jv1beta1.ConnectivityServiceSpec{Expose: []string{"bolt", "http", "https"}},
			},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "bolt-cert"},
					},
					HTTPS: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "https-cert"},
					},
				},
			},
		},
	}
	ep := buildEndpoints(render.ClientServiceContext(neo4j))
	if !strings.HasPrefix(ep.Bolt, "neo4j+s://") {
		t.Fatalf("bolt = %q", ep.Bolt)
	}
	if ep.ConnectionExamples.Neo4jURI != ep.Bolt {
		t.Fatalf("neo4jURI = %q", ep.ConnectionExamples.Neo4jURI)
	}
	if !strings.Contains(ep.ConnectionExamples.PortForward, "bolt+s://127.0.0.1:") {
		t.Fatalf("portForward = %q", ep.ConnectionExamples.PortForward)
	}
	if ep.HTTPS != "https://prod.default.svc:7473" {
		t.Fatalf("https = %q", ep.HTTPS)
	}
}
