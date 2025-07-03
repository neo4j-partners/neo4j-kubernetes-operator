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
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// BackupValidator validates Neo4j backup configuration for Neo4j 5.26+ compatibility
type BackupValidator struct{}

// NewBackupValidator creates a new backup validator
func NewBackupValidator() *BackupValidator {
	return &BackupValidator{}
}

// Validate validates the backup configuration for Neo4j 5.26+ compatibility
func (v *BackupValidator) Validate(backup *neo4jv1alpha1.Neo4jBackup) field.ErrorList {
	var allErrs field.ErrorList

	// Validate backup target
	allErrs = append(allErrs, v.validateBackupTarget(&backup.Spec.Target)...)

	// Validate storage configuration
	allErrs = append(allErrs, v.validateStorageConfiguration(&backup.Spec.Storage)...)

	// Validate schedule if specified
	if backup.Spec.Schedule != "" {
		if err := v.validateSchedule(backup.Spec.Schedule); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "schedule"),
				backup.Spec.Schedule,
				err.Error(),
			))
		}
	}

	// Validate cloud configuration if specified
	if backup.Spec.Cloud != nil {
		allErrs = append(allErrs, v.validateCloudConfiguration(backup.Spec.Cloud)...)
	}

	// Validate retention policy if specified
	if backup.Spec.Retention != nil {
		allErrs = append(allErrs, v.validateRetentionPolicy(backup.Spec.Retention)...)
	}

	// Validate backup options if specified
	if backup.Spec.Options != nil {
		allErrs = append(allErrs, v.validateBackupOptions(backup.Spec.Options)...)
	}

	return allErrs
}

// validateBackupTarget validates the backup target configuration
func (v *BackupValidator) validateBackupTarget(target *neo4jv1alpha1.BackupTarget) field.ErrorList {
	var allErrs field.ErrorList
	targetPath := field.NewPath("spec", "target")

	// Validate target kind
	validKinds := []string{"Cluster", "Database"}
	if target.Kind == "" {
		allErrs = append(allErrs, field.Required(
			targetPath.Child("kind"),
			"backup target kind must be specified",
		))
	} else {
		valid := false
		for _, validKind := range validKinds {
			if target.Kind == validKind {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				targetPath.Child("kind"),
				target.Kind,
				validKinds,
			))
		}
	}

	// Validate target name
	if target.Name == "" {
		allErrs = append(allErrs, field.Required(
			targetPath.Child("name"),
			"backup target name must be specified",
		))
	} else {
		// Validate name format (Kubernetes resource name validation)
		if !v.isValidResourceName(target.Name) {
			allErrs = append(allErrs, field.Invalid(
				targetPath.Child("name"),
				target.Name,
				"invalid resource name format",
			))
		}
	}

	return allErrs
}

// validateStorageConfiguration validates storage configuration for Neo4j 5.26+
func (v *BackupValidator) validateStorageConfiguration(storage *neo4jv1alpha1.StorageLocation) field.ErrorList {
	var allErrs field.ErrorList
	storagePath := field.NewPath("spec", "storage")

	// Validate storage type
	if storage.Type == "" {
		allErrs = append(allErrs, field.Required(
			storagePath.Child("type"),
			"storage type must be specified",
		))
	} else {
		if err := v.validateStorageType(storage.Type); err != nil {
			allErrs = append(allErrs, field.Invalid(
				storagePath.Child("type"),
				storage.Type,
				err.Error(),
			))
		}
	}

	// Validate storage provider specific configurations
	if err := v.validateStorageProvider(storage); err != nil {
		allErrs = append(allErrs, field.Invalid(
			storagePath,
			storage,
			err.Error(),
		))
	}

	return allErrs
}

// validateStorageType validates storage type for Neo4j 5.26+ features
func (v *BackupValidator) validateStorageType(storageType string) error {
	if storageType == "" {
		return fmt.Errorf("storage type cannot be empty")
	}

	// Neo4j 5.26+ supports various storage backends
	supportedTypes := []string{
		"s3",    // AWS S3
		"gcs",   // Google Cloud Storage
		"azure", // Azure Blob Storage
		"pvc",   // Persistent Volume Claim
	}

	for _, supportedType := range supportedTypes {
		if storageType == supportedType {
			return nil
		}
	}

	return fmt.Errorf("unsupported storage type. Supported types for Neo4j 5.26+: %s",
		strings.Join(supportedTypes, ", "))
}

