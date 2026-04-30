package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// TestBuildMCPDeploymentForCluster_HTTPDefaults verifies that an HTTP MCP deployment
// uses the official mcp/neo4j image, port 8080, and the correct env vars.
// In HTTP mode credentials are NOT injected — they come per-request from the MCP client.
func TestBuildMCPDeploymentForCluster_HTTPDefaults(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	sampleSize := int32(200)
	telemetryOff := false
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:          true,
		Transport:        "http",
		ReadOnly:         true,
		Database:         "neo4j",
		SchemaSampleSize: &sampleSize,
		Telemetry:        &telemetryOff,
		LogLevel:         "debug",
		LogFormat:        "json",
		Env: []corev1.EnvVar{
			{Name: "CUSTOM", Value: "1"},
			{Name: "NEO4J_URI", Value: "override"}, // reserved — must be dropped
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)
	assert.Equal(t, "graph-cluster-mcp", deployment.Name)

	container := deployment.Spec.Template.Spec.Containers[0]
	// Official image.
	assert.Equal(t, "mcp/neo4j:latest", container.Image)

	// Port 8080 (K8s-friendly default; official image default is 80).
	require.Len(t, container.Ports, 1)
	assert.Equal(t, int32(8080), container.Ports[0].ContainerPort)

	// Connection URI.
	assertEnvValue(t, container.Env, "NEO4J_URI", "neo4j://graph-cluster-client.default.svc.cluster.local:7687")

	// Transport mode.
	assertEnvValue(t, container.Env, "NEO4J_TRANSPORT_MODE", "http")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_HOST", "0.0.0.0")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_PORT", "8080")

	// Optional fields.
	assertEnvValue(t, container.Env, "NEO4J_READ_ONLY", "true")
	assertEnvValue(t, container.Env, "NEO4J_SCHEMA_SAMPLE_SIZE", "200")
	assertEnvValue(t, container.Env, "NEO4J_TELEMETRY", "false")
	assertEnvValue(t, container.Env, "NEO4J_LOG_LEVEL", "debug")
	assertEnvValue(t, container.Env, "NEO4J_LOG_FORMAT", "json")

	// HTTP mode: credentials must NOT be injected (per-request auth from client).
	assertEnvMissing(t, container.Env, "NEO4J_USERNAME")
	assertEnvMissing(t, container.Env, "NEO4J_PASSWORD")

	// Custom env passthrough (reserved keys are dropped).
	assertEnvValue(t, container.Env, "CUSTOM", "1")

	// No TLS volumes (TLS not configured).
	assert.Empty(t, deployment.Spec.Template.Spec.Volumes)
	assert.Empty(t, container.VolumeMounts)

	// Readiness probe on the MCP port.
	require.NotNil(t, container.ReadinessProbe)
	require.NotNil(t, container.ReadinessProbe.TCPSocket)
	assert.Equal(t, int32(8080), container.ReadinessProbe.TCPSocket.Port.IntVal)
}

// TestBuildMCPDeploymentForCluster_HTTPWithTLS verifies TLS configuration:
// the cert/key are mounted from a K8s secret and env vars point to the mount paths.
func TestBuildMCPDeploymentForCluster_HTTPWithTLS(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1beta1.MCPHTTPConfig{
			TLS: &neo4jv1beta1.MCPTLSSpec{
				SecretName: "my-mcp-tls",
				// CertKey/KeyKey left empty → defaults to tls.crt / tls.key
			},
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]

	// With TLS the default port changes to 8443.
	require.Len(t, container.Ports, 1)
	assert.Equal(t, int32(8443), container.Ports[0].ContainerPort)

	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_ENABLED", "true")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_CERT_FILE", "/var/run/secrets/mcp-tls/tls.crt")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_KEY_FILE", "/var/run/secrets/mcp-tls/tls.key")

	// TLS volume should be present.
	require.Len(t, deployment.Spec.Template.Spec.Volumes, 1)
	assert.Equal(t, "my-mcp-tls", deployment.Spec.Template.Spec.Volumes[0].VolumeSource.Secret.SecretName)

	// VolumeMount inside the container.
	require.Len(t, container.VolumeMounts, 1)
	assert.Equal(t, "/var/run/secrets/mcp-tls", container.VolumeMounts[0].MountPath)
	assert.True(t, container.VolumeMounts[0].ReadOnly)
}

// TestBuildMCPDeploymentForCluster_HTTPWithTLS_CustomKeys verifies that custom
// cert/key field names in the TLS secret are respected.
func TestBuildMCPDeploymentForCluster_HTTPWithTLS_CustomKeys(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1beta1.MCPHTTPConfig{
			Port: 9443,
			TLS: &neo4jv1beta1.MCPTLSSpec{
				SecretName: "custom-tls",
				CertKey:    "cert.pem",
				KeyKey:     "key.pem",
			},
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, int32(9443), container.Ports[0].ContainerPort)
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_CERT_FILE", "/var/run/secrets/mcp-tls/cert.pem")
	assertEnvValue(t, container.Env, "NEO4J_MCP_HTTP_TLS_KEY_FILE", "/var/run/secrets/mcp-tls/key.pem")
}

// TestBuildMCPDeploymentForCluster_HTTPWithAuthHeader verifies the custom auth header name.
func TestBuildMCPDeploymentForCluster_HTTPWithAuthHeader(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1beta1.MCPHTTPConfig{
			AuthHeaderName: "X-Neo4j-Auth",
		},
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	assertEnvValue(t, container.Env, "NEO4J_AUTH_HEADER_NAME", "X-Neo4j-Auth")
}

