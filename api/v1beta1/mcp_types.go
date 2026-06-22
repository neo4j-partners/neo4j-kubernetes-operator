/*
Copyright 2025 Priyo Lahiri.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import corev1 "k8s.io/api/core/v1"

// MCPServerSpec defines configuration for a Neo4j MCP server using the official
// mcp/neo4j image (https://hub.docker.com/r/mcp/neo4j, source: github.com/neo4j/mcp).
//
// Transport behaviour differs between modes:
//   - stdio (default for the binary): operator injects NEO4J_URI + credentials from the
//     admin secret. Suitable for in-cluster clients that exec into the pod.
//   - http: operator injects only NEO4J_URI. Credentials are supplied per-request by the
//     MCP client via Basic Auth or Bearer token — the operator does NOT embed them in env
//     vars for this mode, which is the design of the official image.
type MCPServerSpec struct {
	// Enable MCP server deployment.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Image configuration for the MCP server.
	// Defaults to mcp/neo4j:latest (the official Docker Hub image by Neo4j).
	Image *ImageSpec `json:"image,omitempty"`

	// Transport mode for the MCP server: http or stdio.
	// +kubebuilder:validation:Enum=http;stdio
	// +kubebuilder:default=http
	Transport string `json:"transport,omitempty"`

	// ReadOnly disables the write_neo4j_cypher tool when true.
	// +kubebuilder:default=true
	ReadOnly bool `json:"readOnly,omitempty"`

	// Database is the default Neo4j database name for MCP queries.
	Database string `json:"database,omitempty"`

	// SchemaSampleSize controls the sample size used by get-schema.
	// Lower values are faster; use -1 to sample the entire graph.
	// +kubebuilder:validation:Minimum=-1
	SchemaSampleSize *int32 `json:"schemaSampleSize,omitempty"`

	// Telemetry controls anonymous usage telemetry sent to Neo4j.
	// Defaults to the server's own default (true).
	// Set to false to disable telemetry.
	Telemetry *bool `json:"telemetry,omitempty"`

	// LogLevel sets the server log verbosity.
	// +kubebuilder:validation:Enum=debug;info;notice;warning;error;critical;alert;emergency
	LogLevel string `json:"logLevel,omitempty"`

	// LogFormat controls log output format.
	// +kubebuilder:validation:Enum=text;json
	LogFormat string `json:"logFormat,omitempty"`

	// HTTP settings (only used when transport=http).
	HTTP *MCPHTTPConfig `json:"http,omitempty"`

	// Auth allows overriding the Neo4j credentials used by the MCP server.
	// Only effective for stdio transport: in STDIO mode the operator injects
	// NEO4J_USERNAME and NEO4J_PASSWORD from this secret. When not set, the
	// cluster/standalone admin secret is used.
	//
	// In HTTP mode credentials come from each client request (Basic Auth or
	// Bearer token) and this field is ignored.
	Auth *MCPAuthSpec `json:"auth,omitempty"`

	// Replicas controls the number of MCP server pods (for HTTP mode).
	Replicas *int32 `json:"replicas,omitempty"`

	// Resource requirements for MCP pods.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Environment variables injected into MCP pods.
	// The following operator-managed variables are silently ignored if present:
	// NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD, NEO4J_DATABASE, NEO4J_READ_ONLY,
	// NEO4J_SCHEMA_SAMPLE_SIZE, NEO4J_TELEMETRY, NEO4J_LOG_LEVEL, NEO4J_LOG_FORMAT,
	// NEO4J_TRANSPORT_MODE, NEO4J_MCP_HTTP_HOST, NEO4J_MCP_HTTP_PORT,
	// NEO4J_MCP_HTTP_TLS_ENABLED, NEO4J_MCP_HTTP_TLS_CERT_FILE,
	// NEO4J_MCP_HTTP_TLS_KEY_FILE, NEO4J_AUTH_HEADER_NAME.
	Env []corev1.EnvVar `json:"env,omitempty"`

	// SecurityContext allows overriding pod/container security settings.
	SecurityContext *SecurityContextSpec `json:"securityContext,omitempty"`
}

// MCPHTTPConfig defines HTTP transport settings for the mcp/neo4j server.
//
// Authentication model: when running in HTTP mode, the official image expects Neo4j
// credentials in each HTTP request via the Authorization header (Basic Auth or Bearer
// token). The operator does NOT inject NEO4J_USERNAME/NEO4J_PASSWORD for HTTP transport.
// Configure your MCP client (VSCode, Claude Desktop, etc.) with the Neo4j username and
// password for the Basic Auth header.
type MCPHTTPConfig struct {
	// Host to bind the HTTP server to (defaults to 0.0.0.0).
	Host string `json:"host,omitempty"`

	// Port to bind the HTTP server to.
	// Defaults to 8080 (no TLS) or 8443 (TLS) to avoid privileged ports in containers.
	// The mcp/neo4j image's own default is 80/443; we override to Kubernetes-friendly ports.
	Port int32 `json:"port,omitempty"`

	// TLS enables HTTPS using the official image's built-in TLS termination.
	// When set, the referenced Kubernetes TLS secret is mounted and
	// NEO4J_MCP_HTTP_TLS_ENABLED=true is injected with the cert/key paths.
	// When not set, TLS should be handled at the Ingress or LoadBalancer level instead.
	TLS *MCPTLSSpec `json:"tls,omitempty"`

	// AuthHeaderName is the name of the HTTP header to read credentials from.
	// Defaults to "Authorization" (standard Basic Auth / Bearer token header).
	// Override when a reverse-proxy rewrites the Authorization header to a custom name.
	AuthHeaderName string `json:"authHeaderName,omitempty"`

	// Service exposure settings for HTTP transport.
	Service *MCPServiceSpec `json:"service,omitempty"`
}

// MCPTLSSpec configures container-level TLS for the mcp/neo4j HTTP server.
// The referenced Kubernetes TLS secret is mounted read-only and the cert/key paths
// are injected as NEO4J_MCP_HTTP_TLS_CERT_FILE / NEO4J_MCP_HTTP_TLS_KEY_FILE.
type MCPTLSSpec struct {
	// SecretName is a Kubernetes TLS secret (type kubernetes.io/tls) containing the
	// certificate and private key. Required when TLS is enabled.
	SecretName string `json:"secretName"`

	// CertKey is the key in the secret that contains the certificate file.
	// +kubebuilder:default=tls.crt
	CertKey string `json:"certKey,omitempty"`

	// KeyKey is the key in the secret that contains the private key file.
	// +kubebuilder:default=tls.key
	KeyKey string `json:"keyKey,omitempty"`
}

// MCPServiceSpec defines Service exposure for MCP HTTP server.
type MCPServiceSpec struct {
	// Service type: ClusterIP, NodePort, LoadBalancer.
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default=ClusterIP
	Type string `json:"type,omitempty"`

	// Annotations to add to the service.
	Annotations map[string]string `json:"annotations,omitempty"`

	// LoadBalancer specific configuration.
	LoadBalancerIP           string   `json:"loadBalancerIP,omitempty"`
	LoadBalancerSourceRanges []string `json:"loadBalancerSourceRanges,omitempty"`

	// External traffic policy: Cluster or Local.
	// +kubebuilder:validation:Enum=Cluster;Local
	ExternalTrafficPolicy string `json:"externalTrafficPolicy,omitempty"`

	// Port to expose for MCP HTTP server.
	Port int32 `json:"port,omitempty"`

	// Ingress configuration.
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Route configuration (OpenShift only).
	Route *RouteSpec `json:"route,omitempty"`
}

// MCPAuthSpec overrides the Neo4j credentials used by the MCP server in STDIO mode.
// When set, the operator reads username and password from the referenced secret and injects
// them as NEO4J_USERNAME / NEO4J_PASSWORD environment variables.
// This field is NOT used in HTTP transport mode; credentials come from each HTTP request.
type MCPAuthSpec struct {
	// SecretName references a secret with username/password keys.
	SecretName string `json:"secretName,omitempty"`

	// +kubebuilder:default=username
	UsernameKey string `json:"usernameKey,omitempty"`

	// +kubebuilder:default=password
	PasswordKey string `json:"passwordKey,omitempty"`
}