// validateSchemeSpecificURI validates scheme-specific URI requirements
func (v *BackupValidator) validateSchemeSpecificURI(uri, scheme string) error {
	switch scheme {
	case "s3://":
		// Validate S3 URI format: s3://bucket/path
		if !regexp.MustCompile(`^s3://[a-z0-9.-]+/.+$`).MatchString(uri) {
			return fmt.Errorf("invalid S3 URI format. Expected: s3://bucket/path")
		}
	case "gs://":
		// Validate GCS URI format: gs://bucket/path
		if !regexp.MustCompile(`^gs://[a-z0-9.-]+/.+$`).MatchString(uri) {
			return fmt.Errorf("invalid Google Cloud Storage URI format. Expected: gs://bucket/path")
		}
	case "az://":
		// Validate Azure URI format: az://container/path
		if !regexp.MustCompile(`^az://[a-z0-9.-]+/.+$`).MatchString(uri) {
			return fmt.Errorf("invalid Azure Blob Storage URI format. Expected: az://container/path")
		}
	case "file://":
		// Validate file URI format: file:///path
		if !strings.HasPrefix(uri, "file:///") {
			return fmt.Errorf("invalid file URI format. Expected: file:///absolute/path")
		}
	case "hdfs://":
		// Validate HDFS URI format: hdfs://namenode:port/path
		if !regexp.MustCompile(`^hdfs://[a-zA-Z0-9.-]+:\d+/.+$`).MatchString(uri) {
			return fmt.Errorf("invalid HDFS URI format. Expected: hdfs://namenode:port/path")
		}
	case "ftp://", "sftp://":
		// Validate FTP/SFTP URI format: ftp://user@host:port/path
		if !regexp.MustCompile(`^s?ftp://[^@]+@[a-zA-Z0-9.-]+:\d+/.+$`).MatchString(uri) {
			return fmt.Errorf("invalid FTP/SFTP URI format. Expected: %suser@host:port/path", scheme)
		}
	}

	return nil
}

// validateStorageProvider validates provider-specific storage configurations
func (v *BackupValidator) validateStorageProvider(storage *neo4jv1alpha1.StorageLocation) error {
	storageType := strings.ToLower(storage.Type)

	// For cloud storage providers, validate additional configuration
	switch storageType {
	case "s3":
		// S3 specific validations for Neo4j 5.26+
		if storage.Bucket == "" {
			return fmt.Errorf("S3 storage requires bucket name for Neo4j 5.26+")
		}
		if storage.Cloud == nil || storage.Cloud.Provider != "aws" {
			return fmt.Errorf("S3 storage requires cloud provider to be set to 'aws'")
		}
	case "gcs":
		// GCS specific validations
		if storage.Bucket == "" {
			return fmt.Errorf("Google Cloud Storage requires bucket name for Neo4j 5.26+")
		}
		if storage.Cloud == nil || storage.Cloud.Provider != "gcp" {
			return fmt.Errorf("GCS storage requires cloud provider to be set to 'gcp'")
		}
	case "azure":
		// Azure specific validations
		if storage.Bucket == "" {
			return fmt.Errorf("Azure Blob Storage requires container name for Neo4j 5.26+")
		}
		if storage.Cloud == nil || storage.Cloud.Provider != "azure" {
			return fmt.Errorf("Azure storage requires cloud provider to be set to 'azure'")
		}
	case "pvc":
		// PVC specific validations
		if storage.PVC == nil {
			return fmt.Errorf("PVC storage requires PVC configuration")
		}
	}

	return nil
}

