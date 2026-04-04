package resources_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildAuthConfig_NilAuth(t *testing.T) {
	result := resources.BuildAuthConfig(nil)
	assert.Empty(t, result.Config)
	assert.Empty(t, result.GeneratedKeys)
}

func TestBuildAuthConfig_NativeOnly(t *testing.T) {
	// Empty auth spec (default native) doesn't generate provider lines
	auth := &neo4jv1alpha1.AuthSpec{}
	result := resources.BuildAuthConfig(auth)
	assert.Empty(t, result.Config)
}

func TestBuildAuthConfig_ExplicitNative(t *testing.T) {
	// Explicitly setting native generates the config line
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"native"},
	}
	result := resources.BuildAuthConfig(auth)
	assert.Contains(t, result.Config, "dbms.security.authentication_providers=native")
}

func TestBuildAuthConfig_BackwardCompat_SingleProvider(t *testing.T) {
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"ldap"},
		AuthorizationProviders:  []string{"ldap"},
		LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
			Host: "ldap://ldap.example.com",
		},
	}
	result := resources.BuildAuthConfig(auth)
	assert.Contains(t, result.Config, "dbms.security.authentication_providers=ldap")
	assert.Contains(t, result.Config, "dbms.security.authorization_providers=ldap")
	assert.Contains(t, result.Config, "dbms.security.ldap.host=ldap://ldap.example.com")
}

func TestBuildAuthConfig_MultiProvider(t *testing.T) {
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"ldap", "native"},
		AuthorizationProviders:  []string{"ldap", "native"},
		LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
			Host: "ldaps://ad.corp.example.com:636",
		},
	}
	result := resources.BuildAuthConfig(auth)
	assert.Contains(t, result.Config, "dbms.security.authentication_providers=ldap,native")
	assert.Contains(t, result.Config, "dbms.security.authorization_providers=ldap,native")
	assert.Contains(t, result.Config, "dbms.security.ldap.host=ldaps://ad.corp.example.com:636")
}

func TestBuildAuthConfig_LDAPFullConfig(t *testing.T) {
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"ldap", "native"},
		AuthorizationProviders:  []string{"ldap", "native"},
		LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
			Host:        "ldap://ldap.example.com",
			UseStartTLS: ptr.To(true),
			Authentication: &neo4jv1alpha1.LDAPAuthenticationSpec{
				UserDNTemplate:     "{0}@example.com",
				SearchForAttribute: ptr.To(false),
				CacheEnabled:       ptr.To(true),
			},
			Authorization: &neo4jv1alpha1.LDAPAuthorizationSpec{
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
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"oidc-okta", "native"},
		AuthorizationProviders:  []string{"oidc-okta", "native"},
		OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
			"okta": {
				DisplayName:           "Okta SSO",
				WellKnownDiscoveryURI: "https://dev-123.okta.com/.well-known/openid-configuration",
				Audience:              "0oa1234567",
				AuthFlow:              "pkce",
				Claims: &neo4jv1alpha1.OIDCClaimsSpec{
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
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"oidc-okta", "oidc-azure", "native"},
		AuthorizationProviders:  []string{"oidc-okta", "oidc-azure", "native"},
		OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
			"okta": {
				WellKnownDiscoveryURI: "https://dev-123.okta.com/.well-known/openid-configuration",
				Audience:              "okta-client-id",
			},
			"azure": {
				WellKnownDiscoveryURI: "https://login.microsoftonline.com/tenant/v2.0/.well-known/openid-configuration",
				Audience:              "azure-client-id",
				Claims: &neo4jv1alpha1.OIDCClaimsSpec{
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
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"oidc-custom", "native"},
		AuthorizationProviders:  []string{"oidc-custom", "native"},
		OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
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
	auth := &neo4jv1alpha1.AuthSpec{
		AuthenticationProviders: []string{"native"},
	}
	envVars := resources.BuildAuthEnvVars(auth)
	assert.Empty(t, envVars)
}

func TestBuildAuthEnvVars_LDAPSystemAccount(t *testing.T) {
	auth := &neo4jv1alpha1.AuthSpec{
		LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
			Host: "ldap://ldap.example.com",
			Authorization: &neo4jv1alpha1.LDAPAuthorizationSpec{
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
	auth := &neo4jv1alpha1.AuthSpec{
		LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
			Host: "ldap://ldap.example.com",
			Authorization: &neo4jv1alpha1.LDAPAuthorizationSpec{
				UseSystemAccount: ptr.To(false),
			},
		},
	}
	envVars := resources.BuildAuthEnvVars(auth)
	assert.Empty(t, envVars)
}

func TestBuildTrustStoreInitContainer(t *testing.T) {
	trustStore := &neo4jv1alpha1.SecretKeyRef{
		Name: "my-ca-secret",
		Key:  "custom-ca.pem",
	}

	container := resources.BuildTrustStoreInitContainer("neo4j:5.26-enterprise", trustStore)

	assert.Equal(t, "truststore-init", container.Name)
	assert.Equal(t, "neo4j:5.26-enterprise", container.Image)
	assert.Contains(t, container.Command[2], "/truststore-ca/custom-ca.pem")
	assert.Contains(t, container.Command[2], "keytool")
	require.Len(t, container.VolumeMounts, 2)
}

func TestBuildTrustStoreVolumes(t *testing.T) {
	trustStore := &neo4jv1alpha1.SecretKeyRef{
		Name: "my-ca-secret",
	}

	volumes := resources.BuildTrustStoreVolumes(trustStore)
	require.Len(t, volumes, 2)

	assert.Equal(t, "truststore-ca", volumes[0].Name)
	assert.Equal(t, "my-ca-secret", volumes[0].Secret.SecretName)

	assert.Equal(t, "truststore", volumes[1].Name)
	assert.NotNil(t, volumes[1].EmptyDir)
}
