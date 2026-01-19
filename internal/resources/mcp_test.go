package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildMCPDeploymentForCluster_HTTPWithTLSAndEnv(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	sampleSize := int32(200)
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:          true,
		Transport:        "http",
		ReadOnly:         true,
		Telemetry:        false,
		Database:         "neo4j",
		Image:            mcpImage(),
		SchemaSampleSize: &sampleSize,
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Host:           "0.0.0.0",
			AllowedOrigins: "*",
			TLS: &neo4jv1alpha1.MCPTLSSpec{
				Mode:       "secret",
				SecretName: "mcp-tls",
			},
		},
		Env: []corev1.EnvVar{
			{Name: "CUSTOM", Value: "1"},
			{Name: "NEO4J_URI", Value: "override"},
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)
	assert.Equal(t, "graph-cluster-mcp", deployment.Name)

	container := deployment.Spec.Template.Spec.Containers[0]
	require.Len(t, container.Ports, 1)
	assert.Equal(t, int32(8443), container.Ports[0].ContainerPort)

	assertEnvValue(t, container.Env, "NEO4J_URI", "neo4j://graph-cluster-client.default.svc.cluster.local:7687")
	assertEnvValue(t, container.Env, "NEO4J_MCP_TRANSPORT", "http")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_HOST", "0.0.0.0")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_PORT", "8443")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_ALLOWED_ORIGINS", "*")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_ENABLED", "true")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_CERT_FILE", "/tls/tls.crt")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_KEY_FILE", "/tls/tls.key")
	assertEnvValue(t, container.Env, "NEO4J_SCHEMA_SAMPLE_SIZE", "200")
	assertEnvValue(t, container.Env, "NEO4J_READ_ONLY", "true")
	assertEnvValue(t, container.Env, "NEO4J_TELEMETRY", "false")
	assertEnvValue(t, container.Env, "CUSTOM", "1")
	assertEnvMissing(t, container.Env, "NEO4J_USERNAME")
	assertEnvMissing(t, container.Env, "NEO4J_PASSWORD")

	require.Len(t, deployment.Spec.Template.Spec.Volumes, 1)
	assert.Equal(t, "mcp-tls", deployment.Spec.Template.Spec.Volumes[0].Name)
	assert.Equal(t, "mcp-tls", deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName)
}

func TestBuildMCPDeploymentForStandalone_STDIOAuth(t *testing.T) {
	standalone := baseStandalone("graph-standalone")
	standalone.Spec.TLS = &neo4jv1alpha1.TLSSpec{Mode: resources.CertManagerMode}
	standalone.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "stdio",
		Image:     mcpImage(),
		Auth: &neo4jv1alpha1.MCPAuthSpec{
			SecretName:  "mcp-auth",
			UsernameKey: "user",
			PasswordKey: "pass",
		},
	}

	deployment := resources.BuildMCPDeploymentForStandalone(standalone)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Len(t, container.Ports, 0)
	assertEnvValue(t, container.Env, "NEO4J_MCP_TRANSPORT", "stdio")
	assertEnvValue(t, container.Env, "NEO4J_URI", "bolt+ssc://graph-standalone-service.default.svc.cluster.local:7687")
	assertEnvSecretRef(t, container.Env, "NEO4J_USERNAME", "mcp-auth", "user")
	assertEnvSecretRef(t, container.Env, "NEO4J_PASSWORD", "mcp-auth", "pass")
	assertEnvMissing(t, container.Env, "NEO4J_MCP_HTTP_PORT")
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 0)
}

func TestBuildMCPServiceForCluster_PortOverrides(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		Image:     mcpImage(),
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Port: 8081,
			Service: &neo4jv1alpha1.MCPServiceSpec{
				Type: "LoadBalancer",
				Port: 9000,
				Annotations: map[string]string{
					"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
				},
			},
		},
	}

	service := resources.BuildMCPServiceForCluster(cluster)
	require.NotNil(t, service)
	assert.Equal(t, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
	require.Len(t, service.Spec.Ports, 1)
	assert.Equal(t, int32(9000), service.Spec.Ports[0].Port)
	assert.Equal(t, int32(8081), service.Spec.Ports[0].TargetPort.IntVal)
	assert.Equal(t, "nlb", service.Annotations["service.beta.kubernetes.io/aws-load-balancer-type"])
}

func TestBuildMCPIngressForCluster_HTTP(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		Image:     mcpImage(),
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Service: &neo4jv1alpha1.MCPServiceSpec{
				Port: 9000,
				Ingress: &neo4jv1alpha1.IngressSpec{
					Enabled:       true,
					ClassName:     "nginx",
					Host:          "mcp.example.com",
					TLSSecretName: "mcp-tls",
					Annotations: map[string]string{
						"nginx.ingress.kubernetes.io/backend-protocol": "HTTP",
					},
				},
			},
		},
	}

	ingress := resources.BuildMCPIngressForCluster(cluster)
	require.NotNil(t, ingress)
	require.NotNil(t, ingress.Spec.IngressClassName)
	assert.Equal(t, "nginx", *ingress.Spec.IngressClassName)
	require.Len(t, ingress.Spec.Rules, 1)
	paths := ingress.Spec.Rules[0].HTTP.Paths
	require.Len(t, paths, 1)
	assert.Equal(t, "/mcp", paths[0].Path)
	assert.Equal(t, "graph-cluster-mcp", paths[0].Backend.Service.Name)
	assert.Equal(t, int32(9000), paths[0].Backend.Service.Port.Number)
	assert.Equal(t, "mcp-tls", ingress.Spec.TLS[0].SecretName)
}

