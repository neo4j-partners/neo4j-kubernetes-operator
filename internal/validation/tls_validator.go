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
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	// CertManagerMode represents cert-manager TLS mode
	CertManagerMode = "cert-manager"
)

// TLSValidator validates Neo4j TLS configuration
type TLSValidator struct{}

// NewTLSValidator creates a new TLS validator
func NewTLSValidator() *TLSValidator {
	return &TLSValidator{}
}

// Validate validates the TLS configuration
func (v *TLSValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	if cluster.Spec.TLS == nil {
		return allErrs
	}

	tlsPath := field.NewPath("spec", "tls")
	validModes := []string{"cert-manager", "disabled"}

	if cluster.Spec.TLS.Mode != "" {
		valid := false
		for _, mode := range validModes {
			if cluster.Spec.TLS.Mode == mode {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				tlsPath.Child("mode"),
				cluster.Spec.TLS.Mode,
				validModes,
			))
		}
	}

	// Validate cert-manager specific fields
	if cluster.Spec.TLS.Mode == CertManagerMode {
		if cluster.Spec.TLS.IssuerRef != nil {
			if cluster.Spec.TLS.IssuerRef.Name == "" {
				allErrs = append(allErrs, field.Required(
					tlsPath.Child("issuerRef", "name"),
					"issuer name must be specified when using cert-manager",
				))
			}

			validKinds := []string{"Issuer", "ClusterIssuer"}
			if cluster.Spec.TLS.IssuerRef.Kind != "" {
				valid := false
				for _, kind := range validKinds {
					if cluster.Spec.TLS.IssuerRef.Kind == kind {
						valid = true
						break
					}
				}
				if !valid {
					allErrs = append(allErrs, field.NotSupported(
						tlsPath.Child("issuerRef", "kind"),
						cluster.Spec.TLS.IssuerRef.Kind,
						validKinds,
					))
				}
			}
		}

		// Validate certificate duration and renewal settings
		if cluster.Spec.TLS.Duration != nil {
			if _, err := time.ParseDuration(*cluster.Spec.TLS.Duration); err != nil {
				allErrs = append(allErrs, field.Invalid(
					tlsPath.Child("duration"),
					*cluster.Spec.TLS.Duration,
					"invalid duration format",
				))
			}
		}

		if cluster.Spec.TLS.RenewBefore != nil {
			if _, err := time.ParseDuration(*cluster.Spec.TLS.RenewBefore); err != nil {
				allErrs = append(allErrs, field.Invalid(
					tlsPath.Child("renewBefore"),
					*cluster.Spec.TLS.RenewBefore,
					"invalid duration format",
				))
			}
		}

		// Validate certificate usages
		if len(cluster.Spec.TLS.Usages) > 0 {
			validUsages := []string{
				"digital signature", "key encipherment", "key agreement",
				"server auth", "client auth", "code signing", "email protection",
				"s/mime", "ipsec end system", "ipsec tunnel", "ipsec user",
				"timestamping", "ocsp signing", "microsoft sgc", "netscape sgc",
			}
			for i, usage := range cluster.Spec.TLS.Usages {
				valid := false
				for _, validUsage := range validUsages {
					if usage == validUsage {
						valid = true
						break
					}
				}
				if !valid {
					allErrs = append(allErrs, field.NotSupported(
						tlsPath.Child("usages").Index(i),
						usage,
						validUsages,
					))
				}
			}
		}
	}

	// Validate External Secrets configuration
	if cluster.Spec.TLS.ExternalSecrets != nil && cluster.Spec.TLS.ExternalSecrets.Enabled {
		esPath := tlsPath.Child("externalSecrets")

		if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef == nil {
			allErrs = append(allErrs, field.Required(
				esPath.Child("secretStoreRef"),
				"secretStoreRef is required when external secrets is enabled",
			))
		} else {
			if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Name == "" {
				allErrs = append(allErrs, field.Required(
					esPath.Child("secretStoreRef", "name"),
					"secretStore name is required",
				))
			}

			validKinds := []string{"SecretStore", "ClusterSecretStore"}
			if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Kind != "" {
				valid := false
				for _, kind := range validKinds {
					if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Kind == kind {
						valid = true
						break
					}
				}
				if !valid {
					allErrs = append(allErrs, field.NotSupported(
						esPath.Child("secretStoreRef", "kind"),
						cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Kind,
						validKinds,
					))
				}
			}
		}

		// Validate refresh interval
		if cluster.Spec.TLS.ExternalSecrets.RefreshInterval != "" {
			if _, err := time.ParseDuration(cluster.Spec.TLS.ExternalSecrets.RefreshInterval); err != nil {
				allErrs = append(allErrs, field.Invalid(
					esPath.Child("refreshInterval"),
					cluster.Spec.TLS.ExternalSecrets.RefreshInterval,
					"invalid duration format",
				))
			}
		}

		// Validate data mappings
		if len(cluster.Spec.TLS.ExternalSecrets.Data) == 0 {
			allErrs = append(allErrs, field.Required(
				esPath.Child("data"),
				"at least one data mapping is required when external secrets is enabled",
			))
		}

		for i, data := range cluster.Spec.TLS.ExternalSecrets.Data {
			dataPath := esPath.Child("data").Index(i)
			if data.SecretKey == "" {
				allErrs = append(allErrs, field.Required(
					dataPath.Child("secretKey"),
					"secretKey is required",
				))
			}
			if data.RemoteRef != nil && data.RemoteRef.Key == "" {
				allErrs = append(allErrs, field.Required(
					dataPath.Child("remoteRef", "key"),
					"remoteRef key is required",
				))
			}
		}
	}

	return allErrs
}
