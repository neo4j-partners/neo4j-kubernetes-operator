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
	"k8s.io/apimachinery/pkg/util/intstr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4j edition (V1: enterprise only).
// +kubebuilder:validation:Enum=enterprise
type Edition string

const EditionEnterprise Edition = "enterprise"

// License acceptance.
// +kubebuilder:validation:Enum=yes;eval
type LicenseAccept string

const (
	LicenseAcceptYes  LicenseAccept = "yes"
	LicenseAcceptEval LicenseAccept = "eval"
)

// TopologyMode is Standalone or Cluster (immutable after create).
// +kubebuilder:validation:Enum=Standalone;Cluster
type TopologyMode string

const (
	TopologyModeStandalone TopologyMode = "Standalone"
	TopologyModeCluster    TopologyMode = "Cluster"
)

// PluginCatalogID is a V1 plugin catalog identifier.
// +kubebuilder:validation:Enum=apoc;gds;bloom
type PluginCatalogID string

const (
	PluginAPOC  PluginCatalogID = "apoc"
	PluginGDS   PluginCatalogID = "gds"
	PluginBloom PluginCatalogID = "bloom"
)

// VolumeMode for data and auxiliary volumes.
// +kubebuilder:validation:Enum=Dynamic;Existing;Share
type VolumeMode string

const (
	VolumeModeDynamic  VolumeMode = "Dynamic"
	VolumeModeExisting VolumeMode = "Existing"
	VolumeModeShare    VolumeMode = "Share"
)

// ShareFrom source for Share mode auxiliary volumes (V1: data only).
// +kubebuilder:validation:Enum=data
type ShareFrom string

const ShareFromData ShareFrom = "data"

// ServiceType for client connectivity service.
// +kubebuilder:validation:Enum=ClusterIP;LoadBalancer;NodePort
type ServiceType string

// TLSClientAuth maps to dbms.ssl.policy.{policy}.client_auth.
// +kubebuilder:validation:Enum=None;Optional;Require
type TLSClientAuth string

// PodAntiAffinityPreset for scheduling.
// +kubebuilder:validation:Enum=soft;hard;custom
type PodAntiAffinityPreset string

// IngressPathType for connectivity.ingress rules.
// +kubebuilder:validation:Enum=Prefix;Exact;ImplementationSpecific
type IngressPathType string

// IngressBackend selects client Service or reverse proxy.
// +kubebuilder:validation:Enum=service;reverseProxy
type IngressBackend string

// Neo4jPhase is coarse lifecycle phase (status).
// +kubebuilder:validation:Enum=Pending;Provisioning;Bootstrapping;Running;Degraded;Failed;Maintenance
type Neo4jPhase string

const (
	Neo4jPhasePending       Neo4jPhase = "Pending"
	Neo4jPhaseProvisioning  Neo4jPhase = "Provisioning"
	Neo4jPhaseBootstrapping Neo4jPhase = "Bootstrapping"
	Neo4jPhaseRunning       Neo4jPhase = "Running"
	Neo4jPhaseDegraded      Neo4jPhase = "Degraded"
	Neo4jPhaseFailed        Neo4jPhase = "Failed"
	Neo4jPhaseMaintenance   Neo4jPhase = "Maintenance"
)

// UpgradePhase tracks rolling upgrade state.
// +kubebuilder:validation:Enum=Staging;Rolling;Stabilizing;Verifying;Completed;Failed
type UpgradePhase string

// MemberPool identifies a server pool in cluster mode.
// +kubebuilder:validation:Enum=primary;analytics;read;server
type MemberPool string

// LicenseSpec holds Enterprise license acceptance.
type LicenseSpec struct {
	// Accept must be "yes" for V1 production use.
	// +kubebuilder:validation:Required
	Accept LicenseAccept `json:"accept"`
}

// ImageSpec overrides container image repository and pull policy.
type ImageSpec struct {
	// +kubebuilder:default="neo4j"
	Repository string `json:"repository,omitempty"`
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
	PullSecrets  []string          `json:"pullSecrets,omitempty"`
}

