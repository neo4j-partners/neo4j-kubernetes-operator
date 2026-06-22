package controller

import (
	"context"
	"fmt"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// clusterServiceExternalIP returns the LoadBalancer-assigned external IP/host
// for the cluster's `{name}-client` Service if one has been provisioned, or "".
// Used by the connection-example generator so users get a concrete URL once
// the cloud's LB controller has filled in `Status.LoadBalancer.Ingress`.
func clusterServiceExternalIP(ctx context.Context, c client.Client, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	return serviceExternalIP(ctx, c, cluster.Namespace, fmt.Sprintf("%s-client", cluster.Name))
}

// standaloneServiceExternalIP is the standalone counterpart — resolves the
// `{name}-service` Service.
func standaloneServiceExternalIP(ctx context.Context, c client.Client, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) string {
	// External exposure lives on the canonical {name}-client Service (#215);
	// the deprecated {name}-service alias is ClusterIP-only.
	return serviceExternalIP(ctx, c, standalone.Namespace, fmt.Sprintf("%s-client", standalone.Name))
}

// serviceExternalIP returns the first ingress entry's IP (or hostname) from
// the named Service's LoadBalancer status. Empty string if the Service does
// not exist, isn't a LoadBalancer, or hasn't been assigned an external IP yet.
func serviceExternalIP(ctx context.Context, c client.Client, namespace, serviceName string) string {
	svc := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: serviceName}, svc); err != nil {
		return ""
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP != "" {
			return ing.IP
		}
		if ing.Hostname != "" {
			return ing.Hostname
		}
	}
	return ""
}

// GenerateConnectionExamples creates connection string examples based on service configuration
func GenerateConnectionExamples(name, namespace string, serviceType corev1.ServiceType, externalIP string, hasTLS bool) *neo4jv1beta1.ConnectionExamples {
	examples := &neo4jv1beta1.ConnectionExamples{}

	// Port forwarding example (always available)
	serviceName := fmt.Sprintf("%s-client", name)
	examples.PortForward = fmt.Sprintf("kubectl port-forward -n %s svc/%s 7474:7474 7687:7687", namespace, serviceName)

	// Determine the base URL based on service type
	var baseHost string
	var boltHost string

	// For NodePort, the port IS the dynamically assigned node port, so the host
	// already includes the port placeholder. For other types, port is appended.
	nodePort := serviceType == corev1.ServiceTypeNodePort

	switch serviceType {
	case corev1.ServiceTypeLoadBalancer:
		if externalIP != "" && externalIP != "<pending>" {
			baseHost = externalIP
			boltHost = externalIP
		} else {
			baseHost = "<external-ip>"
			boltHost = "<external-ip>"
		}
	case corev1.ServiceTypeNodePort:
		baseHost = "<node-ip>"
		boltHost = "<node-ip>"
	default: // ClusterIP
		baseHost = "localhost"
		boltHost = "localhost"
	}

	// Browser URL
	if nodePort {
		if hasTLS {
			examples.BrowserURL = fmt.Sprintf("https://%s:<https-node-port>", baseHost)
		} else {
			examples.BrowserURL = fmt.Sprintf("http://%s:<http-node-port>", baseHost)
		}
	} else if hasTLS {
		examples.BrowserURL = fmt.Sprintf("https://%s:7473", baseHost)
	} else {
		examples.BrowserURL = fmt.Sprintf("http://%s:7474", baseHost)
	}

	// Bolt URIs
	if nodePort {
		if hasTLS {
			examples.BoltURI = fmt.Sprintf("bolt+ssc://%s:<bolt-node-port>", boltHost)
			examples.Neo4jURI = fmt.Sprintf("neo4j+ssc://%s:<bolt-node-port>", boltHost)
		} else {
			examples.BoltURI = fmt.Sprintf("bolt://%s:<bolt-node-port>", boltHost)
			examples.Neo4jURI = fmt.Sprintf("neo4j://%s:<bolt-node-port>", boltHost)
		}
	} else if hasTLS {
		examples.BoltURI = fmt.Sprintf("bolt+ssc://%s:7687", boltHost)
		examples.Neo4jURI = fmt.Sprintf("neo4j+ssc://%s:7687", boltHost)
	} else {
		examples.BoltURI = fmt.Sprintf("bolt://%s:7687", boltHost)
		examples.Neo4jURI = fmt.Sprintf("neo4j://%s:7687", boltHost)
	}

	// Python example
	examples.PythonExample = fmt.Sprintf(`from neo4j import GraphDatabase
driver = GraphDatabase.driver("%s", auth=("neo4j", "<password>"))`, examples.BoltURI)

	// Java example
	examples.JavaExample = fmt.Sprintf(`import org.neo4j.driver.*;
Driver driver = GraphDatabase.driver("%s", AuthTokens.basic("neo4j", "<password>"));`, examples.BoltURI)

	return examples
}

