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
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// SecurityValidator validates Neo4j security configuration for Neo4j 5.26+ compatibility
type SecurityValidator struct{}

// NewSecurityValidator creates a new security validator
func NewSecurityValidator() *SecurityValidator {
	return &SecurityValidator{}
}

// Validate validates the security configuration for Neo4j 5.26+ compatibility
func (v *SecurityValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Validate authentication configuration with 5.26+ enhancements
	allErrs = append(allErrs, v.validateAuthenticationConfig(cluster)...)

	// Validate TLS configuration with 5.26+ enhancements
	if cluster.Spec.TLS != nil {
		allErrs = append(allErrs, v.validateTLSConfig(cluster.Spec.TLS)...)
	}

	// Validate custom security configuration for 5.26+ features
	if cluster.Spec.Config != nil {
		allErrs = append(allErrs, v.validateSecurityConfig(cluster.Spec.Config)...)
	}

	return allErrs
}

// validateAuthenticationConfig validates authentication configuration for Neo4j 5.26+
func (v *SecurityValidator) validateAuthenticationConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	if cluster.Spec.Auth == nil {
		return allErrs
	}

	authPath := field.NewPath("spec", "auth")

	// Neo4j 5.26+ supported authentication providers
	validProviders := []string{
		"native",   // Native Neo4j authentication
		"ldap",     // LDAP authentication
		"kerberos", // Kerberos authentication
		"jwt",      // JWT authentication
		"oidc",     // OpenID Connect (enhanced in 5.26+)
		"saml",     // SAML authentication (enhanced in 5.26+)
		"custom",   // Custom authentication plugin
	}

	if cluster.Spec.Auth.Provider != "" {
		valid := false
		for _, provider := range validProviders {
			if cluster.Spec.Auth.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				authPath.Child("provider"),
				cluster.Spec.Auth.Provider,
				validProviders,
			))
		}

		// Validate provider-specific requirements
		switch cluster.Spec.Auth.Provider {
		case "ldap":
			allErrs = append(allErrs, v.validateLDAPConfig(cluster)...)
		case "jwt":
			allErrs = append(allErrs, v.validateJWTConfig(cluster)...)
		case "oidc":
			allErrs = append(allErrs, v.validateOIDCConfig(cluster)...)
		case "saml":
			allErrs = append(allErrs, v.validateSAMLConfig(cluster)...)
		case "kerberos":
			allErrs = append(allErrs, v.validateKerberosConfig(cluster)...)
		}
	}

	// Validate that external auth providers have secretRef
	if cluster.Spec.Auth.Provider != "" && cluster.Spec.Auth.Provider != "native" {
		if cluster.Spec.Auth.SecretRef == "" {
			allErrs = append(allErrs, field.Required(
				authPath.Child("secretRef"),
				fmt.Sprintf("secretRef is required for %s auth provider", cluster.Spec.Auth.Provider),
			))
		}
	}

	return allErrs
}

// validateLDAPConfig validates LDAP authentication configuration for Neo4j 5.26+
func (v *SecurityValidator) validateLDAPConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	authPath := field.NewPath("spec", "auth")

	// Check for LDAP-specific configuration in custom config
	if cluster.Spec.Config != nil {
		// Validate LDAP server configuration
		if ldapHost, exists := cluster.Spec.Config["dbms.security.ldap.host"]; exists {
			if ldapHost == "" {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					ldapHost,
					"LDAP host cannot be empty",
				))
			}
		}

		// Validate LDAP authentication method (Neo4j 5.26+ supports enhanced methods)
		if authMethod, exists := cluster.Spec.Config["dbms.security.ldap.authentication.mechanism"]; exists {
			validMethods := []string{"simple", "DIGEST-MD5", "GSSAPI"}
			valid := false
			for _, method := range validMethods {
				if authMethod == method {
					valid = true
					break
				}
			}
			if !valid {
				allErrs = append(allErrs, field.NotSupported(
					authPath,
					authMethod,
					validMethods,
				))
			}
		}

		// Validate LDAP connection security (Neo4j 5.26+ enhanced TLS support)
		if useStartTLS, exists := cluster.Spec.Config["dbms.security.ldap.use_starttls"]; exists {
			if useStartTLS != "true" && useStartTLS != "false" {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					useStartTLS,
					"LDAP use_starttls must be 'true' or 'false'",
				))
			}
		}
	}

	return allErrs
}

