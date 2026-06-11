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

	// SecurityContext allows overriding pod/container security settings (e.g., for OpenShift SCC compatibility)
	SecurityContext *SecurityContextSpec `json:"securityContext,omitempty"`

	// Custom configuration for Neo4j
	Config map[string]string `json:"config,omitempty"`

	TLS *TLSSpec `json:"tls,omitempty"`

	Auth *AuthSpec `json:"auth,omitempty"`

	Service *ServiceSpec `json:"service,omitempty"`

	// UpgradeStrategy specifies how to handle rolling upgrades
	UpgradeStrategy *UpgradeStrategySpec `json:"upgradeStrategy,omitempty"`

	// Plugin management configuration - DEPRECATED: Use Neo4jPlugin CRD instead

	// Monitoring configuration (Prometheus metrics, query logging, diagnostics)
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`

	// Audit configures compliance-oriented logging. Neo4j Enterprise
	// already produces a security log (auth + admin events) and a
	// query log (data access) by default; this block lets users tune
	// what those logs CONTAIN — most importantly, redaction of query
	// literals to avoid PII leakage in compliance-monitored
	// environments (PCI / HIPAA / GDPR).
	// +optional
	Audit *AuditSpec `json:"audit,omitempty"`

	// NetworkPolicy controls emission of a Kubernetes NetworkPolicy that
	// restricts ingress to the server pods. Public client ports
	// (7474/7473/7687) remain open to any pod; intra-cluster ports
	// (6000/7000/7688) are restricted to peer servers; the backup port
	// (6362) is restricted to operator-managed backup pods only.
	//
	// Disabled by default. NetworkPolicy enforcement depends on the
	// cluster's CNI plugin (Calico/Cilium/Antrea/Weave enforce;
	// flannel does not), so enabling this on a non-enforcing CNI is a
	// safe no-op but does not actually protect the backup port. See
	// docs/user_guide/security.md for the prerequisites.
	// +optional
	NetworkPolicy *NetworkPolicySpec `json:"networkPolicy,omitempty"`

	// MCP server configuration for this cluster
	MCP *MCPServerSpec `json:"mcp,omitempty"`

	// Property Sharding configuration for Neo4j 2025.12+ (Infinigraph)
	// Enables support for creating sharded databases that separate
	// graph topology from node/relationship properties
	PropertySharding *PropertyShardingSpec `json:"propertySharding,omitempty"`

	// AuraFleetManagement enables integration with Neo4j Aura Fleet Management
	// for monitoring and managing this cluster from the Aura console.
	// The fleet-management plugin is pre-bundled in all Neo4j Enterprise images
	// and does not require internet access to install.
	// See: https://neo4j.com/docs/aura/fleet-management/
	// +optional
	AuraFleetManagement *AuraFleetManagementSpec `json:"auraFleetManagement,omitempty"`

	// ExtraEnvFrom projects entire Secrets or ConfigMaps as environment
	// variables on the neo4j container. Intended for credential bundles —
	// e.g. a Secret with AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY /
	// AWS_ENDPOINT_URL_S3 / AWS_REGION used by the AWS SDK default
	// credential chain when Neo4j fetches a seed URI from S3 (or
	// S3-compatible stores like MinIO).
	//
	// To enable Neo4jShardedDatabase.spec.seedBackupRef against a
	// cloud-backed Neo4jBackup, this list MUST contain a SecretRef that
	// matches the backup's `spec.cloud.credentialsSecretRef`. The sharded
	// DB controller validates this at reconcile time and routes to
	// Phase=Failed with an actionable error if absent.
	//
	// Opt-in auto-inherit: set annotation
	// `neo4j.com/auto-inherit-seed-creds=true` on the cluster CR to let the
	// sharded DB controller append the missing entry automatically. This
	// triggers a rolling restart of cluster pods, which is why it's opt-in.
	// +optional
	ExtraEnvFrom []corev1.EnvFromSource `json:"extraEnvFrom,omitempty"`

	// TrustedCASecrets references Secrets containing additional CA certificates
	// (key defaults to "ca.crt") that Neo4j-the-server should trust for outgoing
	// TLS connections — e.g. OIDC providers behind a corporate CA, LDAPS, Aura
	// Fleet Management, plugin download mirrors, and cross-cluster replication
	// when it uses default JVM trust.
	//
	// The operator copies the JDK default cacerts into a writable JKS truststore
	// at `/truststore/truststore.jks` via an init container, then imports each
	// supplied CA with the Secret name as the keytool alias. Public CAs in the
	// JDK default store remain trusted.
	//
	// Cert-manager users: reference the Secret produced by a Certificate CR
	// directly — the default key "ca.crt" matches what cert-manager writes.
	//
	// Supersedes the singular `spec.auth.trustStore` field; if both are set
	// the legacy value is folded into this list and a deprecation warning is
	// emitted by the validator.
	// +optional
	TrustedCASecrets []TrustedCASecret `json:"trustedCASecrets,omitempty"`

	// ExtraVolumes are additional pod volumes to attach to the Neo4j pod.
	// Mount points must be wired separately via `extraVolumeMounts`.
	// Use this for plugin JARs, custom config, per-policy SSL truststores
	// (e.g., for cross-cluster replication policies that use truststore_path),
	// or any other content that needs to land at a specific path inside the
	// Neo4j container.
	// +optional
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`

	// ExtraVolumeMounts are additional volume mounts for the Neo4j container.
	// Each entry must reference a volume defined either in `extraVolumes` or
	// (rarely) one of the operator-managed volumes. Mount paths that collide
	// with operator-managed paths (`/data`, `/logs`, `/conf`, `/ssl`,
	// `/plugins`, `/truststore`, `/truststore-ca`) are rejected by the
	// validator.
	// +optional
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`
}

// TrustedCASecret references a Secret containing a PEM-encoded CA certificate
// to be added to Neo4j-the-server's JVM truststore.
//
// The Secret name is also used as the keytool alias, so names must be unique
// across the list. Defaults to key "ca.crt" — which matches the layout used
// by cert-manager-issued Secrets.
type TrustedCASecret struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key within the Secret. Defaults to "ca.crt".
	// +optional
	Key string `json:"key,omitempty"`
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
	// ClassName is the StorageClass for the data PVCs. If omitted (or empty),
	// the PVCs inherit the cluster's default StorageClass. When set, the named
	// class must exist — the operator verifies this at apply time and reports an
	// explicit error rather than leaving pods Pending indefinitely.
	// +optional
	ClassName string `json:"className,omitempty"`

	// +kubebuilder:validation:Required
	Size string `json:"size"`

	// PVC retention policy when cluster is deleted
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	RetentionPolicy string `json:"retentionPolicy,omitempty"`
}

// TLSSpec defines TLS configuration
type TLSSpec struct {
	// +kubebuilder:validation:Enum=cert-manager;disabled
	// +kubebuilder:default=cert-manager
	Mode string `json:"mode,omitempty"`

	IssuerRef *IssuerRef `json:"issuerRef,omitempty"`

	// Manual certificate configuration
	CertificateSecret string `json:"certificateSecret,omitempty"`

	// TrustedCASecret references a Secret containing a trusted CA certificate (key: "ca.crt")
	// for verifying Neo4j TLS connections. When omitted with cert-manager mode, TLS verification
	// is skipped (suitable for self-signed development certificates).
	// +optional
	TrustedCASecret string `json:"trustedCASecret,omitempty"`

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

	// StrictPeerValidation controls intra-cluster TLS posture for
	// Neo4jEnterpriseCluster. Default true.
	//
	// When true (the default and Neo4j's documented production posture):
	//   - dbms.ssl.policy.cluster.trust_all = false: peers are validated
	//     against the issuer's CA cert at /ssl/trusted/ca.crt (projected
	//     from the cert-manager Secret's ca.crt key).
	//   - dbms.ssl.policy.cluster.client_auth = REQUIRE: mutual TLS;
	//     both ends authenticate.
	//   - dbms.ssl.policy.cluster.verify_hostname = true: peer cert must
	//     match the SAN of the FQDN being connected to. The Certificate
	//     the operator emits already includes every server pod FQDN as
	//     a SAN, so this is a no-op for spec correctness — but the Neo4j
	//     default differs between 5.26 (false) and 2025.01+ (true), so
	//     we set it explicitly for version-portable behavior.
	//
	// When false (opt-out, equivalent to operator behavior prior to
	// adding this field): the cluster SSL policy emits trust_all=true,
	// client_auth=NONE — Neo4j's own docs call this "debugging only,
	// since it does not offer security." Use only if your cert-manager
	// issuer does not populate the Secret's ca.crt key (some external
	// issuers like custom AWSPCAClusterIssuer setups) or you have a
	// specific upgrade-path reason.
	//
	// Strict validation requires the cert-manager-issued Secret to
	// contain a ca.crt key. The controller refuses to apply strict
	// config if it is missing — your status will report
	// `TLSStrictValidationUnready` with a message naming the issuer.
	//
	// No effect on Neo4jEnterpriseStandalone (single-server deployments
	// have no intra-cluster traffic).
	//
	// +optional
	// +kubebuilder:default=true
	StrictPeerValidation *bool `json:"strictPeerValidation,omitempty"`
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

// SecurityContextSpec defines pod/container security overrides for Neo4j workloads
type SecurityContextSpec struct {
	// Pod-level security settings to apply to all Neo4j pods
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Container-level security settings for Neo4j containers
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`
}

