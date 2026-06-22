package resources_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildAuthConfig_NilAuth(t *testing.T) {
	result := resources.BuildAuthConfig(nil)
	assert.Empty(t, result.Config)
	assert.Empty(t, result.GeneratedKeys)
}

func TestBuildAuthConfig_NativeOnly(t *testing.T) {
	// Empty auth spec (default native) doesn't generate provider lines
	auth := &neo4jv1beta1.AuthSpec{}
	result := resources.BuildAuthConfig(auth)
	assert.Empty(t, result.Config)
}

func TestBuildAuthConfig_ExplicitNative(t *testing.T) {
	// Explicitly setting native generates the config line
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"native"},
	}
	result := resources.BuildAuthConfig(auth)
	assert.Contains(t, result.Config, "dbms.security.authentication_providers=native")
}

func TestBuildAuthConfig_BackwardCompat_SingleProvider(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"ldap"},
		AuthorizationProviders:  []string{"ldap"},
		LDAP: &neo4jv1beta1.Neo4jLDAPSpec{
			Host: "ldap://ldap.example.com",
		},
	}
	result := resources.BuildAuthConfig(auth)
	assert.Contains(t, result.Config, "dbms.security.authentication_providers=ldap")
	assert.Contains(t, result.Config, "dbms.security.authorization_providers=ldap")
	assert.Contains(t, result.Config, "dbms.security.ldap.host=ldap://ldap.example.com")
}

func TestBuildAuthConfig_MultiProvider(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"ldap", "native"},
		AuthorizationProviders:  []string{"ldap", "native"},
		LDAP: &neo4jv1beta1.Neo4jLDAPSpec{
			Host: "ldaps://ad.corp.example.com:636",
		},
	}
	result := resources.BuildAuthConfig(auth)
	assert.Contains(t, result.Config, "dbms.security.authentication_providers=ldap,native")
	assert.Contains(t, result.Config, "dbms.security.authorization_providers=ldap,native")
	assert.Contains(t, result.Config, "dbms.security.ldap.host=ldaps://ad.corp.example.com:636")
}