// PasswordSecretRef references an existing auth Secret (key NEO4J_AUTH).
type PasswordSecretRef struct {
	Name string `json:"name"`
}

// LDAPSpec configures LDAP authentication (V2 — fields present but optional).
type LDAPSpec struct {
	Enabled             bool               `json:"enabled,omitempty"`
	PasswordSecretRef   *PasswordSecretRef `json:"passwordSecretRef,omitempty"`
}

// AuthSpec configures Neo4j authentication.
type AuthSpec struct {
	GeneratePassword  *bool              `json:"generatePassword,omitempty"`
	PasswordSecretRef *PasswordSecretRef `json:"passwordSecretRef,omitempty"`
	LDAP              *LDAPSpec          `json:"ldap,omitempty"`
}

// DynamicVolumeSpec configures dynamically provisioned PVCs.
type DynamicVolumeSpec struct {
	// +kubebuilder:validation:Pattern=`^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$`
	Size             string `json:"size,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	// +kubebuilder:validation:Enum=ReadWriteOnce
	// +kubebuilder:default=ReadWriteOnce
	AccessMode string            `json:"accessMode,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// ExistingVolumeSpec binds pre-provisioned storage (exactly one source when mode is Existing).
type ExistingVolumeSpec struct {
	ClaimName           string                            `json:"claimName,omitempty"`
	Volume              *corev1.Volume                    `json:"volume,omitempty"`
	VolumeClaimTemplate *corev1.PersistentVolumeClaimSpec `json:"volumeClaimTemplate,omitempty"`
}

// DataVolumeSpec is required persistence for Neo4j data.
type DataVolumeSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Dynamic;Existing
	Mode VolumeMode `json:"mode"`
	Dynamic *DynamicVolumeSpec `json:"dynamic,omitempty"`
	Existing *ExistingVolumeSpec `json:"existing,omitempty"`
	DisableSubPathExpr bool `json:"disableSubPathExpr,omitempty"`
}

// AuxiliaryVolumeSpec configures backups, logs, metrics, import, or licenses volumes.
type AuxiliaryVolumeSpec struct {
	// +kubebuilder:validation:Enum=Share;Dynamic;Existing
	Mode VolumeMode `json:"mode,omitempty"`
	// +kubebuilder:validation:Enum=data
	ShareFrom *ShareFrom `json:"shareFrom,omitempty"`
	Dynamic   *DynamicVolumeSpec  `json:"dynamic,omitempty"`
	Existing  *ExistingVolumeSpec `json:"existing,omitempty"`
}

// VolumesSpec mirrors Helm values.yaml volumes block (BDR-005).
type VolumesSpec struct {
	// +kubebuilder:validation:Required
	Data DataVolumeSpec `json:"data"`
	Backups  *AuxiliaryVolumeSpec `json:"backups,omitempty"`
	Logs     *AuxiliaryVolumeSpec `json:"logs,omitempty"`
	Metrics  *AuxiliaryVolumeSpec `json:"metrics,omitempty"`
	Import   *AuxiliaryVolumeSpec `json:"import,omitempty"`
	Licenses *AuxiliaryVolumeSpec `json:"licenses,omitempty"`
}

