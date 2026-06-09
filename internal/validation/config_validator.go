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
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// ConfigValidator validates Neo4j configuration settings
type ConfigValidator struct{}

// NewConfigValidator creates a new config validator
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate validates the configuration settings
func (v *ConfigValidator) Validate(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	configPath := field.NewPath("spec", "config")

	if cluster.Spec.Config == nil {
		return allErrs
	}

	// Check for deprecated configuration settings
	deprecatedSettings := map[string]string{
		"dbms.default_database": "use dbms.setDefaultDatabase() procedure instead",
		"db.format":             "standard and high_limit formats are deprecated, use block format",
		"dbms.integrations.cloud_storage.s3.region": "replaced by new cloud storage integration settings",
		"dbms.logs.query.enabled":                   "renamed to db.logs.query.enabled in Neo4j 5.x+",
	}

	// Check for unsupported manual discovery configuration.
	// The operator injects all discovery settings (resolver_type, endpoints, …) through
	// the startup script into /tmp/neo4j-config/neo4j.conf. User-supplied values in
	// Spec.Config would conflict with or override that managed configuration.
	//
	// Discovery mechanism used by this operator:
	//   5.26.x  — LIST resolver, dbms.cluster.discovery.v2.endpoints, V2_ONLY
	//   2025.x+ — LIST resolver, dbms.cluster.endpoints (renamed), no version flag
	//   Both    — port 6000 (tcp-tx), pod FQDNs via headless service
	unsupportedDiscoverySettings := map[string]string{
		"dbms.cluster.discovery.resolver_type":        "discovery resolver is managed by the operator (LIST with static pod FQDNs) — do not override",
		"dbms.cluster.discovery.v2.endpoints":         "discovery endpoints are managed by the operator — do not override (5.26.x setting)",
		"dbms.cluster.endpoints":                      "discovery endpoints are managed by the operator — do not override (2025.x+ setting)",
		"dbms.kubernetes.label_selector":              "Kubernetes service-list discovery is not used; operator uses LIST discovery with pod FQDNs",
		"dbms.kubernetes.discovery.service_port_name": "Kubernetes service-list discovery is not used; operator uses LIST discovery with pod FQDNs",
	}

	// Per-pod / topology values the operator writes into neo4j.conf at startup
	// (advertised addresses use each pod's FQDN; mode constraint and the initial
	// primaries count come from spec.topology). A user value in spec.config would
	// be appended to the static conf and then DECLARED AGAIN at runtime → CalVer
	// Neo4j refuses to start with "<key> declared multiple times". The static-conf
	// de-duplication can't catch this because the second declaration is appended
	// at pod startup, so reject these at apply time.
	operatorRuntimeManagedSettings := map[string]string{
		"server.default_advertised_address":                   "advertised addresses are set per-pod (FQDN) by the operator at runtime — do not set in spec.config",
		"server.cluster.advertised_address":                   "advertised addresses are set per-pod (FQDN) by the operator at runtime — do not set in spec.config",
		"server.routing.advertised_address":                   "advertised addresses are set per-pod (FQDN) by the operator at runtime — do not set in spec.config",
		"server.cluster.raft.advertised_address":              "advertised addresses are set per-pod (FQDN) by the operator at runtime — do not set in spec.config",
		"initial.server.mode_constraint":                      "use spec.topology.serverModeConstraint / serverRoles — the operator sets initial.server.mode_constraint at runtime",
		"dbms.cluster.minimum_initial_system_primaries_count": "managed by the operator (derived from spec.topology) — do not override",
	}

	for configKey, configValue := range cluster.Spec.Config {
		// Special handling for dbms.cluster.discovery.version.
		// In 5.26.x this setting controls the discovery protocol (V1 vs V2); the operator
		// requires V2_ONLY. In 2025.x+ the setting does not exist (V2 is the only protocol).
		// Allow V2_ONLY for 5.x compatibility; any other value is rejected.
		if configKey == "dbms.cluster.discovery.version" {
			if configValue != "V2_ONLY" {
				validValues := []string{"V2_ONLY"}
				allErrs = append(allErrs, field.NotSupported(
					configPath.Child(configKey),
					configValue,
					validValues,
				))
			}
			continue // Skip regular deprecated/unsupported checks for this key
		}

		// Check for deprecated settings
		if deprecationMsg, isDeprecated := deprecatedSettings[configKey]; isDeprecated {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(configKey),
				configValue,
				"deprecated setting: "+deprecationMsg,
			))
		}

		// Check for unsupported manual discovery settings
		if unsupportedMsg, isUnsupported := unsupportedDiscoverySettings[configKey]; isUnsupported {
			allErrs = append(allErrs, field.Forbidden(
				configPath.Child(configKey),
				"unsupported configuration: "+unsupportedMsg,
			))
		}

		// Check for operator runtime-managed settings (advertised addresses,
		// topology). These would collide at startup with the operator's own
		// runtime-appended declaration.
		if runtimeMsg, isManaged := operatorRuntimeManagedSettings[configKey]; isManaged {
			allErrs = append(allErrs, field.Forbidden(
				configPath.Child(configKey),
				"operator-managed configuration: "+runtimeMsg,
			))
		}

		// SSL policies are managed end-to-end by the operator via spec.tls.
		// The operator emits dbms.ssl.policy.{bolt,https,cluster}.* and
		// server.bolt.tls_level / server.directories.certificates with
		// values driven by spec.tls.mode and spec.tls.strictPeerValidation.
		//
		// Because server.config.strict_validation.enabled is set to false
		// (to allow experimental settings elsewhere), Neo4j silently lets
		// duplicate keys later in neo4j.conf override earlier ones. Without
		// this rejection a user could put e.g.
		// `dbms.ssl.policy.cluster.trust_all: "true"` in spec.config and
		// silently downgrade the strict-by-default cluster SSL posture to
		// Neo4j's documented debugging-only configuration. Reject loudly
		// at apply time instead.
		//
		// Set spec.tls.strictPeerValidation: false if you need the legacy
		// trust_all=true posture for an issuer that doesn't populate ca.crt.
		if strings.HasPrefix(configKey, "dbms.ssl.policy.") ||
			configKey == "server.bolt.tls_level" ||
			configKey == "server.directories.certificates" {
			allErrs = append(allErrs, field.Forbidden(
				configPath.Child(configKey),
				"unsupported configuration: TLS / SSL policy is managed by the operator via spec.tls; do not set dbms.ssl.policy.*, server.bolt.tls_level, or server.directories.certificates in spec.config. Use spec.tls.strictPeerValidation: false if you need the legacy trust_all=true posture",
			))
		}

		// Validate database format settings
		if configKey == "db.format" {
			if configValue == "standard" || configValue == "high_limit" {
				allErrs = append(allErrs, field.Invalid(
					configPath.Child(configKey),
					configValue,
					"standard and high_limit database formats are deprecated, use block format",
				))
			}
		}

		// Validate cloud storage integration settings
		if strings.HasPrefix(configKey, "dbms.integrations.cloud_storage.") {
			if err := v.validateCloudStorageConfig(configKey, configValue); err != nil {
				allErrs = append(allErrs, field.Invalid(
					configPath.Child(configKey),
					configValue,
					err.Error(),
				))
			}
		}
	}

	return allErrs
}

// isValidDiscoveryVersion checks if the discovery version is valid.
// Only V2_ONLY is accepted; in 2025.x+ the setting is not used at all
// (V2 is the only supported protocol), but V2_ONLY is harmless if set.
func (v *ConfigValidator) isValidDiscoveryVersion(version string) bool {
	validVersions := []string{"V2_ONLY"}
	for _, valid := range validVersions {
		if version == valid {
			return true
		}
	}
	return false
}

// validateCloudStorageConfig validates cloud storage integration settings
func (v *ConfigValidator) validateCloudStorageConfig(key, value string) error {
	// Validate Azure blob storage settings
	if strings.HasPrefix(key, "dbms.integrations.cloud_storage.azb.") {
		if key == "dbms.integrations.cloud_storage.azb.blob_endpoint_suffix" {
			// Should end with proper domain format
			if value != "" && !strings.Contains(value, ".") {
				return fmt.Errorf("invalid blob endpoint suffix format")
			}
		}
		return nil
	}

	// Other cloud storage providers can be added here
	return nil
}
