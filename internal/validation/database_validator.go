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
	"context"
	"fmt"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// DatabaseValidator validates Neo4jDatabase resources
type DatabaseValidator struct {
	client client.Client
}

// NewDatabaseValidator creates a new database validator
func NewDatabaseValidator(client client.Client) *DatabaseValidator {
	return &DatabaseValidator{
		client: client,
	}
}

// DatabaseValidationResult holds validation results including warnings
type DatabaseValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// Validate validates a Neo4jDatabase resource
func (v *DatabaseValidator) Validate(ctx context.Context, database *neo4jv1alpha1.Neo4jDatabase) *DatabaseValidationResult {
	result := &DatabaseValidationResult{
		Errors:   field.ErrorList{},
		Warnings: []string{},
	}

	// Try to get referenced cluster first
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      database.Spec.ClusterRef,
		Namespace: database.Namespace,
	}

	clusterErr := v.client.Get(ctx, clusterKey, cluster)

	// If cluster not found, try to get standalone
	var standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone
	if errors.IsNotFound(clusterErr) {
		standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
		standaloneKey := types.NamespacedName{
			Name:      database.Spec.ClusterRef,
			Namespace: database.Namespace,
		}

		standaloneErr := v.client.Get(ctx, standaloneKey, standalone)
		if errors.IsNotFound(standaloneErr) {
			result.Errors = append(result.Errors, field.NotFound(
				field.NewPath("spec", "clusterRef"),
				fmt.Sprintf("Referenced cluster %s not found", database.Spec.ClusterRef)))
			return result
		} else if standaloneErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Cannot validate database configuration: failed to get cluster %s", database.Spec.ClusterRef))
			return result
		}
	} else if clusterErr != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Cannot validate database topology: failed to get cluster %s", database.Spec.ClusterRef))
		return result
	}

	// Validate topology if specified
	if database.Spec.Topology != nil {
		if standalone != nil {
			v.validateDatabaseTopologyForStandalone(database, standalone, result)
		} else {
			v.validateDatabaseTopology(database, cluster, result)
		}
	}

	// Validate Cypher language version
	v.validateCypherLanguage(database, result)

	// Validate seed URI configuration
	v.validateSeedURI(ctx, database, result)

	// Validate database options syntax
	v.validateDatabaseOptions(database, result)

	// Validate conflicting configurations
	v.validateConfigurationConflicts(database, result)

	return result
}

func (v *DatabaseValidator) validateDatabaseTopologyForStandalone(database *neo4jv1alpha1.Neo4jDatabase, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone, result *DatabaseValidationResult) {
	topologyPath := field.NewPath("spec", "topology")
	topology := database.Spec.Topology

	// For standalone deployments, topology is not needed and will be ignored
	result.Warnings = append(result.Warnings,
		"Database topology specification is not required for standalone deployments and will be ignored. "+
			"Standalone instances handle all database operations on a single node.")

	// However, if specified, validate for basic sanity
	if topology.Primaries < 0 {
		result.Errors = append(result.Errors, field.Invalid(
			topologyPath.Child("primaries"),
			topology.Primaries,
			"primaries cannot be negative"))
	}

	if topology.Secondaries < 0 {
		result.Errors = append(result.Errors, field.Invalid(
			topologyPath.Child("secondaries"),
			topology.Secondaries,
			"secondaries cannot be negative"))
	}

	// At least one primary is required for any database
	if topology.Primaries == 0 && topology.Primaries >= 0 && topology.Secondaries >= 0 {
		result.Errors = append(result.Errors, field.Invalid(
			topologyPath.Child("primaries"),
			topology.Primaries,
			"at least 1 primary is required for database operation"))
	}

	// Warn about secondaries on standalone
	if topology.Secondaries > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Database topology specifies %d secondaries, but standalone deployments cannot provide read replicas. "+
				"All operations will be handled by the single standalone instance.", topology.Secondaries))
	}
}

