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
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// UpgradeValidator validates Neo4j upgrade configuration
type UpgradeValidator struct{}

// NewUpgradeValidator creates a new upgrade validator
func NewUpgradeValidator() *UpgradeValidator {
	return &UpgradeValidator{}
}

// VersionInfo represents parsed version information
type VersionInfo struct {
	Major int
	Minor int
	Patch int
}

// ValidateVersionUpgrade validates that the version upgrade is supported
func (v *UpgradeValidator) ValidateVersionUpgrade(currentVersion, targetVersion string) field.ErrorList {
	var allErrs field.ErrorList

	// Parse current and target versions
	current := v.parseVersion(currentVersion)
	target := v.parseVersion(targetVersion)

	if current == nil || target == nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "image", "tag"),
			targetVersion,
			"invalid version format",
		))
		return allErrs
	}

	// Prevent downgrades
	if v.isDowngrade(current, target) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "image", "tag"),
			targetVersion,
			fmt.Sprintf("downgrades are not supported (current: %s, target: %s)", currentVersion, targetVersion),
		))
		return allErrs
	}

	// Validate upgrade path based on versioning scheme
	var err error
	if v.isCalVer(current) && v.isCalVer(target) {
		// CalVer to CalVer upgrade (2025.x.x -> 2025.y.y or 2026.x.x)
		err = v.validateCalVerUpgrade(current, target, currentVersion, targetVersion)
	} else if v.isSemVer(current) && v.isSemVer(target) {
		// SemVer to SemVer upgrade (5.x.x -> 5.y.y)
		err = v.validateSemVerUpgrade(current, target, currentVersion, targetVersion)
	} else if v.isSemVer(current) && v.isCalVer(target) {
		// SemVer to CalVer upgrade (5.x.x -> 2025.x.x)
		err = v.validateSemVerToCalVerUpgrade(current, target, currentVersion, targetVersion)
	} else {
		// CalVer to SemVer (not supported)
		err = fmt.Errorf("downgrade from CalVer to SemVer is not supported")
	}

	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "image", "tag"),
			targetVersion,
			err.Error(),
		))
	}

	return allErrs
}

// ValidateUpgradeStrategy validates upgrade strategy configuration
func (v *UpgradeValidator) ValidateUpgradeStrategy(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	strategyPath := field.NewPath("spec", "upgradeStrategy")

	if cluster.Spec.UpgradeStrategy == nil {
		return allErrs
	}

	strategy := cluster.Spec.UpgradeStrategy

	// Validate strategy type
	validStrategies := []string{"RollingUpgrade", "Recreate"}
	if strategy.Strategy != "" {
		valid := false
		for _, validStrategy := range validStrategies {
			if strategy.Strategy == validStrategy {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				strategyPath.Child("strategy"),
				strategy.Strategy,
				validStrategies,
			))
		}
	}

	// Validate timeout durations
	if strategy.UpgradeTimeout != "" {
		if _, err := time.ParseDuration(strategy.UpgradeTimeout); err != nil {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("upgradeTimeout"),
				strategy.UpgradeTimeout,
				"invalid duration format",
			))
		}
	}

	if strategy.HealthCheckTimeout != "" {
		if _, err := time.ParseDuration(strategy.HealthCheckTimeout); err != nil {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("healthCheckTimeout"),
				strategy.HealthCheckTimeout,
				"invalid duration format",
			))
		}
	}

	if strategy.StabilizationTimeout != "" {
		if _, err := time.ParseDuration(strategy.StabilizationTimeout); err != nil {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("stabilizationTimeout"),
				strategy.StabilizationTimeout,
				"invalid duration format",
			))
		}
	}

	// Validate maxUnavailableDuringUpgrade
	if strategy.MaxUnavailableDuringUpgrade != nil {
		if *strategy.MaxUnavailableDuringUpgrade < 0 {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("maxUnavailableDuringUpgrade"),
				*strategy.MaxUnavailableDuringUpgrade,
				"must be non-negative",
			))
		}
	}

	return allErrs
}