func TestBuildAuthConfig_LDAPFullConfig(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"ldap", "native"},
		AuthorizationProviders:  []string{"ldap", "native"},
		LDAP: &neo4jv1beta1.Neo4jLDAPSpec{
			Host:        "ldap://ldap.example.com",
			UseStartTLS: ptr.To(true),
			Authentication: &neo4jv1beta1.LDAPAuthenticationSpec{
				UserDNTemplate:     "{0}@example.com",
				SearchForAttribute: ptr.To(false),
				CacheEnabled:       ptr.To(true),
			},
			Authorization: &neo4jv1beta1.LDAPAuthorizationSpec{
				UserSearchBase:            "dc=example,dc=com",
				UserSearchFilter:          "(&(objectClass=user)(sAMAccountName={0}))",
				GroupMembershipAttributes: []string{"memberOf"},
				GroupToRoleMapping: map[string]string{
					"cn=Neo4j Admin,dc=example,dc=com":  "admin",
					"cn=Neo4j Reader,dc=example,dc=com": "reader,editor",
				},
				AccessPermittedGroup:     "cn=Neo4j Users,dc=example,dc=com",
				UseSystemAccount:         ptr.To(true),
				SystemAccountSecretRef:   "ldap-system-secret",
				NestedGroupsEnabled:      ptr.To(true),
				NestedGroupsSearchFilter: "(&(objectclass=group)(member:1.2.840.113556.1.4.1941:={0}))",
			},
			DebugGroupLogging: ptr.To(false),
		},
		AuthCacheTTL: "5m",
	}

	result := resources.BuildAuthConfig(auth)
	config := result.Config

	// Provider lines
	assert.Contains(t, config, "dbms.security.authentication_providers=ldap,native")

	// LDAP host and StartTLS
	assert.Contains(t, config, "dbms.security.ldap.host=ldap://ldap.example.com")
	assert.Contains(t, config, "dbms.security.ldap.use_starttls=true")

	// Authentication
	assert.Contains(t, config, "dbms.security.ldap.authentication.user_dn_template={0}@example.com")
	assert.Contains(t, config, "dbms.security.ldap.authentication.search_for_attribute=false")
	assert.Contains(t, config, "dbms.security.ldap.authentication.cache_enabled=true")

	// Authorization
	assert.Contains(t, config, "dbms.security.ldap.authorization.user_search_base=dc=example,dc=com")
	assert.Contains(t, config, "dbms.security.ldap.authorization.user_search_filter=(&(objectClass=user)(sAMAccountName={0}))")
	assert.Contains(t, config, "dbms.security.ldap.authorization.group_membership_attributes=memberOf")
	assert.Contains(t, config, "dbms.security.ldap.authorization.access_permitted_group=cn=Neo4j Users,dc=example,dc=com")
	assert.Contains(t, config, "dbms.security.ldap.authorization.use_system_account=true")
	assert.Contains(t, config, "dbms.security.ldap.authorization.nested_groups_enabled=true")
	assert.Contains(t, config, "dbms.security.ldap.authorization.nested_groups_search_filter=(&(objectclass=group)(member:1.2.840.113556.1.4.1941:={0}))")

	// Group-to-role mapping (semicolon-separated, sorted by key)
	assert.Contains(t, config, `dbms.security.ldap.authorization.group_to_role_mapping=`)
	// Verify the mapping contains both entries
	for _, line := range strings.Split(config, "\n") {
		if strings.HasPrefix(line, "dbms.security.ldap.authorization.group_to_role_mapping=") {
			assert.Contains(t, line, `"cn=Neo4j Admin,dc=example,dc=com"=admin`)
			assert.Contains(t, line, `"cn=Neo4j Reader,dc=example,dc=com"=reader,editor`)
			assert.Contains(t, line, ";") // semicolon separator
		}
	}

	// System password must NOT be in config (injected via env var)
	assert.NotContains(t, config, "system_password")
	assert.NotContains(t, config, "system_username")

	// Debug logging
	assert.Contains(t, config, "dbms.security.logs.ldap.groups_at_debug_level_enabled=false")

	// Cache TTL
	assert.Contains(t, config, "dbms.security.auth_cache_ttl=5m")

	// Generated keys should include all the keys we set
	require.NotEmpty(t, result.GeneratedKeys)
	assert.Contains(t, result.GeneratedKeys, "dbms.security.ldap.host")
	assert.Contains(t, result.GeneratedKeys, "dbms.security.authentication_providers")
	assert.Contains(t, result.GeneratedKeys, "dbms.security.auth_cache_ttl")
}

func TestBuildAuthConfig_OIDCSingleProvider(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"oidc-okta", "native"},
		AuthorizationProviders:  []string{"oidc-okta", "native"},
		OIDC: map[string]neo4jv1beta1.Neo4jOIDCProviderSpec{
			"okta": {
				DisplayName:           "Okta SSO",
				WellKnownDiscoveryURI: "https://dev-123.okta.com/.well-known/openid-configuration",
				Audience:              "0oa1234567",
				AuthFlow:              "pkce",
				Claims: &neo4jv1beta1.OIDCClaimsSpec{
					Username: "email",
					Groups:   "groups",
				},
				GetGroupsFromUserInfo: ptr.To(true),
				GroupToRoleMapping: map[string]string{
					"neo4j-admins":  "admin,architect",
					"neo4j-readers": "reader",
				},
			},
		},
	}

	result := resources.BuildAuthConfig(auth)
	config := result.Config

	assert.Contains(t, config, "dbms.security.authentication_providers=oidc-okta,native")
	assert.Contains(t, config, "dbms.security.oidc.okta.display_name=Okta SSO")
	assert.Contains(t, config, "dbms.security.oidc.okta.well_known_discovery_uri=https://dev-123.okta.com/.well-known/openid-configuration")
	assert.Contains(t, config, "dbms.security.oidc.okta.audience=0oa1234567")
	assert.Contains(t, config, "dbms.security.oidc.okta.auth_flow=pkce")
	assert.Contains(t, config, "dbms.security.oidc.okta.claims.username=email")
	assert.Contains(t, config, "dbms.security.oidc.okta.claims.groups=groups")
	assert.Contains(t, config, "dbms.security.oidc.okta.get_groups_from_user_info=true")
	assert.Contains(t, config, "dbms.security.oidc.okta.authorization.group_to_role_mapping=")
}