func (v *DatabaseValidator) validateDatabaseTopology(database *neo4jv1alpha1.Neo4jDatabase, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, result *DatabaseValidationResult) {
	topologyPath := field.NewPath("spec", "topology")
	topology := database.Spec.Topology

	// Basic validation
	if topology.Primaries < 0 {
		result.Errors = append(result.Errors, field.Invalid(
			topologyPath.Child("primaries"),
			topology.Primaries,
			"primaries cannot be negative"))
	}

	if topology.Secondaries < 0 {
		result.Errors = append(result.Errors, field.Invalid(
			topologyPath.Child("secondaries"),
			topology.Secondaries,
			"secondaries cannot be negative"))
	}

	// At least one primary is required for a database (only check if values are valid)
	if topology.Primaries == 0 && topology.Primaries >= 0 && topology.Secondaries >= 0 {
		result.Errors = append(result.Errors, field.Invalid(
			topologyPath.Child("primaries"),
			topology.Primaries,
			"at least 1 primary is required for database operation"))
	}

	// Only proceed with further validation if values are non-negative
	if topology.Primaries >= 0 && topology.Secondaries >= 0 {
		// Check that total database topology doesn't exceed cluster servers
		totalDatabaseServers := topology.Primaries + topology.Secondaries
		clusterServers := cluster.Spec.Topology.Servers

		if totalDatabaseServers > clusterServers {
			result.Errors = append(result.Errors, field.Invalid(
				topologyPath,
				fmt.Sprintf("%d primaries + %d secondaries = %d servers",
					topology.Primaries, topology.Secondaries, totalDatabaseServers),
				fmt.Sprintf("database topology requires %d servers but cluster only has %d servers available",
					totalDatabaseServers, clusterServers)))
		}

		// Add warnings for potentially suboptimal configurations
		v.addTopologyWarnings(database, cluster, result)
	}
}

func (v *DatabaseValidator) addTopologyWarnings(database *neo4jv1alpha1.Neo4jDatabase, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, result *DatabaseValidationResult) {
	topology := database.Spec.Topology
	totalDatabaseServers := topology.Primaries + topology.Secondaries
	clusterServers := cluster.Spec.Topology.Servers

	// Warn if database uses all available servers (no room for other databases)
	if totalDatabaseServers == clusterServers && clusterServers >= 2 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Database topology uses all %d cluster servers. "+
				"Consider using fewer servers to allow multiple databases with different topologies.",
				clusterServers))
	}

	// Warn about excessive secondaries
	if topology.Secondaries > topology.Primaries*2 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Database has %d secondaries for %d primaries. "+
				"More than 2:1 secondary-to-primary ratio may impact write performance.",
				topology.Secondaries, topology.Primaries))
	}

	// Warn about single primary with many secondaries (potential bottleneck)
	if topology.Primaries == 1 && topology.Secondaries > 3 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Database has 1 primary with %d secondaries. "+
				"Consider adding more primaries to distribute write load.",
				topology.Secondaries))
	}

	// Warn about cluster constraint conflicts
	if cluster.Spec.Topology.ServerModeConstraint == "PRIMARY" && topology.Secondaries > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Cluster constrains all servers to PRIMARY mode, but database topology specifies %d secondaries. "+
				"Secondaries will be allocated but cannot serve read-only queries.",
				topology.Secondaries))
	}

	if cluster.Spec.Topology.ServerModeConstraint == "SECONDARY" && topology.Primaries > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Cluster constrains all servers to SECONDARY mode, but database topology specifies %d primaries. "+
				"This configuration may prevent database writes.",
				topology.Primaries))
	}

	// Suggest optimal distribution for available servers
	if totalDatabaseServers < clusterServers-1 && clusterServers >= 3 {
		remainingServers := clusterServers - totalDatabaseServers
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Database uses %d of %d available servers. "+
				"Consider utilizing remaining %d servers for better fault tolerance or read scaling.",
				totalDatabaseServers, clusterServers, remainingServers))
	}
}

func (v *DatabaseValidator) validateCypherLanguage(database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	if database.Spec.DefaultCypherLanguage != "" {
		cypherPath := field.NewPath("spec", "defaultCypherLanguage")
		version := database.Spec.DefaultCypherLanguage

		// Only specific versions are supported
		if version != "5" && version != "25" {
			result.Errors = append(result.Errors, field.NotSupported(
				cypherPath,
				version,
				[]string{"5", "25"}))
		}

		// Add informational warning about version usage
		if version == "5" {
			result.Warnings = append(result.Warnings,
				"Cypher language version '5' is supported for backward compatibility. "+
					"Consider migrating to version '25' for new features and improvements.")
		}
	}
}

