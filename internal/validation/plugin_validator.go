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
	"net/url"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// checksumPattern matches "sha256:<64-hex>" or "sha512:<128-hex>".
// The algorithm prefix is required so verification tooling never has to
// guess. SHA1 and MD5 are deliberately excluded — supply-chain protection
// against a malicious upstream demands a collision-resistant hash.
var checksumPattern = regexp.MustCompile(`^(sha256:[a-fA-F0-9]{64}|sha512:[a-fA-F0-9]{128})$`)

// pluginConfigKeyPattern constrains Neo4jPlugin.spec.config keys: they become
// neo4j.conf keys / env-var names, so only letters, digits, dots, underscores
// and dashes are allowed.
var pluginConfigKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// PluginValidator validates Neo4j plugin configuration for Neo4j 5.26+ compatibility
type PluginValidator struct{}

// NewPluginValidator creates a new plugin validator
func NewPluginValidator() *PluginValidator {
	return &PluginValidator{}
}

// PluginValidationResult carries hard errors (which block install) separately
// from advisory warnings (surfaced as events but never block). Mirrors
// DatabaseValidationResult so callers handle both validators uniformly.
type PluginValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// Validate validates the plugin configuration for Neo4j 5.26+ compatibility.
// Hard errors land in Errors; advisory compatibility notes land in Warnings.
func (v *PluginValidator) Validate(plugin *neo4jv1beta1.Neo4jPlugin) *PluginValidationResult {
	result := &PluginValidationResult{}

	// Validate plugin name
	if plugin.Spec.Name == "" {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec", "name"),
			"plugin name must be specified",
		))
	}

	// Validate plugin version
	if plugin.Spec.Version == "" {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec", "version"),
			"plugin version must be specified",
		))
	}

	// Plugin compatibility with the known matrix is advisory only — the
	// operator installs arbitrary (incl. URL-sourced) plugins and is
	// forward-compatible across Neo4j versions, so an unknown/older entry is
	// a warning, never a hard reject.
	if plugin.Spec.Name != "" && plugin.Spec.Version != "" {
		if msg := v.pluginCompatibilityWarning(plugin.Spec.Name, plugin.Spec.Version); msg != "" {
			result.Warnings = append(result.Warnings, msg)
		}
	}

	// Validate plugin source
	if plugin.Spec.Source != nil {
		result.Errors = append(result.Errors, v.validatePluginSource(plugin.Spec.Source)...)
	}

	// Validate plugin dependencies
	if len(plugin.Spec.Dependencies) > 0 {
		result.Errors = append(result.Errors, v.validatePluginDependencies(plugin.Spec.Dependencies)...)
	}

	// Validate plugin security configuration
	if plugin.Spec.Security != nil {
		result.Errors = append(result.Errors, v.validatePluginSecurity(plugin.Spec.Security)...)
	}

	// Validate plugin resources
	if plugin.Spec.Resources != nil {
		result.Errors = append(result.Errors, v.validatePluginResources(plugin.Spec.Resources)...)
	}

	// Cross-field gates for installMode: VerifiedDownload.
	result.Errors = append(result.Errors, v.validateVerifiedDownloadMode(plugin)...)

	// Validate plugin config: keys become neo4j.conf keys / env-var names and
	// values are rendered into neo4j.conf as `key=value` (standalone) or env
	// vars, so constrain keys to a safe identifier set and reject control
	// characters in values that could forge an extra config line. This map was
	// previously unvalidated entirely.
	configPath := field.NewPath("spec", "config")
	for key, value := range plugin.Spec.Config {
		if !pluginConfigKeyPattern.MatchString(key) {
			result.Errors = append(result.Errors, field.Invalid(
				configPath.Key(key),
				key,
				"config key may contain only letters, digits, dots, underscores and dashes",
			))
		}
		if ConfigValueHasControlChars(value) {
			result.Errors = append(result.Errors, field.Invalid(
				configPath.Key(key),
				value,
				"value may not contain newline or carriage-return characters",
			))
		}
	}

	return result
}

// validateVerifiedDownloadMode enforces the gates the VerifiedDownload
// install mode requires for the init container's verified-download
// flow to be coherent:
//
//   - source.url + source.checksum are mandatory (init container has
//     nothing to download / verify without them).
//   - source.type must be "url" or "custom" — the entrypoint's
//     "official"/"community" types resolve via an internal manifest the
//     user can't point at a verifiable URL.
//   - dependencies are rejected. Mixed install paths in one CR
//     (main plugin via init container, dependencies via NEO4J_PLUGINS
//     entrypoint download) confuse the supply-chain story — each
//     dependency should be its own Neo4jPlugin CR with its own
//     verifiable URL.
//
// Same-name CR duplicate detection is enforced controller-side via
// the plugin controller's reconcile (needs K8s client access).
func (v *PluginValidator) validateVerifiedDownloadMode(plugin *neo4jv1beta1.Neo4jPlugin) field.ErrorList {
	if plugin.Spec.InstallMode != "VerifiedDownload" {
		return nil
	}
	var allErrs field.ErrorList

	if plugin.Spec.Source == nil || plugin.Spec.Source.URL == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec", "source", "url"),
			"installMode: VerifiedDownload requires spec.source.url so the init container has somewhere to download from",
		))
	}
	if plugin.Spec.Source == nil || plugin.Spec.Source.Checksum == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec", "source", "checksum"),
			"installMode: VerifiedDownload requires spec.source.checksum (sha256:<64 hex> or sha512:<128 hex>) for the init container to verify against",
		))
	}
	if plugin.Spec.Source != nil {
		switch plugin.Spec.Source.Type {
		case "official", "community":
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "source", "type"),
				plugin.Spec.Source.Type,
				"installMode: VerifiedDownload requires source.type=url or source.type=custom — official/community resolve via the Neo4j entrypoint's internal manifest and cannot be pointed at a verifiable URL",
			))
		}
	}

	if len(plugin.Spec.Dependencies) > 0 {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec", "dependencies"),
			"installMode: VerifiedDownload does not support spec.dependencies — each dependency must be its own Neo4jPlugin CR with its own spec.source.url + checksum so the entire chain is verified",
		))
	}

	return allErrs
}

