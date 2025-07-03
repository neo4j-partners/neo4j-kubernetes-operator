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

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestSecurityValidator_Validate(t *testing.T) {
	validator := NewSecurityValidator()

	tests := []struct {
		name        string
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectError bool
		errorCount  int
	}{
		{
			name: "valid native authentication",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider: "native",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid LDAP authentication",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "ldap",
						SecretRef: "ldap-secret",
					},
					Config: map[string]string{
						"dbms.security.ldap.host":                     "ldap.example.com",
						"dbms.security.ldap.authentication.mechanism": "simple",
						"dbms.security.ldap.use_starttls":             "true",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid OIDC authentication",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "oidc",
						SecretRef: "oidc-secret",
					},
					Config: map[string]string{
						"dbms.security.oidc.issuer":    "https://auth.example.com",
						"dbms.security.oidc.client_id": "neo4j-client",
						"dbms.security.oidc.scopes":    "openid profile email",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid JWT authentication",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "jwt",
						SecretRef: "jwt-secret",
					},
					Config: map[string]string{
						"dbms.security.jwt.public_key": "/ssl/jwt-public.key",
						"dbms.security.jwt.algorithm":  "RS256",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid SAML authentication",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "saml",
						SecretRef: "saml-secret",
					},
					Config: map[string]string{
						"dbms.security.saml.metadata_url": "https://idp.example.com/metadata",
						"dbms.security.saml.entity_id":    "neo4j-sp",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid Kerberos authentication",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "kerberos",
						SecretRef: "kerberos-secret",
					},
					Config: map[string]string{
						"dbms.security.kerberos.service_principal": "neo4j/server.example.com@EXAMPLE.COM",
						"dbms.security.kerberos.keytab":            "/etc/neo4j/krb5.keytab",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid authentication provider",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider: "invalid-provider",
					},
				},
			},
			expectError: true,
			errorCount:  2, // Invalid provider + missing secret
		},
		{
			name: "external auth without secret",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider: "ldap",
						// Missing SecretRef
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid LDAP configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "ldap",
						SecretRef: "ldap-secret",
					},
					Config: map[string]string{
						"dbms.security.ldap.host":                     "",        // Empty host
						"dbms.security.ldap.authentication.mechanism": "invalid", // Invalid mechanism
						"dbms.security.ldap.use_starttls":             "maybe",   // Invalid boolean
					},
				},
			},
			expectError: true,
			errorCount:  3,
		},
		{
			name: "invalid OIDC configuration - missing issuer",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "oidc",
						SecretRef: "oidc-secret",
					},
					Config: map[string]string{
						// Missing issuer
						"dbms.security.oidc.client_id": "neo4j-client",
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid OIDC configuration - invalid issuer URL",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "oidc",
						SecretRef: "oidc-secret",
					},
					Config: map[string]string{
						"dbms.security.oidc.issuer":    "invalid-url", // Invalid URL
						"dbms.security.oidc.client_id": "neo4j-client",
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid OIDC configuration - missing openid scope",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "oidc",
						SecretRef: "oidc-secret",
					},
					Config: map[string]string{
						"dbms.security.oidc.issuer":    "https://auth.example.com",
						"dbms.security.oidc.client_id": "neo4j-client",
						"dbms.security.oidc.scopes":    "profile email", // Missing openid
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid JWT configuration - no key or secret",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "jwt",
						SecretRef: "jwt-secret",
					},
					Config: map[string]string{
						// Missing jwt.secret or jwt.public_key
						"dbms.security.jwt.algorithm": "RS256",
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid JWT algorithm",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "jwt",
						SecretRef: "jwt-secret",
					},
					Config: map[string]string{
						"dbms.security.jwt.secret":    "secret-key",
						"dbms.security.jwt.algorithm": "INVALID", // Invalid algorithm
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid Kerberos configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:  "kerberos",
						SecretRef: "kerberos-secret",
					},
					Config: map[string]string{
						"dbms.security.kerberos.service_principal": "invalid-principal", // Invalid format
						"dbms.security.kerberos.keytab":            "",                  // Empty keytab
					},
				},
			},
			expectError: true,
			errorCount:  2,
		},
		{
			name: "valid TLS configuration with cert-manager",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "letsencrypt-prod",
							Kind: "ClusterIssuer",
						},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid TLS configuration - missing issuer",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						// Missing IssuerRef
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "deprecated security configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{
						"dbms.security.auth_cache_max_capacity": "1000", // Deprecated in 5.26+
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "valid security configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{
						"dbms.security.auth_enabled":                 "true",
						"dbms.security.auth_minimum_password_length": "8",
						"dbms.security.ssl.policy.default.enabled":   "true",
						"dbms.security.procedures.allowlist":         "apoc.*,gds.*",
						"dbms.security.procedures.default_allowed":   "false",
						"dbms.security.logs.password_obfuscation":    "true",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid security configuration values",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{
						"dbms.security.auth_enabled":                 "maybe",     // Invalid boolean
						"dbms.security.auth_minimum_password_length": "0",         // Invalid (should be positive)
						"dbms.security.ssl.policy.default.enabled":   "yes",       // Invalid boolean
						"dbms.security.procedures.default_allowed":   "sometimes", // Invalid boolean
						"dbms.security.logs.password_obfuscation":    "no",        // Invalid boolean
					},
				},
			},
			expectError: true,
			errorCount:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.cluster)

			if tt.expectError {
				if len(errors) == 0 {
					t.Errorf("expected validation errors but got none")
				}
				if len(errors) != tt.errorCount {
					t.Errorf("expected %d errors but got %d: %v", tt.errorCount, len(errors), errors)
				}
			} else {
				if len(errors) > 0 {
					t.Errorf("expected no validation errors but got %d: %v", len(errors), errors)
				}
			}
		})
	}
}

