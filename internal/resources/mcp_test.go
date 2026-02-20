package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// TestBuildMCPDeploymentForCluster_HTTPDefaults verifies that an HTTP MCP deployment
// uses the official mcp/neo4j-cypher image, port 8000, /mcp/ path, and injects
// both username and password secret refs (required by the official image).
func TestBuildMCPDeploymentForCluster_HTTPDefaults(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	sampleSize := int32(200)
	tokenLimit := int32(10000)
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:            true,
		Transport:          "http",
		ReadOnly:           true,
		Database:           "neo4j",
		SchemaSampleSize:   &sampleSize,
		ResponseTokenLimit: &tokenLimit,
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Host:           "0.0.0.0",
			AllowedOrigins: "*",
			AllowedHosts:   "localhost,myapp.example.com",
		},
		Env: []corev1.EnvVar{
			{Name: "CUSTOM", Value: "1"},
			{Name: "NEO4J_URL", Value: "override"}, // reserved — should be dropped
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)
	assert.Equal(t, "graph-cluster-mcp", deployment.Name)

	container := deployment.Spec.Template.Spec.Containers[0]
	// Official image is the default.
	assert.Equal(t, "mcp/neo4j-cypher:latest", container.Image)

	require.Len(t, container.Ports, 1)
	assert.Equal(t, int32(8000), container.Ports[0].ContainerPort)

	// Official image env vars.
	assertEnvValue(t, container.Env, "NEO4J_URL", "neo4j://graph-cluster-client.default.svc.cluster.local:7687")
	assertEnvValue(t, container.Env, "NEO4J_TRANSPORT", "http")
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_HOST", "0.0.0.0")
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_PORT", "8000")
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_PATH", "/mcp/")
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_ALLOW_ORIGINS", "*")
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_ALLOWED_HOSTS", "localhost,myapp.example.com")
	assertEnvValue(t, container.Env, "NEO4J_SCHEMA_SAMPLE_SIZE", "200")
	assertEnvValue(t, container.Env, "NEO4J_RESPONSE_TOKEN_LIMIT", "10000")
	assertEnvValue(t, container.Env, "NEO4J_READ_ONLY", "true")

	// Credentials must be injected for HTTP transport too (official image requires them).
	assertEnvSecretRef(t, container.Env, "NEO4J_USERNAME", "neo4j-admin-secret", "username")
	assertEnvSecretRef(t, container.Env, "NEO4J_PASSWORD", "neo4j-admin-secret", "password")

	// Custom env passthrough (reserved keys are dropped).
	assertEnvValue(t, container.Env, "CUSTOM", "1")
	assertEnvMissing(t, container.Env, "NEO4J_TELEMETRY") // removed field

	// No TLS volume mounts — official image does not terminate TLS.
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 0)
	assert.Len(t, container.VolumeMounts, 0)
}

// TestBuildMCPDeploymentForCluster_CustomPort verifies that a user-specified port
// is propagated correctly.
func TestBuildMCPDeploymentForCluster_CustomPort(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Port: 9000,
			Path: "/custom-mcp/",
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	require.Len(t, container.Ports, 1)
	assert.Equal(t, int32(9000), container.Ports[0].ContainerPort)
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_PORT", "9000")
	assertEnvValue(t, container.Env, "NEO4J_MCP_SERVER_PATH", "/custom-mcp/")
}

// TestBuildMCPDeploymentForStandalone_STDIOAuth verifies STDIO transport injects
// credentials from the specified auth secret, not the default admin secret.
func TestBuildMCPDeploymentForStandalone_STDIOAuth(t *testing.T) {
	standalone := baseStandalone("graph-standalone")
	standalone.Spec.TLS = &neo4jv1alpha1.TLSSpec{Mode: resources.CertManagerMode}
	standalone.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "stdio",
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
	assertEnvValue(t, container.Env, "NEO4J_TRANSPORT", "stdio")
	assertEnvValue(t, container.Env, "NEO4J_URL", "bolt+ssc://graph-standalone-service.default.svc.cluster.local:7687")
	assertEnvSecretRef(t, container.Env, "NEO4J_USERNAME", "mcp-auth", "user")
	assertEnvSecretRef(t, container.Env, "NEO4J_PASSWORD", "mcp-auth", "pass")
	assertEnvMissing(t, container.Env, "NEO4J_MCP_SERVER_PORT")
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 0)
}

// TestBuildMCPDeploymentForCluster_NamespaceAndReadTimeout verifies new optional
// fields added for the official image (Namespace, ReadTimeout).
func TestBuildMCPDeploymentForCluster_NamespaceAndReadTimeout(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	timeout := int32(60)
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Namespace: "movies",
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			ReadTimeout: &timeout,
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	assertEnvValue(t, container.Env, "NEO4J_NAMESPACE", "movies")
	assertEnvValue(t, container.Env, "NEO4J_READ_TIMEOUT", "60")
}

func TestBuildMCPServiceForCluster_PortOverrides(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1alpha1.MCPHTTPConfig{
			Port: 8001,
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
	assert.Equal(t, int32(8001), service.Spec.Ports[0].TargetPort.IntVal)
	assert.Equal(t, "nlb", service.Annotations["service.beta.kubernetes.io/aws-load-balancer-type"])
}

func TestBuildMCPIngressForCluster_HTTP(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
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
	// Default path is /mcp/ (official image).
	assert.Equal(t, "/mcp/", paths[0].Path)
	assert.Equal(t, "graph-cluster-mcp", paths[0].Backend.Service.Name)
	assert.Equal(t, int32(9000), paths[0].Backend.Service.Port.Number)
	assert.Equal(t, "mcp-tls", ingress.Spec.TLS[0].SecretName)
}

func TestBuildMCPRouteForCluster_DefaultPath(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
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
	// Default path is /mcp/ (official image).
	assert.Equal(t, "/mcp/", path)

	targetPort, _, err := unstructured.NestedFieldNoCopy(route.Object, "spec", "port", "targetPort")
	require.NoError(t, err)
	assert.Equal(t, int32(9443), targetPort)
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