// AdditionalMount pairs volume source with mount (BDR-005 Option E).
type AdditionalMount struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	Volume corev1.Volume `json:"volume"`
	// +kubebuilder:validation:Required
	MountPath string `json:"mountPath"`
	SubPath   string `json:"subPath,omitempty"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

// SecretKeyToPath maps Secret keys to paths.
type SecretKeyToPath struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

// SecretMountSpec projects a Secret into the Neo4j container.
type SecretMountSpec struct {
	SecretName  string            `json:"secretName"`
	MountPath   string            `json:"mountPath"`
	Items       []SecretKeyToPath `json:"items,omitempty"`
	DefaultMode *int32            `json:"defaultMode,omitempty"`
}

// StorageSpec groups persistence volumes and pod-level mount configuration (BDR-005).
// +kubebuilder:validation:XValidation:rule="!has(self.volumes) || !has(self.volumes.data) || self.volumes.data.mode != 'Dynamic' || (has(self.volumes.data.dynamic) && has(self.volumes.data.dynamic.size) && self.volumes.data.dynamic.size != '')",message="data volume size is required when mode is Dynamic"
// +kubebuilder:validation:XValidation:rule="!has(self.volumes) || !has(self.volumes.data) || self.volumes.data.mode != 'Share'",message="data volume cannot use Share mode"
type StorageSpec struct {
	Volumes          *VolumesSpec               `json:"volumes,omitempty"`
	AdditionalMounts []AdditionalMount          `json:"additionalMounts,omitempty"`
	SecretMounts     map[string]SecretMountSpec `json:"secretMounts,omitempty"`
}

// ConfigSpec groups Neo4j configuration files and JVM settings (BDR-008).
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.jvm.additional' in self.neo4j)",message="use spec.config.jvm.additionalArguments instead of server.jvm.additional"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.http.listen_address' in self.neo4j)",message="server.http.listen_address is owned by connectivity.listeners.http"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.http.enabled' in self.neo4j)",message="server.http.enabled is owned by connectivity.listeners.http"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.bolt.listen_address' in self.neo4j)",message="server.bolt.listen_address is owned by connectivity.listeners.bolt"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.bolt.enabled' in self.neo4j)",message="server.bolt.enabled is owned by connectivity.listeners.bolt"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.https.listen_address' in self.neo4j)",message="server.https.listen_address is owned by connectivity.listeners.https"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.https.enabled' in self.neo4j)",message="server.https.enabled is owned by connectivity.listeners.https"
// +kubebuilder:validation:XValidation:rule="!has(self.neo4j) || !('server.backup.listen_address' in self.neo4j)",message="server.backup.listen_address is owned by connectivity.listeners.backup"
type ConfigSpec struct {
	JVM   *JVMSpec          `json:"jvm,omitempty"`
	Neo4j map[string]string `json:"neo4j,omitempty"`
	Apoc  map[string]string `json:"apoc,omitempty"`
}

// JVMSpec configures JVM flags (BDR-008).
type JVMSpec struct {
	// +kubebuilder:default=true
	UseDefaults *bool `json:"useDefaults,omitempty"`
	// +kubebuilder:validation:MaxItems=64
	AdditionalArguments []string `json:"additionalArguments,omitempty"`
}

// BackupFeatureSpec enables online backup (BDR-010).
type BackupFeatureSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

// PrometheusMonitoringSpec configures Prometheus metrics export.
type PrometheusMonitoringSpec struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// CSVRotationSpec configures CSV metrics rotation.
type CSVRotationSpec struct {
	KeepNumber   *int   `json:"keepNumber,omitempty"`
	Size         string `json:"size,omitempty"`
	// +kubebuilder:validation:Enum=NONE;ZIP;GZ
	Compression string `json:"compression,omitempty"`
}

// CSVMonitoringSpec configures CSV metrics export.
type CSVMonitoringSpec struct {
	Enabled  bool             `json:"enabled,omitempty"`
	Interval string           `json:"interval,omitempty"`
	Rotation *CSVRotationSpec `json:"rotation,omitempty"`
}

// JMXMonitoringSpec configures JMX metrics.
type JMXMonitoringSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

// GraphiteMonitoringSpec configures Graphite metrics export.
type GraphiteMonitoringSpec struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Server   string `json:"server,omitempty"`
	Interval string `json:"interval,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
}

