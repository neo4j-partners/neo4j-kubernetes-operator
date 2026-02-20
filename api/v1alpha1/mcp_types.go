/*
Copyright 2025.

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

package v1alpha1

import corev1 "k8s.io/api/core/v1"

// MCPServerSpec defines configuration for a Neo4j MCP server using the official
// mcp/neo4j-cypher image (https://hub.docker.com/r/mcp/neo4j-cypher).
type MCPServerSpec struct {
	// Enable MCP server deployment.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Image configuration for the MCP server.
	// Defaults to mcp/neo4j-cypher:latest (the official Docker Hub image).
	Image *ImageSpec `json:"image,omitempty"`

	// Transport mode for MCP: http or stdio.
	// +kubebuilder:validation:Enum=http;stdio
	// +kubebuilder:default=http
	Transport string `json:"transport,omitempty"`

	// ReadOnly disables write tools when true.
	// +kubebuilder:default=true
	ReadOnly bool `json:"readOnly,omitempty"`

	// Default Neo4j database for MCP queries.
	Database string `json:"database,omitempty"`

	// Namespace restricts the MCP server to a specific Neo4j namespace (label prefix).
	Namespace string `json:"namespace,omitempty"`

	// SchemaSampleSize controls the schema sampling size used by get_neo4j_schema.
	// Lower values are faster; use -1 to sample the entire graph.
	// +kubebuilder:validation:Minimum=-1
	SchemaSampleSize *int32 `json:"schemaSampleSize,omitempty"`

	// ResponseTokenLimit limits the maximum number of tokens in a response.
	// +kubebuilder:validation:Minimum=1
	ResponseTokenLimit *int32 `json:"responseTokenLimit,omitempty"`

	// HTTP settings (only used for HTTP transport).
	HTTP *MCPHTTPConfig `json:"http,omitempty"`

	// Auth allows overriding the Neo4j credentials used by the MCP server.
	// When not set the operator uses the cluster/standalone admin secret.
	// Applies to both http and stdio transport.
	Auth *MCPAuthSpec `json:"auth,omitempty"`

	// Replicas controls the number of MCP server pods.
	Replicas *int32 `json:"replicas,omitempty"`

	// Resource requirements for MCP pods.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Environment variables for MCP pods.
	// Reserved operator-managed variables (NEO4J_URL, NEO4J_USERNAME, NEO4J_PASSWORD,
	// NEO4J_DATABASE, NEO4J_NAMESPACE, NEO4J_READ_ONLY, NEO4J_SCHEMA_SAMPLE_SIZE,
	// NEO4J_RESPONSE_TOKEN_LIMIT, NEO4J_TRANSPORT, NEO4J_MCP_SERVER_HOST,
	// NEO4J_MCP_SERVER_PORT, NEO4J_MCP_SERVER_PATH, NEO4J_MCP_SERVER_ALLOW_ORIGINS,
	// NEO4J_MCP_SERVER_ALLOWED_HOSTS, NEO4J_READ_TIMEOUT) are silently ignored.
	Env []corev1.EnvVar `json:"env,omitempty"`

	// SecurityContext allows overriding pod/container security settings.
	SecurityContext *SecurityContextSpec `json:"securityContext,omitempty"`
}

// MCPHTTPConfig defines HTTP transport settings for MCP.
type MCPHTTPConfig struct {
	// Host to bind the HTTP server to (defaults to 0.0.0.0).
	Host string `json:"host,omitempty"`

	// Port to bind the HTTP server to (defaults to 8000).
	Port int32 `json:"port,omitempty"`

	// Path at which the MCP endpoint is served (defaults to /mcp/).
	Path string `json:"path,omitempty"`

	// AllowedOrigins restricts CORS origins (comma-separated or "*").
	AllowedOrigins string `json:"allowedOrigins,omitempty"`

	// AllowedHosts restricts which hostnames may connect to the MCP endpoint
	// (comma-separated). Defaults to localhost,127.0.0.1.
	AllowedHosts string `json:"allowedHosts,omitempty"`

	// ReadTimeout is the maximum number of seconds to wait for a Neo4j query response.
	// +kubebuilder:validation:Minimum=1
	ReadTimeout *int32 `json:"readTimeout,omitempty"`

	// Service exposure settings for HTTP transport.
	Service *MCPServiceSpec `json:"service,omitempty"`
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

// MCPAuthSpec allows overriding the Neo4j credentials used by the MCP server.
// When set, the operator reads username and password from the referenced secret
// instead of the cluster/standalone admin secret.
// Applies to both http and stdio transport.
type MCPAuthSpec struct {
	// SecretName references a secret with username/password keys.
	SecretName string `json:"secretName,omitempty"`

	// +kubebuilder:default=username
	UsernameKey string `json:"usernameKey,omitempty"`

	// +kubebuilder:default=password
	PasswordKey string `json:"passwordKey,omitempty"`
}