// IssuerRef references a cert-manager-compatible issuer.
// Supports standard cert-manager issuers (Issuer, ClusterIssuer) as well as
// third-party external issuers (e.g. AWSPCAClusterIssuer, VaultIssuer) by
// specifying the appropriate kind and group.
type IssuerRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Kind of the issuer resource. Defaults to ClusterIssuer for standard
	// cert-manager usage. Set to the custom resource kind for external issuers
	// (e.g. AWSPCAClusterIssuer).
	// +kubebuilder:default=ClusterIssuer
	Kind string `json:"kind,omitempty"`

	// Group of the issuer's API. Defaults to cert-manager.io for standard
	// cert-manager issuers. Set to the external issuer's API group for
	// third-party issuers (e.g. awspca.cert-manager.io).
	Group string `json:"group,omitempty"`
}

// AuthSpec defines authentication and authorization configuration.
// Supports multi-provider setups (e.g., ldap+native) and typed configuration
// for LDAP, OIDC/SSO, and Kerberos that generates correct neo4j.conf entries.
type AuthSpec struct {
	// AuthenticationProviders is an ordered list of authentication providers.
	// Neo4j evaluates them in order during login. Valid values: native, ldap, oidc-<name>, kerberos.
	// For OIDC providers, use the format "oidc-<name>" where <name> matches a key in the OIDC map.
	// Defaults to ["native"] if empty.
	// +optional
	AuthenticationProviders []string `json:"authenticationProviders,omitempty"`

	// AuthorizationProviders is an ordered list of authorization providers.
	// Valid values: native, ldap, oidc-<name>, kerberos.
	// Defaults to ["native"] if empty.
	// +optional
	AuthorizationProviders []string `json:"authorizationProviders,omitempty"`

	// AdminSecret is the name of the Secret containing initial admin credentials (keys: username, password)
	// +optional
	AdminSecret string `json:"adminSecret,omitempty"`

	// External Secrets configuration for auth secrets
	// +optional
	ExternalSecrets *ExternalSecretsConfig `json:"externalSecrets,omitempty"`

	// LDAP configures LDAP authentication and authorization
	// +optional
	LDAP *Neo4jLDAPSpec `json:"ldap,omitempty"`

	// OIDC configures one or more OIDC/SSO providers. Map keys become the provider name
	// in Neo4j config (dbms.security.oidc.<name>.*) and in the authentication_providers list (oidc-<name>).
	// +optional
	OIDC map[string]Neo4jOIDCProviderSpec `json:"oidc,omitempty"`

	// AuthCacheTTL sets dbms.security.auth_cache_ttl (e.g., "10m", "600s").
	// Controls how long authentication/authorization results are cached.
	// +optional
	AuthCacheTTL string `json:"authCacheTTL,omitempty"`

	// TrustStore configures a custom JVM truststore for LDAPS or OIDC with internal CAs.
	// The operator mounts the CA certificate and creates a JKS truststore via an init container.
	// Name is the Secret name; Key defaults to "ca.crt" if omitted.
	// +optional
	TrustStore *SecretKeyRef `json:"trustStore,omitempty"`
}