// ServiceMonitorSpec configures Prometheus Operator ServiceMonitor CR.
type ServiceMonitorSpec struct {
	Enabled           bool              `json:"enabled,omitempty"`
	Interval          string            `json:"interval,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	JobLabel          string            `json:"jobLabel,omitempty"`
	Port              string            `json:"port,omitempty"`
	Path              string            `json:"path,omitempty"`
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	TargetLabels      []string          `json:"targetLabels,omitempty"`
	Selector          *metav1.LabelSelector `json:"selector,omitempty"`
}

// MonitoringFeaturesSpec groups observability feature toggles (BDR-010 Option C).
type MonitoringFeaturesSpec struct {
	Prometheus     *PrometheusMonitoringSpec `json:"prometheus,omitempty"`
	CSV            *CSVMonitoringSpec        `json:"csv,omitempty"`
	JMX            *JMXMonitoringSpec        `json:"jmx,omitempty"`
	Graphite       *GraphiteMonitoringSpec   `json:"graphite,omitempty"`
	ServiceMonitor *ServiceMonitorSpec       `json:"serviceMonitor,omitempty"`
}

// FeaturesSpec optional workload capabilities (BDR-007, BDR-010).
type FeaturesSpec struct {
	Backup     *BackupFeatureSpec      `json:"backup,omitempty"`
	Monitoring *MonitoringFeaturesSpec `json:"monitoring,omitempty"`
}

// ConnectivityListenersSpec defines Neo4j listen ports (connector present ⇒ enabled).
type ConnectivityListenersSpec struct {
	Bolt    *int32 `json:"bolt,omitempty"`
	HTTP    *int32 `json:"http,omitempty"`
	HTTPS   *int32 `json:"https,omitempty"`
	Backup  *int32 `json:"backup,omitempty"`
	Metrics *int32 `json:"metrics,omitempty"`
}

// ServicePortsSpec optional Service façade ports (targetPort from listeners).
type ServicePortsSpec struct {
	Bolt    *int32 `json:"bolt,omitempty"`
	HTTP    *int32 `json:"http,omitempty"`
	HTTPS   *int32 `json:"https,omitempty"`
	Backup  *int32 `json:"backup,omitempty"`
	Metrics *int32 `json:"metrics,omitempty"`
}

// ConnectivityServiceSpec configures the client Kubernetes Service.
type ConnectivityServiceSpec struct {
	// +kubebuilder:validation:Enum=ClusterIP;LoadBalancer;NodePort
	// +kubebuilder:default=ClusterIP
	Type                     ServiceType       `json:"type,omitempty"`
	Annotations              map[string]string `json:"annotations,omitempty"`
	LoadBalancerSourceRanges []string          `json:"loadBalancerSourceRanges,omitempty"`
	Expose                   []string          `json:"expose,omitempty"`
	Ports                    *ServicePortsSpec `json:"ports,omitempty"`
}

// ReverseProxyServiceSpec configures the reverse-proxy front Service.
type ReverseProxyServiceSpec struct {
	Type  ServiceType       `json:"type,omitempty"`
	Ports *ServicePortsSpec `json:"ports,omitempty"`
}

// ReverseProxySpec optional HTTP/Bolt-ws front door (V1.1+ — schema present, default off).
type ReverseProxySpec struct {
	Enabled   bool                     `json:"enabled,omitempty"`
	Image     string                   `json:"image,omitempty"`
	Expose    []string                 `json:"expose,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	Service   *ReverseProxyServiceSpec `json:"service,omitempty"`
}

// IngressTLSBlock holds TLS hosts and secret for Ingress.
type IngressTLSBlock struct {
	Hosts      []string `json:"hosts,omitempty"`
	SecretName string   `json:"secretName,omitempty"`
}

// IngressPathSpec routes a path to service or reverseProxy backend.
type IngressPathSpec struct {
	Path     string           `json:"path,omitempty"`
	PathType IngressPathType  `json:"pathType,omitempty"`
	Backend  IngressBackend   `json:"backend,omitempty"`
	Port     string           `json:"port,omitempty"`
}

// IngressRuleSpec defines per-host Ingress routing.
type IngressRuleSpec struct {
	Host  string            `json:"host,omitempty"`
	Paths []IngressPathSpec `json:"paths,omitempty"`
}

