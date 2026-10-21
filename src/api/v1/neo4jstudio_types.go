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
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
	PullSecrets []string          `json:"pullSecrets,omitempty"`
}

// StudioRoleKind selects what a role-mapping member is (a Neo4j database user or a database role).
// +kubebuilder:validation:Enum=databaseUser;databaseRole
type StudioRoleKind string

const (
	StudioRoleKindUser StudioRoleKind = "databaseUser"
	StudioRoleKindRole StudioRoleKind = "databaseRole"
)

// StudioRole is an Enterprise Studio role granted by a role mapping.
// +kubebuilder:validation:Enum=studioAdmin;studioCreator
type StudioRole string

const (
	StudioRoleAdmin   StudioRole = "studioAdmin"
	StudioRoleCreator StudioRole = "studioCreator"
)

// StudioRoleMember is one Neo4j user/role that a mapping grants a Studio role to.
type StudioRoleMember struct {
	// Kind is databaseUser or databaseRole.
	// +kubebuilder:validation:Required
	Kind StudioRoleKind `json:"kind"`
	// Name is the Neo4j user or role name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// StudioRoleMapping grants a Studio role (studioAdmin/studioCreator) to Neo4j users/roles for a
// deployment (maps to neo4jDeployments.<id>.authorization.roleMapping). When omitted, NES defaults to
// studioAdmin→databaseRole:admin and studioCreator→databaseRole:PUBLIC.
type StudioRoleMapping struct {
	// Role is the Studio role to grant.
	// +kubebuilder:validation:Required
	Role StudioRole `json:"role"`
	// Members are the Neo4j users/roles granted the role.
	// +kubebuilder:validation:MinItems=1
	Members []StudioRoleMember `json:"members"`
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

// StudioDeployment is one Neo4j deployment that Studio serves (BDR-015 §2 — one-to-many). Exactly
// one of neo4jRef (in-cluster, resolved by the operator) or uri (external, used verbatim).
// +kubebuilder:validation:XValidation:rule="has(self.neo4jRef) != has(self.uri)",message="set deployments[].neo4jRef (in-cluster) or deployments[].uri (external), not both"
type StudioDeployment struct {
	// Name is the display name of this deployment in Studio.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Neo4jRef references an in-cluster Neo4j in the same namespace; the operator resolves it to the
	// workload's client Service URI (ADR-018 — never re-derived).
	Neo4jRef *Neo4jRef `json:"neo4jRef,omitempty"`
	// URI is an external Neo4j endpoint (e.g. neo4j+s://…), used verbatim; the operator does not
	// resolve or validate its reachability at admission (BDR-015 §2).
	URI string `json:"uri,omitempty"`
	// Mode selects Direct (browser dials the DBMS over Bolt) or ReverseProxy (Studio proxies over the
	// DBMS HTTP connector) for this deployment.
	// +kubebuilder:validation:Required
	Mode StudioMode `json:"mode"`
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
// visualizations, per-user state). The referenced account needs the architect role. In the first
// release the operator does not create it — a missing/unreachable asset DB surfaces as Degraded
// (BDR-015 §4, BYO).
type StudioAssetStore struct {
	// URI of the Neo4j instance holding the asset database (e.g. neo4j://my-graph.ns.svc:7687). Studio
	// connects to it as a Bolt driver client, so it must use a Neo4j/Bolt scheme (not http/https) —
	// independent of any served deployment's mode.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^(neo4j|neo4j\+s|neo4j\+ssc|bolt|bolt\+s|bolt\+ssc)://`
	URI string `json:"uri"`
	// Database is the asset database name; it must already exist (BYO — BDR-015 §4-a).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Database string `json:"database"`
	// Auth references the asset-store credentials.
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
	ClassName   string            `json:"className,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	TLS         []IngressTLSBlock `json:"tls,omitempty"`
	Rules       []StudioIngressRule `json:"rules,omitempty"`
}

// StudioConnectivitySpec groups Studio's own Service and optional Ingress (BDR-015 §8).
type StudioConnectivitySpec struct {
	Service *StudioServiceSpec `json:"service,omitempty"`
	Ingress *StudioIngressSpec `json:"ingress,omitempty"`
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

// StudioServerSpec configures the Studio HTTP(S) server itself.
type StudioServerSpec struct {
	// TLS enables HTTPS on Studio's listener.
	TLS *StudioServerTLS `json:"tls,omitempty"`
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
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
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
	// Connectivity is Studio's own front door (BDR-015 §8).
	Connectivity *StudioConnectivitySpec `json:"connectivity,omitempty"`
	// Server configures the Studio HTTP(S) listener (e.g. TLS).
	Server *StudioServerSpec `json:"server,omitempty"`
	// Tools toggles which tools appear in the Studio UI.
	Tools *StudioToolsSpec `json:"tools,omitempty"`
	// Replicas is the Studio Deployment replica count. Studio is stateless; defaults to 1 (BDR-015 §10).
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`
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
	// Reachable reports whether the operator could reach this deployment.
	Reachable bool `json:"reachable,omitempty"`
}

// Neo4jStudioStatus is the observed state of a Neo4jStudio (ADR-018).
type Neo4jStudioStatus struct {
	// Phase is the coarse lifecycle state.
	Phase StudioPhase `json:"phase,omitempty"`
	// Conditions are Kubernetes-standard signals; Ready is gated on the Deployment being Available
	// and the asset DB being reachable.
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