func TestBuildAuthConfig_OIDCMultipleProviders(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"oidc-okta", "oidc-azure", "native"},
		AuthorizationProviders:  []string{"oidc-okta", "oidc-azure", "native"},
		OIDC: map[string]neo4jv1beta1.Neo4jOIDCProviderSpec{
			"okta": {
				WellKnownDiscoveryURI: "https://dev-123.okta.com/.well-known/openid-configuration",
				Audience:              "okta-client-id",
			},
			"azure": {
				WellKnownDiscoveryURI: "https://login.microsoftonline.com/tenant/v2.0/.well-known/openid-configuration",
				Audience:              "azure-client-id",
				Claims: &neo4jv1beta1.OIDCClaimsSpec{
					Username: "preferred_username",
					Groups:   "roles",
				},
			},
		},
	}

	result := resources.BuildAuthConfig(auth)
	config := result.Config

	// Both providers should be present (sorted alphabetically)
	assert.Contains(t, config, "dbms.security.oidc.azure.audience=azure-client-id")
	assert.Contains(t, config, "dbms.security.oidc.okta.audience=okta-client-id")

	// Azure provider should have claims
	assert.Contains(t, config, "dbms.security.oidc.azure.claims.username=preferred_username")
}

func TestBuildAuthConfig_OIDCManualEndpoints(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"oidc-custom", "native"},
		AuthorizationProviders:  []string{"oidc-custom", "native"},
		OIDC: map[string]neo4jv1beta1.Neo4jOIDCProviderSpec{
			"custom": {
				AuthEndpoint:  "https://idp.example.com/authorize",
				TokenEndpoint: "https://idp.example.com/token",
				JWKSURI:       "https://idp.example.com/.well-known/jwks.json",
				UserInfoURI:   "https://idp.example.com/userinfo",
				Issuer:        "https://idp.example.com/",
				Audience:      "my-app",
				AuthParams:    "scope=openid+email",
				TokenParams:   "grant_type=authorization_code",
			},
		},
	}

	result := resources.BuildAuthConfig(auth)
	config := result.Config

	assert.Contains(t, config, "dbms.security.oidc.custom.auth_endpoint=https://idp.example.com/authorize")
	assert.Contains(t, config, "dbms.security.oidc.custom.token_endpoint=https://idp.example.com/token")
	assert.Contains(t, config, "dbms.security.oidc.custom.jwks_uri=https://idp.example.com/.well-known/jwks.json")
	assert.Contains(t, config, "dbms.security.oidc.custom.issuer=https://idp.example.com/")
	assert.Contains(t, config, "dbms.security.oidc.custom.auth_params=scope=openid+email")
	assert.Contains(t, config, "dbms.security.oidc.custom.token_params=grant_type=authorization_code")
	assert.NotContains(t, config, "well_known_discovery_uri") // not set
}

func TestBuildAuthEnvVars_NoLDAP(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		AuthenticationProviders: []string{"native"},
	}
	envVars := resources.BuildAuthEnvVars(auth)
	assert.Empty(t, envVars)
}

func TestBuildAuthEnvVars_LDAPSystemAccount(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		LDAP: &neo4jv1beta1.Neo4jLDAPSpec{
			Host: "ldap://ldap.example.com",
			Authorization: &neo4jv1beta1.LDAPAuthorizationSpec{
				UseSystemAccount:       ptr.To(true),
				SystemAccountSecretRef: "ldap-bind-secret",
			},
		},
	}

	envVars := resources.BuildAuthEnvVars(auth)
	require.Len(t, envVars, 2)

	assert.Equal(t, "NEO4J_dbms_security_ldap_authorization_system__username", envVars[0].Name)
	assert.Equal(t, "ldap-bind-secret", envVars[0].ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "username", envVars[0].ValueFrom.SecretKeyRef.Key)

	assert.Equal(t, "NEO4J_dbms_security_ldap_authorization_system__password", envVars[1].Name)
	assert.Equal(t, "ldap-bind-secret", envVars[1].ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "password", envVars[1].ValueFrom.SecretKeyRef.Key)
}

