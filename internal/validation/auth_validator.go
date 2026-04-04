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
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

// validAuthProviders is the set of authentication/authorization providers Neo4j supports (5.26+).
var validAuthProviders = []string{"native", "ldap", "oidc", "jwt", "kerberos", "saml", "custom"}

// oidcProviderNameRegex validates OIDC provider names (alphanumeric + hyphens, used as Neo4j config key segments)
var oidcProviderNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

// AuthValidator validates Neo4j authentication configuration
type AuthValidator struct{}

// NewAuthValidator creates a new auth validator
func NewAuthValidator() *AuthValidator {
	return &AuthValidator{}
}

// Validate validates the authentication configuration
func (v *AuthValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	return v.ValidateAuthSpec(cluster.Spec.Auth, field.NewPath("spec", "auth"))
}

// ValidateAuthSpec validates an AuthSpec (shared between cluster and standalone).
func (v *AuthValidator) ValidateAuthSpec(auth *neo4jv1alpha1.AuthSpec, authPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if auth == nil {
		return allErrs
	}

	// Validate provider lists
	allErrs = append(allErrs, v.validateProviderList(auth.AuthenticationProviders, authPath.Child("authenticationProviders"))...)
	allErrs = append(allErrs, v.validateProviderList(auth.AuthorizationProviders, authPath.Child("authorizationProviders"))...)

	// Validate LDAP typed fields
	if auth.LDAP != nil {
		allErrs = append(allErrs, v.validateLDAP(auth.LDAP, authPath.Child("ldap"))...)
	}

	// Validate OIDC providers
	if len(auth.OIDC) > 0 {
		allErrs = append(allErrs, v.validateOIDCProviders(auth.OIDC, authPath.Child("oidc"))...)
	}

	// Validate TrustStore
	if auth.TrustStore != nil {
		if auth.TrustStore.Name == "" {
			allErrs = append(allErrs, field.Required(
				authPath.Child("trustStore", "name"),
				"name must specify the Secret containing the CA certificate",
			))
		}
	}

	return allErrs
}

// validateProviderList validates a list of provider names.
func (v *AuthValidator) validateProviderList(providers []string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	for i, provider := range providers {
		// OIDC providers are referenced as "oidc-<name>" in the provider list
		if strings.HasPrefix(provider, "oidc-") {
			continue // valid OIDC provider reference
		}
		allErrs = append(allErrs, v.validateProviderName(provider, fldPath.Index(i))...)
	}
	return allErrs
}

// validateProviderName checks a single provider name against the valid set.
func (v *AuthValidator) validateProviderName(provider string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	valid := false
	for _, vp := range validAuthProviders {
		if provider == vp {
			valid = true
			break
		}
	}
	if !valid {
		allErrs = append(allErrs, field.NotSupported(fldPath, provider, validAuthProviders))
	}
	return allErrs
}

// validateLDAP validates Neo4jLDAPSpec typed fields.
func (v *AuthValidator) validateLDAP(ldap *neo4jv1alpha1.Neo4jLDAPSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if ldap.Host == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("host"), "LDAP host is required"))
	}

	if ldap.Authorization != nil {
		authzPath := fldPath.Child("authorization")
		// If useSystemAccount is true, systemAccountSecretRef must be set
		if ldap.Authorization.UseSystemAccount != nil && *ldap.Authorization.UseSystemAccount {
			if ldap.Authorization.SystemAccountSecretRef == "" {
				allErrs = append(allErrs, field.Required(
					authzPath.Child("systemAccountSecretRef"),
					"systemAccountSecretRef is required when useSystemAccount is true",
				))
			}
		}
	}

	return allErrs
}

// validateOIDCProviders validates all OIDC provider specs.
func (v *AuthValidator) validateOIDCProviders(providers map[string]neo4jv1alpha1.Neo4jOIDCProviderSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	for name, provider := range providers {
		providerPath := fldPath.Key(name)

		// Validate provider name format (used as Neo4j config key segment)
		if !oidcProviderNameRegex.MatchString(name) {
			allErrs = append(allErrs, field.Invalid(
				providerPath,
				name,
				"OIDC provider name must start with a letter and contain only alphanumeric characters and hyphens",
			))
		}

		// Audience is required
		if provider.Audience == "" {
			allErrs = append(allErrs, field.Required(
				providerPath.Child("audience"),
				"audience is required for OIDC providers",
			))
		}

		// Either discovery URI or manual endpoints must be provided
		hasDiscovery := provider.WellKnownDiscoveryURI != ""
		hasManualEndpoints := provider.AuthEndpoint != "" || provider.TokenEndpoint != "" || provider.JWKSURI != "" || provider.Issuer != ""
		if !hasDiscovery && !hasManualEndpoints {
			allErrs = append(allErrs, field.Required(
				providerPath.Child("wellKnownDiscoveryURI"),
				"either wellKnownDiscoveryURI or manual endpoints (authEndpoint, tokenEndpoint, jwksURI, issuer) must be provided",
			))
		}
	}

	return allErrs
}
