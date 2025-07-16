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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Neo4jEnterpriseClusterSpec defines the desired state of Neo4jEnterpriseCluster
type Neo4jEnterpriseClusterSpec struct {
	// +kubebuilder:validation:Enum=enterprise
	// +kubebuilder:default=enterprise
	Edition string `json:"edition,omitempty"`

	// +kubebuilder:validation:Required
	Image ImageSpec `json:"image"`

	// +kubebuilder:validation:Required
	Topology TopologyConfiguration `json:"topology"`

	// +kubebuilder:validation:Required
	Storage StorageSpec `json:"storage"`

	// Resource requirements for Neo4j pods
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Environment variables for Neo4j pods
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Node selector for pod scheduling
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for pod scheduling
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity rules for pod scheduling
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Custom configuration for Neo4j
	Config map[string]string `json:"config,omitempty"`

	TLS *TLSSpec `json:"tls,omitempty"`

	Auth *AuthSpec `json:"auth,omitempty"`

	Service *ServiceSpec `json:"service,omitempty"`

	Backups *BackupsSpec `json:"backups,omitempty"`

	UI *UISpec `json:"ui,omitempty"`

	// RestoreFrom specifies backup to restore from during cluster creation
	RestoreFrom *RestoreSpec `json:"restoreFrom,omitempty"`

	// UpgradeStrategy specifies how to handle rolling upgrades
	UpgradeStrategy *UpgradeStrategySpec `json:"upgradeStrategy,omitempty"`

	// Auto-scaling configuration for both primaries and secondaries
	AutoScaling *AutoScalingSpec `json:"autoScaling,omitempty"`

	// Plugin management configuration
	Plugins []PluginSpec `json:"plugins,omitempty"`

	// Query performance monitoring
	QueryMonitoring *QueryMonitoringSpec `json:"queryMonitoring,omitempty"`
}

// ImageSpec defines the Neo4j image configuration
type ImageSpec struct {
	// +kubebuilder:validation:Required
	Repo string `json:"repo"`

	// +kubebuilder:validation:Required
	Tag string `json:"tag"`

	// +kubebuilder:default=IfNotPresent
	PullPolicy string `json:"pullPolicy,omitempty"`

	PullSecrets []string `json:"pullSecrets,omitempty"`
}

// StorageSpec defines storage configuration
type StorageSpec struct {
	// +kubebuilder:validation:Required
	ClassName string `json:"className"`

	// +kubebuilder:validation:Required
	Size string `json:"size"`

	// PVC retention policy when cluster is deleted
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	RetentionPolicy string `json:"retentionPolicy,omitempty"`

	// Additional storage for backups
	BackupStorage *BackupStorageSpec `json:"backupStorage,omitempty"`
}

// BackupStorageSpec defines backup storage configuration
type BackupStorageSpec struct {
	ClassName string `json:"className,omitempty"`
	Size      string `json:"size,omitempty"`
}

// TLSSpec defines TLS configuration
type TLSSpec struct {
	// +kubebuilder:validation:Enum=cert-manager;disabled
	// +kubebuilder:default=cert-manager
	Mode string `json:"mode,omitempty"`

	IssuerRef *IssuerRef `json:"issuerRef,omitempty"`

	// Manual certificate configuration
	CertificateSecret string `json:"certificateSecret,omitempty"`

	// External Secrets configuration for TLS certificates
	ExternalSecrets *ExternalSecretsConfig `json:"externalSecrets,omitempty"`

	// Certificate duration and renewal settings
	Duration *string `json:"duration,omitempty"`

	// Certificate renewal before expiry
	RenewBefore *string `json:"renewBefore,omitempty"`

	// Additional certificate subject fields
	Subject *CertificateSubject `json:"subject,omitempty"`

	// Certificate usage settings
	Usages []string `json:"usages,omitempty"`
}

// CertificateSubject defines certificate subject fields
type CertificateSubject struct {
	Organizations       []string `json:"organizations,omitempty"`
	Countries           []string `json:"countries,omitempty"`
	OrganizationalUnits []string `json:"organizationalUnits,omitempty"`
	Localities          []string `json:"localities,omitempty"`
	Provinces           []string `json:"provinces,omitempty"`
}

// ExternalSecretsConfig defines External Secrets Operator configuration
type ExternalSecretsConfig struct {
	// Enable External Secrets Operator integration
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// SecretStore reference for External Secrets
	SecretStoreRef *SecretStoreRef `json:"secretStoreRef,omitempty"`

	// Refresh interval for external secrets
	// +kubebuilder:default="1h"
	RefreshInterval string `json:"refreshInterval,omitempty"`

	// Data mapping from external secret store
	Data []ExternalSecretData `json:"data,omitempty"`
}