// IngressSpec configures Kubernetes Ingress (V1.1+ when enabled).
type IngressSpec struct {
	Enabled     bool              `json:"enabled,omitempty"`
	ClassName   string            `json:"className,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	TLS         []IngressTLSBlock `json:"tls,omitempty"`
	Rules       []IngressRuleSpec `json:"rules,omitempty"`
}

// MultiClusterSpec exposes cluster ports on client Service (V1: enabled false only).
type MultiClusterSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

// ConnectivitySpec groups listen ports, Service, Ingress, and cluster DNS (BDR-007).
type ConnectivitySpec struct {
	Listeners     *ConnectivityListenersSpec `json:"listeners,omitempty"`
	Service       *ConnectivityServiceSpec   `json:"service,omitempty"`
	ReverseProxy  *ReverseProxySpec          `json:"reverseProxy,omitempty"`
	Ingress       *IngressSpec               `json:"ingress,omitempty"`
	ClusterDomain string                     `json:"clusterDomain,omitempty"`
	MultiCluster  *MultiClusterSpec          `json:"multiCluster,omitempty"`
}

// IssuerRef references a cert-manager Issuer or ClusterIssuer.
type IssuerRef struct {
	Name string `json:"name"`
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +kubebuilder:default=ClusterIssuer
	Kind string `json:"kind,omitempty"`
}

// CertManagerSpec configures cert-manager certificate provisioning (BDR-006).
type CertManagerSpec struct {
	Enabled              bool      `json:"enabled,omitempty"`
	IssuerRef            *IssuerRef `json:"issuerRef,omitempty"`
	IncludeIngressHosts  bool      `json:"includeIngressHosts,omitempty"`
	DNSNames             []string  `json:"dnsNames,omitempty"`
}

// TLSSecretKeyRef references a TLS key or certificate in a Secret.
type TLSSecretKeyRef struct {
	SecretName string `json:"secretName"`
	SubPath    string `json:"subPath,omitempty"`
}

// TLSTrustedCertsSpec holds projected volume sources for client/peer CA certs.
type TLSTrustedCertsSpec struct {
	Sources []corev1.VolumeProjection `json:"sources,omitempty"`
}

// TLSPolicySpec holds TLS configuration for bolt, https, or cluster policy.
// BYO (privateKey+publicCertificate) XOR cert-manager (secretName) — enforced at webhook.
type TLSPolicySpec struct {
	PrivateKey        *TLSSecretKeyRef `json:"privateKey,omitempty"`
	PublicCertificate *TLSSecretKeyRef `json:"publicCertificate,omitempty"`
	SecretName        string           `json:"secretName,omitempty"`
	DNSNames          []string         `json:"dnsNames,omitempty"`
	ClientAuth        TLSClientAuth    `json:"clientAuth,omitempty"`
	TrustedCerts      *TLSTrustedCertsSpec `json:"trustedCerts,omitempty"`
}

// TrustCertificatesSpec groups per-connector TLS policies.
type TrustCertificatesSpec struct {
	Bolt    *TLSPolicySpec `json:"bolt,omitempty"`
	HTTPS   *TLSPolicySpec `json:"https,omitempty"`
	Cluster *TLSPolicySpec `json:"cluster,omitempty"`
}

// TrustReloadSpec configures dbms.security.tls_reload_enabled.
type TrustReloadSpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

// TrustSpec embeds TLS / mTLS configuration (BDR-006 Option B).
type TrustSpec struct {
	Enabled     bool                   `json:"enabled,omitempty"`
	Reload      *TrustReloadSpec       `json:"reload,omitempty"`
	CertManager *CertManagerSpec       `json:"certManager,omitempty"`
	Certificates *TrustCertificatesSpec `json:"certificates,omitempty"`
}

// SecondaryPoolSpec configures analytics or read secondary pool (BDR-002).
type SecondaryPoolSpec struct {
	// +kubebuilder:validation:Minimum=0
	Members int32 `json:"members,omitempty"`
	// +kubebuilder:validation:MaxItems=8
	Plugins []string `json:"plugins,omitempty"`
}

// SecondariesSpec holds fixed V1 secondary pools analytics and read.
type SecondariesSpec struct {
	Analytics *SecondaryPoolSpec `json:"analytics,omitempty"`
	Read      *SecondaryPoolSpec `json:"read,omitempty"`
}

// PrimariesSpec configures primary (quorum) members in Cluster mode.
type PrimariesSpec struct {
	// +kubebuilder:validation:Minimum=1
	Members int32 `json:"members"`
	// +kubebuilder:validation:MaxItems=8
	Plugins []string `json:"plugins,omitempty"`
}

// TopologySpec defines deployment mode and cluster composition (BDR-002).
type TopologySpec struct {
	// +kubebuilder:validation:Required
	Mode TopologyMode `json:"mode"`
	Primaries       *PrimariesSpec  `json:"primaries,omitempty"`
	Secondaries     *SecondariesSpec `json:"secondaries,omitempty"`
	MinimumMembers  *int32          `json:"minimumMembers,omitempty"`
}

// PluginDefinitionSpec holds per-plugin install configuration (BDR-004 Option E).
// licenseSecretRef is optional for GDS Community Edition; set it (with config e.g.
// gds.enterprise.license_file) to unlock GDS Enterprise features.
type PluginDefinitionSpec struct {
	LicenseSecretRef string            `json:"licenseSecretRef,omitempty"`
	Version          string            `json:"version,omitempty"`
	Config           map[string]string `json:"config,omitempty"`
}

// SchedulingAffinitySpec wraps pod anti-affinity presets.
type SchedulingAffinitySpec struct {
	PodAntiAffinity PodAntiAffinityPreset `json:"podAntiAffinity,omitempty"`
	Custom          *corev1.Affinity      `json:"custom,omitempty"`
}

// SchedulingSpec configures pod placement (NEO-2-008).
type SchedulingSpec struct {
	NodeSelector              map[string]string                  `json:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration                `json:"tolerations,omitempty"`
	Affinity                  *SchedulingAffinitySpec            `json:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint  `json:"topologySpreadConstraints,omitempty"`
	PriorityClassName         string                             `json:"priorityClassName,omitempty"`
}

// PodDisruptionBudgetSpec configures PDB for cluster workloads.
type PodDisruptionBudgetSpec struct {
	Enabled      bool                `json:"enabled,omitempty"`
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`
}

