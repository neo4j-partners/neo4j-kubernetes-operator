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

package validation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

func clusterWithAuth(provider string) *neo4jv1alpha1.Neo4jEnterpriseCluster {
	var auth *neo4jv1alpha1.AuthSpec
	if provider != "" {
		auth = &neo4jv1alpha1.AuthSpec{
			AuthenticationProviders: []string{provider},
		}
	}
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: auth,
		},
	}
}

// ---- Backward compatibility tests (old Provider field) ----

func TestAuthValidator_Validate_NilAuth(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors for nil auth, got: %v", errs)
	}
}

func TestAuthValidator_BackwardCompat_OldProviderField(t *testing.T) {
	v := NewAuthValidator()

	cases := []struct {
		name     string
		provider string
		wantErrs int
	}{
		{"native provider - no errors", "native", 0},
		{"ldap provider - no errors", "ldap", 0},
		{"kerberos provider - no errors", "kerberos", 0},
		{"jwt provider - no errors", "jwt", 0},
		{"invalid provider - NotSupported", "invalid", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := clusterWithAuth(tc.provider)
			errs := v.Validate(cluster)
			if len(errs) != tc.wantErrs {
				t.Errorf("expected %d errors, got %d: %v", tc.wantErrs, len(errs), errs)
			}
		})
	}
}

func TestAuthValidator_BackwardCompat_LDAPWithTypedConfig(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AuthenticationProviders: []string{"ldap"},
				AuthorizationProviders:  []string{"ldap"},
				LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
					Host: "ldap://ldap.example.com",
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors when typed LDAP config replaces secretRef, got: %v", errs)
	}
}

// ---- Multi-provider list tests ----

func TestAuthValidator_ProviderList_Valid(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AuthenticationProviders: []string{"ldap", "native"},
				AuthorizationProviders:  []string{"ldap", "native"},
				LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
					Host: "ldap://ldap.example.com",
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestAuthValidator_ProviderList_OIDCFormat(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AuthenticationProviders: []string{"oidc-okta", "native"},
				AuthorizationProviders:  []string{"oidc-okta", "native"},
				OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
					"okta": {
						WellKnownDiscoveryURI: "https://dev-123.okta.com/.well-known/openid-configuration",
						Audience:              "client-id",
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors for oidc-<name> format, got: %v", errs)
	}
}

func TestAuthValidator_ProviderList_InvalidName(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AuthenticationProviders: []string{"invalid-provider"},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for invalid provider name, got %d: %v", len(errs), errs)
	}
}

// ---- LDAP typed field validation ----

func TestAuthValidator_LDAP_HostRequired(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				AuthenticationProviders: []string{"ldap", "native"},
				LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
					Host: "", // empty
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for empty LDAP host, got %d: %v", len(errs), errs)
	}
}

func TestAuthValidator_LDAP_SystemAccountRequiresSecret(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
					Host: "ldap://ldap.example.com",
					Authorization: &neo4jv1alpha1.LDAPAuthorizationSpec{
						UseSystemAccount:       ptr.To(true),
						SystemAccountSecretRef: "", // missing
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for missing systemAccountSecretRef, got %d: %v", len(errs), errs)
	}
}

func TestAuthValidator_LDAP_SystemAccountWithSecret_OK(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				LDAP: &neo4jv1alpha1.Neo4jLDAPSpec{
					Host: "ldap://ldap.example.com",
					Authorization: &neo4jv1alpha1.LDAPAuthorizationSpec{
						UseSystemAccount:       ptr.To(true),
						SystemAccountSecretRef: "ldap-bind-creds",
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// ---- OIDC validation ----

func TestAuthValidator_OIDC_AudienceRequired(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
					"okta": {
						WellKnownDiscoveryURI: "https://dev-123.okta.com/.well-known/openid-configuration",
						Audience:              "", // missing
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for missing audience, got %d: %v", len(errs), errs)
	}
}

func TestAuthValidator_OIDC_EndpointsRequired(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
					"custom": {
						Audience: "my-app",
						// No discovery URI and no manual endpoints
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for missing endpoints, got %d: %v", len(errs), errs)
	}
}

func TestAuthValidator_OIDC_ManualEndpoints_OK(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
					"custom": {
						Audience:      "my-app",
						AuthEndpoint:  "https://idp.example.com/authorize",
						TokenEndpoint: "https://idp.example.com/token",
						JWKSURI:       "https://idp.example.com/jwks",
						Issuer:        "https://idp.example.com/",
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestAuthValidator_OIDC_InvalidProviderName(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				OIDC: map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec{
					"123-bad": { // starts with number
						WellKnownDiscoveryURI: "https://example.com/.well-known/openid-configuration",
						Audience:              "my-app",
					},
				},
			},
		},
	}
	errs := v.Validate(cluster)
	hasNameError := false
	for _, err := range errs {
		if err.Field == `spec.auth.oidc[123-bad]` {
			hasNameError = true
		}
	}
	if !hasNameError {
		t.Errorf("expected error for invalid OIDC provider name, got: %v", errs)
	}
}

// ---- TrustStore validation ----

func TestAuthValidator_TrustStore_SecretRefRequired(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				TrustStore: &neo4jv1alpha1.SecretKeyRef{
					Name: "", // empty
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for missing trustStore.secretRef, got %d: %v", len(errs), errs)
	}
}

func TestAuthValidator_TrustStore_Valid(t *testing.T) {
	v := NewAuthValidator()
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Auth: &neo4jv1alpha1.AuthSpec{
				TrustStore: &neo4jv1alpha1.SecretKeyRef{
					Name: "my-ca-cert",
					Key:  "ca.crt",
				},
			},
		},
	}
	errs := v.Validate(cluster)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}