// Neo4jLDAPSpec configures LDAP authentication and authorization.
// Fields map directly to Neo4j's dbms.security.ldap.* configuration keys.
type Neo4jLDAPSpec struct {
	// Host is the LDAP server URL (e.g., "ldap://ldap.example.com:389" or "ldaps://ldap.example.com:636")
	// Maps to: dbms.security.ldap.host
	// +kubebuilder:validation:Required
	Host string `json:"host"`

	// UseStartTLS enables STARTTLS on the LDAP connection (use with
	// ldap:// scheme, not ldaps://).
	//
	// Defaults to true when host starts with `ldap://` and this field
	// is unset — secure-by-default per the Neo4j security checklist
	// ("Configure your LDAP system with encryption via StartTLS").
	// Set to false explicitly to opt out (e.g. dev / mock-LDAP setups
	// that don't speak StartTLS). `ldaps://` hosts are unaffected by
	// the default — they're already encrypted at the protocol level.
	//
	// Maps to: dbms.security.ldap.use_starttls
	// +optional
	UseStartTLS *bool `json:"useStartTLS,omitempty"`

	// Authentication configures how users authenticate against LDAP
	// +optional
	Authentication *LDAPAuthenticationSpec `json:"authentication,omitempty"`

	// Authorization configures how Neo4j resolves roles/groups from LDAP
	// +optional
	Authorization *LDAPAuthorizationSpec `json:"authorization,omitempty"`

	// DebugGroupLogging enables debug-level logging for LDAP group lookups.
	// WARNING: Disable in production as it logs sensitive group information.
	// Maps to: dbms.security.logs.ldap.groups_at_debug_level_enabled
	// +optional
	DebugGroupLogging *bool `json:"debugGroupLogging,omitempty"`
}

// LDAPAuthenticationSpec configures LDAP authentication settings.
type LDAPAuthenticationSpec struct {
	// UserDNTemplate is the DN template for binding users. Use {0} as the username placeholder.
	// Examples: "uid={0},ou=users,dc=example,dc=com" or "{0}@example.com" (Active Directory UPN)
	// Maps to: dbms.security.ldap.authentication.user_dn_template
	// +optional
	UserDNTemplate string `json:"userDNTemplate,omitempty"`

	// SearchForAttribute enables attribute-based user lookup instead of DN template binding.
	// When true, the operator uses Attribute to find the user before binding.
	// Maps to: dbms.security.ldap.authentication.search_for_attribute
	// +optional
	SearchForAttribute *bool `json:"searchForAttribute,omitempty"`

	// Attribute is the LDAP attribute to search when SearchForAttribute is true (e.g., "samaccountname")
	// Maps to: dbms.security.ldap.authentication.attribute
	// +optional
	Attribute string `json:"attribute,omitempty"`

	// CacheEnabled enables caching of LDAP authentication results
	// Maps to: dbms.security.ldap.authentication.cache_enabled
	// +optional
	CacheEnabled *bool `json:"cacheEnabled,omitempty"`
}