func TestBuildMCPRouteForCluster_DefaultPath(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		Image:     mcpImage(),
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Service: &neo4jv1alpha1.MCPServiceSpec{
				Annotations: map[string]string{
					"service": "annotation",
				},
				Route: &neo4jv1alpha1.RouteSpec{
					Enabled: true,
					Host:    "mcp.example.com",
					Annotations: map[string]string{
						"route": "annotation",
					},
					TargetPort: 9443,
				},
			},
		},
	}

	route := resources.BuildMCPRouteForCluster(cluster)
	require.NotNil(t, route)
	assert.Equal(t, "graph-cluster-mcp-route", route.GetName())

	metadata, ok := route.Object["metadata"].(map[string]interface{})
	require.True(t, ok)
	annotations, ok := metadata["annotations"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "annotation", annotations["service"])
	assert.Equal(t, "annotation", annotations["route"])

	path, _, err := unstructured.NestedString(route.Object, "spec", "path")
	require.NoError(t, err)
	assert.Equal(t, "/mcp", path)

	targetPort, _, err := unstructured.NestedFieldNoCopy(route.Object, "spec", "port", "targetPort")
	require.NoError(t, err)
	assert.Equal(t, int32(9443), targetPort)
}

func TestBuildMCPCertificateForCluster_CertManager(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		Image:     mcpImage(),
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			TLS: &neo4jv1alpha1.MCPTLSSpec{
				Mode:       resources.CertManagerMode,
				SecretName: "mcp-tls",
				IssuerRef: &neo4jv1alpha1.IssuerRef{
					Name: "issuer",
					Kind: "ClusterIssuer",
				},
			},
		},
	}

	cert := resources.BuildMCPCertificateForCluster(cluster)
	require.NotNil(t, cert)
	assert.Equal(t, "graph-cluster-mcp-tls", cert.Name)
	assert.Equal(t, "mcp-tls", cert.Spec.SecretName)
	assert.Equal(t, "issuer", cert.Spec.IssuerRef.Name)
}

func TestBuildMCPCertificateForCluster_StdioIgnored(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "stdio",
		Image:     mcpImage(),
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			TLS: &neo4jv1alpha1.MCPTLSSpec{
				Mode:       resources.CertManagerMode,
				SecretName: "mcp-tls",
				IssuerRef:  &neo4jv1alpha1.IssuerRef{Name: "issuer"},
			},
		},
	}

	cert := resources.BuildMCPCertificateForCluster(cluster)
	assert.Nil(t, cert)
}

func baseCluster(name string) *neo4jv1alpha1.Neo4jEnterpriseCluster {
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AdminSecret: "neo4j-admin-secret",
			},
			Service: &neo4jv1alpha1.ServiceSpec{
				Ingress: &neo4jv1alpha1.IngressSpec{},
				Route:   &neo4jv1alpha1.RouteSpec{},
			},
		},
	}
}

func baseStandalone(name string) *neo4jv1alpha1.Neo4jEnterpriseStandalone {
	return &neo4jv1alpha1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AdminSecret: "neo4j-admin-secret",
			},
			Service: &neo4jv1alpha1.ServiceSpec{
				Ingress: &neo4jv1alpha1.IngressSpec{},
				Route:   &neo4jv1alpha1.RouteSpec{},
			},
		},
	}
}

func mcpImage() *neo4jv1alpha1.ImageSpec {
	return &neo4jv1alpha1.ImageSpec{
		Repo: "ghcr.io/priyolahiri/neo4j-kubernetes-operator-mcp",
		Tag:  "v1.0.0",
	}
}

func assertEnvValue(t *testing.T, env []corev1.EnvVar, name, value string) {
	t.Helper()
	for _, entry := range env {
		if entry.Name == name {
			assert.Equal(t, value, entry.Value)
			return
		}
	}
	assert.Failf(t, "missing env var", "expected %s", name)
}

func assertEnvMissing(t *testing.T, env []corev1.EnvVar, name string) {
	t.Helper()
	for _, entry := range env {
		if entry.Name == name {
			assert.Failf(t, "unexpected env var", "did not expect %s", name)
			return
		}
	}
}

func assertEnvSecretRef(t *testing.T, env []corev1.EnvVar, name, secretName, key string) {
	t.Helper()
	for _, entry := range env {
		if entry.Name == name {
			require.NotNil(t, entry.ValueFrom)
			require.NotNil(t, entry.ValueFrom.SecretKeyRef)
			assert.Equal(t, secretName, entry.ValueFrom.SecretKeyRef.Name)
			assert.Equal(t, key, entry.ValueFrom.SecretKeyRef.Key)
			return
		}
	}
	assert.Failf(t, "missing env var", "expected %s", name)
}