// SecretStoreRef references an External Secrets SecretStore or ClusterSecretStore
type SecretStoreRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Enum=SecretStore;ClusterSecretStore
	// +kubebuilder:default=SecretStore
	Kind string `json:"kind,omitempty"`
}

// ExternalSecretData defines data mapping from external secret store
type ExternalSecretData struct {
	// +kubebuilder:validation:Required
	SecretKey string `json:"secretKey"`

	// Remote reference to the secret in external store
	RemoteRef *ExternalSecretRemoteRef `json:"remoteRef,omitempty"`
}

// ExternalSecretRemoteRef defines reference to external secret
type ExternalSecretRemoteRef struct {
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// Property within the secret (for JSON/YAML secrets)
	Property string `json:"property,omitempty"`

	// Version of the secret
	Version string `json:"version,omitempty"`
}

// IssuerRef references a cert-manager issuer
type IssuerRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +kubebuilder:default=ClusterIssuer
	Kind string `json:"kind,omitempty"`

	// Group of the issuer (defaults to cert-manager.io)
	Group string `json:"group,omitempty"`
}

// AuthSpec defines authentication configuration
type AuthSpec struct {
	// +kubebuilder:validation:Enum=native;ldap;kerberos;jwt
	// +kubebuilder:default=native
	Provider string `json:"provider,omitempty"`

	// Secret containing authentication provider configuration
	SecretRef string `json:"secretRef,omitempty"`

	// Admin secret for initial setup
	AdminSecret string `json:"adminSecret,omitempty"`

	// External Secrets configuration for auth secrets
	ExternalSecrets *ExternalSecretsConfig `json:"externalSecrets,omitempty"`

	// Password policy configuration
	PasswordPolicy *PasswordPolicySpec `json:"passwordPolicy,omitempty"`

	// JWT configuration for JWT auth provider
	JWT *JWTAuthSpec `json:"jwt,omitempty"`

	// LDAP configuration for LDAP auth provider
	LDAP *LDAPAuthSpec `json:"ldap,omitempty"`

	// Kerberos configuration for Kerberos auth provider
	Kerberos *KerberosAuthSpec `json:"kerberos,omitempty"`
}

// PasswordPolicySpec defines Neo4j password policy
type PasswordPolicySpec struct {
	// Minimum password length
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=8
	MinLength int `json:"minLength,omitempty"`

	// Require uppercase characters
	// +kubebuilder:default=true
	RequireUppercase bool `json:"requireUppercase,omitempty"`

	// Require lowercase characters
	// +kubebuilder:default=true
	RequireLowercase bool `json:"requireLowercase,omitempty"`

	// Require numeric characters
	// +kubebuilder:default=true
	RequireNumbers bool `json:"requireNumbers,omitempty"`

	// Require special characters
	// +kubebuilder:default=false
	RequireSpecialChars bool `json:"requireSpecialChars,omitempty"`
}

// JWTAuthSpec defines JWT authentication configuration
type JWTAuthSpec struct {
	// JWT validation settings
	Validation *JWTValidationSpec `json:"validation,omitempty"`

	// Claims mapping
	ClaimsMapping map[string]string `json:"claimsMapping,omitempty"`
}

// JWTValidationSpec defines JWT validation settings
type JWTValidationSpec struct {
	// JWKS endpoint URL
	JWKSURL string `json:"jwksUrl,omitempty"`

	// JWT issuer
	Issuer string `json:"issuer,omitempty"`

	// JWT audience
	Audience []string `json:"audience,omitempty"`
}

// LDAPAuthSpec defines LDAP authentication configuration
type LDAPAuthSpec struct {
	// LDAP server settings
	Server *LDAPServerSpec `json:"server,omitempty"`

	// User search settings
	UserSearch *LDAPSearchSpec `json:"userSearch,omitempty"`

	// Group search settings
	GroupSearch *LDAPSearchSpec `json:"groupSearch,omitempty"`
}

