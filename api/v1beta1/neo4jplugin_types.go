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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4jPluginSpec defines the desired state of Neo4jPlugin
type Neo4jPluginSpec struct {
	// +kubebuilder:validation:Required
	// Target cluster reference
	ClusterRef string `json:"clusterRef"`

	// +kubebuilder:validation:Required
	// Plugin name
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// Plugin version
	Version string `json:"version"`

	// +kubebuilder:default=true
	// Enable the plugin
	Enabled bool `json:"enabled,omitempty"`

	// InstallMode selects how the plugin's JAR reaches /plugins.
	//
	// "Managed" (default) — operator adds the plugin to NEO4J_PLUGINS so the
	// upstream Neo4j Docker entrypoint resolves and installs the JAR at pod
	// startup. For non-bundled plugins (everything except APOC core) this
	// fetches from the internet on every pod start, which is a poor fit for
	// air-gapped or regulated environments. source.checksum is recorded for
	// attestation but the upstream entrypoint does not verify it.
	//
	// "PreBaked" — operator does NOT touch NEO4J_PLUGINS. The user is
	// responsible for delivering the JAR via a custom Neo4j image referenced
	// from spec.image on the cluster/standalone CR. The operator still
	// applies the plugin's configuration (security allowlists, unrestricted
	// procedures, ConfigMap entries) so Neo4j accepts the procedures the
	// pre-baked JAR exposes.
	//
	// "VerifiedDownload" — the operator injects an init container into the
	// target StatefulSet's pod template. The init container downloads
	// source.url, verifies the SHA256/SHA512 against source.checksum,
	// and drops the JAR into the shared /plugins emptyDir BEFORE the
	// Neo4j entrypoint runs. NEO4J_PLUGINS is NOT mutated (preventing
	// the entrypoint's own download from racing the verified JAR).
	// Authenticated mirrors are supported via source.authSecret;
	// internal CAs are supported via the cluster's spec.trustedCASecrets.
	// On checksum mismatch the init container exits non-zero, the pod
	// stays Pending, and Neo4jPlugin.status surfaces the failure.
	//
	// +kubebuilder:validation:Enum=Managed;PreBaked;VerifiedDownload
	// +kubebuilder:default=Managed
	// +optional
	InstallMode string `json:"installMode,omitempty"`

	// Plugin source configuration. Required when installMode is
	// "VerifiedDownload". Ignored when installMode is "PreBaked".
	Source *PluginSource `json:"source,omitempty"`

	// Plugin configuration
	Config map[string]string `json:"config,omitempty"`

	// Dependencies
	Dependencies []PluginDependency `json:"dependencies,omitempty"`

	// Security configuration
	Security *PluginSecurity `json:"security,omitempty"`

	// Resource requirements for the plugin
	Resources *PluginResourceRequirements `json:"resources,omitempty"`
}

// PluginSource defines how to obtain the plugin
type PluginSource struct {
	// +kubebuilder:validation:Enum=official;community;custom;url
	// +kubebuilder:default=official
	// Source type
	Type string `json:"type,omitempty"`

	// URL for custom plugins
	URL string `json:"url,omitempty"`

	// Checksum for verification
	Checksum string `json:"checksum,omitempty"`

	// Secret containing authentication for private repositories
	AuthSecret string `json:"authSecret,omitempty"`

	// Custom registry configuration
	Registry *PluginRegistry `json:"registry,omitempty"`
}

// PluginRegistry defines a custom plugin registry
type PluginRegistry struct {
	// Registry URL
	URL string `json:"url"`

	// Authentication secret
	AuthSecret string `json:"authSecret,omitempty"`

	// TLS configuration
	TLS *RegistryTLSConfig `json:"tls,omitempty"`
}

// RegistryTLSConfig defines TLS settings for plugin registry
type RegistryTLSConfig struct {
	// Skip TLS verification
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CA certificate secret
	CASecret string `json:"caSecret,omitempty"`
}

// PluginDependency defines a plugin dependency
type PluginDependency struct {
	// Dependency name
	Name string `json:"name"`

	// Version constraint
	VersionConstraint string `json:"versionConstraint,omitempty"`

	// Whether dependency is optional
	Optional bool `json:"optional,omitempty"`
}

// PluginSecurity defines security settings for plugins
type PluginSecurity struct {
	// Allowed procedures
	AllowedProcedures []string `json:"allowedProcedures,omitempty"`

	// Denied procedures
	DeniedProcedures []string `json:"deniedProcedures,omitempty"`

	// Sandbox mode
	Sandbox bool `json:"sandbox,omitempty"`

	// Security policy
	SecurityPolicy string `json:"securityPolicy,omitempty"`
}

// PluginResourceRequirements defines resource requirements for plugins
type PluginResourceRequirements struct {
	// Memory limit for the plugin
	MemoryLimit string `json:"memoryLimit,omitempty"`

	// CPU limit for the plugin
	CPULimit string `json:"cpuLimit,omitempty"`

	// Thread pool size
	ThreadPoolSize int32 `json:"threadPoolSize,omitempty"`
}

// Neo4jPluginStatus defines the observed state of Neo4jPlugin
type Neo4jPluginStatus struct {
	// Conditions represent the current state of the plugin
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase
	Phase string `json:"phase,omitempty"`

	// Message provides additional information
	Message string `json:"message,omitempty"`

	// Installed version
	InstalledVersion string `json:"installedVersion,omitempty"`

	// Installation time
	InstallationTime *metav1.Time `json:"installationTime,omitempty"`

	// Plugin health status
	Health *PluginHealth `json:"health,omitempty"`

	// Usage statistics
	Usage *PluginUsage `json:"usage,omitempty"`

	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// PluginHealth defines plugin health information
type PluginHealth struct {
	// Plugin status
	Status string `json:"status,omitempty"`

	// Last health check
	LastHealthCheck *metav1.Time `json:"lastHealthCheck,omitempty"`

	// Error messages
	Errors []string `json:"errors,omitempty"`

	// Performance metrics
	Performance *PluginPerformance `json:"performance,omitempty"`
}

// PluginPerformance defines plugin performance metrics
type PluginPerformance struct {
	// Memory usage
	MemoryUsage string `json:"memoryUsage,omitempty"`

	// CPU usage
	CPUUsage string `json:"cpuUsage,omitempty"`

	// Execution count
	ExecutionCount int64 `json:"executionCount,omitempty"`

	// Average execution time
	AvgExecutionTime string `json:"avgExecutionTime,omitempty"`
}

// PluginUsage defines plugin usage statistics
type PluginUsage struct {
	// Procedures called
	ProceduresCalled map[string]int64 `json:"proceduresCalled,omitempty"`

	// Last used time
	LastUsed *metav1.Time `json:"lastUsed,omitempty"`

	// Usage frequency
	UsageFrequency string `json:"usageFrequency,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Plugin",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jPlugin declaratively installs and manages a Neo4j plugin
// (APOC, Graph Data Science, Bloom, GenAI, n10s, GraphQL, or a custom
// plugin) on a referenced cluster or standalone. The controller
// merges the plugin name into the StatefulSet's NEO4J_PLUGINS env
// var (without overwriting plugins added by other controllers),
// applies plugin-specific configuration, and triggers a controlled
// rolling restart. Multiple Neo4jPlugin resources targeting the same
// deployment coexist and stack additively.
type Neo4jPlugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jPluginSpec   `json:"spec,omitempty"`
	Status Neo4jPluginStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jPluginList contains a list of Neo4jPlugin
type Neo4jPluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jPlugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jPlugin{}, &Neo4jPluginList{})
}
