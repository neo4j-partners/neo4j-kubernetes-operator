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

	// Plugin management configuration - DEPRECATED: Use Neo4jPlugin CRD instead

	// Query performance monitoring
	QueryMonitoring *QueryMonitoringSpec `json:"queryMonitoring,omitempty"`

	// Property Sharding configuration for Neo4j 2025.06+
	// Enables support for creating sharded databases that separate
	// graph topology from node/relationship properties
	PropertySharding *PropertyShardingSpec `json:"propertySharding,omitempty"`

	// Routing defines load balancing and routing configuration
	// +optional
	Routing *RoutingSpec `json:"routing,omitempty"`
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
	// Service type: ClusterIP, NodePort, LoadBalancer
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default=ClusterIP
	Type string `json:"type,omitempty"`

	// Annotations to add to the service
	Annotations map[string]string `json:"annotations,omitempty"`

	// LoadBalancer specific configuration
	LoadBalancerIP           string   `json:"loadBalancerIP,omitempty"`
	LoadBalancerSourceRanges []string `json:"loadBalancerSourceRanges,omitempty"`

	// External traffic policy: Cluster or Local
	// +kubebuilder:validation:Enum=Cluster;Local
	ExternalTrafficPolicy string `json:"externalTrafficPolicy,omitempty"`

	// Ingress configuration
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

	// PropertyShardingReady indicates whether property sharding is configured and ready
	//
	// This field tracks the operational status of property sharding capability
	// on this cluster. When true, the cluster is ready to host sharded databases
	// that separate graph topology from node/relationship properties.
	//
	// Prerequisites for PropertyShardingReady=true:
	// - Neo4j version >= 2025.06.0
	// - Cluster phase = Ready
	// - Property sharding configuration applied successfully
	// - All required Neo4j configuration settings validated
	PropertyShardingReady *bool `json:"propertyShardingReady,omitempty"`

	// ServerDistribution shows the distribution of servers across zones/regions
	// +optional
	ServerDistribution *ServerDistributionStatus `json:"serverDistribution,omitempty"`

	// DisasterRecoveryCapable indicates if the cluster can survive zone/region failures
	// +optional
	DisasterRecoveryCapable *bool `json:"disasterRecoveryCapable,omitempty"`

	// MinimumQuorumAvailable indicates if the cluster has minimum quorum for writes
	// +optional
	MinimumQuorumAvailable *bool `json:"minimumQuorumAvailable,omitempty"`
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

// UpgradeProgress tracks upgrade progress across servers
type UpgradeProgress struct {
	// Total number of servers to upgrade
	Total int32 `json:"total,omitempty"`

	// Number of servers successfully upgraded
	Upgraded int32 `json:"upgraded,omitempty"`

	// Number of servers currently being upgraded
	InProgress int32 `json:"inProgress,omitempty"`

	// Number of servers pending upgrade
	Pending int32 `json:"pending,omitempty"`

	// Server upgrade details
	Servers *NodeUpgradeProgress `json:"servers,omitempty"`
}

// NodeUpgradeProgress tracks upgrade progress for servers
type NodeUpgradeProgress struct {
	// Total number of servers
	Total int32 `json:"total,omitempty"`

	// Number of servers successfully upgraded
	Upgraded int32 `json:"upgraded,omitempty"`

	// Number of servers currently being upgraded
	InProgress int32 `json:"inProgress,omitempty"`

	// Number of servers pending upgrade
	Pending int32 `json:"pending,omitempty"`

	// Current leader server
	CurrentLeader string `json:"currentLeader,omitempty"`
}

// ReplicaStatus shows replica information
type ReplicaStatus struct {
	Servers int32 `json:"servers,omitempty"`
	Ready   int32 `json:"ready,omitempty"`
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

	// Connection examples for external access
	ConnectionExamples *ConnectionExamples `json:"connectionExamples,omitempty"`
}

// InternalEndpoints provides internal service endpoints
type InternalEndpoints struct {
	Headless string `json:"headless,omitempty"`
	Client   string `json:"client,omitempty"`
}