// LDAPServerSpec defines LDAP server configuration
type LDAPServerSpec struct {
	// LDAP server URLs
	URLs []string `json:"urls,omitempty"`

	// Enable TLS for LDAP connection
	// +kubebuilder:default=true
	TLS bool `json:"tls,omitempty"`

	// Skip TLS certificate verification
	// +kubebuilder:default=false
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// LDAPSearchSpec defines LDAP search configuration
type LDAPSearchSpec struct {
	// Search base DN
	BaseDN string `json:"baseDN,omitempty"`

	// Search filter
	Filter string `json:"filter,omitempty"`

	// Search scope
	// +kubebuilder:validation:Enum=base;one;sub
	// +kubebuilder:default=sub
	Scope string `json:"scope,omitempty"`
}

// KerberosAuthSpec defines Kerberos authentication configuration
type KerberosAuthSpec struct {
	// Kerberos realm
	Realm string `json:"realm,omitempty"`

	// Service principal name
	ServicePrincipal string `json:"servicePrincipal,omitempty"`

	// Keytab configuration
	Keytab *KerberosKeytabSpec `json:"keytab,omitempty"`
}

// KerberosKeytabSpec defines Kerberos keytab configuration
type KerberosKeytabSpec struct {
	// Secret containing keytab file
	SecretRef string `json:"secretRef,omitempty"`

	// Key in secret containing keytab
	// +kubebuilder:default=keytab
	Key string `json:"key,omitempty"`
}

// ServiceSpec defines service configuration
type ServiceSpec struct {
	Type string `json:"type,omitempty"`

	Annotations map[string]string `json:"annotations,omitempty"`

	Ingress *IngressSpec `json:"ingress,omitempty"`
}

// IngressSpec defines ingress configuration
type IngressSpec struct {
	Enabled bool `json:"enabled,omitempty"`

	ClassName string `json:"className,omitempty"`

	Annotations map[string]string `json:"annotations,omitempty"`

	Host string `json:"host,omitempty"`

	TLSSecretName string `json:"tlsSecretName,omitempty"`
}

// BackupsSpec defines default backup configuration
type BackupsSpec struct {
	DefaultStorage *StorageLocation `json:"defaultStorage,omitempty"`

	Cloud *CloudBlock `json:"cloud,omitempty"`
}

// UISpec defines Web UI configuration
type UISpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Resource limits for UI pods
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// RestoreSpec defines restore configuration
type RestoreSpec struct {
	// Backup reference to restore from
	BackupRef string `json:"backupRef,omitempty"`

	// Direct storage location
	Storage *StorageLocation `json:"storage,omitempty"`

	// Point in time for restore
	PointInTime *metav1.Time `json:"pointInTime,omitempty"`
}

// StorageLocation defines storage location for backups
type StorageLocation struct {
	// +kubebuilder:validation:Enum=s3;gcs;azure;pvc
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	Bucket string `json:"bucket,omitempty"`

	Path string `json:"path,omitempty"`

	// PVC configuration
	PVC *PVCSpec `json:"pvc,omitempty"`

	// Cloud provider configuration
	Cloud *CloudBlock `json:"cloud,omitempty"`
}

// PVCSpec defines PVC configuration for backups
type PVCSpec struct {
	// Name of the PVC to use (for referencing existing PVCs)
	Name string `json:"name,omitempty"`

	StorageClassName string `json:"storageClassName,omitempty"`
	Size             string `json:"size,omitempty"`
}

// CloudBlock defines cloud provider configuration
type CloudBlock struct {
	// +kubebuilder:validation:Enum=aws;gcp;azure
	Provider string `json:"provider,omitempty"`

	Identity *CloudIdentity `json:"identity,omitempty"`
}

// CloudIdentity defines cloud identity configuration
type CloudIdentity struct {
	// +kubebuilder:validation:Enum=aws;gcp;azure
	// +kubebuilder:validation:Required
	Provider string `json:"provider"`

	ServiceAccount string `json:"serviceAccount,omitempty"`

	AutoCreate *AutoCreateSpec `json:"autoCreate,omitempty"`
}

// AutoCreateSpec defines auto-creation of service accounts
type AutoCreateSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	Annotations map[string]string `json:"annotations,omitempty"`
}

// ResourceRequirements defines resource requirements
type ResourceRequirements struct {
	Limits   map[string]string `json:"limits,omitempty"`
	Requests map[string]string `json:"requests,omitempty"`
}

// Neo4jEnterpriseClusterStatus defines the observed state of Neo4jEnterpriseCluster
type Neo4jEnterpriseClusterStatus struct {
	// Conditions represent the current state of the cluster
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Replicas shows the current number of replicas
	Replicas *ReplicaStatus `json:"replicas,omitempty"`

	// Phase represents the current phase of the cluster
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current state
	Message string `json:"message,omitempty"`

	// Endpoints provides connection information
	Endpoints *EndpointStatus `json:"endpoints,omitempty"`

	// Version shows the current Neo4j version
	Version string `json:"version,omitempty"`

	// LastUpgradeTime shows when the last upgrade was performed
	LastUpgradeTime *metav1.Time `json:"lastUpgradeTime,omitempty"`

	// UpgradeStatus provides detailed upgrade progress information
	UpgradeStatus *UpgradeStatus `json:"upgradeStatus,omitempty"`

	// ScalingStatus provides detailed scaling operation progress
	ScalingStatus *ScalingStatus `json:"scalingStatus,omitempty"`
}