func (v *DatabaseValidator) validateSeedURI(ctx context.Context, database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	seedURIPath := field.NewPath("spec", "seedURI")

	// If no seed URI is specified, skip validation
	if database.Spec.SeedURI == "" {
		return
	}

	// Validate seed URI format
	seedURI := database.Spec.SeedURI
	parsedURI, err := url.Parse(seedURI)
	if err != nil {
		result.Errors = append(result.Errors, field.Invalid(
			seedURIPath,
			seedURI,
			fmt.Sprintf("invalid URI format: %v", err)))
		return
	}

	// Validate supported URI schemes
	supportedSchemes := []string{"s3", "gs", "azb", "https", "http", "ftp"}
	if !containsSlice(supportedSchemes, parsedURI.Scheme) {
		result.Errors = append(result.Errors, field.NotSupported(
			seedURIPath,
			parsedURI.Scheme,
			supportedSchemes))
		return
	}

	// Validate URI has required components
	if parsedURI.Host == "" {
		result.Errors = append(result.Errors, field.Invalid(
			seedURIPath,
			seedURI,
			"URI must specify a host"))
		return
	}

	if parsedURI.Path == "" || parsedURI.Path == "/" {
		result.Errors = append(result.Errors, field.Invalid(
			seedURIPath,
			seedURI,
			"URI must specify a path to the backup file"))
		return
	}

	// Validate seed configuration if provided
	if database.Spec.SeedConfig != nil {
		v.validateSeedConfiguration(database, result)
	}

	// Validate seed credentials if provided
	if database.Spec.SeedCredentials != nil {
		v.validateSeedCredentials(ctx, database, result)
	}

	// Add warnings for optimal configurations
	v.addSeedURIWarnings(database, result)
}

func (v *DatabaseValidator) validateSeedConfiguration(database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	seedConfigPath := field.NewPath("spec", "seedConfig")
	seedConfig := database.Spec.SeedConfig

	// Validate RestoreUntil format if specified
	if seedConfig.RestoreUntil != "" {
		restoreUntilPath := seedConfigPath.Child("restoreUntil")
		restoreUntil := seedConfig.RestoreUntil

		// Check for supported formats: RFC3339 timestamp or txId:number
		if strings.HasPrefix(restoreUntil, "txId:") {
			// Validate transaction ID format
			txId := strings.TrimPrefix(restoreUntil, "txId:")
			if txId == "" {
				result.Errors = append(result.Errors, field.Invalid(
					restoreUntilPath,
					restoreUntil,
					"transaction ID cannot be empty when using txId: format"))
			}
		} else {
			// Assume RFC3339 format and validate basic structure
			if !strings.Contains(restoreUntil, "T") || !strings.Contains(restoreUntil, ":") {
				result.Errors = append(result.Errors, field.Invalid(
					restoreUntilPath,
					restoreUntil,
					"restoreUntil must be RFC3339 timestamp (e.g., '2025-01-15T10:30:00Z') or transaction ID (e.g., 'txId:12345')"))
			}
		}
	}

	// Validate configuration options
	if seedConfig.Config != nil {
		configPath := seedConfigPath.Child("config")
		for key, value := range seedConfig.Config {
			// Validate known configuration keys
			switch key {
			case "compression":
				if !containsSlice([]string{"gzip", "lz4", "none"}, value) {
					result.Errors = append(result.Errors, field.NotSupported(
						configPath.Key(key),
						value,
						[]string{"gzip", "lz4", "none"}))
				}
			case "validation":
				if !containsSlice([]string{"strict", "lenient"}, value) {
					result.Errors = append(result.Errors, field.NotSupported(
						configPath.Key(key),
						value,
						[]string{"strict", "lenient"}))
				}
			case "bufferSize":
				if value == "" {
					result.Errors = append(result.Errors, field.Invalid(
						configPath.Key(key),
						value,
						"bufferSize cannot be empty"))
				}
			default:
				// Allow unknown keys with warning
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("Unknown seed configuration option '%s'. "+
						"Refer to Neo4j CloudSeedProvider documentation for supported options.", key))
			}
		}
	}
}

func (v *DatabaseValidator) validateSeedCredentials(ctx context.Context, database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	credentialsPath := field.NewPath("spec", "seedCredentials")
	credentials := database.Spec.SeedCredentials

	// Validate secret reference
	if credentials.SecretRef == "" {
		result.Errors = append(result.Errors, field.Required(
			credentialsPath.Child("secretRef"),
			"secretRef is required when seedCredentials is specified"))
		return
	}

	// Check if the referenced secret exists
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      credentials.SecretRef,
		Namespace: database.Namespace,
	}

	if err := v.client.Get(ctx, secretKey, secret); err != nil {
		if errors.IsNotFound(err) {
			result.Errors = append(result.Errors, field.NotFound(
				credentialsPath.Child("secretRef"),
				fmt.Sprintf("Secret %s not found", credentials.SecretRef)))
		} else {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Cannot validate seed credentials secret %s: %v", credentials.SecretRef, err))
		}
		return
	}

	// Validate secret contains expected keys based on seed URI scheme
	if database.Spec.SeedURI != "" {
		parsedURI, err := url.Parse(database.Spec.SeedURI)
		if err == nil {
			v.validateSecretKeysForScheme(parsedURI.Scheme, secret, credentials, result)
		}
	}
}

