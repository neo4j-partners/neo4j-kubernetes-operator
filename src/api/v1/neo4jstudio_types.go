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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StudioMode selects how Studio reaches a served deployment (BDR-015). Direct = the browser dials
// the DBMS over Bolt; ReverseProxy = Studio proxies HTTP to the DBMS's HTTP connector.
// +kubebuilder:validation:Enum=Direct;ReverseProxy
type StudioMode string

const (
	StudioModeDirect       StudioMode = "Direct"
	StudioModeReverseProxy StudioMode = "ReverseProxy"
)

// StudioPhase is the coarse lifecycle of a Neo4jStudio (BDR-015 §10, ADR-018).
// +kubebuilder:validation:Enum=Pending;Running;Degraded;Failed
type StudioPhase string

const (
	StudioPhasePending  StudioPhase = "Pending"
	StudioPhaseRunning  StudioPhase = "Running"
	StudioPhaseDegraded StudioPhase = "Degraded"
	StudioPhaseFailed   StudioPhase = "Failed"
)

// SecretRef references a Secret in the same namespace by name (BDR-015 §5 — secrets are always
// referenced, never inline).
type SecretRef struct {
	// Name of the Secret in the same namespace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// CredentialRef wraps a referenced Secret ({ secretRef: { name } }). Used for the Studio license,
// per-deployment auth, and asset-store auth so no credential ever lands inline in the spec or in
// `kubectl describe` (BDR-015 §5).
type CredentialRef struct {
	// SecretRef names the Secret holding the credentials.
	// +kubebuilder:validation:Required
	SecretRef SecretRef `json:"secretRef"`
}

// StudioImageSpec overrides the Enterprise Studio container image. Unlike the Neo4j workload image,
// the repository has no built-in default here (it is the Studio image, not the Neo4j image); the
// operator supplies its pinned Studio image when this block is omitted.
type StudioImageSpec struct {
	// Repository is the Studio image name without tag. Omit to use the operator's pinned Studio image.
	Repository string `json:"repository,omitempty"`
	// Digest pins the image by digest (repo@sha256:...). When set, spec.version is not used in the ref.
	// +kubebuilder:validation:Pattern=`^sha256:[a-fA-F0-9]{64}$`
	Digest string `json:"digest,omitempty"`
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	PullPolicy  corev1.PullPolicy `json:"pullPolicy,omitempty"`
	PullSecrets []string          `json:"pullSecrets,omitempty"`
}

// StudioRole is an Enterprise Studio role granted by a role mapping.
// +kubebuilder:validation:Enum=studioAdmin;studioCreator
type StudioRole string

const (
	StudioRoleAdmin   StudioRole = "studioAdmin"
	StudioRoleCreator StudioRole = "studioCreator"
)

// StudioRoleMapping grants a Studio role (studioAdmin/studioCreator) to Neo4j database roles and/or
// users for a deployment (maps to neo4jDeployments.<id>.authorization.roleMapping). When omitted, NES
// defaults to studioAdmin→databaseRole:admin and studioCreator→databaseRole:PUBLIC.
// +kubebuilder:validation:XValidation:rule="has(self.databaseRoles) || has(self.databaseUsers)",message="roleMapping[]: set at least one of databaseRoles or databaseUsers"
type StudioRoleMapping struct {
	// Role is the Studio role to grant.
	// +kubebuilder:validation:Required
	Role StudioRole `json:"role"`
	// DatabaseRoles are the Neo4j database roles granted this Studio role (e.g. admin, PUBLIC).
	DatabaseRoles []string `json:"databaseRoles,omitempty"`
	// DatabaseUsers are the Neo4j database users granted this Studio role.
	DatabaseUsers []string `json:"databaseUsers,omitempty"`
}

// StudioDeploymentAuth toggles the sign-in methods end users may use for a served deployment (maps to
// neo4jDeployments.<id>.authentication.{basic,oidc}.enabled). Both default to enabled in NES; set a
// field to override. Studio stores no per-deployment service credentials — proxy mode uses the user's
// session and Direct mode uses the browser's own login.
type StudioDeploymentAuth struct {
	// Basic enables username/password sign-in. Omit to use the NES default (enabled).
	Basic *bool `json:"basic,omitempty"`
	// OIDC enables single sign-on. Omit to use the NES default (enabled).
	OIDC *bool `json:"oidc,omitempty"`
}

// StudioNeo4jRef references an in-cluster Neo4j in the same namespace, optionally targeting one of its
// cluster pools. The operator resolves it to that pool's Service URI (ADR-018 — never re-derived).
type StudioNeo4jRef struct {
	// Name of the target Neo4j resource in the same namespace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Pool optionally targets a specific cluster pool of the referenced Neo4j: analytics (the GDS
	// analytics pool), read, or primary. Omit for the default north-south client Service (primaries).
	// Only meaningful for a Cluster — ignored for a Standalone. analytics/read resolve to that pool's
	// headless Service (per-member DNS); set spec.deployments[].mode accordingly for multi-member pools.
	// +kubebuilder:validation:Enum=primary;analytics;read
	Pool string `json:"pool,omitempty"`
}

// StudioDeployment is one Neo4j deployment that Studio serves (BDR-015 §2 — one-to-many). Exactly
// one of neo4jRef (in-cluster, resolved by the operator) or uri (external, used verbatim).
// +kubebuilder:validation:XValidation:rule="has(self.neo4jRef) != has(self.uri)",message="set deployments[].neo4jRef (in-cluster) or deployments[].uri (external), not both"
// +kubebuilder:validation:XValidation:rule="!(has(self.neo4jRef) && self.mode == 'Direct') || has(self.publicUri)",message="deployments[].mode Direct against an in-cluster neo4jRef needs a browser-reachable publicUri — the in-cluster Bolt Service DNS the operator would derive is not reachable from a browser (use ReverseProxy, or set publicUri)"
type StudioDeployment struct {
	// Name is the display name of this deployment in Studio.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Neo4jRef references an in-cluster Neo4j in the same namespace; the operator resolves it to the
	// workload's client Service URI (ADR-018 — never re-derived). Optionally targets a cluster pool.
	Neo4jRef *StudioNeo4jRef `json:"neo4jRef,omitempty"`
	// URI is an external Neo4j endpoint (e.g. neo4j+s://…), used verbatim; the operator does not
	// resolve or validate its reachability at admission (BDR-015 §2).
	URI string `json:"uri,omitempty"`
	// Mode selects Direct (browser dials the DBMS over Bolt) or ReverseProxy (Studio proxies over the
	// DBMS HTTP connector) for this deployment. Optional — defaults to ReverseProxy, the in-cluster-safe
	// choice (works for standalone and, via BDR-016, clusters).
	// +kubebuilder:default=ReverseProxy
	Mode StudioMode `json:"mode,omitempty"`
	// PublicURI optionally overrides the browser-reachable Bolt URI for Direct mode (maps to
	// directClientQuery.publicUri). For a neo4jRef the operator derives the in-cluster Bolt URI when
	// this is omitted; set it when the browser reaches the DBMS at a different, external address.
	PublicURI string `json:"publicUri,omitempty"`
	// Auth toggles the end-user sign-in methods for this deployment (basic / oidc).
	Auth *StudioDeploymentAuth `json:"auth,omitempty"`
	// RoleMapping maps Neo4j users/roles to Studio roles for this deployment.
	RoleMapping []StudioRoleMapping `json:"roleMapping,omitempty"`
}

// StudioAssetStore is the Neo4j database where Studio persists its own assets (saved queries,
// visualizations, per-user state). The referenced account needs the architect role. The asset store
// is either an external uri (BYO) or an in-cluster neo4jRef; with provision: true the operator creates
// the database, the architect account, and the role grant on the in-cluster target (BDR-015 §4).
// +kubebuilder:validation:XValidation:rule="has(self.uri) != has(self.neo4jRef)",message="assetStore: set exactly one of uri (external) or neo4jRef (in-cluster)"
// +kubebuilder:validation:XValidation:rule="!self.provision || has(self.neo4jRef)",message="assetStore.provision requires assetStore.neo4jRef — the operator provisions only an in-cluster, operator-managed Neo4j (it needs that instance's admin credentials; an external uri cannot be provisioned)"
type StudioAssetStore struct {
	// URI of an external Neo4j holding the asset database (e.g. neo4j://my-graph.ns.svc:7687). Studio
	// connects to it as a Bolt driver client, so it must use a Neo4j/Bolt scheme (not http/https) —
	// independent of any served deployment's mode. Mutually exclusive with neo4jRef.
	// +kubebuilder:validation:Pattern=`^(neo4j|neo4j\+s|neo4j\+ssc|bolt|bolt\+s|bolt\+ssc)://`
	URI string `json:"uri,omitempty"`
	// Neo4jRef references an in-cluster, operator-managed Neo4j in the same namespace; the operator
	// resolves it to the Bolt client Service URI (neo4j://<svc>:<boltPort>). Required for provision.
	// Mutually exclusive with uri.
	Neo4jRef *Neo4jRef `json:"neo4jRef,omitempty"`
	// Provision opts in to the operator creating the asset database, the architect account (from auth),
	// and the role grant on the neo4jRef target via admin Bolt — idempotently (BDR-015 §4). Requires
	// neo4jRef. Default false keeps the asset store BYO. Admin Bolt needs the target's NEO-004 TLS trust
	// (trust.certificates.bolt or trust.insecureAdminConnection).
	Provision bool `json:"provision,omitempty"`
	// Database is the asset database name. With provision it is created (IF NOT EXISTS); otherwise it
	// must already exist (BYO — BDR-015 §4-a).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Database string `json:"database"`
	// Auth references the asset-store architect credentials (Secret keys username/password). With
	// provision the operator creates this account with that password; otherwise it is used as-is.
	// +kubebuilder:validation:Required
	Auth CredentialRef `json:"auth"`
}

// StudioServiceSpec configures Studio's own front-door Service.
type StudioServiceSpec struct {
	// +kubebuilder:validation:Enum=ClusterIP;LoadBalancer;NodePort
	// +kubebuilder:default=ClusterIP
	Type        ServiceType       `json:"type,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// StudioIngressRule routes a host to Studio (single backend, unlike the Neo4j multi-backend rules).
type StudioIngressRule struct {
	// Host is the external hostname routed to Studio.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`
}

// StudioIngressSpec configures an optional Ingress in front of Studio (BDR-015 §8, reuses BDR-007).
type StudioIngressSpec struct {
	ClassName   string              `json:"className,omitempty"`
	Annotations map[string]string   `json:"annotations,omitempty"`
	TLS         []IngressTLSBlock   `json:"tls,omitempty"`
	Rules       []StudioIngressRule `json:"rules,omitempty"`
}

// StudioConnectivitySpec groups Studio's own front door — Service, optional Ingress, and HTTPS
// (BDR-015 §8; HTTPS regrouped here in the usability revision so exposure lives in one place).
type StudioConnectivitySpec struct {
	Service *StudioServiceSpec `json:"service,omitempty"`
	Ingress *StudioIngressSpec `json:"ingress,omitempty"`
	// TLS enables HTTPS on Studio's own listener.
	TLS *StudioServerTLS `json:"tls,omitempty"`
}

// StudioServerTLS enables HTTPS on Studio's own listener (maps to server.https.*). The referenced
// Secret must be a kubernetes.io/tls Secret (tls.crt + tls.key); the operator mounts it and points
// NES at the PEM files. Note tls.key must be an unencrypted PKCS#8 PEM (BEGIN PRIVATE KEY).
type StudioServerTLS struct {
	// Enabled turns on HTTPS on server.port.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
	// SecretRef names the kubernetes.io/tls Secret with the server certificate and key.
	// +kubebuilder:validation:Required
	SecretRef SecretRef `json:"secretRef"`
}

// StudioToolsSpec toggles which tools appear in the Studio UI (maps to tools.*.enabled). Omit a field
// to use the NES default (all enabled).
type StudioToolsSpec struct {
	Dashboards *bool `json:"dashboards,omitempty"`
	Bloom      *bool `json:"bloom,omitempty"`
	Query      *bool `json:"query,omitempty"`
}

// StudioSecuritySpec overrides the pod/container securityContext the operator renders. The operator's
// hardened, non-root defaults (runAsUser 7474, drop ALL, seccomp RuntimeDefault) are used when unset;
// provided fields are merged over those defaults (privileged/allowPrivilegeEscalation stay disabled).
type StudioSecuritySpec struct {
	PodSecurityContext       *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	ContainerSecurityContext *corev1.SecurityContext    `json:"containerSecurityContext,omitempty"`
}

// StudioSecretKeyRef selects one key of a Secret in the same namespace.
type StudioSecretKeyRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// StudioSecretConfig sources one NES_* configuration key from a Secret, injected as a secretKeyRef
// environment variable. This is the secure passthrough for any NES setting that carries a secret
// (e.g. generativeAi API keys, OIDC client secrets) without the operator modelling each one inline.
type StudioSecretConfig struct {
	// Name is the exact NES_* environment variable name (e.g. NES_generativeAi_chatCompletionProvider_default_upstream_apiKey).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^NES_`
	Name string `json:"name"`
	// SecretRef selects the Secret key holding the value.
	// +kubebuilder:validation:Required
	SecretRef StudioSecretKeyRef `json:"secretRef"`
}

// Neo4jStudioSpec is the desired state of an Enterprise Studio deployment (BDR-015).
type Neo4jStudioSpec struct {
	// Version is the Studio image tag; a bump is a rolling Deployment update (stateless — BDR-015 §10).
	// Optional — omit to use the operator's pinned, validated Studio tag (BDR-015 usability revision).
	Version string `json:"version,omitempty"`
	// Image optionally overrides the Studio image repository / digest / pull policy.
	Image *StudioImageSpec `json:"image,omitempty"`
	// License is the OPTIONAL Enterprise Studio license — distinct from the Neo4j Enterprise license;
	// Studio starts without it (BDR-015 §3).
	License *CredentialRef `json:"license,omitempty"`
	// Deployments are the served Neo4j deployments (one-to-many; at least one — BDR-015 §2).
	// +kubebuilder:validation:MinItems=1
	Deployments []StudioDeployment `json:"deployments"`
	// AssetStore is where Studio persists its own assets (BDR-015 §4).
	// +kubebuilder:validation:Required
	AssetStore StudioAssetStore `json:"assetStore"`
	// Connectivity is Studio's own front door — Service, Ingress, and HTTPS (BDR-015 §8).
	Connectivity *StudioConnectivitySpec `json:"connectivity,omitempty"`
	// Tools toggles which tools appear in the Studio UI.
	Tools *StudioToolsSpec `json:"tools,omitempty"`
	// Replicas is the Studio Deployment replica count. Studio is stateless; defaults to 1 (BDR-015 §10).
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`
	// Resources sets CPU and memory requests and limits for the Studio container. Any field left unset
	// is defaulted by the operator so the container is always bounded (NEO-014): requests 250m CPU /
	// 512Mi, limits 1 CPU / 1Gi. Set explicit values for heavier Studio usage.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Security overrides the operator's hardened pod/container securityContext defaults.
	Security *StudioSecuritySpec `json:"security,omitempty"`
	// Config passes through non-secret NES_* keys verbatim (BDR-015 §9), e.g. tuning or generativeAi
	// URLs/models. Secret-valued keys go through configFromSecrets, never here.
	Config map[string]string `json:"config,omitempty"`
	// ConfigFromSecrets sources secret-valued NES_* keys from Secrets (secretKeyRef env). This is the
	// secure passthrough for any NES setting carrying a secret (generativeAi API keys, OIDC client
	// secrets, …) that the operator does not model as a first-class field.
	ConfigFromSecrets []StudioSecretConfig `json:"configFromSecrets,omitempty"`
}

// StudioDeploymentStatus is the resolved, observed state of one served deployment.
type StudioDeploymentStatus struct {
	// Name mirrors spec.deployments[].name.
	Name string `json:"name"`
	// ResolvedURI is the client URI the operator resolved (from neo4jRef) or echoed (external uri).
	ResolvedURI string `json:"resolvedUri,omitempty"`
}

// Neo4jStudioStatus is the observed state of a Neo4jStudio (ADR-018).
type Neo4jStudioStatus struct {
	// Phase is the coarse lifecycle state.
	Phase StudioPhase `json:"phase,omitempty"`
	// Conditions are Kubernetes-standard signals; Ready gates on the Studio Deployment becoming
	// Available. A Deployment that fails to roll out (e.g. the asset store is unreachable and Studio
	// crash-loops at startup) surfaces as phase Degraded with a reason, not a stuck Ready=false.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last spec generation reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// URL is the resolved external URL of Studio (from the Service/Ingress).
	URL string `json:"url,omitempty"`
	// Deployments reports per-deployment resolution and reachability.
	Deployments []StudioDeploymentStatus `json:"deployments,omitempty"`
	// Reason is a stable machine reason (oracle catalog) when Degraded/Failed.
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable detail.
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=n4js,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// Neo4jStudio stands up Enterprise Studio and points it at one-to-many Neo4j deployments (BDR-015).
type Neo4jStudio struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jStudioSpec   `json:"spec,omitempty"`
	Status Neo4jStudioStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Neo4jStudioList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jStudio `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jStudio{}, &Neo4jStudioList{})
}