// UpgradeStatus tracks the progress of an ongoing upgrade
type UpgradeStatus struct {
	// Phase represents the current phase of the upgrade
	// +kubebuilder:validation:Enum=Pending;InProgress;Paused;Completed;Failed
	Phase string `json:"phase,omitempty"`

	// StartTime shows when the upgrade started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime shows when the upgrade completed
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// CurrentStep describes the current upgrade step
	CurrentStep string `json:"currentStep,omitempty"`

	// PreviousVersion shows the version before upgrade
	PreviousVersion string `json:"previousVersion,omitempty"`

	// TargetVersion shows the version being upgraded to
	TargetVersion string `json:"targetVersion,omitempty"`

	// Progress shows upgrade progress statistics
	Progress *UpgradeProgress `json:"progress,omitempty"`

	// Message provides additional details about the upgrade
	Message string `json:"message,omitempty"`

	// LastError contains the last error encountered during upgrade
	LastError string `json:"lastError,omitempty"`
}

// UpgradeProgress tracks upgrade progress across different node types
type UpgradeProgress struct {
	// Total number of nodes to upgrade
	Total int32 `json:"total,omitempty"`

	// Number of nodes successfully upgraded
	Upgraded int32 `json:"upgraded,omitempty"`

	// Number of nodes currently being upgraded
	InProgress int32 `json:"inProgress,omitempty"`

	// Number of nodes pending upgrade
	Pending int32 `json:"pending,omitempty"`

	// Primary nodes upgrade progress
	Primaries *NodeUpgradeProgress `json:"primaries,omitempty"`

	// Secondary nodes upgrade progress
	Secondaries *NodeUpgradeProgress `json:"secondaries,omitempty"`
}

// NodeUpgradeProgress tracks upgrade progress for a specific node type
type NodeUpgradeProgress struct {
	// Total number of nodes of this type
	Total int32 `json:"total,omitempty"`

	// Number of nodes successfully upgraded
	Upgraded int32 `json:"upgraded,omitempty"`

	// Number of nodes currently being upgraded
	InProgress int32 `json:"inProgress,omitempty"`

	// Number of nodes pending upgrade
	Pending int32 `json:"pending,omitempty"`

	// Current leader node (for primaries)
	CurrentLeader string `json:"currentLeader,omitempty"`
}

// ReplicaStatus shows replica information
type ReplicaStatus struct {
	Primaries   int32 `json:"primaries,omitempty"`
	Secondaries int32 `json:"secondaries,omitempty"`
	Ready       int32 `json:"ready,omitempty"`
}

// EndpointStatus provides connection endpoints
type EndpointStatus struct {
	// Bolt protocol endpoint
	Bolt string `json:"bolt,omitempty"`

	// HTTP endpoint
	HTTP string `json:"http,omitempty"`

	// HTTPS endpoint
	HTTPS string `json:"https,omitempty"`

	// Internal service endpoints
	Internal *InternalEndpoints `json:"internal,omitempty"`
}

// InternalEndpoints provides internal service endpoints
type InternalEndpoints struct {
	Headless string `json:"headless,omitempty"`
	Client   string `json:"client,omitempty"`
}

// UpgradeStrategySpec defines upgrade strategy configuration
type UpgradeStrategySpec struct {
	// Strategy specifies the upgrade strategy
	// +kubebuilder:validation:Enum=RollingUpgrade;Recreate
	// +kubebuilder:default:=RollingUpgrade
	Strategy string `json:"strategy,omitempty"`

	// PreUpgradeHealthCheck enables cluster health validation before upgrade
	// +kubebuilder:default=true
	PreUpgradeHealthCheck bool `json:"preUpgradeHealthCheck,omitempty"`

	// MaxUnavailableDuringUpgrade specifies max unavailable replicas during upgrade
	// +kubebuilder:default=1
	MaxUnavailableDuringUpgrade *int32 `json:"maxUnavailableDuringUpgrade,omitempty"`

	// UpgradeTimeout specifies timeout for the entire upgrade process
	// +kubebuilder:default="30m"
	UpgradeTimeout string `json:"upgradeTimeout,omitempty"`

	// PostUpgradeHealthCheck enables cluster health validation after upgrade
	// +kubebuilder:default=true
	PostUpgradeHealthCheck bool `json:"postUpgradeHealthCheck,omitempty"`

	// HealthCheckTimeout specifies timeout for health checks
	// +kubebuilder:default="5m"
	HealthCheckTimeout string `json:"healthCheckTimeout,omitempty"`

	// StabilizationTimeout specifies how long to wait for cluster stabilization
	// +kubebuilder:default="3m"
	StabilizationTimeout string `json:"stabilizationTimeout,omitempty"`

	// AutoPauseOnFailure pauses upgrade on failure for manual intervention
	// +kubebuilder:default=true
	AutoPauseOnFailure bool `json:"autoPauseOnFailure,omitempty"`
}