// validatePluginCompatibility validates plugin compatibility with Neo4j 5.26+
// pluginCompatibilityWarning returns an advisory message when a plugin isn't
// in the known compatibility matrix, is deprecated, or is below the matrix's
// recorded minimum version. It NEVER hard-rejects: the operator installs
// arbitrary (incl. URL-sourced) plugins and is forward-compatible across
// Neo4j versions, and this matrix is a best-effort convenience that drifts as
// Neo4j ships new plugins (and uses different labels, e.g. "neo4j-bloom" vs
// the operator's "bloom"). Returns "" when nothing noteworthy.
func (v *PluginValidator) pluginCompatibilityWarning(name, version string) string {
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
		// Unknown plugins are common and legitimate (URL-sourced, newly
		// shipped, or labelled differently than this matrix records) — note
		// it, don't block.
		return fmt.Sprintf("plugin '%s' is not in the operator's known compatibility matrix — verify it supports your Neo4j version", name)
	}

	// Check for deprecated plugins
	if minVersion == "deprecated" {
		return fmt.Sprintf("plugin '%s' is deprecated in Neo4j 5.26+ — consider an alternative", name)
	}

	// Validate version against minimum requirement
	if !v.isPluginVersionCompatible(version, minVersion) {
		return fmt.Sprintf("plugin '%s' version '%s' is below the operator's recorded minimum for Neo4j 5.26+ (%s) — verify compatibility", name, version, minVersion)
	}

	return ""
}

// validatePluginSource validates plugin source configuration
func (v *PluginValidator) validatePluginSource(source *neo4jv1beta1.PluginSource) field.ErrorList {
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

	// Supply-chain: transport integrity. The JAR is fetched over the network
	// (Neo4j entrypoint or the VerifiedDownload init container) and the
	// recorded checksum is not enforced at download time on the entrypoint
	// path (see below), so cleartext http would leave a network attacker free
	// to swap the JAR. Only https is accepted — file:/ftp:/etc. give no
	// transport integrity either and the downloaders don't support them.
	if (source.Type == "custom" || source.Type == "url") && source.URL != "" {
		parsed, err := url.Parse(source.URL)
		switch {
		case err != nil:
			allErrs = append(allErrs, field.Invalid(
				sourcePath.Child("url"),
				source.URL,
				fmt.Sprintf("must be a valid URL: %v", err),
			))
		case !strings.EqualFold(parsed.Scheme, "https"):
			allErrs = append(allErrs, field.Invalid(
				sourcePath.Child("url"),
				source.URL,
				"URL scheme must be https — plugin JARs are downloaded over the network and a non-https scheme has no transport integrity. Host the plugin on an https endpoint (an in-cluster mirror with a cert-manager certificate works)",
			))
		case parsed.Host == "":
			allErrs = append(allErrs, field.Invalid(
				sourcePath.Child("url"),
				source.URL,
				"URL must include a host",
			))
		}
	}

	// Supply-chain: any source that fetches a JAR from an arbitrary URL
	// MUST commit to a checksum. Both "url" and "custom" reach an outside
	// endpoint; "official" and "community" resolve via the Neo4j Docker
	// entrypoint's curated manifest and don't accept a user-supplied URL.
	// Note: today the Neo4j entrypoint does not verify this checksum at
	// download time — it is recorded by the operator and surfaced as a
	// StatefulSet annotation so users (and audit tooling) can verify
	// out-of-band, or so a future init-container verifier can enforce.
	// See docs/user_guide/plugin_supply_chain.md for the model.
	if (source.Type == "url" || source.Type == "custom") && source.Checksum == "" {
		allErrs = append(allErrs, field.Required(
			sourcePath.Child("checksum"),
			"checksum is required for url and custom source types — supply-chain protection",
		))
	}

	// Validate checksum format when present. Accept only sha256/sha512
	// with the correct hex digit count; reject SHA1/MD5 (collision-prone)
	// and unprefixed hex (verification tools shouldn't have to guess).
	if source.Checksum != "" && !checksumPattern.MatchString(source.Checksum) {
		allErrs = append(allErrs, field.Invalid(
			sourcePath.Child("checksum"),
			source.Checksum,
			"checksum must be of the form sha256:<64 hex chars> or sha512:<128 hex chars>",
		))
	}

	return allErrs
}

// validatePluginDependencies validates plugin dependencies
func (v *PluginValidator) validatePluginDependencies(dependencies []neo4jv1beta1.PluginDependency) field.ErrorList {
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
func (v *PluginValidator) validatePluginSecurity(security *neo4jv1beta1.PluginSecurity) field.ErrorList {
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
func (v *PluginValidator) validatePluginResources(resources *neo4jv1beta1.PluginResourceRequirements) field.ErrorList {
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