// validateSchedule validates cron schedule format
func (v *BackupValidator) validateSchedule(schedule string) error {
	// Basic cron validation (5 or 6 fields)
	fields := strings.Fields(schedule)
	if len(fields) < 5 || len(fields) > 6 {
		return fmt.Errorf("invalid cron schedule format. Expected 5 or 6 fields, got %d", len(fields))
	}

	// Validate each field in the cron expression

	for i, field := range fields {
		switch i {
		case 0: // Second (if 6 fields) or Minute (if 5 fields)
			if len(fields) == 6 {
				// Second field (0-59)
				if !v.validateCronField(field, 0, 59) {
					return fmt.Errorf("invalid second field in cron schedule: %s", field)
				}
			} else {
				// Minute field (0-59)
				if !v.validateCronField(field, 0, 59) {
					return fmt.Errorf("invalid minute field in cron schedule: %s", field)
				}
			}
		case 1: // Minute (if 6 fields) or Hour (if 5 fields)
			if len(fields) == 6 {
				// Minute field (0-59)
				if !v.validateCronField(field, 0, 59) {
					return fmt.Errorf("invalid minute field in cron schedule: %s", field)
				}
			} else {
				// Hour field (0-23)
				if !v.validateCronField(field, 0, 23) {
					return fmt.Errorf("invalid hour field in cron schedule: %s", field)
				}
			}
		case 2: // Hour (if 6 fields) or Day (if 5 fields)
			if len(fields) == 6 {
				// Hour field (0-23)
				if !v.validateCronField(field, 0, 23) {
					return fmt.Errorf("invalid hour field in cron schedule: %s", field)
				}
			} else {
				// Day field (1-31)
				if !v.validateCronField(field, 1, 31) {
					return fmt.Errorf("invalid day field in cron schedule: %s", field)
				}
			}
		case 3: // Day (if 6 fields) or Month (if 5 fields)
			if len(fields) == 6 {
				// Day field (1-31)
				if !v.validateCronField(field, 1, 31) {
					return fmt.Errorf("invalid day field in cron schedule: %s", field)
				}
			} else {
				// Month field (1-12)
				if !v.validateCronField(field, 1, 12) {
					return fmt.Errorf("invalid month field in cron schedule: %s", field)
				}
			}
		case 4: // Month (if 6 fields) or Day of week (if 5 fields)
			if len(fields) == 6 {
				// Month field (1-12)
				if !v.validateCronField(field, 1, 12) {
					return fmt.Errorf("invalid month field in cron schedule: %s", field)
				}
			} else {
				// Day of week field (0-7, where 0 and 7 are Sunday)
				if !v.validateCronField(field, 0, 7) {
					return fmt.Errorf("invalid day of week field in cron schedule: %s", field)
				}
			}
		case 5: // Day of week (only if 6 fields)
			// Day of week field (0-7, where 0 and 7 are Sunday)
			if !v.validateCronField(field, 0, 7) {
				return fmt.Errorf("invalid day of week field in cron schedule: %s", field)
			}
		}
	}

	return nil
}

// validateCronField validates individual cron field
func (v *BackupValidator) validateCronField(field string, min, max int) bool {
	if field == "*" {
		return true
	}

	// Handle step values (*/n)
	if strings.HasPrefix(field, "*/") {
		stepStr := field[2:]
		step, err := strconv.Atoi(stepStr)
		return err == nil && step > 0 && step <= max
	}

	// Handle ranges (n-m)
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) != 2 {
			return false
		}
		start, err1 := strconv.Atoi(parts[0])
		end, err2 := strconv.Atoi(parts[1])
		return err1 == nil && err2 == nil && start >= min && start <= max && end >= min && end <= max && start <= end
	}

	// Handle single number
	num, err := strconv.Atoi(field)
	return err == nil && num >= min && num <= max
}

// validateCloudConfiguration validates cloud-specific backup configuration
func (v *BackupValidator) validateCloudConfiguration(cloud *neo4jv1alpha1.CloudBlock) field.ErrorList {
	var allErrs field.ErrorList
	cloudPath := field.NewPath("spec", "cloud")

	// Validate cloud provider if specified
	if cloud.Provider != "" {
		validProviders := []string{"aws", "gcp", "azure", "custom"}
		valid := false
		for _, provider := range validProviders {
			if cloud.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				cloudPath.Child("provider"),
				cloud.Provider,
				validProviders,
			))
		}
	}

	// Validate cloud identity if specified
	if cloud.Identity != nil {
		if cloud.Identity.Provider == "" {
			allErrs = append(allErrs, field.Required(
				cloudPath.Child("identity", "provider"),
				"identity provider must be specified",
			))
		}
	}

	return allErrs
}