// TopologySpreadConfig defines how to distribute Neo4j instances across cluster topology
type TopologySpreadConfig struct {
	// Enabled indicates whether topology spread constraints should be applied
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// TopologyKey specifies the topology domain (e.g., "topology.kubernetes.io/zone")
	// +optional
	TopologyKey string `json:"topologyKey,omitempty"`

	// MaxSkew describes the degree to which instances may be unevenly distributed
	// +optional
	MaxSkew int32 `json:"maxSkew,omitempty"`

	// WhenUnsatisfiable indicates how to deal with a Pod if it doesn't satisfy the spread constraint
	// +optional
	WhenUnsatisfiable string `json:"whenUnsatisfiable,omitempty"`

	// MinDomains indicates a minimum number of eligible domains
	// +optional
	MinDomains *int32 `json:"minDomains,omitempty"`
}

// PlacementConfig defines advanced placement and scheduling configuration
type PlacementConfig struct {
	// TopologySpread configures topology spread constraints
	// +optional
	TopologySpread *TopologySpreadConfig `json:"topologySpread,omitempty"`

	// AntiAffinity configures pod anti-affinity rules
	// +optional
	AntiAffinity *PodAntiAffinityConfig `json:"antiAffinity,omitempty"`

	// NodeSelector specifies node selection constraints
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// RequiredDuringScheduling indicates hard placement requirements
	// +optional
	RequiredDuringScheduling bool `json:"requiredDuringScheduling,omitempty"`
}

// PodAntiAffinityConfig defines anti-affinity configuration
type PodAntiAffinityConfig struct {
	// Enabled indicates whether anti-affinity should be applied
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// TopologyKey specifies the anti-affinity topology domain
	// +optional
	TopologyKey string `json:"topologyKey,omitempty"`

	// Type specifies whether anti-affinity is required or preferred
	// +optional
	Type string `json:"type,omitempty"` // "required" or "preferred"
}