// validateJWTConfig validates JWT authentication configuration for Neo4j 5.26+
func (v *SecurityValidator) validateJWTConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	authPath := field.NewPath("spec", "auth")

	if cluster.Spec.Config != nil {
		// Validate JWT secret or public key configuration
		hasSecret := false
		hasPublicKey := false

		if _, exists := cluster.Spec.Config["dbms.security.jwt.secret"]; exists {
			hasSecret = true
		}
		if _, exists := cluster.Spec.Config["dbms.security.jwt.public_key"]; exists {
			hasPublicKey = true
		}

		if !hasSecret && !hasPublicKey {
			allErrs = append(allErrs, field.Required(
				authPath,
				"JWT authentication requires either 'dbms.security.jwt.secret' or 'dbms.security.jwt.public_key'",
			))
		}

		// Validate JWT algorithm (Neo4j 5.26+ supports additional algorithms)
		if algorithm, exists := cluster.Spec.Config["dbms.security.jwt.algorithm"]; exists {
			validAlgorithms := []string{
				"HS256", "HS384", "HS512", // HMAC algorithms
				"RS256", "RS384", "RS512", // RSA algorithms
				"ES256", "ES384", "ES512", // ECDSA algorithms (enhanced in 5.26+)
				"PS256", "PS384", "PS512", // PSS algorithms (enhanced in 5.26+)
			}
			valid := false
			for _, alg := range validAlgorithms {
				if algorithm == alg {
					valid = true
					break
				}
			}
			if !valid {
				allErrs = append(allErrs, field.NotSupported(
					authPath,
					algorithm,
					validAlgorithms,
				))
			}
		}
	}

	return allErrs
}

// validateOIDCConfig validates OpenID Connect configuration for Neo4j 5.26+
func (v *SecurityValidator) validateOIDCConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	authPath := field.NewPath("spec", "auth")

	if cluster.Spec.Config != nil {
		// Validate OIDC issuer URL
		if issuer, exists := cluster.Spec.Config["dbms.security.oidc.issuer"]; exists {
			if !v.isValidURL(issuer) {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					issuer,
					"OIDC issuer must be a valid URL",
				))
			}
		} else {
			allErrs = append(allErrs, field.Required(
				authPath,
				"OIDC authentication requires 'dbms.security.oidc.issuer'",
			))
		}

		// Validate OIDC client ID
		if clientID, exists := cluster.Spec.Config["dbms.security.oidc.client_id"]; exists {
			if clientID == "" {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					clientID,
					"OIDC client ID cannot be empty",
				))
			}
		} else {
			allErrs = append(allErrs, field.Required(
				authPath,
				"OIDC authentication requires 'dbms.security.oidc.client_id'",
			))
		}

		// Validate OIDC scopes (Neo4j 5.26+ enhanced scope support)
		if scopes, exists := cluster.Spec.Config["dbms.security.oidc.scopes"]; exists {
			if !strings.Contains(scopes, "openid") {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					scopes,
					"OIDC scopes must include 'openid'",
				))
			}
		}
	}

	return allErrs
}

// validateSAMLConfig validates SAML authentication configuration for Neo4j 5.26+
func (v *SecurityValidator) validateSAMLConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	authPath := field.NewPath("spec", "auth")

	if cluster.Spec.Config != nil {
		// Validate SAML metadata URL or file
		hasMetadataURL := false
		hasMetadataFile := false

		if metadataURL, exists := cluster.Spec.Config["dbms.security.saml.metadata_url"]; exists {
			hasMetadataURL = true
			if !v.isValidURL(metadataURL) {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					metadataURL,
					"SAML metadata URL must be a valid URL",
				))
			}
		}

		if _, exists := cluster.Spec.Config["dbms.security.saml.metadata_file"]; exists {
			hasMetadataFile = true
		}

		if !hasMetadataURL && !hasMetadataFile {
			allErrs = append(allErrs, field.Required(
				authPath,
				"SAML authentication requires either 'dbms.security.saml.metadata_url' or 'dbms.security.saml.metadata_file'",
			))
		}

		// Validate SAML entity ID
		if entityID, exists := cluster.Spec.Config["dbms.security.saml.entity_id"]; exists {
			if entityID == "" {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					entityID,
					"SAML entity ID cannot be empty",
				))
			}
		}
	}

	return allErrs
}

