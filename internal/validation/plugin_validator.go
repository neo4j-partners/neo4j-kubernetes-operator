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

// PluginValidator validates Neo4j plugin configuration for Neo4j 5.26+ compatibility
type PluginValidator struct{}

// NewPluginValidator creates a new plugin validator
func NewPluginValidator() *PluginValidator {
	return &PluginValidator{}
}

// Validate validates the plugin configuration for Neo4j 5.26+ compatibility
func (v *PluginValidator) Validate(plugin *neo4jv1alpha1.Neo4jPlugin) field.ErrorList {
	var allErrs field.ErrorList

	// Validate plugin name
	if plugin.Spec.Name == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec", "name"),
			"plugin name must be specified",
		))
	}

	// Validate plugin version
	if plugin.Spec.Version == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec", "version"),
			"plugin version must be specified",
		))
	}

	// Validate plugin compatibility with Neo4j 5.26+
	if plugin.Spec.Name != "" && plugin.Spec.Version != "" {
		if err := v.validatePluginCompatibility(plugin.Spec.Name, plugin.Spec.Version); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "name"),
				plugin.Spec.Name,
				err.Error(),
			))
		}
	}

	// Validate plugin source
	if plugin.Spec.Source != nil {
		allErrs = append(allErrs, v.validatePluginSource(plugin.Spec.Source)...)
	}

	// Validate plugin dependencies
	if len(plugin.Spec.Dependencies) > 0 {
		allErrs = append(allErrs, v.validatePluginDependencies(plugin.Spec.Dependencies)...)
	}

	// Validate plugin security configuration
	if plugin.Spec.Security != nil {
		allErrs = append(allErrs, v.validatePluginSecurity(plugin.Spec.Security)...)
	}

	// Validate plugin resources
	if plugin.Spec.Resources != nil {
		allErrs = append(allErrs, v.validatePluginResources(plugin.Spec.Resources)...)
	}

	return allErrs
}

// validatePluginCompatibility validates plugin compatibility with Neo4j 5.26+
func (v *PluginValidator) validatePluginCompatibility(name, version string) error {
	// Define known plugins and their minimum compatible versions with Neo4j 5.26+
	compatibilityMatrix := map[string]string{
		"apoc":                   "5.26.0",
		"apoc-extended":          "5.26.0",
		"graph-data-science":     "2.9.0",
		"neo4j-streams":          "5.26.0",
		"neo4j-kafka-connect":    "5.26.0",
		"neo4j-bloom":            "2.9.0",
		"neo4j-browser":          "5.26.0",
		"neo4j-desktop":          "1.6.0",
		"neo4j-etl":              "1.6.0",
		"neo4j-graph-algorithms": "deprecated", // Deprecated in favor of GDS
		"neo4j-graphql":          "5.26.0",
		"neo4j-spark-connector":  "5.3.0",
		"neo4j-fabric":           "5.26.0", // Part of core in 5.26+
		"neo4j-vector":           "5.26.0",
		"neo4j-genai":            "2025.01.0",
	}

	// Check if plugin is known
	minVersion, exists := compatibilityMatrix[strings.ToLower(name)]
	if !exists {
		// For unknown plugins, require they explicitly state Neo4j 5.26+ compatibility
		return fmt.Errorf("plugin '%s' is not in the known compatibility matrix. Please verify it supports Neo4j 5.26+", name)
	}

	// Check for deprecated plugins
	if minVersion == "deprecated" {
		return fmt.Errorf("plugin '%s' is deprecated in Neo4j 5.26+. Please use alternative plugins", name)
	}

	// Validate version against minimum requirement
	if !v.isPluginVersionCompatible(version, minVersion) {
		return fmt.Errorf("plugin '%s' version '%s' is not compatible with Neo4j 5.26+. Minimum version required: %s", name, version, minVersion)
	}

	return nil
}

// validatePluginSource validates plugin source configuration
func (v *PluginValidator) validatePluginSource(source *neo4jv1alpha1.PluginSource) field.ErrorList {
	var allErrs field.ErrorList
	sourcePath := field.NewPath("spec", "source")

	validSourceTypes := []string{"official", "community", "custom", "url"}
	if source.Type != "" {
		valid := false
		for _, validType := range validSourceTypes {
			if source.Type == validType {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				sourcePath.Child("type"),
				source.Type,
				validSourceTypes,
			))
		}
	}

	// Validate URL for custom and url types
	if (source.Type == "custom" || source.Type == "url") && source.URL == "" {
		allErrs = append(allErrs, field.Required(
			sourcePath.Child("url"),
			"URL must be specified for custom and url source types",
		))
	}

	// Validate checksum for security
	if source.Type == "url" && source.Checksum == "" {
		allErrs = append(allErrs, field.Required(
			sourcePath.Child("checksum"),
			"checksum must be specified for url source type for security",
		))
	}

	return allErrs
}

// validatePluginDependencies validates plugin dependencies
func (v *PluginValidator) validatePluginDependencies(dependencies []neo4jv1alpha1.PluginDependency) field.ErrorList {
	var allErrs field.ErrorList
	dependenciesPath := field.NewPath("spec", "dependencies")

	for i, dep := range dependencies {
		depPath := dependenciesPath.Index(i)

		if dep.Name == "" {
			allErrs = append(allErrs, field.Required(
				depPath.Child("name"),
				"dependency name must be specified",
			))
		}

		// Validate version constraint format
		if dep.VersionConstraint != "" {
			if !v.isValidVersionConstraint(dep.VersionConstraint) {
				allErrs = append(allErrs, field.Invalid(
					depPath.Child("versionConstraint"),
					dep.VersionConstraint,
					"invalid version constraint format. Use semver constraints like >=1.0.0, ~1.0.0, ^1.0.0",
				))
			}
		}
	}

	return allErrs
}