// TestBuildMCPDeploymentForStandalone_STDIOAuth verifies that STDIO transport injects
// credentials (username + password) from the specified auth secret.
func TestBuildMCPDeploymentForStandalone_STDIOAuth(t *testing.T) {
	standalone := baseStandalone("graph-standalone")
	standalone.Spec.TLS = &neo4jv1beta1.TLSSpec{Mode: resources.CertManagerMode}
	standalone.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "stdio",
		Auth: &neo4jv1beta1.MCPAuthSpec{
			SecretName:  "mcp-auth",
			UsernameKey: "user",
			PasswordKey: "pass",
		},
	}

	deployment := resources.BuildMCPDeploymentForStandalone(standalone)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	// STDIO: no port exposed.
	assert.Empty(t, container.Ports)

	// STDIO: credentials MUST be injected.
	assertEnvValue(t, container.Env, "NEO4J_URI", "neo4j+ssc://graph-standalone-service.default.svc.cluster.local:7687")
	assertEnvValue(t, container.Env, "NEO4J_TRANSPORT_MODE", "stdio")
	assertEnvSecretRef(t, container.Env, "NEO4J_USERNAME", "mcp-auth", "user")
	assertEnvSecretRef(t, container.Env, "NEO4J_PASSWORD", "mcp-auth", "pass")

	// No HTTP-specific vars.
	assertEnvMissing(t, container.Env, "NEO4J_MCP_HTTP_PORT")

	// No TLS volumes (TLS not configured for MCP container).
	assert.Empty(t, deployment.Spec.Template.Spec.Volumes)
}

// TestBuildMCPDeploymentForCluster_STDIOUsesAdminSecret verifies that when no MCP auth
// override is set, STDIO mode uses the cluster admin secret.
func TestBuildMCPDeploymentForCluster_STDIOUsesAdminSecret(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "stdio",
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	assertEnvSecretRef(t, container.Env, "NEO4J_USERNAME", "neo4j-admin-secret", "username")
	assertEnvSecretRef(t, container.Env, "NEO4J_PASSWORD", "neo4j-admin-secret", "password")
}

// TestBuildMCPDeploymentForCluster_ReadinessProbe verifies the readiness probe exists
// for HTTP and is absent for STDIO.
func TestBuildMCPDeploymentForCluster_ReadinessProbe(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
	}

	deployment := resources.BuildMCPDeploymentForCluster(cluster)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	require.NotNil(t, container.ReadinessProbe)
	require.NotNil(t, container.ReadinessProbe.TCPSocket)
	assert.Equal(t, int32(8080), container.ReadinessProbe.TCPSocket.Port.IntVal)
}

// TestBuildMCPDeploymentForStandalone_NoReadinessProbeForSTDIO verifies no probe for STDIO.
func TestBuildMCPDeploymentForStandalone_NoReadinessProbeForSTDIO(t *testing.T) {
	standalone := baseStandalone("graph-standalone")
	standalone.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "stdio",
	}

	deployment := resources.BuildMCPDeploymentForStandalone(standalone)
	require.NotNil(t, deployment)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Nil(t, container.ReadinessProbe)
}

func TestBuildMCPServiceForCluster_PortOverrides(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1beta1.MCPHTTPConfig{
			Port: 8001,
			Service: &neo4jv1beta1.MCPServiceSpec{
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

func TestBuildMCPIngressForCluster_UsesFixedMCPPath(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1beta1.MCPHTTPConfig{
			Service: &neo4jv1beta1.MCPServiceSpec{
				Port: 9000,
				Ingress: &neo4jv1beta1.IngressSpec{
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
	// Fixed path /mcp (official image; not configurable via env var).
	assert.Equal(t, "/mcp", paths[0].Path)
	assert.Equal(t, "graph-cluster-mcp", paths[0].Backend.Service.Name)
	assert.Equal(t, int32(9000), paths[0].Backend.Service.Port.Number)
	assert.Equal(t, "mcp-tls", ingress.Spec.TLS[0].SecretName)
}

func TestBuildMCPRouteForCluster_DefaultPath(t *testing.T) {
	cluster := baseCluster("graph-cluster")
	cluster.Spec.MCP = &neo4jv1beta1.MCPServerSpec{
		Enabled:   true,
		Transport: "http",
		HTTP: &neo4jv1beta1.MCPHTTPConfig{
			Service: &neo4jv1beta1.MCPServiceSpec{
				Annotations: map[string]string{
					"service": "annotation",
				},
				Route: &neo4jv1beta1.RouteSpec{
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
	// Fixed path /mcp (official image).
	assert.Equal(t, "/mcp", path)

	targetPort, _, err := unstructured.NestedFieldNoCopy(route.Object, "spec", "port", "targetPort")
	require.NoError(t, err)
	assert.Equal(t, int32(9443), targetPort)
}

func baseCluster(name string) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1beta1.AuthSpec{
				AdminSecret: "neo4j-admin-secret",
			},
			Service: &neo4jv1beta1.ServiceSpec{
				Ingress: &neo4jv1beta1.IngressSpec{},
				Route:   &neo4jv1beta1.RouteSpec{},
			},
		},
	}
}

func baseStandalone(name string) *neo4jv1beta1.Neo4jEnterpriseStandalone {
	return &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Auth: &neo4jv1beta1.AuthSpec{
				AdminSecret: "neo4j-admin-secret",
			},
			Service: &neo4jv1beta1.ServiceSpec{
				Ingress: &neo4jv1beta1.IngressSpec{},
				Route:   &neo4jv1beta1.RouteSpec{},
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
