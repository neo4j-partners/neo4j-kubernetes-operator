package resources

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestBuildRouteForEnterprise(t *testing.T) {
	g := NewWithT(t)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: testObjectMeta("test-cluster", "default"),
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Service: &neo4jv1alpha1.ServiceSpec{
				Annotations: map[string]string{"svc": "anno"},
				Route: &neo4jv1alpha1.RouteSpec{
					Enabled:     true,
					Host:        "example.com",
					Path:        "/",
					Annotations: map[string]string{"route": "anno"},
					TargetPort:  8080,
					TLS: &neo4jv1alpha1.RouteTLSSpec{
						Termination:                   "edge",
						InsecureEdgeTerminationPolicy: "Redirect",
					},
				},
			},
		},
	}

	route := BuildRouteForEnterprise(cluster)
	g.Expect(route).ToNot(BeNil())
	spec, found, _ := unstructuredNestedMap(route.Object, "spec")
	g.Expect(found).To(BeTrue())
	g.Expect(spec["host"]).To(Equal("example.com"))
	g.Expect(spec["path"]).To(Equal("/"))
}

func TestBuildRouteForStandalone(t *testing.T) {
	g := NewWithT(t)

	standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
		ObjectMeta: testObjectMeta("standalone", "default"),
		Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
			Service: &neo4jv1alpha1.ServiceSpec{
				Route: &neo4jv1alpha1.RouteSpec{
					Enabled: true,
				},
			},
		},
	}

	route := BuildRouteForStandalone(standalone)
	g.Expect(route).ToNot(BeNil())
	spec, found, _ := unstructuredNestedMap(route.Object, "spec")
	g.Expect(found).To(BeTrue())
	// Default path and targetPort
	g.Expect(spec["path"]).To(Equal("/"))
	port, _, _ := unstructuredNestedMap(spec, "port")
	g.Expect(port["targetPort"]).To(Equal(int32(7474)))
}

// unstructuredNestedMap is a helper for tests to retrieve nested maps safely
func unstructuredNestedMap(obj map[string]interface{}, fields ...string) (map[string]interface{}, bool, error) {
	current := obj
	for i, field := range fields {
		val, found := current[field]
		if !found {
			return nil, false, nil
		}
		if i == len(fields)-1 {
			if m, ok := val.(map[string]interface{}); ok {
				return m, true, nil
			}
			return nil, false, nil
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		current = next
	}
	return current, true, nil
}

// testObjectMeta provides minimal metadata for test objects.
func testObjectMeta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}