// validateRetentionPolicy validates backup retention policy
func (v *BackupValidator) validateRetentionPolicy(retention *neo4jv1alpha1.RetentionPolicy) field.ErrorList {
	var allErrs field.ErrorList
	retentionPath := field.NewPath("spec", "retention")

	// Validate max age format if specified
	if retention.MaxAge != "" {
		if _, err := time.ParseDuration(retention.MaxAge); err != nil {
			allErrs = append(allErrs, field.Invalid(
				retentionPath.Child("maxAge"),
				retention.MaxAge,
				"invalid duration format. Use format like '24h', '7d', '30d'",
			))
		}
	}

	// Validate max count
	if retention.MaxCount < 0 {
		allErrs = append(allErrs, field.Invalid(
			retentionPath.Child("maxCount"),
			retention.MaxCount,
			"max count must be non-negative",
		))
	}

	// Validate delete policy
	if retention.DeletePolicy != "" {
		validPolicies := []string{"Delete", "Archive"}
		valid := false
		for _, policy := range validPolicies {
			if retention.DeletePolicy == policy {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				retentionPath.Child("deletePolicy"),
				retention.DeletePolicy,
				validPolicies,
			))
		}
	}

	// Ensure at least one retention criterion is specified
	if retention.MaxAge == "" && retention.MaxCount == 0 {
		allErrs = append(allErrs, field.Invalid(
			retentionPath,
			retention,
			"at least one retention criterion (maxAge or maxCount) must be specified",
		))
	}

	return allErrs
}

// validateBackupOptions validates backup options for Neo4j 5.26+
func (v *BackupValidator) validateBackupOptions(options *neo4jv1alpha1.BackupOptions) field.ErrorList {
	var allErrs field.ErrorList
	optionsPath := field.NewPath("spec", "options")

	// Validate encryption configuration if specified
	if options.Encryption != nil {
		allErrs = append(allErrs, v.validateEncryptionConfiguration(options.Encryption)...)
	}

	// Validate additional args for Neo4j 5.26+ compatibility
	if len(options.AdditionalArgs) > 0 {
		for i, arg := range options.AdditionalArgs {
			if err := v.validateBackupArg(arg); err != nil {
				allErrs = append(allErrs, field.Invalid(
					optionsPath.Child("additionalArgs").Index(i),
					arg,
					err.Error(),
				))
			}
		}
	}

	return allErrs
}

// validateEncryptionConfiguration validates backup encryption for Neo4j 5.26+
func (v *BackupValidator) validateEncryptionConfiguration(encryption *neo4jv1alpha1.EncryptionSpec) field.ErrorList {
	var allErrs field.ErrorList
	encryptionPath := field.NewPath("spec", "options", "encryption")

	// If encryption is enabled, validate configuration
	if encryption.Enabled {
		// Validate key secret
		if encryption.KeySecret == "" {
			allErrs = append(allErrs, field.Required(
				encryptionPath.Child("keySecret"),
				"encryption key secret must be specified when encryption is enabled",
			))
		}

		// Validate algorithm
		if encryption.Algorithm != "" {
			validAlgorithms := []string{"AES256", "ChaCha20"}
			valid := false
			for _, alg := range validAlgorithms {
				if encryption.Algorithm == alg {
					valid = true
					break
				}
			}
			if !valid {
				allErrs = append(allErrs, field.NotSupported(
					encryptionPath.Child("algorithm"),
					encryption.Algorithm,
					validAlgorithms,
				))
			}
		}
	}

	return allErrs
}

// validateBackupArg validates additional backup arguments for Neo4j 5.26+
func (v *BackupValidator) validateBackupArg(arg string) error {
	// Neo4j 5.26+ deprecated and removed some backup arguments
	deprecatedArgs := []string{
		"--cc-graph",            // Consistency check graph (deprecated in 5.26+)
		"--cc-indexes",          // Consistency check indexes (deprecated in 5.26+)
		"--cc-label-scan-store", // Label scan store check (removed in 5.26+)
		"--legacy-format",       // Legacy format support (removed in 5.26+)
	}

	for _, deprecated := range deprecatedArgs {
		if strings.HasPrefix(arg, deprecated) {
			return fmt.Errorf("argument '%s' is deprecated/removed in Neo4j 5.26+", deprecated)
		}
	}

	// Validate that arguments are properly formatted
	if !strings.HasPrefix(arg, "--") && !strings.HasPrefix(arg, "-") {
		return fmt.Errorf("backup argument must start with '--' or '-'")
	}

	return nil
}

// isValidResourceName validates Kubernetes resource name format
func (v *BackupValidator) isValidResourceName(name string) bool {
	// Basic Kubernetes resource name validation
	if len(name) == 0 || len(name) > 253 {
		return false
	}

	// Must start and end with alphanumeric character
	validName := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	return validName.MatchString(name)
}