// LDAPAuthorizationSpec configures LDAP authorization (group/role lookup) settings.
type LDAPAuthorizationSpec struct {
	// UserSearchBase is the base DN for searching user objects (e.g., "ou=users,dc=example,dc=com")
	// Maps to: dbms.security.ldap.authorization.user_search_base
	// +optional
	UserSearchBase string `json:"userSearchBase,omitempty"`

	// UserSearchFilter is the LDAP filter for finding user objects. {0} is replaced by the username.
	// Example: "(&(objectClass=user)(sAMAccountName={0}))"
	// Maps to: dbms.security.ldap.authorization.user_search_filter
	// +optional
	UserSearchFilter string `json:"userSearchFilter,omitempty"`

	// GroupMembershipAttributes are the user object attributes containing group membership (e.g., ["memberOf"])
	// Maps to: dbms.security.ldap.authorization.group_membership_attributes (comma-separated)
	// +optional
	GroupMembershipAttributes []string `json:"groupMembershipAttributes,omitempty"`

	// GroupToRoleMapping maps LDAP group DNs to Neo4j roles.
	// Key: LDAP group DN, Value: comma-separated Neo4j roles (e.g., "admin,architect").
	// Built-in roles: admin, architect, publisher, editor, reader.
	// Maps to: dbms.security.ldap.authorization.group_to_role_mapping (semicolon-separated)
	// +optional
	GroupToRoleMapping map[string]string `json:"groupToRoleMapping,omitempty"`

	// AccessPermittedGroup restricts LDAP authentication to members of this group only.
	// Users with valid LDAP credentials but not in this group are denied access.
	// Maps to: dbms.security.ldap.authorization.access_permitted_group
	// +optional
	AccessPermittedGroup string `json:"accessPermittedGroup,omitempty"`

	// UseSystemAccount enables using a dedicated system account for LDAP authorization queries
	// instead of the authenticating user's own credentials.
	// Maps to: dbms.security.ldap.authorization.use_system_account
	// +optional
	UseSystemAccount *bool `json:"useSystemAccount,omitempty"`

	// SystemAccountSecretRef is the name of a Secret containing the LDAP system account credentials.
	// The Secret must have keys "username" (full DN) and "password".
	// These are injected as env vars, never stored in the ConfigMap.
	// Required when UseSystemAccount is true.
	// +optional
	SystemAccountSecretRef string `json:"systemAccountSecretRef,omitempty"`

	// NestedGroupsEnabled enables recursive group membership resolution.
	// Maps to: dbms.security.ldap.authorization.nested_groups_enabled
	// +optional
	NestedGroupsEnabled *bool `json:"nestedGroupsEnabled,omitempty"`

	// NestedGroupsSearchFilter is the LDAP filter for resolving nested groups. {0} is replaced by the user DN.
	// For Active Directory, use: "(&(objectclass=group)(member:1.2.840.113556.1.4.1941:={0}))"
	// Maps to: dbms.security.ldap.authorization.nested_groups_search_filter
	// +optional
	NestedGroupsSearchFilter string `json:"nestedGroupsSearchFilter,omitempty"`
}

// Neo4jOIDCProviderSpec configures a single OIDC/SSO provider.
// The map key in AuthSpec.OIDC becomes the <provider> in dbms.security.oidc.<provider>.*.
type Neo4jOIDCProviderSpec struct {
	// DisplayName shown on the Neo4j Browser/Bloom login screen
	// Maps to: dbms.security.oidc.<name>.display_name
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// WellKnownDiscoveryURI is the OIDC discovery endpoint that auto-configures other endpoints.
	// Either this or manual endpoints (AuthEndpoint, TokenEndpoint, JWKSURI, Issuer) must be set.
	// Maps to: dbms.security.oidc.<name>.well_known_discovery_uri
	// +optional
	WellKnownDiscoveryURI string `json:"wellKnownDiscoveryURI,omitempty"`

	// AuthEndpoint is the authorization endpoint (manual override, auto-populated from discovery)
	// Maps to: dbms.security.oidc.<name>.auth_endpoint
	// +optional
	AuthEndpoint string `json:"authEndpoint,omitempty"`

	// TokenEndpoint is the token endpoint (manual override)
	// Maps to: dbms.security.oidc.<name>.token_endpoint
	// +optional
	TokenEndpoint string `json:"tokenEndpoint,omitempty"`

	// JWKSURI is the JSON Web Key Set endpoint for JWT signature verification (manual override)
	// Maps to: dbms.security.oidc.<name>.jwks_uri
	// +optional
	JWKSURI string `json:"jwksURI,omitempty"`

	// UserInfoURI is the UserInfo endpoint (manual override)
	// Maps to: dbms.security.oidc.<name>.user_info_uri
	// +optional
	UserInfoURI string `json:"userInfoURI,omitempty"`

	// Issuer identifier, validated against the JWT "iss" claim (manual override)
	// Maps to: dbms.security.oidc.<name>.issuer
	// +optional
	Issuer string `json:"issuer,omitempty"`

	// Audience is the expected JWT "aud" claim value (typically your OIDC client ID)
	// Maps to: dbms.security.oidc.<name>.audience
	// +kubebuilder:validation:Required
	Audience string `json:"audience"`

	// AuthFlow selects the OAuth2 flow: "pkce" (recommended) or "implicit"
	// Maps to: dbms.security.oidc.<name>.auth_flow
	// +kubebuilder:validation:Enum=pkce;implicit
	// +kubebuilder:default=pkce
	// +optional
	AuthFlow string `json:"authFlow,omitempty"`

	// Claims configures JWT claim mapping for username and group extraction
	// +optional
	Claims *OIDCClaimsSpec `json:"claims,omitempty"`

	// GetGroupsFromUserInfo fetches the groups claim from the UserInfo endpoint instead of the JWT
	// Maps to: dbms.security.oidc.<name>.get_groups_from_user_info
	// +optional
	GetGroupsFromUserInfo *bool `json:"getGroupsFromUserInfo,omitempty"`

	// GetUsernameFromUserInfo fetches the username claim from the UserInfo endpoint instead of the JWT
	// Maps to: dbms.security.oidc.<name>.get_username_from_user_info
	// +optional
	GetUsernameFromUserInfo *bool `json:"getUsernameFromUserInfo,omitempty"`

	// GroupToRoleMapping maps OIDC groups/roles to Neo4j roles.
	// Key: IdP group name, Value: comma-separated Neo4j roles.
	// Maps to: dbms.security.oidc.<name>.authorization.group_to_role_mapping
	// +optional
	GroupToRoleMapping map[string]string `json:"groupToRoleMapping,omitempty"`

	// AuthParams are additional parameters sent to the authorization endpoint (semicolon-separated key=value)
	// Maps to: dbms.security.oidc.<name>.auth_params
	// +optional
	AuthParams string `json:"authParams,omitempty"`

	// TokenParams are additional parameters sent to the token endpoint (semicolon-separated key=value)
	// Maps to: dbms.security.oidc.<name>.token_params
	// +optional
	TokenParams string `json:"tokenParams,omitempty"`
}