// validatePluginSecurity validates plugin security configuration
func (v *PluginValidator) validatePluginSecurity(security *neo4jv1alpha1.PluginSecurity) field.ErrorList {
	var allErrs field.ErrorList
	securityPath := field.NewPath("spec", "security")

	// Validate security policy
	if security.SecurityPolicy != "" {
		validPolicies := []string{"strict", "moderate", "permissive"}
		valid := false
		for _, policy := range validPolicies {
			if security.SecurityPolicy == policy {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				securityPath.Child("securityPolicy"),
				security.SecurityPolicy,
				validPolicies,
			))
		}
	}

	// Validate procedure restrictions
	if len(security.AllowedProcedures) > 0 && len(security.DeniedProcedures) > 0 {
		// Check for conflicts between allowed and denied procedures
		for _, allowed := range security.AllowedProcedures {
			for _, denied := range security.DeniedProcedures {
				if allowed == denied {
					allErrs = append(allErrs, field.Invalid(
						securityPath.Child("allowedProcedures"),
						allowed,
						fmt.Sprintf("procedure '%s' cannot be both allowed and denied", allowed),
					))
				}
			}
		}
	}

	return allErrs
}

// validatePluginResources validates plugin resource configuration
func (v *PluginValidator) validatePluginResources(resources *neo4jv1alpha1.PluginResourceRequirements) field.ErrorList {
	var allErrs field.ErrorList
	resourcesPath := field.NewPath("spec", "resources")

	// Validate thread pool size
	if resources.ThreadPoolSize < 0 {
		allErrs = append(allErrs, field.Invalid(
			resourcesPath.Child("threadPoolSize"),
			resources.ThreadPoolSize,
			"thread pool size must be non-negative",
		))
	}

	// Validate memory and CPU limits format (basic validation)
	if resources.MemoryLimit != "" {
		if !v.isValidResourceQuantity(resources.MemoryLimit) {
			allErrs = append(allErrs, field.Invalid(
				resourcesPath.Child("memoryLimit"),
				resources.MemoryLimit,
				"invalid memory limit format. Use Kubernetes resource quantity format (e.g., 512Mi, 1Gi)",
			))
		}
	}

	if resources.CPULimit != "" {
		if !v.isValidResourceQuantity(resources.CPULimit) {
			allErrs = append(allErrs, field.Invalid(
				resourcesPath.Child("cpuLimit"),
				resources.CPULimit,
				"invalid CPU limit format. Use Kubernetes resource quantity format (e.g., 500m, 1)",
			))
		}
	}

	return allErrs
}

// isPluginVersionCompatible checks if plugin version meets minimum requirement
func (v *PluginValidator) isPluginVersionCompatible(version, minVersion string) bool {
	// Clean versions
	cleanVersion := v.cleanVersion(version)
	cleanMinVersion := v.cleanVersion(minVersion)

	// Simple version comparison (should use proper semver library in production)
	return v.compareVersions(cleanVersion, cleanMinVersion) >= 0
}

// cleanVersion removes prefixes and suffixes from version string
func (v *PluginValidator) cleanVersion(version string) string {
	version = strings.TrimPrefix(version, "v")
	if idx := strings.Index(version, "-"); idx != -1 {
		version = version[:idx]
	}
	return version
}

// compareVersions compares two version strings (simplified implementation)
func (v *PluginValidator) compareVersions(version1, version2 string) int {
	v1Parts := strings.Split(version1, ".")
	v2Parts := strings.Split(version2, ".")

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var v1Part, v2Part int

		if i < len(v1Parts) {
			fmt.Sscanf(v1Parts[i], "%d", &v1Part)
		}
		if i < len(v2Parts) {
			fmt.Sscanf(v2Parts[i], "%d", &v2Part)
		}

		if v1Part > v2Part {
			return 1
		}
		if v1Part < v2Part {
			return -1
		}
	}

	return 0
}

// isValidVersionConstraint validates version constraint format
func (v *PluginValidator) isValidVersionConstraint(constraint string) bool {
	// Basic validation for semver constraints
	validPrefixes := []string{">=", "<=", "~", "^", "=", ">", "<"}

	for _, prefix := range validPrefixes {
		if strings.HasPrefix(constraint, prefix) {
			return true
		}
	}

	// Check if it's a plain version number
	parts := strings.Split(constraint, ".")
	return len(parts) >= 2
}

// isValidResourceQuantity validates Kubernetes resource quantity format
func (v *PluginValidator) isValidResourceQuantity(quantity string) bool {
	// Basic validation for resource quantities
	if quantity == "" {
		return false
	}

	// Should end with valid units
	validUnits := []string{"m", "Mi", "Gi", "Ti", "Ki", "k", "M", "G", "T"}
	for _, unit := range validUnits {
		if strings.HasSuffix(quantity, unit) {
			return true
		}
	}

	// Check if it's a plain number
	for _, char := range quantity {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}
