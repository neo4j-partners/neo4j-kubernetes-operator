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
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
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
func (v *TLSValidator) Validate(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
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
			// kind is intentionally not restricted to a fixed allowlist: cert-manager's
			// external issuer interface allows any registered CRD (e.g. AWSPCAClusterIssuer,
			// VaultIssuer) to act as an issuer. Restricting to Issuer/ClusterIssuer would
			// block third-party issuers that are fully supported by cert-manager.
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
			provided := make(map[string]bool, len(cluster.Spec.TLS.Usages))
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
				provided[usage] = true
			}

			// Neo4j's TLS posture needs both EKUs:
			//   - server auth: every Neo4j server presents its cert to
			//     incoming clients (Bolt, HTTPS, intra-cluster).
			//   - client auth: under strict peer validation (the default)
			//     the cluster SSL policy emits client_auth=REQUIRE, which
			//     means each server also acts as a TLS client when
			//     connecting to peers and must present a cert with the
			//     clientAuth EKU.
			//
			// When the user supplies an explicit usages list, it REPLACES
			// the operator's defaults (server auth + client auth +
			// digital signature + key encipherment) — see
			// internal/resources/cluster.go:BuildCertificateForEnterprise.
			// If either required EKU is missing, the issued cert won't
			// satisfy Neo4j's runtime requirements and the cluster's
			// mutual TLS handshakes will fail silently at the network
			// layer. Reject loudly here.
			for _, required := range []string{"server auth", "client auth"} {
				if !provided[required] {
					allErrs = append(allErrs, field.Required(
						tlsPath.Child("usages"),
						fmt.Sprintf("%q is required: Neo4j needs both server-auth and client-auth EKUs (server-auth for incoming Bolt/HTTPS/cluster TLS, client-auth for the mutual TLS the operator emits on cluster links under strict peer validation). Add %q to spec.tls.usages, or omit the field entirely to use the operator's defaults", required, required),
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