// ProbesSpec allows probe overrides.
type ProbesSpec struct {
	Startup   *corev1.Probe `json:"startup,omitempty"`
	Liveness  *corev1.Probe `json:"liveness,omitempty"`
	Readiness *corev1.Probe `json:"readiness,omitempty"`
}

// ServiceAccountSpec configures workload ServiceAccount.
type ServiceAccountSpec struct {
	Create      bool              `json:"create,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// SecuritySpec configures pod and container security contexts.
type SecuritySpec struct {
	PodSecurityContext       *corev1.PodSecurityContext       `json:"podSecurityContext,omitempty"`
	ContainerSecurityContext *corev1.SecurityContext          `json:"containerSecurityContext,omitempty"`
	ServiceAccount           *ServiceAccountSpec              `json:"serviceAccount,omitempty"`
	NetworkPolicy            *NetworkPolicySpec               `json:"networkPolicy,omitempty"`
}

// NetworkPolicySpec opt-in NetworkPolicy creation.
type NetworkPolicySpec struct {
	Enabled bool `json:"enabled,omitempty"`
}

// MaintenanceSpec configures maintenance windows.
type MaintenanceSpec struct {
	OfflineMode bool `json:"offlineMode,omitempty"`
}

// PodTemplateSpec escape hatch for init containers, sidecars, and env.
type PodTemplateSpec struct {
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
	Sidecars       []corev1.Container `json:"sidecars,omitempty"`
	Env            []corev1.EnvVar    `json:"env,omitempty"`
}

// --- Status types (ADR-004, status.md) ---

// ReplicaSummary is lightweight StatefulSet replica counts.
type ReplicaSummary struct {
	Servers int32 `json:"servers,omitempty"`
	Ready   int32 `json:"ready,omitempty"`
}

// UpgradeProgress tracks upgrade member counts.
type UpgradeProgress struct {
	Total     int32 `json:"total,omitempty"`
	Upgraded  int32 `json:"upgraded,omitempty"`
	Pending   int32 `json:"pending,omitempty"`
}

// UpgradeStatus tracks rolling upgrade state machine.
type UpgradeStatus struct {
	Phase            UpgradePhase       `json:"phase,omitempty"`
	TargetVersion    string             `json:"targetVersion,omitempty"`
	PreviousVersion  string             `json:"previousVersion,omitempty"`
	CurrentPartition int32              `json:"currentPartition,omitempty"`
	StepStartTime    *metav1.Time       `json:"stepStartTime,omitempty"`
	Progress         *UpgradeProgress   `json:"progress,omitempty"`
	LastError        string             `json:"lastError,omitempty"`
}

// PodSummary summarizes Kubernetes pod state for a member.
type PodSummary struct {
	PodName      string `json:"podName,omitempty"`
	PodIP        string `json:"podIP,omitempty"`
	NodeName     string `json:"nodeName,omitempty"`
	RestartCount int32  `json:"restartCount,omitempty"`
	Phase        string `json:"phase,omitempty"`
}

// MemberStatus summarizes one Neo4j server (Cluster detail path).
type MemberStatus struct {
	Name              string     `json:"name,omitempty"`
	Pool              MemberPool `json:"pool,omitempty"`
	Address           string     `json:"address,omitempty"`
	Plugins           []string   `json:"plugins,omitempty"`
	Neo4jState        string     `json:"neo4jState,omitempty"`
	Neo4jHealth       string     `json:"neo4jHealth,omitempty"`
	HostingDatabases  int32      `json:"hostingDatabases,omitempty"`
	Version           string     `json:"version,omitempty"`
	PodReady          bool       `json:"podReady,omitempty"`
	StorageBound      bool       `json:"storageBound,omitempty"`
	Pod               *PodSummary `json:"pod,omitempty"`
}

// ServerDiagnostic mirrors SHOW SERVERS row (diagnostics path).
type ServerDiagnostic struct {
	Name    string `json:"name,omitempty"`
	State   string `json:"state,omitempty"`
	Health  string `json:"health,omitempty"`
	Address string `json:"address,omitempty"`
}

// DatabaseDiagnostic mirrors SHOW DATABASES row.
type DatabaseDiagnostic struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
}

// DiagnosticsStatus holds deep Bolt observability (non-blocking for Ready).
type DiagnosticsStatus struct {
	LastCollectedTime *metav1.Time         `json:"lastCollectedTime,omitempty"`
	CollectionError   string               `json:"collectionError,omitempty"`
	Servers           []ServerDiagnostic   `json:"servers,omitempty"`
	Databases         []DatabaseDiagnostic `json:"databases,omitempty"`
}

// ConnectionExamples provides onboarding URI helpers.
type ConnectionExamples struct {
	BoltURI      string `json:"boltURI,omitempty"`
	Neo4jURI     string `json:"neo4jURI,omitempty"`
	PortForward  string `json:"portForward,omitempty"`
	Python       string `json:"python,omitempty"`
	Java         string `json:"java,omitempty"`
}

// EndpointsStatus exposes client connection URIs.
type EndpointsStatus struct {
	Bolt               string              `json:"bolt,omitempty"`
	Neo4j              string              `json:"neo4j,omitempty"`
	HTTP               string              `json:"http,omitempty"`
	HTTPS              string              `json:"https,omitempty"`
	Internal           string              `json:"internal,omitempty"`
	Backup             string              `json:"backup,omitempty"`
	ConnectionExamples *ConnectionExamples `json:"connectionExamples,omitempty"`
}

// CredentialsStatus references auth Secret (never the password).
type CredentialsStatus struct {
	SecretName string `json:"secretName,omitempty"`
	Generated  bool   `json:"generated,omitempty"`
}

// DatabaseSummary is lightweight database state in clusterInfo.
type DatabaseSummary struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
}

// ClusterInfoStatus summarizes cluster identity and database states.
type ClusterInfoStatus struct {
	ClusterID string            `json:"clusterId,omitempty"`
	Databases []DatabaseSummary `json:"databases,omitempty"`
}