// validateKerberosConfig validates Kerberos authentication configuration for Neo4j 5.26+
func (v *SecurityValidator) validateKerberosConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	authPath := field.NewPath("spec", "auth")

	if cluster.Spec.Config != nil {
		// Validate Kerberos principal
		if principal, exists := cluster.Spec.Config["dbms.security.kerberos.service_principal"]; exists {
			if !v.isValidKerberosPrincipal(principal) {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					principal,
					"Kerberos service principal must be in format 'service/hostname@REALM'",
				))
			}
		} else {
			allErrs = append(allErrs, field.Required(
				authPath,
				"Kerberos authentication requires 'dbms.security.kerberos.service_principal'",
			))
		}

		// Validate Kerberos keytab file
		if keytab, exists := cluster.Spec.Config["dbms.security.kerberos.keytab"]; exists {
			if keytab == "" {
				allErrs = append(allErrs, field.Invalid(
					authPath,
					keytab,
					"Kerberos keytab file path cannot be empty",
				))
			}
		} else {
			allErrs = append(allErrs, field.Required(
				authPath,
				"Kerberos authentication requires 'dbms.security.kerberos.keytab'",
			))
		}
	}

	return allErrs
}

// validateTLSConfig validates TLS configuration for Neo4j 5.26+
func (v *SecurityValidator) validateTLSConfig(tls *neo4jv1alpha1.TLSSpec) field.ErrorList {
	var allErrs field.ErrorList
	tlsPath := field.NewPath("spec", "tls")

	// Validate TLS mode
	validModes := []string{"cert-manager", "disabled"}
	if tls.Mode != "" {
		valid := false
		for _, mode := range validModes {
			if tls.Mode == mode {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				tlsPath.Child("mode"),
				tls.Mode,
				validModes,
			))
		}
	}

	// Validate cert-manager specific configuration
	if tls.Mode == "cert-manager" {
		if tls.IssuerRef == nil {
			allErrs = append(allErrs, field.Required(
				tlsPath.Child("issuerRef"),
				"issuerRef is required for cert-manager TLS mode",
			))
		} else {
			if tls.IssuerRef.Name == "" {
				allErrs = append(allErrs, field.Required(
					tlsPath.Child("issuerRef", "name"),
					"issuer name must be specified",
				))
			}
			if tls.IssuerRef.Kind == "" {
				tls.IssuerRef.Kind = "Issuer" // Default to Issuer
			}
			validKinds := []string{"Issuer", "ClusterIssuer"}
			valid := false
			for _, kind := range validKinds {
				if tls.IssuerRef.Kind == kind {
					valid = true
					break
				}
			}
			if !valid {
				allErrs = append(allErrs, field.NotSupported(
					tlsPath.Child("issuerRef", "kind"),
					tls.IssuerRef.Kind,
					validKinds,
				))
			}
		}
	}

	// Validate custom certificate configuration
	if tls.CertificateSecret != "" && tls.Mode != "cert-manager" {
		// Custom certificate provided
		if tls.Mode == "disabled" {
			allErrs = append(allErrs, field.Invalid(
				tlsPath.Child("certificateSecret"),
				tls.CertificateSecret,
				"certificate secret should not be specified when TLS is disabled",
			))
		}
	}

	return allErrs
}

// validateSecurityConfig validates custom security configuration for Neo4j 5.26+
func (v *SecurityValidator) validateSecurityConfig(config map[string]string) field.ErrorList {
	var allErrs field.ErrorList
	configPath := field.NewPath("spec", "config")

	// Validate Neo4j 5.26+ security settings
	for key, value := range config {
		switch {
		case strings.HasPrefix(key, "dbms.security.auth"):
			allErrs = append(allErrs, v.validateAuthConfig(key, value, configPath)...)
		case strings.HasPrefix(key, "dbms.security.ssl"):
			allErrs = append(allErrs, v.validateSSLConfig(key, value, configPath)...)
		case strings.HasPrefix(key, "dbms.security.procedures"):
			allErrs = append(allErrs, v.validateProcedureSecurityConfig(key, value, configPath)...)
		case strings.HasPrefix(key, "dbms.security.logs"):
			allErrs = append(allErrs, v.validateLogSecurityConfig(key, value, configPath)...)
		}
	}

	// Check for deprecated security settings in Neo4j 5.26+
	deprecatedSettings := map[string]string{
		"dbms.security.auth_cache_max_capacity":          "Use dbms.security.authentication_cache.max_capacity",
		"dbms.security.auth_cache_ttl":                   "Use dbms.security.authentication_cache.ttl",
		"dbms.security.authorization_cache_max_capacity": "Use dbms.security.authorization_cache.max_capacity",
	}

	for deprecatedKey, replacement := range deprecatedSettings {
		if _, exists := config[deprecatedKey]; exists {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(deprecatedKey),
				config[deprecatedKey],
				fmt.Sprintf("setting is deprecated in Neo4j 5.26+. %s", replacement),
			))
		}
	}

	return allErrs
}

