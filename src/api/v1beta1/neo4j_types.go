/*
Copyright Neo4j.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4jSpec defines the desired state of Neo4j.
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Standalone' || (!has(self.topology.primaries) && !has(self.topology.secondaries) && !has(self.topology.minimumMembers))",message="members fields are not allowed when mode is Standalone"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Cluster' || (has(self.topology.primaries) && has(self.topology.primaries.members) && self.topology.primaries.members >= 1)",message="primaries.members is required when mode is Cluster"
// +kubebuilder:validation:XValidation:rule="!has(self.topology.secondaries) || (has(self.topology.primaries) && has(self.topology.primaries.members))",message="primaries.members must be set before secondaries"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Standalone' || !has(self.topology.secondaries)",message="Secondaries require mode: Cluster"
// +kubebuilder:validation:XValidation:rule="!has(self.topology.secondaries) || !has(self.topology.secondaries.read) || !has(self.topology.secondaries.read.plugins) || self.topology.secondaries.read.plugins.all(p, p != 'gds' && p != 'bloom')",message="GDS and Bloom must be declared on secondaries.analytics, not secondaries.read"
// +kubebuilder:validation:XValidation:rule="!has(self.topology.primaries) || self.topology.primaries.members == 0 || self.topology.primaries.members % 2 == 1",message="primary count must be odd for quorum"
// +kubebuilder:validation:XValidation:rule="!has(self.topology.secondaries) || !has(self.topology.secondaries.analytics) || self.topology.secondaries.analytics.members >= 1",message="analytics pool members must be at least 1 when pool is configured"
// +kubebuilder:validation:XValidation:rule="!has(self.topology.secondaries) || !has(self.topology.secondaries.read) || self.topology.secondaries.read.members >= 1",message="read pool members must be at least 1 when pool is configured"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Standalone' || !has(self.topology.minimumMembers)",message="minimumMembers not allowed in Standalone"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Cluster' || !has(self.topology.primaries) || !has(self.topology.primaries.plugins) || self.topology.primaries.plugins.all(p, p != 'gds' && p != 'bloom')",message="GDS and Bloom cannot be installed on primary members in Cluster mode"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Cluster' || !has(self.plugins)",message="spec.plugins is not allowed when mode is Cluster"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Standalone' || (!has(self.topology.primaries) && !has(self.topology.secondaries))",message="use spec.plugins in standalone mode"
// +kubebuilder:validation:XValidation:rule="!( (has(self.topology.primaries) && has(self.topology.primaries.plugins) && self.topology.primaries.plugins.exists(p, p == 'gds')) || (has(self.topology.secondaries) && has(self.topology.secondaries.analytics) && has(self.topology.secondaries.analytics.plugins) && self.topology.secondaries.analytics.plugins.exists(p, p == 'gds')) || (has(self.plugins) && self.plugins.exists(p, p == 'gds')) ) || (has(self.pluginDefinitions) && has(self.pluginDefinitions.gds) && has(self.pluginDefinitions.gds.licenseSecretRef) && self.pluginDefinitions.gds.licenseSecretRef != '')",message="gds requires pluginDefinitions.gds.licenseSecretRef when referenced"
// +kubebuilder:validation:XValidation:rule="self.edition == 'enterprise'",message="V1 supports Enterprise edition only"
// +kubebuilder:validation:XValidation:rule="self.license.accept == 'yes' || self.license.accept == 'eval'",message="Enterprise license must be explicitly accepted"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Cluster' || self.edition == 'enterprise'",message="Cluster mode requires Enterprise edition"
// +kubebuilder:validation:XValidation:rule="self.version != ''",message="spec.version is required"
// +kubebuilder:validation:XValidation:rule="!(has(self.auth) && has(self.auth.generatePassword) && self.auth.generatePassword == true && has(self.auth.passwordSecretRef))",message="provide generatePassword or passwordSecretRef, not both"
// +kubebuilder:validation:XValidation:rule="!has(self.trust) || !has(self.trust.certManager) || self.trust.certManager.enabled != true || (has(self.trust.certManager.issuerRef) && has(self.trust.certManager.issuerRef.name) && self.trust.certManager.issuerRef.name != '')",message="cert-manager issuerRef is required when cert-manager is enabled"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Cluster' || !has(self.trust) || self.trust.enabled != true || (has(self.trust.certificates) && has(self.trust.certificates.cluster))",message="cluster TLS is required for clustered deployments when trust is enabled"
// +kubebuilder:validation:XValidation:rule="self.topology.mode != 'Cluster' || !has(self.connectivity) || !has(self.connectivity.multiCluster) || self.connectivity.multiCluster.enabled != true",message="multi-cluster not in V1"
// +kubebuilder:validation:XValidation:rule="!has(self.connectivity) || !has(self.connectivity.listeners) || !has(self.connectivity.listeners.backup) || (has(self.features) && has(self.features.backup) && self.features.backup.enabled == true)",message="backup listener requires features.backup.enabled"
// +kubebuilder:validation:XValidation:rule="!has(self.connectivity) || !has(self.connectivity.listeners) || !has(self.connectivity.listeners.metrics) || (has(self.features) && has(self.features.monitoring) && has(self.features.monitoring.prometheus) && self.features.monitoring.prometheus.enabled == true)",message="metrics listener requires features.monitoring.prometheus.enabled"
// +kubebuilder:validation:XValidation:rule="!has(self.config) || !has(self.config.neo4j) || !has(self.features) || !has(self.features.backup) || !('server.backup.listen_address' in self.config.neo4j)",message="use connectivity.listeners.backup for backup listen address"
type Neo4jSpec struct {
	// Edition selects the Neo4j product tier (V1: enterprise only).
	// +kubebuilder:validation:Required
	Edition Edition `json:"edition"`
	// Version is the Neo4j image tag driving deploy and rolling upgrade.
	// +kubebuilder:validation:Required
	Version string `json:"version"`
	// License records explicit Enterprise license acceptance.
	// +kubebuilder:validation:Required
	License LicenseSpec `json:"license"`
	// Topology defines Standalone vs Cluster mode and member pool sizes.
	// +kubebuilder:validation:Required
	Topology TopologySpec `json:"topology"`

	// Plugins lists catalog plugin ids installed on every server (Standalone only).
	Plugins []string `json:"plugins,omitempty"`
	// PluginDefinitions holds per-plugin license, version, and config keyed by catalog id.
	PluginDefinitions map[string]PluginDefinitionSpec `json:"pluginDefinitions,omitempty"`

	// Image overrides container image repository and pull settings.
	Image *ImageSpec `json:"image,omitempty"`
	// Auth configures bootstrap credentials and optional LDAP (V2).
	Auth *AuthSpec `json:"auth,omitempty"`
	// Storage configures data and auxiliary volumes plus extra mounts and Secret projections.
	Storage *StorageSpec `json:"storage,omitempty"`
	// Resources sets CPU and memory requests and limits for the Neo4j container.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Config merges neo4j.conf, apoc.conf drop-ins, and JVM flags.
	Config *ConfigSpec `json:"config,omitempty"`
	// Features toggles optional capabilities such as backup and monitoring export.
	Features *FeaturesSpec `json:"features,omitempty"`
	// Trust configures TLS certificates and mTLS policies.
	Trust *TrustSpec `json:"trust,omitempty"`
	// Connectivity defines listen ports, Services, Ingress, and cluster DNS.
	Connectivity *ConnectivitySpec `json:"connectivity,omitempty"`
	// Scheduling controls node placement, affinity, and topology spread.
	Scheduling *SchedulingSpec `json:"scheduling,omitempty"`
	// PodDisruptionBudget limits voluntary disruption during cluster maintenance.
	PodDisruptionBudget *PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Probes overrides startup, liveness, and readiness probes.
	Probes *ProbesSpec `json:"probes,omitempty"`
	// Security sets pod and container security contexts and ServiceAccount.
	Security *SecuritySpec `json:"security,omitempty"`
	// Maintenance enables operator-led maintenance such as offline mode.
	Maintenance *MaintenanceSpec `json:"maintenance,omitempty"`
	// PodTemplate is an escape hatch for init containers, sidecars, and env vars.
	PodTemplate *PodTemplateSpec `json:"podTemplate,omitempty"`
}

// Neo4jStatus defines the observed state of Neo4j (ADR-004).
type Neo4jStatus struct {
	// Phase is the coarse lifecycle stage (Pending, Running, Failed, etc.).
	Phase Neo4jPhase `json:"phase,omitempty"`
	// Conditions are Kubernetes-standard signals for automation and kubectl wait.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last metadata.generation fully reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Version is the effective Neo4j version running on members (may lag spec during upgrade).
	Version string `json:"version,omitempty"`
	// LastUpgradeTime records when status.upgrade.phase last reached Completed.
	LastUpgradeTime *metav1.Time `json:"lastUpgradeTime,omitempty"`
	// ServerSummary reports desired and ready server counts without Bolt queries.
	ServerSummary *ReplicaSummary `json:"serverSummary,omitempty"`
	// Upgrade tracks the rolling version-change state machine.
	Upgrade *UpgradeStatus `json:"upgrade,omitempty"`
	// Members holds per-server summary when cluster detail is collected.
	Members []MemberStatus `json:"members,omitempty"`
	// Diagnostics exposes deep Bolt observability; failures do not block Ready.
	Diagnostics *DiagnosticsStatus `json:"diagnostics,omitempty"`
	// Endpoints publishes client URIs and onboarding connection examples.
	Endpoints *EndpointsStatus `json:"endpoints,omitempty"`
	// Credentials references the auth Secret; never contains the password itself.
	Credentials *CredentialsStatus `json:"credentials,omitempty"`
	// ClusterInfo summarizes cluster identity and logical database states.
	ClusterInfo *ClusterInfoStatus `json:"clusterInfo,omitempty"`
	// ReadPoolReplicas is the observed replica count of the read pool StatefulSet (scale subresource).
	ReadPoolReplicas *int32 `json:"readPoolReplicas,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.topology.secondaries.read.members,statuspath=.status.readPoolReplicas
// +kubebuilder:resource:shortName=n4j,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Edition",type=string,JSONPath=`.spec.edition`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.topology.mode`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self == oldSelf || self.spec.topology.mode == oldSelf.spec.topology.mode",message="topology.mode cannot change"
type Neo4j struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jSpec   `json:"spec,omitempty"`
	Status Neo4jStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Neo4jList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4j `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4j{}, &Neo4jList{})
}