func TestSecurityValidator_isValidURL(t *testing.T) {
	validator := NewSecurityValidator()

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "valid HTTPS URL",
			url:      "https://auth.example.com",
			expected: true,
		},
		{
			name:     "valid HTTP URL",
			url:      "http://auth.example.com",
			expected: true,
		},
		{
			name:     "valid URL with path",
			url:      "https://auth.example.com/oauth",
			expected: true,
		},
		{
			name:     "invalid URL - no scheme",
			url:      "auth.example.com",
			expected: false,
		},
		{
			name:     "invalid URL - empty",
			url:      "",
			expected: false,
		},
		{
			name:     "invalid URL - malformed",
			url:      "not-a-url",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.isValidURL(tt.url)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSecurityValidator_isValidKerberosPrincipal(t *testing.T) {
	validator := NewSecurityValidator()

	tests := []struct {
		name      string
		principal string
		expected  bool
	}{
		{
			name:      "valid principal",
			principal: "neo4j/server.example.com@EXAMPLE.COM",
			expected:  true,
		},
		{
			name:      "valid principal with different service",
			principal: "HTTP/web.example.com@EXAMPLE.COM",
			expected:  true,
		},
		{
			name:      "invalid principal - missing realm",
			principal: "neo4j/server.example.com",
			expected:  false,
		},
		{
			name:      "invalid principal - missing hostname",
			principal: "neo4j@EXAMPLE.COM",
			expected:  false,
		},
		{
			name:      "invalid principal - malformed",
			principal: "invalid-principal",
			expected:  false,
		},
		{
			name:      "empty principal",
			principal: "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.isValidKerberosPrincipal(tt.principal)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSecurityValidator_isValidCipherSuite(t *testing.T) {
	validator := NewSecurityValidator()

	tests := []struct {
		name     string
		cipher   string
		expected bool
	}{
		{
			name:     "valid TLS 1.3 cipher",
			cipher:   "TLS_AES_256_GCM_SHA384",
			expected: true,
		},
		{
			name:     "valid ECDHE cipher",
			cipher:   "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			expected: true,
		},
		{
			name:     "valid ChaCha20 cipher",
			cipher:   "TLS_CHACHA20_POLY1305_SHA256",
			expected: true,
		},
		{
			name:     "invalid cipher",
			cipher:   "INVALID_CIPHER_SUITE",
			expected: false,
		},
		{
			name:     "empty cipher",
			cipher:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.isValidCipherSuite(tt.cipher)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSecurityValidator_isValidProcedureName(t *testing.T) {
	validator := NewSecurityValidator()

	tests := []struct {
		name     string
		procName string
		expected bool
	}{
		{
			name:     "valid procedure name",
			procName: "apoc.load.json",
			expected: true,
		},
		{
			name:     "valid wildcard",
			procName: "apoc.*",
			expected: true,
		},
		{
			name:     "valid namespace wildcard",
			procName: "gds.*",
			expected: true,
		},
		{
			name:     "valid simple name",
			procName: "dbms",
			expected: true,
		},
		{
			name:     "invalid - starts with number",
			procName: "1apoc.load.json",
			expected: false,
		},
		{
			name:     "invalid - contains spaces",
			procName: "apoc load json",
			expected: false,
		},
		{
			name:     "invalid - starts with dot",
			procName: ".apoc.load.json",
			expected: false,
		},
		{
			name:     "empty name",
			procName: "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.isValidProcedureName(tt.procName)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
