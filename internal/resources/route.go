package resources

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

// buildRoute constructs an OpenShift Route as an unstructured object.
// It is used by both cluster and standalone controllers to avoid a hard dependency
// on the OpenShift API types while still reconciling Route resources when available.
func buildRoute(name, namespace, serviceName string, labels map[string]string, annotations map[string]string, host, path string, targetPort int32, tls *neo4jv1alpha1.RouteTLSSpec) *unstructured.Unstructured {
	if path == "" {
		path = "/"
	}
	if targetPort == 0 {
		targetPort = HTTPPort
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "route.openshift.io/v1",
			"kind":       "Route",
			"metadata": map[string]interface{}{
				"name":        name,
				"namespace":   namespace,
				"labels":      labels,
				"annotations": annotations,
			},
			"spec": map[string]interface{}{
				"to": map[string]interface{}{
					"kind":   "Service",
					"name":   serviceName,
					"weight": 100,
				},
				"port": map[string]interface{}{
					"targetPort": targetPort,
				},
				"path": path,
			},
		},
	}

	if host != "" {
		_ = unstructured.SetNestedField(route.Object, host, "spec", "host")
	}

	if tls != nil {
		tlsFields := map[string]interface{}{}
		if tls.Termination != "" {
			tlsFields["termination"] = tls.Termination
		}
		if tls.InsecureEdgeTerminationPolicy != "" {
			tlsFields["insecureEdgeTerminationPolicy"] = tls.InsecureEdgeTerminationPolicy
		}
		if len(tlsFields) > 0 {
			_ = unstructured.SetNestedField(route.Object, tlsFields, "spec", "tls")
		}
	}

	route.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "route.openshift.io",
		Version: "v1",
		Kind:    "Route",
	})
	return route
}

// BuildRouteForEnterprise creates a Route targeting the cluster client service.
func BuildRouteForEnterprise(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *unstructured.Unstructured {
	if cluster.Spec.Service == nil || cluster.Spec.Service.Route == nil || !cluster.Spec.Service.Route.Enabled {
		return nil
	}

	routeSpec := cluster.Spec.Service.Route
	labels := getLabelsForEnterprise(cluster, "route")
	delete(labels, "neo4j.com/clustering")

	annotations := map[string]string{}
	for k, v := range cluster.Spec.Service.Annotations {
		annotations[k] = v
	}
	for k, v := range routeSpec.Annotations {
		annotations[k] = v
	}

	targetPort := routeSpec.TargetPort
	if targetPort == 0 {
		targetPort = HTTPPort
	}

	return buildRoute(
		fmt.Sprintf("%s-client-route", cluster.Name),
		cluster.Namespace,
		fmt.Sprintf("%s-client", cluster.Name),
		labels,
		annotations,
		routeSpec.Host,
		routeSpec.Path,
		targetPort,
		routeSpec.TLS,
	)
}

// BuildRouteForStandalone creates a Route targeting the standalone service.
func BuildRouteForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *unstructured.Unstructured {
	if standalone.Spec.Service == nil || standalone.Spec.Service.Route == nil || !standalone.Spec.Service.Route.Enabled {
		return nil
	}

	routeSpec := standalone.Spec.Service.Route

	annotations := map[string]string{}
	for k, v := range standalone.Spec.Service.Annotations {
		annotations[k] = v
	}
	for k, v := range routeSpec.Annotations {
		annotations[k] = v
	}

	targetPort := routeSpec.TargetPort
	if targetPort == 0 {
		targetPort = 7474
	}

	return buildRoute(
		fmt.Sprintf("%s-route", standalone.Name),
		standalone.Namespace,
		fmt.Sprintf("%s-service", standalone.Name),
		map[string]string{
			"app.kubernetes.io/name":       "neo4j",
			"app.kubernetes.io/instance":   standalone.Name,
			"app.kubernetes.io/component":  "route",
			"app.kubernetes.io/managed-by": "neo4j-operator",
		},
		annotations,
		routeSpec.Host,
		routeSpec.Path,
		targetPort,
		routeSpec.TLS,
	)
}