func (v *DatabaseValidator) validateSecretKeysForScheme(scheme string, secret *corev1.Secret, credentials *neo4jv1alpha1.SeedCredentials, result *DatabaseValidationResult) {
	credentialsPath := field.NewPath("spec", "seedCredentials", "secretRef")

	switch scheme {
	case "s3":
		requiredKeys := []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
		optionalKeys := []string{"AWS_SESSION_TOKEN", "AWS_REGION"}
		v.validateSecretKeys(secret, requiredKeys, optionalKeys, credentialsPath, result)

	case "gs":
		requiredKeys := []string{"GOOGLE_APPLICATION_CREDENTIALS"}
		optionalKeys := []string{"GOOGLE_CLOUD_PROJECT"}
		v.validateSecretKeys(secret, requiredKeys, optionalKeys, credentialsPath, result)

	case "azb":
		// Either AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN is required along with account
		hasAccountName := hasSecretKey(secret, "AZURE_STORAGE_ACCOUNT")
		hasStorageKey := hasSecretKey(secret, "AZURE_STORAGE_KEY")
		hasSASToken := hasSecretKey(secret, "AZURE_STORAGE_SAS_TOKEN")

		if !hasAccountName {
			result.Errors = append(result.Errors, field.Required(
				credentialsPath,
				"secret must contain AZURE_STORAGE_ACCOUNT for Azure Blob Storage"))
		}

		if !hasStorageKey && !hasSASToken {
			result.Errors = append(result.Errors, field.Required(
				credentialsPath,
				"secret must contain either AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN for Azure Blob Storage"))
		}

	case "http", "https", "ftp":
		optionalKeys := []string{"USERNAME", "PASSWORD", "AUTH_HEADER"}
		v.validateSecretKeys(secret, []string{}, optionalKeys, credentialsPath, result)

	default:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Unknown URI scheme '%s' - cannot validate credential requirements", scheme))
	}
}

func (v *DatabaseValidator) validateSecretKeys(secret *corev1.Secret, requiredKeys, optionalKeys []string, path *field.Path, result *DatabaseValidationResult) {
	for _, key := range requiredKeys {
		if !hasSecretKey(secret, key) {
			result.Errors = append(result.Errors, field.Required(
				path,
				fmt.Sprintf("secret must contain required key '%s'", key)))
		}
	}

	// Warn about missing optional keys that might be useful
	missingOptional := []string{}
	for _, key := range optionalKeys {
		if !hasSecretKey(secret, key) {
			missingOptional = append(missingOptional, key)
		}
	}

	if len(missingOptional) > 0 && len(requiredKeys) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Secret is missing optional keys that may be needed: %v", missingOptional))
	}
}

func (v *DatabaseValidator) addSeedURIWarnings(database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	// Warn about system-wide authentication vs explicit credentials
	if database.Spec.SeedCredentials == nil {
		result.Warnings = append(result.Warnings,
			"Using system-wide cloud authentication for seed URI. "+
				"Ensure workload identity, IAM roles, or service accounts are properly configured.")
	}

	// Warn about point-in-time recovery availability
	if database.Spec.SeedConfig != nil && database.Spec.SeedConfig.RestoreUntil != "" {
		result.Warnings = append(result.Warnings,
			"Point-in-time recovery (restoreUntil) is only available with Neo4j 2025.x and CloudSeedProvider.")
	}

	// Warn about backup file format recommendations
	seedURI := database.Spec.SeedURI
	if strings.HasSuffix(seedURI, ".dump") {
		result.Warnings = append(result.Warnings,
			"Using dump file format. For better performance with large databases, "+
				"consider using Neo4j backup format (.backup) instead.")
	}
}

func (v *DatabaseValidator) validateDatabaseOptions(database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	if database.Spec.Options == nil || len(database.Spec.Options) == 0 {
		return
	}

	optionsPath := field.NewPath("spec", "options")

	for key, value := range database.Spec.Options {
		// Validate key format - warn about dotted keys conversion
		cleanKey := strings.Trim(key, `"`)
		if strings.Contains(cleanKey, ".") {
			convertedKey := strings.ReplaceAll(cleanKey, ".", "_")
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Database option key '%s' contains dots. "+
					"Neo4j CREATE DATABASE OPTIONS syntax only supports simple identifiers. "+
					"The key will be converted to '%s' during database creation.", cleanKey, convertedKey))
		}

		// Validate empty values
		if value == "" {
			result.Errors = append(result.Errors, field.Invalid(
				optionsPath.Key(key),
				value,
				"option value cannot be empty"))
		}

		// Validate known problematic keys
		v.validateKnownOptionKeys(key, value, optionsPath, result)
	}
}

