package controller

import (
	"fmt"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// GenerateConnectionExamples creates connection string examples based on service configuration
func GenerateConnectionExamples(name, namespace string, serviceType corev1.ServiceType, externalIP string, hasTLS bool) *neo4jv1alpha1.ConnectionExamples {
	examples := &neo4jv1alpha1.ConnectionExamples{}

	// Port forwarding example (always available)
	serviceName := fmt.Sprintf("%s-client", name)
	examples.PortForward = fmt.Sprintf("kubectl port-forward -n %s svc/%s 7474:7474 7687:7687", namespace, serviceName)

	// Determine the base URL based on service type
	var baseHost string
	var boltHost string

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
		baseHost = "<node-ip>:<node-port>"
		boltHost = "<node-ip>:<bolt-node-port>"
	default: // ClusterIP
		baseHost = "localhost"
		boltHost = "localhost"
	}

	// Browser URL
	if hasTLS {
		examples.BrowserURL = fmt.Sprintf("https://%s:7473", baseHost)
	} else {
		examples.BrowserURL = fmt.Sprintf("http://%s:7474", baseHost)
	}

	// Bolt URIs
	if hasTLS {
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
func GenerateStandaloneConnectionExamples(name, namespace string, serviceType corev1.ServiceType, externalIP string, hasTLS bool) *neo4jv1alpha1.ConnectionExamples {
	examples := &neo4jv1alpha1.ConnectionExamples{}

	// Port forwarding example (always available)
	serviceName := fmt.Sprintf("%s-service", name)
	examples.PortForward = fmt.Sprintf("kubectl port-forward -n %s svc/%s 7474:7474 7687:7687", namespace, serviceName)

	// Determine the base URL based on service type
	var baseHost string
	var boltHost string

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
		baseHost = "<node-ip>:<node-port>"
		boltHost = "<node-ip>:<bolt-node-port>"
	default: // ClusterIP
		baseHost = "localhost"
		boltHost = "localhost"
	}

	// Browser URL
	if hasTLS {
		examples.BrowserURL = fmt.Sprintf("https://%s:7473", baseHost)
	} else {
		examples.BrowserURL = fmt.Sprintf("http://%s:7474", baseHost)
	}

	// Bolt URIs
	if hasTLS {
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