// isDowngrade checks if target version is lower than current version
func (v *UpgradeValidator) isDowngrade(current, target *VersionInfo) bool {
	// For CalVer (year-based)
	if v.isCalVer(current) && v.isCalVer(target) {
		if target.Major < current.Major {
			return true
		}
		if target.Major == current.Major && target.Minor < current.Minor {
			return true
		}
		if target.Major == current.Major && target.Minor == current.Minor && target.Patch < current.Patch {
			return true
		}
		return false
	}

	// For SemVer or mixed comparison
	if target.Major < current.Major {
		return true
	}
	if target.Major == current.Major && target.Minor < current.Minor {
		return true
	}
	if target.Major == current.Major && target.Minor == current.Minor && target.Patch < current.Patch {
		return true
	}

	// Special case: CalVer to SemVer is always a downgrade
	if v.isCalVer(current) && v.isSemVer(target) {
		return true
	}

	return false
}

// isCalVer checks if version follows CalVer format (2025+)
func (v *UpgradeValidator) isCalVer(version *VersionInfo) bool {
	return version.Major >= 2025
}

// isSemVer checks if version follows SemVer format (5.x)
func (v *UpgradeValidator) isSemVer(version *VersionInfo) bool {
	return version.Major >= 4 && version.Major <= 10 // Neo4j 4.x, 5.x
}

// validateCalVerUpgrade validates CalVer to CalVer upgrades
func (v *UpgradeValidator) validateCalVerUpgrade(current, target *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades from 2025.x.x and up
	if current.Major < 2025 || target.Major < 2025 {
		return fmt.Errorf("CalVer upgrades are only supported from 2025.x.x and up")
	}

	// Allow upgrades within same year (patch/minor)
	if current.Major == target.Major {
		return nil // 2025.1.0 -> 2025.1.1 or 2025.1.0 -> 2025.2.0
	}

	// Allow upgrades to newer years
	if target.Major > current.Major {
		return nil // 2025.x.x -> 2026.x.x
	}

	return fmt.Errorf("unsupported CalVer upgrade path from %s to %s", currentStr, targetStr)
}

// validateSemVerUpgrade validates SemVer to SemVer upgrades
func (v *UpgradeValidator) validateSemVerUpgrade(current, target *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades within same major version
	if target.Major != current.Major {
		return fmt.Errorf("major version upgrades are not supported")
	}

	// Allow minor and patch upgrades within supported range
	if current.Major == 5 && target.Major == 5 {
		if current.Minor >= 26 && target.Minor >= 26 {
			return nil // Allow upgrades within 5.26+
		}
		return fmt.Errorf("only Neo4j 5.26+ versions are supported")
	}

	// Neo4j 4.x is no longer supported - only 5.26+ versions are supported
	if current.Major == 4 || target.Major == 4 {
		return fmt.Errorf("Neo4j 4.x versions are not supported - only 5.26+ versions are supported")
	}

	return fmt.Errorf("unsupported SemVer upgrade path from %s to %s", currentStr, targetStr)
}

// validateSemVerToCalVerUpgrade validates upgrades from SemVer to CalVer
func (v *UpgradeValidator) validateSemVerToCalVerUpgrade(current, _ *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades from Neo4j 5.26+ to CalVer
	if current.Major == 5 && current.Minor >= 26 {
		return nil // 5.26+ -> 2025.x.x is allowed
	}

	return fmt.Errorf("upgrade from %s to CalVer %s requires Neo4j 5.26 or higher", currentStr, targetStr)
}

// parseVersion parses a version string into components, handling both SemVer and CalVer
func (v *UpgradeValidator) parseVersion(version string) *VersionInfo {
	// Remove any prefix like "v" and suffixes like "-enterprise"
	version = strings.TrimPrefix(version, "v")
	if idx := strings.Index(version, "-"); idx != -1 {
		version = version[:idx]
	}

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	patch := 0
	if len(parts) > 2 {
		if p, err := strconv.Atoi(parts[2]); err == nil {
			patch = p
		}
	}

	return &VersionInfo{
		Major: major,
		Minor: minor,
		Patch: patch,
	}
}