// OIDCClaimsSpec configures which JWT claims are used for username and group extraction
type OIDCClaimsSpec struct {
	// Username is the JWT claim used as the Neo4j database username (default: "sub")
	// Maps to: dbms.security.oidc.<name>.claims.username
	// +optional
	Username string `json:"username,omitempty"`

	// Groups is the JWT claim containing roles/groups for authorization mapping
	// Maps to: dbms.security.oidc.<name>.claims.groups
	// +optional
	Groups string `json:"groups,omitempty"`
}

// SecretKeyRef references a specific key within a Kubernetes Secret.
type SecretKeyRef struct {
	// +kubebuilder:validation:Required
	// Name of the Kubernetes Secret
	Name string `json:"name"`

	// Key within the Secret. The default depends on context (e.g., "ca.crt" for TrustStore, "token" for fleet management).
	// +optional
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

	// DNSName is the public DNS hostname that should resolve to this
	// deployment's front-facing Service (e.g. "neo4j.example.com").
	// When set, the operator:
	//   - Adds the `external-dns.alpha.kubernetes.io/hostname` annotation
	//     to the front-facing Service and (when enabled) the Ingress, so
	//     external-dns (https://github.com/kubernetes-sigs/external-dns)
	//     manages the matching cloud DNS record automatically.
	//   - Injects DNSName into the SAN list of the cert-manager Certificate
	//     when spec.tls is enabled, so TLS connections to the public name
	//     pass hostname verification.
	// external-dns must be installed separately. This field has no effect
	// on cluster routing — external clients still use single-endpoint
	// `bolt+s://` against this name; routing requires per-pod public
	// endpoints, which is not in scope here.
	// +optional
	DNSName string `json:"dnsName,omitempty"`

	// Ingress configuration
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Route configuration (OpenShift only)
	Route *RouteSpec `json:"route,omitempty"`
}

// IngressSpec defines ingress configuration
type IngressSpec struct {
	Enabled bool `json:"enabled,omitempty"`

	ClassName string `json:"className,omitempty"`

	Annotations map[string]string `json:"annotations,omitempty"`

	Host string `json:"host,omitempty"`

	TLSSecretName string `json:"tlsSecretName,omitempty"`
}

// RouteSpec defines OpenShift Route configuration
type RouteSpec struct {
	// Enable Route exposure (OpenShift only)
	Enabled bool `json:"enabled,omitempty"`

	// Hostname for the Route (optional; generated by OpenShift if empty)
	Host string `json:"host,omitempty"`

	// Path for the Route (defaults to \"/\")
	Path string `json:"path,omitempty"`

	// Annotations to add to the Route
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS configuration for the Route
	TLS *RouteTLSSpec `json:"tls,omitempty"`

	// Target port on the service (defaults to HTTP port 7474)
	TargetPort int32 `json:"targetPort,omitempty"`
}

