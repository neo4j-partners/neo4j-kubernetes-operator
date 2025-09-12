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

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4jEnterpriseStandaloneSpec defines the desired state of Neo4jEnterpriseStandalone
type Neo4jEnterpriseStandaloneSpec struct {
	// +kubebuilder:validation:Required
	Image ImageSpec `json:"image"`

	// +kubebuilder:validation:Required
	Storage StorageSpec `json:"storage"`

	// Resource requirements for Neo4j pod
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Environment variables for Neo4j pod
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Node selector for pod scheduling
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for pod scheduling
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity rules for pod scheduling
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Custom configuration for Neo4j (single mode only)
	Config map[string]string `json:"config,omitempty"`

	TLS *TLSSpec `json:"tls,omitempty"`

	Auth *AuthSpec `json:"auth,omitempty"`

	Service *ServiceSpec `json:"service,omitempty"`

	Backups *BackupsSpec `json:"backups,omitempty"`

	UI *UISpec `json:"ui,omitempty"`

	// RestoreFrom specifies backup to restore from during standalone creation
	RestoreFrom *RestoreSpec `json:"restoreFrom,omitempty"`

	// Plugin management configuration - DEPRECATED: Use Neo4jPlugin CRD instead

	// Query performance monitoring
	QueryMonitoring *QueryMonitoringSpec `json:"queryMonitoring,omitempty"`

	// Persistence configuration for standalone deployment
	Persistence *PersistenceSpec `json:"persistence,omitempty"`

	// ServerTags defines Neo4j server tags for this standalone instance
	// Used for routing and load balancing policies in multi-region setups
	// +optional
	ServerTags []string `json:"serverTags,omitempty"`

	// Placement defines zone/region placement preferences
	// +optional
	Placement *StandalonePlacementSpec `json:"placement,omitempty"`
}

// PersistenceSpec defines persistence configuration for standalone deployments
type PersistenceSpec struct {
	// +kubebuilder:default=true
	// Enable persistent storage
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	// PVC retention policy when standalone is deleted
	RetentionPolicy string `json:"retentionPolicy,omitempty"`

	// Access modes for the PVC
	// +kubebuilder:default={"ReadWriteOnce"}
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// StandalonePlacementSpec defines placement preferences for standalone instances
type StandalonePlacementSpec struct {
	// Zone specifies the availability zone preference
	// +optional
	Zone string `json:"zone,omitempty"`

	// Region specifies the region preference
	// +optional
	Region string `json:"region,omitempty"`

	// AntiAffinity configures anti-affinity rules
	// +optional
	AntiAffinity *AntiAffinitySpec `json:"antiAffinity,omitempty"`
}

// Neo4jEnterpriseStandaloneStatus defines the observed state of Neo4jEnterpriseStandalone
type Neo4jEnterpriseStandaloneStatus struct {
	// Conditions represent the current state of the standalone deployment
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the standalone deployment
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current state
	Message string `json:"message,omitempty"`

	// Endpoints provides connection information
	Endpoints *EndpointStatus `json:"endpoints,omitempty"`

	// Version shows the current Neo4j version
	Version string `json:"version,omitempty"`

	// Ready indicates if the standalone deployment is ready for connections
	Ready bool `json:"ready,omitempty"`

	// LastStartTime shows when the standalone deployment was last started
	LastStartTime *metav1.Time `json:"lastStartTime,omitempty"`

	// PodStatus provides information about the Neo4j pod
	PodStatus *StandalonePodStatus `json:"podStatus,omitempty"`

	// DatabaseStatus provides information about the Neo4j database
	DatabaseStatus *StandaloneDatabaseStatus `json:"databaseStatus,omitempty"`
}

// StandalonePodStatus provides information about the Neo4j pod
type StandalonePodStatus struct {
	// PodName is the name of the Neo4j pod
	PodName string `json:"podName,omitempty"`

	// PodIP is the IP address of the Neo4j pod
	PodIP string `json:"podIP,omitempty"`

	// NodeName is the name of the Kubernetes node hosting the pod
	NodeName string `json:"nodeName,omitempty"`

	// Zone is the availability zone where the pod is running
	Zone string `json:"zone,omitempty"`

	// Region is the region where the pod is running
	Region string `json:"region,omitempty"`

	// Phase is the current phase of the pod
	Phase corev1.PodPhase `json:"phase,omitempty"`

	// StartTime is when the pod was started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// RestartCount is the number of times the pod has been restarted
	RestartCount int32 `json:"restartCount,omitempty"`

	// Conditions represent the current conditions of the pod
	Conditions []corev1.PodCondition `json:"conditions,omitempty"`
}

// StandaloneDatabaseStatus provides information about the Neo4j database
type StandaloneDatabaseStatus struct {
	// DatabaseMode shows the current database mode (should be "SINGLE")
	DatabaseMode string `json:"databaseMode,omitempty"`

	// DatabaseName shows the active database name
	DatabaseName string `json:"databaseName,omitempty"`

	// LastBackupTime shows when the last backup was completed
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// StorageSize shows the current storage usage
	StorageSize string `json:"storageSize,omitempty"`

	// ConnectionCount shows the number of active connections
	ConnectionCount int32 `json:"connectionCount,omitempty"`

	// LastHealthCheck shows when the last health check was performed
	LastHealthCheck *metav1.Time `json:"lastHealthCheck,omitempty"`

	// HealthStatus shows the current health status of the database
	HealthStatus string `json:"healthStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jEnterpriseStandalone is the Schema for the neo4jenterprisestandalones API
type Neo4jEnterpriseStandalone struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jEnterpriseStandaloneSpec   `json:"spec,omitempty"`
	Status Neo4jEnterpriseStandaloneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jEnterpriseStandaloneList contains a list of Neo4jEnterpriseStandalone
type Neo4jEnterpriseStandaloneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jEnterpriseStandalone `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jEnterpriseStandalone{}, &Neo4jEnterpriseStandaloneList{})
}