// TopologyConfiguration defines cluster topology requirements
type TopologyConfiguration struct {
	// Primaries specifies the number of primary (core) servers
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=7
	// Note: Odd numbers (1,3,5,7) are recommended for optimal fault tolerance
	Primaries int32 `json:"primaries"`

	// Secondaries specifies the number of secondary (read replica) servers
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20
	// +optional
	Secondaries int32 `json:"secondaries,omitempty"`

	// Placement defines how instances should be distributed across the cluster
	// +optional
	Placement *PlacementConfig `json:"placement,omitempty"`

	// AvailabilityZones specifies the expected availability zones for distribution
	// +optional
	AvailabilityZones []string `json:"availabilityZones,omitempty"`

	// EnforceDistribution ensures primaries are distributed across topology domains
	// +optional
	EnforceDistribution bool `json:"enforceDistribution,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Primaries",type=integer,JSONPath=`.spec.topology.primaries`
// +kubebuilder:printcolumn:name="Secondaries",type=integer,JSONPath=`.spec.topology.secondaries`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jEnterpriseCluster is the Schema for the neo4jenterpriseclusters API
type Neo4jEnterpriseCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jEnterpriseClusterSpec   `json:"spec,omitempty"`
	Status Neo4jEnterpriseClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jEnterpriseClusterList contains a list of Neo4jEnterpriseCluster
type Neo4jEnterpriseClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jEnterpriseCluster `json:"items"`
}

// AutoScalingSpec defines auto-scaling configuration for both primaries and secondaries
type AutoScalingSpec struct {
	// Enable auto-scaling
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Primary node auto-scaling configuration
	Primaries *PrimaryAutoScalingConfig `json:"primaries,omitempty"`

	// Secondary node auto-scaling configuration
	Secondaries *SecondaryAutoScalingConfig `json:"secondaries,omitempty"`

	// Global scaling behavior configuration
	Behavior *GlobalScalingBehavior `json:"behavior,omitempty"`

	// Advanced scaling features
	Advanced *AdvancedScalingConfig `json:"advanced,omitempty"`
}

// PrimaryAutoScalingConfig defines auto-scaling for primary nodes
type PrimaryAutoScalingConfig struct {
	// Enable primary auto-scaling
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Minimum number of primary replicas (must be odd for quorum)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	MinReplicas int32 `json:"minReplicas,omitempty"`

	// Maximum number of primary replicas (must be odd for quorum)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=7
	MaxReplicas int32 `json:"maxReplicas,omitempty"`

	// Scaling metrics for primary nodes
	Metrics []AutoScalingMetric `json:"metrics,omitempty"`

	// Allow breaking quorum requirements (emergency use only)
	// +kubebuilder:default=false
	AllowQuorumBreak bool `json:"allowQuorumBreak,omitempty"`

	// Quorum-aware scaling behavior
	QuorumProtection *QuorumProtectionConfig `json:"quorumProtection,omitempty"`
}

// SecondaryAutoScalingConfig defines auto-scaling for secondary nodes
type SecondaryAutoScalingConfig struct {
	// Enable secondary auto-scaling
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Minimum number of secondary replicas
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	MinReplicas int32 `json:"minReplicas,omitempty"`

	// Maximum number of secondary replicas
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=20
	MaxReplicas int32 `json:"maxReplicas,omitempty"`

	// Scaling metrics for secondary nodes
	Metrics []AutoScalingMetric `json:"metrics,omitempty"`

	// Zone-aware scaling
	ZoneAware *ZoneAwareScalingConfig `json:"zoneAware,omitempty"`
}

// AutoScalingMetric defines a metric for auto-scaling decisions
type AutoScalingMetric struct {
	// +kubebuilder:validation:Enum=cpu;memory;query_latency;connection_count;throughput;custom
	// Metric type
	Type string `json:"type"`

	// Target value for the metric
	Target string `json:"target"`

	// Weight of this metric in scaling decisions as string (e.g., "1.0", "2.5")
	// +kubebuilder:default="1.0"
	Weight string `json:"weight,omitempty"`

	// Custom metric query (for Prometheus)
	CustomQuery string `json:"customQuery,omitempty"`

	// Metric source configuration
	Source *MetricSourceConfig `json:"source,omitempty"`
}

// MetricSourceConfig defines the source of a metric
type MetricSourceConfig struct {
	// +kubebuilder:validation:Enum=kubernetes;prometheus;neo4j
	// +kubebuilder:default=kubernetes
	Type string `json:"type,omitempty"`

	// Prometheus configuration
	Prometheus *PrometheusMetricConfig `json:"prometheus,omitempty"`

	// Neo4j metrics configuration
	Neo4j *Neo4jMetricConfig `json:"neo4j,omitempty"`
}

// PrometheusMetricConfig defines Prometheus metric source
type PrometheusMetricConfig struct {
	// Prometheus server URL
	ServerURL string `json:"serverUrl,omitempty"`

	// Metric query
	Query string `json:"query"`

	// Query interval
	// +kubebuilder:default="30s"
	Interval string `json:"interval,omitempty"`
}

// Neo4jMetricConfig defines Neo4j-specific metrics
type Neo4jMetricConfig struct {
	// Cypher query to get metric value
	CypherQuery string `json:"cypherQuery,omitempty"`

	// JMX bean path
	JMXBean string `json:"jmxBean,omitempty"`

	// Metric name
	MetricName string `json:"metricName"`
}

// QuorumProtectionConfig defines quorum protection settings
type QuorumProtectionConfig struct {
	// Enable quorum protection
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Minimum healthy primaries before blocking scale-down
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	MinHealthyPrimaries int32 `json:"minHealthyPrimaries,omitempty"`

	// Health check configuration
	HealthCheck *QuorumHealthCheckConfig `json:"healthCheck,omitempty"`
}

// QuorumHealthCheckConfig defines health checks for quorum protection
type QuorumHealthCheckConfig struct {
	// Health check interval
	// +kubebuilder:default="30s"
	Interval string `json:"interval,omitempty"`

	// Health check timeout
	// +kubebuilder:default="10s"
	Timeout string `json:"timeout,omitempty"`

	// Failure threshold
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// ZoneAwareScalingConfig defines zone-aware scaling for secondaries
type ZoneAwareScalingConfig struct {
	// Enable zone-aware scaling
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Minimum replicas per zone
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	MinReplicasPerZone int32 `json:"minReplicasPerZone,omitempty"`

	// Maximum zone skew
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	MaxZoneSkew int32 `json:"maxZoneSkew,omitempty"`

	// Zone preference for scaling
	ZonePreference []string `json:"zonePreference,omitempty"`
}

// GlobalScalingBehavior defines global scaling behavior
type GlobalScalingBehavior struct {
	// Scale up behavior
	ScaleUp *ScalingBehaviorConfig `json:"scaleUp,omitempty"`

	// Scale down behavior
	ScaleDown *ScalingBehaviorConfig `json:"scaleDown,omitempty"`

	// Coordination between primary and secondary scaling
	Coordination *ScalingCoordinationConfig `json:"coordination,omitempty"`
}

// ScalingBehaviorConfig defines scaling behavior
type ScalingBehaviorConfig struct {
	// Stabilization window
	// +kubebuilder:default="60s"
	StabilizationWindow string `json:"stabilizationWindow,omitempty"`

	// Scaling policies
	Policies []ScalingPolicy `json:"policies,omitempty"`

	// Policy selection mode
	// +kubebuilder:validation:Enum=Min;Max;Disabled
	// +kubebuilder:default="Max"
	SelectPolicy string `json:"selectPolicy,omitempty"`
}

// ScalingPolicy defines a scaling policy
type ScalingPolicy struct {
	// +kubebuilder:validation:Enum=Pods;Percent
	// Policy type
	Type string `json:"type"`

	// Policy value
	Value int32 `json:"value"`

	// Period for the policy
	// +kubebuilder:default="60s"
	Period string `json:"period,omitempty"`
}

// ScalingCoordinationConfig defines coordination between primary and secondary scaling
type ScalingCoordinationConfig struct {
	// Enable coordination
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Primary scaling priority
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=5
	PrimaryPriority int32 `json:"primaryPriority,omitempty"`

	// Secondary scaling priority
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	SecondaryPriority int32 `json:"secondaryPriority,omitempty"`

	// Delay between primary and secondary scaling
	// +kubebuilder:default="30s"
	ScalingDelay string `json:"scalingDelay,omitempty"`
}

// AdvancedScalingConfig defines advanced scaling features
type AdvancedScalingConfig struct {
	// Predictive scaling
	Predictive *PredictiveScalingConfig `json:"predictive,omitempty"`

	// Custom scaling algorithms
	CustomAlgorithms []CustomScalingAlgorithm `json:"customAlgorithms,omitempty"`

	// Machine learning-based scaling
	MachineLearning *MLScalingConfig `json:"machineLearning,omitempty"`
}

// PredictiveScalingConfig defines predictive scaling
type PredictiveScalingConfig struct {
	// Enable predictive scaling
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Historical data window
	// +kubebuilder:default="7d"
	HistoricalWindow string `json:"historicalWindow,omitempty"`

	// Prediction horizon
	// +kubebuilder:default="1h"
	PredictionHorizon string `json:"predictionHorizon,omitempty"`

	// Confidence threshold as string (e.g., "0.8", "0.95")
	// +kubebuilder:default="0.8"
	ConfidenceThreshold string `json:"confidenceThreshold,omitempty"`
}

// CustomScalingAlgorithm defines a custom scaling algorithm
type CustomScalingAlgorithm struct {
	// Algorithm name
	Name string `json:"name"`

	// Algorithm type
	// +kubebuilder:validation:Enum=webhook;lua;wasm
	Type string `json:"type"`

	// Algorithm configuration
	Config map[string]string `json:"config,omitempty"`

	// Webhook configuration (if type=webhook)
	Webhook *WebhookScalingConfig `json:"webhook,omitempty"`
}

// WebhookScalingConfig defines webhook-based scaling
type WebhookScalingConfig struct {
	// Webhook URL
	URL string `json:"url"`

	// HTTP method
	// +kubebuilder:validation:Enum=GET;POST
	// +kubebuilder:default="POST"
	Method string `json:"method,omitempty"`

	// Headers
	Headers map[string]string `json:"headers,omitempty"`

	// Timeout
	// +kubebuilder:default="30s"
	Timeout string `json:"timeout,omitempty"`
}

// MLScalingConfig defines machine learning-based scaling
type MLScalingConfig struct {
	// Enable ML scaling
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// ML model configuration
	Model *MLModelConfig `json:"model,omitempty"`

	// Feature engineering
	Features []MLFeatureConfig `json:"features,omitempty"`
}

// MLModelConfig defines ML model configuration
type MLModelConfig struct {
	// Model type
	// +kubebuilder:validation:Enum=linear_regression;random_forest;neural_network
	// +kubebuilder:default="linear_regression"
	Type string `json:"type,omitempty"`

	// Model parameters
	Parameters map[string]string `json:"parameters,omitempty"`

	// Training data source
	TrainingDataSource string `json:"trainingDataSource,omitempty"`
}

// MLFeatureConfig defines ML feature configuration
type MLFeatureConfig struct {
	// Feature name
	Name string `json:"name"`

	// Feature type
	// +kubebuilder:validation:Enum=metric;time;categorical
	Type string `json:"type"`

	// Feature source
	Source string `json:"source"`

	// Transformation
	Transformation string `json:"transformation,omitempty"`
}

// PluginSpec defines a plugin configuration
type PluginSpec struct {
	// +kubebuilder:validation:Required
	// Plugin name
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// Plugin version
	Version string `json:"version"`

	// +kubebuilder:default=true
	// Enable the plugin
	Enabled bool `json:"enabled,omitempty"`

	// Plugin configuration
	Config map[string]string `json:"config,omitempty"`

	// Plugin source
	Source *PluginSource `json:"source,omitempty"`
}

// QueryMonitoringSpec defines query performance monitoring
type QueryMonitoringSpec struct {
	// +kubebuilder:default=true
	// Enable query monitoring
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:default="5s"
	// Slow query threshold
	SlowQueryThreshold string `json:"slowQueryThreshold,omitempty"`

	// +kubebuilder:default=true
	// Enable query plan explanation
	ExplainPlan bool `json:"explainPlan,omitempty"`

	// +kubebuilder:default=true
	// Enable index recommendations
	IndexRecommendations bool `json:"indexRecommendations,omitempty"`

	// Query sampling configuration
	Sampling *QuerySamplingConfig `json:"sampling,omitempty"`

	// Metrics export configuration
	MetricsExport *QueryMetricsExportConfig `json:"metricsExport,omitempty"`
}

// QuerySamplingConfig defines query sampling
type QuerySamplingConfig struct {
	// Sampling rate (0.0 to 1.0)
	Rate string `json:"rate,omitempty"`

	// Maximum queries to sample per second
	MaxQueriesPerSecond int32 `json:"maxQueriesPerSecond,omitempty"`
}

// QueryMetricsExportConfig defines metrics export
type QueryMetricsExportConfig struct {
	// Export to Prometheus
	Prometheus bool `json:"prometheus,omitempty"`

	// Export to custom endpoint
	CustomEndpoint string `json:"customEndpoint,omitempty"`

	// Export interval
	Interval string `json:"interval,omitempty"`
}

// ScalingStatus tracks the progress of scaling operations
type ScalingStatus struct {
	// Phase represents the current phase of the scaling operation
	// +kubebuilder:validation:Enum=Pending;InProgress;Completed;Failed;Paused
	Phase string `json:"phase,omitempty"`

	// StartTime is when the scaling operation started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the scaling operation completed
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// CurrentPrimaries shows the current number of primary nodes
	CurrentPrimaries int `json:"currentPrimaries"`

	// CurrentSecondaries shows the current number of secondary nodes
	CurrentSecondaries int `json:"currentSecondaries"`

	// TargetPrimaries shows the target number of primary nodes
	TargetPrimaries int `json:"targetPrimaries"`

	// TargetSecondaries shows the target number of secondary nodes
	TargetSecondaries int `json:"targetSecondaries"`

	// Conditions represent detailed scaling status conditions
	Conditions []ScalingCondition `json:"conditions,omitempty"`

	// LastScaleTime shows when the last successful scaling occurred
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	// Message provides additional information about the scaling operation
	Message string `json:"message,omitempty"`

	// LastError contains the last error message if scaling failed
	LastError string `json:"lastError,omitempty"`

	// Progress shows detailed progress of the scaling operation
	Progress *ScalingProgress `json:"progress,omitempty"`
}

// ScalingCondition represents a condition during scaling operations
type ScalingCondition struct {
	// Type of scaling condition
	// +kubebuilder:validation:Enum=ResourceValidated;PodsScheduled;PodsReady;ClusterFormed;Completed
	Type string `json:"type"`

	// Status of the condition
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status corev1.ConditionStatus `json:"status"`

	// LastTransitionTime is the time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// Reason contains a programmatic identifier indicating the reason for the condition's last transition
	Reason string `json:"reason"`

	// Message is a human-readable message indicating details about the transition
	Message string `json:"message"`

	// LastProbeTime is the time the condition was last probed
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`
}

// ScalingProgress tracks detailed progress of scaling operations
type ScalingProgress struct {
	// TotalSteps is the total number of steps in the scaling operation
	TotalSteps int `json:"totalSteps"`

	// CompletedSteps is the number of completed steps
	CompletedSteps int `json:"completedSteps"`

	// CurrentStep describes what is currently happening
	CurrentStep string `json:"currentStep,omitempty"`

	// PendingPods shows pods that are pending scheduling
	PendingPods []string `json:"pendingPods,omitempty"`

	// RunningPods shows pods that are running but not ready
	RunningPods []string `json:"runningPods,omitempty"`

	// ReadyPods shows pods that are ready
	ReadyPods []string `json:"readyPods,omitempty"`

	// FailedPods shows pods that have failed
	FailedPods []string `json:"failedPods,omitempty"`

	// EstimatedCompletion provides an estimate of when scaling will complete
	EstimatedCompletion *metav1.Time `json:"estimatedCompletion,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Neo4jEnterpriseCluster{}, &Neo4jEnterpriseClusterList{})
}
