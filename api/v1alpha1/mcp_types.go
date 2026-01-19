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

// MCPServerSpec defines configuration for a Neo4j MCP server.
type MCPServerSpec struct {
	// Enable MCP server deployment.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Image configuration for the MCP server.
	Image *ImageSpec `json:"image,omitempty"`

	// Transport mode for MCP: http or stdio.
	// +kubebuilder:validation:Enum=http;stdio
	// +kubebuilder:default=http
	Transport string `json:"transport,omitempty"`

	// ReadOnly disables write tools when true.
	// +kubebuilder:default=true
	ReadOnly bool `json:"readOnly,omitempty"`

	// Telemetry enables anonymous usage data collection.
	// +kubebuilder:default=false
	Telemetry bool `json:"telemetry,omitempty"`

	// Default Neo4j database for MCP.
	Database string `json:"database,omitempty"`

	// SchemaSampleSize controls the schema sampling size.
	// +kubebuilder:validation:Minimum=1
	SchemaSampleSize *int32 `json:"schemaSampleSize,omitempty"`

	// LogLevel controls MCP logging verbosity.
	LogLevel string `json:"logLevel,omitempty"`

	// LogFormat controls MCP logging format.
	LogFormat string `json:"logFormat,omitempty"`

	// HTTP settings (only used for HTTP transport).
	HTTP *MCPHTTPConfig `json:"http,omitempty"`

	// Auth settings (only used for STDIO transport).
	Auth *MCPAuthSpec `json:"auth,omitempty"`

	// Replicas controls the number of MCP server pods.
	Replicas *int32 `json:"replicas,omitempty"`

	// Resource requirements for MCP pods.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Environment variables for MCP pods.
	Env []corev1.EnvVar `json:"env,omitempty"`

	// SecurityContext allows overriding pod/container security settings.
	SecurityContext *SecurityContextSpec `json:"securityContext,omitempty"`
}

// MCPHTTPConfig defines HTTP transport settings for MCP.
type MCPHTTPConfig struct {
	// Host to bind the HTTP server to (defaults to 0.0.0.0).
	Host string `json:"host,omitempty"`

	// Port to bind the HTTP server to (defaults to 8080 or 8443 when TLS enabled).
	Port int32 `json:"port,omitempty"`

	// Allowed origins for CORS (comma-separated or "*").
	AllowedOrigins string `json:"allowedOrigins,omitempty"`

	// TLS settings for MCP HTTP server.
	TLS *MCPTLSSpec `json:"tls,omitempty"`

	// Service exposure settings for HTTP transport.
	Service *MCPServiceSpec `json:"service,omitempty"`
}

// MCPTLSSpec defines TLS configuration for MCP HTTP server.
type MCPTLSSpec struct {
	// +kubebuilder:validation:Enum=cert-manager;disabled;secret
	// +kubebuilder:default=disabled
	Mode string `json:"mode,omitempty"`

	// SecretName references a TLS secret with tls.crt and tls.key.
	SecretName string `json:"secretName,omitempty"`

	IssuerRef *IssuerRef `json:"issuerRef,omitempty"`

	// Certificate duration and renewal settings.
	Duration *string `json:"duration,omitempty"`

	// Certificate renewal before expiry.
	RenewBefore *string `json:"renewBefore,omitempty"`

	// Additional certificate subject fields.
	Subject *CertificateSubject `json:"subject,omitempty"`

	// Certificate usage settings.
	Usages []string `json:"usages,omitempty"`
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

// MCPAuthSpec defines STDIO auth settings for MCP.
type MCPAuthSpec struct {
	// SecretName references a secret with username/password keys.
	SecretName string `json:"secretName,omitempty"`

	// +kubebuilder:default=username
	UsernameKey string `json:"usernameKey,omitempty"`

	// +kubebuilder:default=password
	PasswordKey string `json:"passwordKey,omitempty"`
}
