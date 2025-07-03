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

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// ConfigValidator validates Neo4j configuration settings
type ConfigValidator struct{}

// NewConfigValidator creates a new config validator
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate validates the configuration settings
func (v *ConfigValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	configPath := field.NewPath("spec", "config")

	if cluster.Spec.Config == nil {
		return allErrs
	}

	// Check for deprecated configuration settings
	deprecatedSettings := map[string]string{
		"dbms.default_database":                     "use dbms.setDefaultDatabase() procedure instead",
		"dbms.cluster.discovery.version":            "deprecated in 5.26+, will be removed in future versions",
		"db.format":                                 "standard and high_limit formats are deprecated, use block format",
		"dbms.integrations.cloud_storage.s3.region": "replaced by new cloud storage integration settings",
	}

	for configKey, configValue := range cluster.Spec.Config {
		// Check for deprecated settings
		if deprecationMsg, isDeprecated := deprecatedSettings[configKey]; isDeprecated {
			allErrs = append(allErrs, field.Invalid(
				configPath.Child(configKey),
				configValue,
				"deprecated setting: "+deprecationMsg,
			))
		}

		// Validate Discovery version settings for 5.26+
		if configKey == "dbms.cluster.discovery.version" {
			validValues := []string{"V2_ONLY"}
			if !v.isValidDiscoveryVersion(configValue) {
				allErrs = append(allErrs, field.NotSupported(
					configPath.Child(configKey),
					configValue,
					validValues,
				))
			}
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

// isValidDiscoveryVersion checks if the discovery version is valid for 5.26+
func (v *ConfigValidator) isValidDiscoveryVersion(version string) bool {
	// For Neo4j 5.26+, only V2_ONLY is recommended
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