// ConnectionExamples provides example connection strings
type ConnectionExamples struct {
	// Port forwarding command example
	PortForward string `json:"portForward,omitempty"`

	// Browser URL examples
	BrowserURL string `json:"browserURL,omitempty"`

	// Driver connection examples
	BoltURI  string `json:"boltURI,omitempty"`
	Neo4jURI string `json:"neo4jURI,omitempty"`

	// Python driver example
	PythonExample string `json:"pythonExample,omitempty"`

	// Java driver example
	JavaExample string `json:"javaExample,omitempty"`
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
	// Servers specifies the number of Neo4j servers in the cluster
	// Servers self-organize and can host databases in primary or secondary mode
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=20
	Servers int32 `json:"servers"`

	// ServerModeConstraint optionally constrains all servers to a specific mode
	// Valid values: "PRIMARY", "SECONDARY", "NONE" (default: "NONE")
	// +kubebuilder:validation:Enum=NONE;PRIMARY;SECONDARY
	// +kubebuilder:default=NONE
	// +optional
	ServerModeConstraint string `json:"serverModeConstraint,omitempty"`

	// ServerRoles allows specifying role constraints for individual servers
	// Takes precedence over ServerModeConstraint for specified servers
	// +optional
	ServerRoles []ServerRoleHint `json:"serverRoles,omitempty"`

	// ServerGroups allows organizing servers into logical groups for multi-region deployment
	// Each group can have its own placement, tags, and node selectors
	// +optional
	ServerGroups []ServerGroup `json:"serverGroups,omitempty"`

	// Placement defines how instances should be distributed across the cluster
	// +optional
	Placement *PlacementConfig `json:"placement,omitempty"`

	// AvailabilityZones specifies the expected availability zones for distribution
	// +optional
	AvailabilityZones []string `json:"availabilityZones,omitempty"`

	// EnforceDistribution ensures servers are distributed across topology domains
	// +optional
	EnforceDistribution bool `json:"enforceDistribution,omitempty"`

	// AntiAffinity configures anti-affinity rules for server distribution
	// +optional
	AntiAffinity *AntiAffinitySpec `json:"antiAffinity,omitempty"`
}

// ServerRoleHint specifies a preferred role constraint for a specific server
type ServerRoleHint struct {
	// ServerIndex specifies which server this role hint applies to (0-based)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	ServerIndex int32 `json:"serverIndex"`

	// ModeConstraint specifies the preferred role constraint for this server
	// Valid values: "PRIMARY", "SECONDARY", "NONE"
	// - PRIMARY: Server should only host databases in primary mode
	// - SECONDARY: Server should only host databases in secondary mode
	// - NONE: Server can host databases in any mode (default)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=NONE;PRIMARY;SECONDARY
	ModeConstraint string `json:"modeConstraint"`
}