// GenerateStandaloneConnectionExamples creates connection examples for standalone deployments
func GenerateStandaloneConnectionExamples(name, namespace string, serviceType corev1.ServiceType, externalIP string, hasTLS bool) *neo4jv1beta1.ConnectionExamples {
	examples := &neo4jv1beta1.ConnectionExamples{}

	// Port forwarding example (always available)
	serviceName := fmt.Sprintf("%s-client", name) // canonical client Service (#215)
	examples.PortForward = fmt.Sprintf("kubectl port-forward -n %s svc/%s 7474:7474 7687:7687", namespace, serviceName)

	// Determine the base URL based on service type
	var baseHost string
	var boltHost string

	// For NodePort, the port IS the dynamically assigned node port, so the host
	// already includes the port placeholder. For other types, port is appended.
	nodePort := serviceType == corev1.ServiceTypeNodePort

	switch serviceType {
	case corev1.ServiceTypeLoadBalancer:
		if externalIP != "" && externalIP != "<pending>" {
			baseHost = externalIP
			boltHost = externalIP
		} else {
			baseHost = "<external-ip>"
			boltHost = "<external-ip>"
		}
	case corev1.ServiceTypeNodePort:
		baseHost = "<node-ip>"
		boltHost = "<node-ip>"
	default: // ClusterIP
		baseHost = "localhost"
		boltHost = "localhost"
	}

	// Browser URL
	if nodePort {
		if hasTLS {
			examples.BrowserURL = fmt.Sprintf("https://%s:<https-node-port>", baseHost)
		} else {
			examples.BrowserURL = fmt.Sprintf("http://%s:<http-node-port>", baseHost)
		}
	} else if hasTLS {
		examples.BrowserURL = fmt.Sprintf("https://%s:7473", baseHost)
	} else {
		examples.BrowserURL = fmt.Sprintf("http://%s:7474", baseHost)
	}

	// Bolt URIs
	if nodePort {
		if hasTLS {
			examples.BoltURI = fmt.Sprintf("bolt+ssc://%s:<bolt-node-port>", boltHost)
			examples.Neo4jURI = fmt.Sprintf("neo4j+ssc://%s:<bolt-node-port>", boltHost)
		} else {
			examples.BoltURI = fmt.Sprintf("bolt://%s:<bolt-node-port>", boltHost)
			examples.Neo4jURI = fmt.Sprintf("neo4j://%s:<bolt-node-port>", boltHost)
		}
	} else if hasTLS {
		examples.BoltURI = fmt.Sprintf("bolt+ssc://%s:7687", boltHost)
		examples.Neo4jURI = fmt.Sprintf("neo4j+ssc://%s:7687", boltHost)
	} else {
		examples.BoltURI = fmt.Sprintf("bolt://%s:7687", boltHost)
		examples.Neo4jURI = fmt.Sprintf("neo4j://%s:7687", boltHost)
	}

	// Python example
	examples.PythonExample = fmt.Sprintf(`from neo4j import GraphDatabase
driver = GraphDatabase.driver("%s", auth=("neo4j", "<password>"))`, examples.BoltURI)

	// Java example
	examples.JavaExample = fmt.Sprintf(`import org.neo4j.driver.*;
Driver driver = GraphDatabase.driver("%s", AuthTokens.basic("neo4j", "<password>"));`, examples.BoltURI)

	return examples
}