// RouteTLSSpec defines TLS options for an OpenShift Route
type RouteTLSSpec struct {
	// +kubebuilder:validation:Enum=edge;reencrypt;passthrough
	Termination string `json:"termination,omitempty"`

	// +kubebuilder:validation:Enum=None;Allow;Redirect
	InsecureEdgeTerminationPolicy string `json:"insecureEdgeTerminationPolicy,omitempty"`

	// Secret containing the certificate (for reencrypt/passthrough)
	SecretName string `json:"secretName,omitempty"`
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

	// CredentialsSecretRef is the name of a Kubernetes Secret containing
	// cloud provider credentials as environment variables. Optional when
	// using workload identity / IAM instance profiles.
	// For S3:    keys AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION
	// For GCS:   key  GOOGLE_APPLICATION_CREDENTIALS_JSON (base64 service-account JSON)
	// For Azure: keys AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY
	CredentialsSecretRef string `json:"credentialsSecretRef,omitempty"`

	// EndpointURL overrides the S3 API endpoint URL. Use this to target
	// S3-compatible stores such as MinIO, Ceph RGW, or Cloudflare R2.
	// Example: "http://minio.minio-ns.svc:9000"
	// Only applies to the "aws" provider; ignored for gcp and azure.
	EndpointURL string `json:"endpointURL,omitempty"`

	// ForcePathStyle forces S3 path-style addressing, where the bucket name
	// appears in the URL path (e.g. http://endpoint/bucket/key) rather than
	// the subdomain (e.g. http://bucket.endpoint/key).
	// Required for MinIO and most self-hosted S3-compatible stores.
	// Only effective when EndpointURL is set.
	ForcePathStyle bool `json:"forcePathStyle,omitempty"`
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

	// Ready indicates if the cluster is ready for connections
	Ready bool `json:"ready,omitempty"`

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
	// - Neo4j version >= 2025.12.0
	// - Cluster phase = Ready
	// - Property sharding configuration applied successfully
	// - All required Neo4j configuration settings validated
	PropertyShardingReady *bool `json:"propertyShardingReady,omitempty"`

	// AuraFleetManagementStatus reports the current state of the Aura Fleet Management integration.
	AuraFleetManagement *AuraFleetManagementStatus `json:"auraFleetManagement,omitempty"`

	// Diagnostics holds the most recently collected live diagnostics from the cluster.
	// Populated when spec.monitoring.enabled=true and the cluster is Ready.
	// +optional
	Diagnostics *ClusterDiagnosticsStatus `json:"diagnostics,omitempty"`

	// ObservedGeneration reflects the generation most recently observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// AuraFleetManagementStatus reports the registration state of the Aura Fleet Management plugin.
type AuraFleetManagementStatus struct {
	// Registered is true once the deployment has successfully called
	// fleetManagement.registerToken and been acknowledged by Aura.
	Registered bool `json:"registered"`

	// LastRegistrationTime records when the token was last successfully registered.
	// +optional
	LastRegistrationTime *metav1.Time `json:"lastRegistrationTime,omitempty"`

	// Message provides additional information about the registration state,
	// including error details when registration fails.
	// +optional
	Message string `json:"message,omitempty"`
}

// ClusterDiagnosticsStatus holds the most recent live diagnostics collected from
// the Neo4j cluster via Cypher queries. Populated only when spec.monitoring.enabled=true
// and the cluster is in Ready phase.
type ClusterDiagnosticsStatus struct {
	// Servers lists the most recently observed state of each server in the cluster.
	// +optional
	Servers []ServerDiagnosticInfo `json:"servers,omitempty"`

	// Databases lists the most recently observed state of each database.
	// +optional
	Databases []DatabaseDiagnosticInfo `json:"databases,omitempty"`

	// Users lists the users currently present in the cluster's `system` database
	// (from SHOW USERS). Useful for observing the effect of Neo4jUser /
	// Neo4jRoleBinding reconciliation. Bounded summary — see UserCount for the
	// total when this list is truncated.
	// +optional
	Users []UserDiagnosticInfo `json:"users,omitempty"`

	// UserCount is the total number of users observed via SHOW USERS, even
	// when the Users slice is truncated for size.
	// +optional
	UserCount int `json:"userCount,omitempty"`

	// Roles lists the roles currently present in the cluster's `system`
	// database (from SHOW ROLES YIELD role, immutable). Useful for observing
	// the effect of Neo4jRole reconciliation.
	// +optional
	Roles []RoleDiagnosticInfo `json:"roles,omitempty"`

	// RoleCount is the total number of roles observed via SHOW ROLES, even
	// when the Roles slice is truncated for size.
	// +optional
	RoleCount int `json:"roleCount,omitempty"`

	// LastCollected is the timestamp of the most recent successful diagnostics collection.
	// +optional
	LastCollected *metav1.Time `json:"lastCollected,omitempty"`

	// CollectionError holds the last error message if diagnostics collection failed.
	// Empty when collection succeeds.
	// +optional
	CollectionError string `json:"collectionError,omitempty"`
}

// UserDiagnosticInfo summarises one row of `SHOW USERS`.
type UserDiagnosticInfo struct {
	// User is the username.
	User string `json:"user"`

	// Roles is the set of roles directly granted to the user (excluding the
	// implicit PUBLIC role).
	// +optional
	Roles []string `json:"roles,omitempty"`

	// Suspended is true when STATUS = SUSPENDED.
	// +optional
	Suspended bool `json:"suspended,omitempty"`

	// HomeDatabase is the user's configured home database, if any.
	// +optional
	HomeDatabase string `json:"homeDatabase,omitempty"`
}

// RoleDiagnosticInfo summarises one row of `SHOW ROLES YIELD role, immutable`.
type RoleDiagnosticInfo struct {
	// Role is the role name.
	Role string `json:"role"`

	// Immutable is true when the role was created with `CREATE IMMUTABLE ROLE`
	// or is one of the built-in immutable roles.
	// +optional
	Immutable bool `json:"immutable,omitempty"`
}

// ServerDiagnosticInfo holds the observed state of a single Neo4j server.
type ServerDiagnosticInfo struct {
	// Name is the server's display name (from SHOW SERVERS).
	Name string `json:"name"`

	// Address is the Bolt address of the server.
	Address string `json:"address"`

	// State is the server lifecycle state (e.g. "Enabled", "Cordoned", "Deallocating").
	State string `json:"state"`

	// Health is the server health status (e.g. "Available", "Unavailable").
	Health string `json:"health"`

	// HostingDatabases is the number of databases hosted by this server.
	HostingDatabases int `json:"hostingDatabases"`
}

// DatabaseDiagnosticInfo holds the observed state of a single Neo4j database.
type DatabaseDiagnosticInfo struct {
	// Name is the database name.
	Name string `json:"name"`

	// Status is the current operational status (e.g. "online", "offline", "quarantined").
	Status string `json:"status"`

	// RequestedStatus is the desired operational status.
	RequestedStatus string `json:"requestedStatus"`

	// Role is the database role on the most recently contacted server (e.g. "primary", "secondary").
	Role string `json:"role"`

	// Default indicates whether this is the default database.
	// +optional
	Default bool `json:"default,omitempty"`
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
	// +kubebuilder:default=RollingUpgrade
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
// EffectiveMinSystemPrimaries returns the value to write for
// dbms.cluster.minimum_initial_system_primaries_count at cluster formation —
// the system-DB primary count and the floor the cluster can be scaled down to.
// Explicit spec.topology.minSystemPrimaries wins (clamped to [2, servers]);
// otherwise the default is min(3, servers). See #173.
func (c *Neo4jEnterpriseCluster) EffectiveMinSystemPrimaries() int32 {
	servers := max(c.Spec.Topology.Servers, 2)
	if m := c.Spec.Topology.MinSystemPrimaries; m != nil {
		return min(max(*m, 2), servers)
	}
	return min(servers, 3)
}

type TopologyConfiguration struct {
	// Servers specifies the number of Neo4j servers in the cluster
	// Servers self-organize and can host databases in primary or secondary mode
	// The hard cap of 100 is an operator-imposed safety rail, not a Neo4j
	// limit. Realistic deployments rarely exceed ~10 servers; see the soft
	// warning emitted by internal/validation/topology_validator.go for the
	// coordination-overhead rationale. Scale reads via secondary replicas
	// (spec.topology.serverRoles) rather than raw server count.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Servers int32 `json:"servers"`

	// MinSystemPrimaries sets dbms.cluster.minimum_initial_system_primaries_count
	// — the number of servers that host the `system` database as a primary, which
	// is also the floor the cluster can be scaled DOWN to (Neo4j refuses to drop a
	// server below it). It is applied only at initial cluster formation.
	//
	// Default (unset): min(3, servers) — i.e. 2 for a 2-server cluster, 3 for any
	// cluster of 3+. This lets clusters of 3+ scale down to 3 (the recommended odd
	// quorum) while smaller ones keep their size. Raise it for more system-DB
	// resilience (pins a higher scale-down floor); it must be >= 2 and <= servers,
	// and an odd value (3, 5, …) is recommended (even counts give no clean write
	// majority). See #173.
	// +kubebuilder:validation:Minimum=2
	// +optional
	MinSystemPrimaries *int32 `json:"minSystemPrimaries,omitempty"`

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

	// Placement defines how instances should be distributed across the cluster
	// +optional
	Placement *PlacementConfig `json:"placement,omitempty"`

	// AvailabilityZones specifies the expected availability zones for distribution
	// +optional
	AvailabilityZones []string `json:"availabilityZones,omitempty"`

	// EnforceDistribution ensures servers are distributed across topology domains
	// +optional
	EnforceDistribution bool `json:"enforceDistribution,omitempty"`
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Servers",type=integer,JSONPath=`.spec.topology.servers`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jEnterpriseCluster declaratively manages a Neo4j Enterprise
// causal cluster (minimum 2 servers). The operator builds a single
// `{name}-server` StatefulSet whose pods self-organize into
// primary/secondary roles at runtime via `serverModeConstraint`,
// wires up V2 discovery on port 6000, manages cert-manager-driven
// TLS, plugin installation (APOC, GDS, Bloom, …), backup and restore
// integration, optional Aura Fleet Management registration, optional
// MCP server, and live cluster diagnostics. For single-node
// development workloads, use Neo4jEnterpriseStandalone instead.
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

// AuditSpec configures audit-grade logging — the Neo4j security log
// (authentication + admin commands) and query log (data access) — for
// compliance use cases (PCI / HIPAA / GDPR).
//
// Important framing: Neo4j 5.x / 2025.x has NO unified
// `dbms.security.audit.*` config (those were 4.x keys, removed). What
// modern Neo4j calls "audit logging" is the combination of
// security.log (controlled by `dbms.security.*` keys) and query.log
// (controlled by `db.logs.query.*` keys). This spec exposes the
// audit-relevant subset of those keys as typed fields so users don't
// have to hand-roll spec.config entries.
//
// Overlap with spec.monitoring: spec.monitoring owns the
// PERFORMANCE-monitoring view of the query log (slow-query threshold,
// plan capture). spec.audit owns the COMPLIANCE view (literal/parameter
// redaction, successful-auth logging). Where they overlap on
// `db.logs.query.obfuscate_literals`, spec.audit fields are emitted
// AFTER monitoring fields in the rendered neo4j.conf, so audit values
// take priority on the audit-relevant keys. User-supplied spec.config
// entries still win over both (they're appended last).
type AuditSpec struct {
	// Enabled, when true, opts the cluster into compliance-oriented
	// defaults: ObfuscateQueryLiterals defaults to true when unset.
	// When false (default), each field still behaves independently
	// when set, but the secure-by-default behavior of the
	// ObfuscateQueryLiterals nil case is not applied. This lets users
	// opt in to "the right thing by default" with one flag rather
	// than having to know which knobs matter.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// LogSuccessfulAuthentication controls whether successful login
	// events appear in security.log. Defaults to Neo4j upstream
	// (true) when unset. Set false in high-volume production
	// environments where success-rate logging would dominate the log
	// — failures remain logged.
	// Maps to: dbms.security.log_successful_authentication
	// +optional
	LogSuccessfulAuthentication *bool `json:"logSuccessfulAuthentication,omitempty"`

	// ObfuscateQueryLiterals controls whether literal values in
	// query.log are redacted. Neo4j's upstream default is false (raw
	// literals logged), which leaks any PII / password / secret
	// passed as a query literal into the log file. Strongly
	// recommended true for PCI / HIPAA / GDPR compliance.
	//
	// When spec.audit.enabled=true AND this field is unset, the
	// operator defaults the emitted value to true (secure-by-default).
	// When spec.audit.enabled=false OR this field is set explicitly,
	// the explicit value (or Neo4j default) wins.
	//
	// Note: Neo4j docs flag that obfuscation does not apply to node
	// labels, relationship types, or property keys. Set
	// ParameterLogging=false to also redact parameter VALUES.
	// Maps to: db.logs.query.obfuscate_literals
	// +optional
	ObfuscateQueryLiterals *bool `json:"obfuscateQueryLiterals,omitempty"`

	// ParameterLogging controls whether query parameter VALUES are
	// included in query.log. Defaults to Neo4j upstream (true) when
	// unset. Set false when parameter values themselves are sensitive
	// (passwords passed as parameters) and a query-shape audit trail
	// is sufficient.
	// Maps to: db.logs.query.parameter_logging_enabled
	// +optional
	ParameterLogging *bool `json:"parameterLogging,omitempty"`
}

// NetworkPolicySpec controls emission of a Kubernetes NetworkPolicy that
// hardens ingress to the Neo4j server pods — most importantly closing the
// backup port (6362) to pods other than operator-managed backup workloads.
//
// See spec.networkPolicy on Neo4jEnterpriseCluster /
// Neo4jEnterpriseStandalone for usage. Default is disabled — a
// NetworkPolicy enables only when the cluster's CNI plugin enforces them
// (Calico, Cilium, Antrea, Weave; NOT flannel), so enabling this on a
// non-enforcing CNI is a safe no-op rather than a failure.
type NetworkPolicySpec struct {
	// Enabled turns the NetworkPolicy on. When true, the operator emits a
	// NetworkPolicy that scopes ingress on port 6362 to operator-managed
	// backup pods and leaves public/peer ports open to their normal
	// callers. When false (default) no NetworkPolicy is emitted; any pod
	// that can reach the Service on 6362 can attempt a backup, per Neo4j
	// upstream behavior. See the Neo4j security checklist:
	// "failing to protect this port may open a security hole by which an
	// unauthorized user can make a copy of the database."
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// MonitoringSpec defines monitoring, metrics, and query logging configuration.
type MonitoringSpec struct {
	// +kubebuilder:default=true
	// Enable query monitoring
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:default="5s"
	// Slow query threshold — maps to db.logs.query.threshold in Neo4j config
	SlowQueryThreshold string `json:"slowQueryThreshold,omitempty"`

	// +kubebuilder:default=false
	// Enable query plan explanation in logs. WARNING: enabling this has measurable
	// performance impact on high-throughput workloads. Recommended false in production.
	ExplainPlan bool `json:"explainPlan,omitempty"`

	// +kubebuilder:default="INFO"
	// +kubebuilder:validation:Enum=OFF;INFO;VERBOSE
	// Query log verbosity level. Use OFF to disable query logging, INFO to
	// log only slow queries (those exceeding slowQueryThreshold), and
	// VERBOSE to log all queries regardless of duration.
	QueryLogLevel string `json:"queryLogLevel,omitempty"`

	// +kubebuilder:default=false
	// Obfuscate literal values in query logs. Recommended true in production
	// to avoid leaking sensitive data (passwords, PII) into log files.
	ObfuscateLiterals bool `json:"obfuscateLiterals,omitempty"`

	// Glob pattern to select which Neo4j metrics are active (maps to server.metrics.filter).
	// Only a subset of metrics is enabled by default. Use "*" to enable all metrics,
	// or specific patterns like "*bolt*,*transaction*,*page_cache*".
	MetricsFilter string `json:"metricsFilter,omitempty"`

	// Custom prefix for all Neo4j metric names (maps to server.metrics.prefix).
	// Useful when multiple Neo4j deployments share one Prometheus instance.
	MetricsPrefix string `json:"metricsPrefix,omitempty"`

	// Query sampling configuration
	Sampling *QuerySamplingConfig `json:"sampling,omitempty"`

	// Metrics export configuration
	MetricsExport *QueryMetricsExportConfig `json:"metricsExport,omitempty"`
}

// QuerySamplingConfig defines query sampling
type QuerySamplingConfig struct {
	// Sampling rate as a decimal between 0 and 1 inclusive — for example
	// "0.5" for 50% sampling, "1.0" for every query, "0" to disable.
	// +kubebuilder:validation:Pattern=`^(0(\.\d+)?|1(\.0+)?)$`
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

// AuraFleetManagementSpec defines configuration for Neo4j Aura Fleet Management integration.
//
// Fleet Management allows you to monitor all Neo4j deployments (both Aura-managed and
// self-managed) from a single Neo4j Aura console view. The operator installs the
// pre-bundled fleet-management plugin and registers the provided Aura token automatically.
//
// Setup workflow:
//  1. In the Aura console, navigate to Instances → Self-managed → Add deployment
//  2. Follow the wizard to generate a registration token
//  3. Store the token in a Kubernetes Secret
//  4. Reference the Secret in this spec
//
// See: https://neo4j.com/docs/aura/fleet-management/setup/
type AuraFleetManagementSpec struct {
	// Enabled activates Aura Fleet Management integration.
	// When true, the fleet-management plugin is installed automatically
	// (from the pre-bundled jar in the Neo4j Enterprise image) and the
	// registration token is applied after the cluster becomes ready.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// TokenSecretRef references a Kubernetes Secret containing the Aura
	// Fleet Management registration token obtained from the Aura console wizard.
	//
	// The Secret must be in the same namespace as the cluster and contain
	// the key specified by tokenSecretRef.key (defaults to "token").
	//
	// Example:
	//   kubectl create secret generic aura-fleet-token --from-literal=token='<token-from-aura>'
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
}

// PropertyShardingSpec defines property sharding configuration
// for Neo4j 2025.12+ (Infinigraph) to enable separated storage of graph topology and properties
type PropertyShardingSpec struct {
	// Enable property sharding support on this cluster
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Property sharding specific Neo4j configuration
	// Applied when propertySharding.enabled is true
	// These settings are required for Neo4j property sharding functionality
	Config map[string]string `json:"config,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Neo4jEnterpriseCluster{}, &Neo4jEnterpriseClusterList{})
}