func TestBuildAuthEnvVars_LDAPNoSystemAccount(t *testing.T) {
	auth := &neo4jv1beta1.AuthSpec{
		LDAP: &neo4jv1beta1.Neo4jLDAPSpec{
			Host: "ldap://ldap.example.com",
			Authorization: &neo4jv1beta1.LDAPAuthorizationSpec{
				UseSystemAccount: ptr.To(false),
			},
		},
	}
	envVars := resources.BuildAuthEnvVars(auth)
	assert.Empty(t, envVars)
}

func TestBuildTrustStoreInitContainer_SingleCA(t *testing.T) {
	cas := []neo4jv1beta1.TrustedCASecret{
		{Name: "my-ca-secret", Key: "custom-ca.pem"},
	}

	container := resources.BuildTrustStoreInitContainer("neo4j:5.26-enterprise", cas)

	assert.Equal(t, "truststore-init", container.Name)
	assert.Equal(t, "neo4j:5.26-enterprise", container.Image)
	// Init container seeds from JDK cacerts then imports each CA
	assert.Contains(t, container.Command[2], "JAVA_HOME")
	assert.Contains(t, container.Command[2], "cacerts")
	assert.Contains(t, container.Command[2], "/trusted-ca/my-ca-secret/custom-ca.pem")
	assert.Contains(t, container.Command[2], "keytool")
	// 1 truststore EmptyDir + 1 per-CA Secret mount
	require.Len(t, container.VolumeMounts, 2)
}

func TestBuildTrustStoreInitContainer_MultipleCAs(t *testing.T) {
	cas := []neo4jv1beta1.TrustedCASecret{
		{Name: "oidc-ca"}, // default key ca.crt
		{Name: "ldap-ca", Key: "ldap.pem"},
	}

	container := resources.BuildTrustStoreInitContainer("neo4j:5.26-enterprise", cas)

	// Each CA gets its own mount + alias
	assert.Contains(t, container.Command[2], "/trusted-ca/oidc-ca/ca.crt")
	assert.Contains(t, container.Command[2], "/trusted-ca/ldap-ca/ldap.pem")
	assert.Contains(t, container.Command[2], `-alias "oidc-ca"`)
	assert.Contains(t, container.Command[2], `-alias "ldap-ca"`)
	require.Len(t, container.VolumeMounts, 3) // truststore + 2 CAs
}

func TestBuildTrustStoreInitContainer_Empty(t *testing.T) {
	container := resources.BuildTrustStoreInitContainer("neo4j:5.26-enterprise", nil)
	assert.Empty(t, container.Name, "no CAs → no init container")
}

func TestBuildTrustStoreVolumes(t *testing.T) {
	cas := []neo4jv1beta1.TrustedCASecret{
		{Name: "my-ca-secret"},
	}

	volumes := resources.BuildTrustStoreVolumes(cas)
	require.Len(t, volumes, 2)

	assert.Equal(t, "trusted-ca-my-ca-secret", volumes[0].Name)
	assert.Equal(t, "my-ca-secret", volumes[0].Secret.SecretName)

	assert.Equal(t, "truststore", volumes[1].Name)
	assert.NotNil(t, volumes[1].EmptyDir)
}

func TestCollectTrustedCASecrets_LegacyOnly(t *testing.T) {
	legacy := &neo4jv1beta1.SecretKeyRef{Name: "legacy-ca", Key: "ca.crt"}
	cas := resources.CollectTrustedCASecrets(legacy, nil)
	require.Len(t, cas, 1)
	assert.Equal(t, "legacy-ca", cas[0].Name)
}

func TestCollectTrustedCASecrets_PluralOnly(t *testing.T) {
	plural := []neo4jv1beta1.TrustedCASecret{{Name: "a"}, {Name: "b"}}
	cas := resources.CollectTrustedCASecrets(nil, plural)
	require.Len(t, cas, 2)
}