func (v *DatabaseValidator) validateKnownOptionKeys(key, value string, basePath *field.Path, result *DatabaseValidationResult) {
	path := basePath.Key(key)

	// Neo4j CREATE DATABASE OPTIONS only supports specific system-level options
	// Convert dotted keys to understand the intended option
	cleanKey := strings.Trim(key, `"`)
	convertedKey := strings.ReplaceAll(cleanKey, ".", "_")

	// Valid Neo4j CREATE DATABASE OPTIONS (as per Neo4j 5.26+ documentation)
	validOptions := []string{
		"seedConfig", "existingDataSeedServer", "existingData", "seedCredentials",
		"seedURI", "existingDataSeedInstance", "existingMetadata",
		"txLogEnrichment", "storeFormat",
	}

	// Check if this is a valid option (case-insensitive)
	isValidOption := false
	for _, validOption := range validOptions {
		if strings.EqualFold(convertedKey, validOption) || strings.EqualFold(cleanKey, validOption) {
			isValidOption = true
			break
		}
	}

	// If not a valid option, provide an error with guidance
	if !isValidOption {
		result.Errors = append(result.Errors, field.Invalid(
			path,
			key,
			fmt.Sprintf("'%s' is not a valid CREATE DATABASE OPTIONS parameter. "+
				"Valid options are: %v. "+
				"Note: Database-level configuration should be set via ALTER DATABASE or server configuration, not CREATE DATABASE OPTIONS.",
				cleanKey, validOptions)))
		return
	}

	// Validate specific valid options
	switch strings.ToLower(convertedKey) {
	case "storeformat":
		validFormats := []string{"standard", "high_limit", "block"}
		if !containsSlice(validFormats, value) {
			result.Errors = append(result.Errors, field.NotSupported(
				path, value, validFormats))
		}

	case "txlogenrichment":
		validValues := []string{"OFF", "DIFF"}
		if !containsSlice(validValues, strings.ToUpper(value)) {
			result.Errors = append(result.Errors, field.NotSupported(
				path, strings.ToUpper(value), validValues))
		}

	case "seedconfig":
		result.Warnings = append(result.Warnings,
			"seedConfig in OPTIONS is deprecated. Use the seedConfig field in the Neo4jDatabase spec instead.")

	case "seeduri":
		result.Warnings = append(result.Warnings,
			"seedURI in OPTIONS is deprecated. Use the seedURI field in the Neo4jDatabase spec instead.")

	case "existingdata":
		validValues := []string{"use", "fail"}
		if !containsSlice(validValues, value) {
			result.Errors = append(result.Errors, field.NotSupported(
				path, value, validValues))
		}
	}
}

func (v *DatabaseValidator) isValidMemorySize(size string) bool {
	if size == "" {
		return false
	}
	// Simple validation - ends with common memory units
	size = strings.ToLower(size)
	return strings.HasSuffix(size, "k") || strings.HasSuffix(size, "m") ||
		strings.HasSuffix(size, "g") || strings.HasSuffix(size, "kb") ||
		strings.HasSuffix(size, "mb") || strings.HasSuffix(size, "gb")
}

func (v *DatabaseValidator) isValidDuration(duration string) bool {
	if duration == "" {
		return false
	}
	// Simple validation - ends with common duration units
	duration = strings.ToLower(duration)
	return strings.HasSuffix(duration, "s") || strings.HasSuffix(duration, "m") ||
		strings.HasSuffix(duration, "h") || strings.HasSuffix(duration, "ms") ||
		strings.HasSuffix(duration, "us") || strings.HasSuffix(duration, "ns")
}

func (v *DatabaseValidator) validateConfigurationConflicts(database *neo4jv1alpha1.Neo4jDatabase, result *DatabaseValidationResult) {
	// Check for conflicting seed URI and initial data configurations
	if database.Spec.SeedURI != "" && database.Spec.InitialData != nil {
		result.Errors = append(result.Errors, field.Invalid(
			field.NewPath("spec", "seedURI"),
			database.Spec.SeedURI,
			"seedURI and initialData cannot be specified together - seed URI provides the initial data"))

		result.Warnings = append(result.Warnings,
			"Database configuration specifies both seedURI and initialData. The seedURI will be used and initialData will be ignored.")
	}
}

// Helper functions
func containsSlice(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func hasSecretKey(secret *corev1.Secret, key string) bool {
	if secret.Data == nil {
		return false
	}
	_, exists := secret.Data[key]
	return exists
}