// validateAuthConfig validates authentication-related configuration
func (v *SecurityValidator) validateAuthConfig(key, value string, configPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	switch key {
	case "dbms.security.auth_enabled":
		if value != "true" && value != "false" {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(key),
				value,
				"must be 'true' or 'false'",
			))
		}
	case "dbms.security.auth_minimum_password_length":
		if !v.isValidPositiveInteger(value) {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(key),
				value,
				"must be a positive integer",
			))
		}
	}

	return allErrs
}

// validateSSLConfig validates SSL-related configuration for Neo4j 5.26+
func (v *SecurityValidator) validateSSLConfig(key, value string, configPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	switch key {
	case "dbms.security.ssl.policy.default.enabled":
		if value != "true" && value != "false" {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(key),
				value,
				"must be 'true' or 'false'",
			))
		}
	case "dbms.security.ssl.policy.default.ciphers":
		// Validate cipher suites for Neo4j 5.26+
		ciphers := strings.Split(value, ",")
		for _, cipher := range ciphers {
			cipher = strings.TrimSpace(cipher)
			if !v.isValidCipherSuite(cipher) {
				allErrs = append(allErrs, field.Invalid(
					configPath.Child(key),
					cipher,
					"invalid cipher suite for Neo4j 5.26+",
				))
			}
		}
	}

	return allErrs
}

// validateProcedureSecurityConfig validates procedure security configuration for Neo4j 5.26+
func (v *SecurityValidator) validateProcedureSecurityConfig(key, value string, configPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	switch key {
	case "dbms.security.procedures.allowlist":
		// Validate procedure allowlist format
		procedures := strings.Split(value, ",")
		for _, proc := range procedures {
			proc = strings.TrimSpace(proc)
			if proc != "*" && !v.isValidProcedureName(proc) {
				allErrs = append(allErrs, field.Invalid(
					configPath.Child(key),
					proc,
					"invalid procedure name format",
				))
			}
		}
	case "dbms.security.procedures.default_allowed":
		if value != "true" && value != "false" {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(key),
				value,
				"must be 'true' or 'false'",
			))
		}
	}

	return allErrs
}

// validateLogSecurityConfig validates logging security configuration for Neo4j 5.26+
func (v *SecurityValidator) validateLogSecurityConfig(key, value string, configPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	switch key {
	case "dbms.security.logs.password_obfuscation":
		if value != "true" && value != "false" {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(key),
				value,
				"must be 'true' or 'false'",
			))
		}
	}

	return allErrs
}

// Helper validation functions

// isValidURL validates URL format
func (v *SecurityValidator) isValidURL(url string) bool {
	urlRegex := regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)
	return urlRegex.MatchString(url)
}

// isValidKerberosPrincipal validates Kerberos principal format
func (v *SecurityValidator) isValidKerberosPrincipal(principal string) bool {
	principalRegex := regexp.MustCompile(`^[^/@]+/[^/@]+@[^/@]+$`)
	return principalRegex.MatchString(principal)
}

// isValidPositiveInteger validates positive integer strings
func (v *SecurityValidator) isValidPositiveInteger(value string) bool {
	positiveIntRegex := regexp.MustCompile(`^[1-9]\d*$`)
	return positiveIntRegex.MatchString(value)
}

// isValidCipherSuite validates cipher suite names for Neo4j 5.26+
func (v *SecurityValidator) isValidCipherSuite(cipher string) bool {
	// Basic validation for common cipher suites supported in Neo4j 5.26+
	validCiphers := []string{
		"TLS_AES_256_GCM_SHA384",
		"TLS_AES_128_GCM_SHA256",
		"TLS_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
		"TLS_DHE_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_DHE_RSA_WITH_AES_128_GCM_SHA256",
	}

	for _, validCipher := range validCiphers {
		if cipher == validCipher {
			return true
		}
	}
	return false
}

// isValidProcedureName validates procedure name format
func (v *SecurityValidator) isValidProcedureName(procName string) bool {
	// Validate procedure name format (namespace.procedure or namespace.*)
	procedureRegex := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_*][a-zA-Z0-9_*]*)*$`)
	return procedureRegex.MatchString(procName)
}