// ServerGroup defines a logical group of servers with shared configuration
type ServerGroup struct {
	// Name identifies this server group
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Count specifies the number of servers in this group
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	Count int32 `json:"count"`

	// ServerTags defines Neo4j server tags for servers in this group
	// Used for routing and load balancing policies
	// +optional
	ServerTags []string `json:"serverTags,omitempty"`

	// NodeSelector specifies Kubernetes node selection constraints for this group
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// RoleHint suggests a preferred role for servers in this group
	// Valid values: "PRIMARY_PREFERRED", "SECONDARY_PREFERRED", "NONE"
	// +kubebuilder:validation:Enum=NONE;PRIMARY_PREFERRED;SECONDARY_PREFERRED
	// +kubebuilder:default=NONE
	// +optional
	RoleHint string `json:"roleHint,omitempty"`

	// Resources allows overriding resource requirements for this group
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Tolerations allows overriding tolerations for this group
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// AntiAffinitySpec defines anti-affinity configuration for server distribution
type AntiAffinitySpec struct {
	// Type specifies the anti-affinity type: zone, region, or node
	// +kubebuilder:validation:Enum=zone;region;node
	// +kubebuilder:default=zone
	Type string `json:"type"`

	// TopologyKey specifies the Kubernetes topology key to use
	// Defaults based on type: zone -> topology.kubernetes.io/zone
	// +optional
	TopologyKey string `json:"topologyKey,omitempty"`

	// Required indicates whether anti-affinity is required (hard) or preferred (soft)
	// +kubebuilder:default=false
	// +optional
	Required bool `json:"required,omitempty"`

	// Weight specifies the weight for preferred anti-affinity (1-100)
	// Only used when Required=false
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	// +optional
	Weight int32 `json:"weight,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Servers",type=integer,JSONPath=`.spec.topology.servers`
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

// DEPRECATED: PluginSpec is deprecated. Use Neo4jPlugin CRD instead.
// This type is kept for backward compatibility but will be removed in future versions.

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

// PropertyShardingSpec defines property sharding configuration
// for Neo4j 2025.06+ to enable separated storage of graph topology and properties
type PropertyShardingSpec struct {
	// Enable property sharding support on this cluster
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Property sharding specific Neo4j configuration
	// Applied when propertySharding.enabled is true
	// These settings are required for Neo4j property sharding functionality
	Config map[string]string `json:"config,omitempty"`
}

// RoutingSpec defines load balancing and routing configuration
type RoutingSpec struct {
	// LoadBalancingPolicy specifies the load balancing plugin to use
	// +kubebuilder:validation:Enum=server_policies;client_policies;least_connected
	// +kubebuilder:default=server_policies
	// +optional
	LoadBalancingPolicy string `json:"loadBalancingPolicy,omitempty"`

	// Policies defines named routing policies using Neo4j DSL
	// Key is the policy name, value is the DSL expression
	// Example: "tags(primary-dc)->min(2);tags(secondary-dc);all();"
	// +optional
	Policies map[string]string `json:"policies,omitempty"`

	// DefaultPolicy specifies which policy to use as default
	// +optional
	DefaultPolicy string `json:"defaultPolicy,omitempty"`

	// DatabasePolicies maps databases to specific routing policies
	// +optional
	DatabasePolicies []DatabaseRoutingPolicy `json:"databasePolicies,omitempty"`

	// CatchupStrategy defines how secondaries synchronize with primaries
	// +kubebuilder:validation:Enum=CONNECT_RANDOMLY_TO_PRIMARY_SERVER;CONNECT_RANDOMLY_TO_SECONDARY_SERVER;USER_DEFINED
	// +kubebuilder:default=CONNECT_RANDOMLY_TO_PRIMARY_SERVER
	// +optional
	CatchupStrategy string `json:"catchupStrategy,omitempty"`

	// UserDefinedCatchupStrategy provides custom catchup configuration
	// Used when CatchupStrategy is USER_DEFINED
	// +optional
	UserDefinedCatchupStrategy string `json:"userDefinedCatchupStrategy,omitempty"`
}

// DatabaseRoutingPolicy maps a database to a routing policy
type DatabaseRoutingPolicy struct {
	// Database name or pattern (supports wildcards)
	// +kubebuilder:validation:Required
	Database string `json:"database"`

	// Policy name to use for this database
	// +kubebuilder:validation:Required
	Policy string `json:"policy"`
}

// ServerDistributionStatus shows server distribution across zones/regions
type ServerDistributionStatus struct {
	// Zones maps zone names to server distribution info
	// +optional
	Zones map[string]*ZoneDistribution `json:"zones,omitempty"`

	// Regions maps region names to server distribution info
	// +optional
	Regions map[string]*RegionDistribution `json:"regions,omitempty"`

	// TotalServers is the total number of servers in the cluster
	TotalServers int32 `json:"totalServers,omitempty"`

	// HealthyServers is the number of healthy servers
	HealthyServers int32 `json:"healthyServers,omitempty"`

	// DistributionBalance indicates if servers are evenly distributed
	// +optional
	DistributionBalance string `json:"distributionBalance,omitempty"`
}

// ZoneDistribution shows server distribution in a zone
type ZoneDistribution struct {
	// Servers is the number of servers in this zone
	Servers int32 `json:"servers"`

	// Healthy is the number of healthy servers in this zone
	Healthy int32 `json:"healthy"`

	// Primaries is the number of primary databases hosted
	Primaries int32 `json:"primaries,omitempty"`

	// Secondaries is the number of secondary databases hosted
	Secondaries int32 `json:"secondaries,omitempty"`

	// ServerNames lists the server pod names in this zone
	ServerNames []string `json:"serverNames,omitempty"`
}

// RegionDistribution shows server distribution in a region
type RegionDistribution struct {
	// Servers is the number of servers in this region
	Servers int32 `json:"servers"`

	// Healthy is the number of healthy servers in this region
	Healthy int32 `json:"healthy"`

	// Zones lists the zones in this region
	Zones []string `json:"zones,omitempty"`

	// Primaries is the number of primary databases hosted
	Primaries int32 `json:"primaries,omitempty"`

	// Secondaries is the number of secondary databases hosted
	Secondaries int32 `json:"secondaries,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Neo4jEnterpriseCluster{}, &Neo4jEnterpriseClusterList{})
}