func TestCollectTrustedCASecrets_PluralWinsOverLegacyOnDuplicate(t *testing.T) {
	legacy := &neo4jv1beta1.SecretKeyRef{Name: "shared", Key: "legacy-key"}
	plural := []neo4jv1beta1.TrustedCASecret{{Name: "shared", Key: "plural-key"}}
	cas := resources.CollectTrustedCASecrets(legacy, plural)
	require.Len(t, cas, 1, "duplicate Secret name de-duplicated")
	assert.Equal(t, "plural-key", cas[0].Key, "explicit list wins over legacy field")
}

func TestCollectTrustedCASecrets_Both(t *testing.T) {
	legacy := &neo4jv1beta1.SecretKeyRef{Name: "legacy-only"}
	plural := []neo4jv1beta1.TrustedCASecret{{Name: "plural-1"}, {Name: "plural-2"}}
	cas := resources.CollectTrustedCASecrets(legacy, plural)
	require.Len(t, cas, 3)
	// Plural list comes first (matches code order)
	assert.Equal(t, "plural-1", cas[0].Name)
	assert.Equal(t, "plural-2", cas[1].Name)
	assert.Equal(t, "legacy-only", cas[2].Name)
}

// TestBuildAuthConfig_LDAP_UseStartTLSDefault pins the secure-by-default
// behavior for plain ldap:// hosts: when UseStartTLS is unset, the
// operator emits use_starttls=true to force TLS upgrade on the bind.
// Neo4j's own default is false (silent unencrypted bind); without this
// the most common LDAP misconfig in production deployments was
// "I forgot to set useStartTLS".
//
// Per the Neo4j security checklist:
// "Configure your LDAP system with encryption via StartTLS."
func TestBuildAuthConfig_LDAP_UseStartTLSDefault(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		useStartTLS *bool
		want        string // expected value of use_starttls=, or "" if line absent
	}{
		{
			name:        "plain ldap:// + unset → default to true",
			host:        "ldap://ldap.example.com",
			useStartTLS: nil,
			want:        "true",
		},
		{
			name:        "plain LDAP:// (uppercase) + unset → default to true",
			host:        "LDAP://ldap.example.com",
			useStartTLS: nil,
			want:        "true",
		},
		{
			name:        "ldap:// + explicit false → honors opt-out (dev / mock LDAP)",
			host:        "ldap://mock-ldap:389",
			useStartTLS: ptr.To(false),
			want:        "false",
		},
		{
			name:        "ldap:// + explicit true → honors explicit value",
			host:        "ldap://ldap.example.com",
			useStartTLS: ptr.To(true),
			want:        "true",
		},
		{
			name:        "ldaps:// + unset → no line (already TLS at protocol level)",
			host:        "ldaps://ldap.example.com:636",
			useStartTLS: nil,
			want:        "", // no use_starttls line at all
		},
		{
			name:        "ldaps:// + explicit true → still emitted if user asked",
			host:        "ldaps://ldap.example.com:636",
			useStartTLS: ptr.To(true),
			want:        "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &neo4jv1beta1.AuthSpec{
				LDAP: &neo4jv1beta1.Neo4jLDAPSpec{
					Host:        tt.host,
					UseStartTLS: tt.useStartTLS,
				},
			}
			cfg := resources.BuildAuthConfig(auth).Config

			if tt.want == "" {
				// Explicit "absence" check — `Contains "use_starttls"` on
				// the raw config would also match commented lines if any
				// were ever added; use a line-level check instead.
				for _, line := range strings.Split(cfg, "\n") {
					assert.False(t, strings.HasPrefix(strings.TrimSpace(line), "dbms.security.ldap.use_starttls"),
						"no use_starttls line should be emitted for host=%s + UseStartTLS=nil; got line: %q",
						tt.host, line)
				}
				return
			}
			assert.Contains(t, cfg, "dbms.security.ldap.use_starttls="+tt.want,
				"want use_starttls=%s for host=%s UseStartTLS=%v; got:\n%s",
				tt.want, tt.host, tt.useStartTLS, cfg)
		})
	}
}
