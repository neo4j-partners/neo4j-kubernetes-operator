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

	cron "github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// maxScheduledBackupNameLength bounds a scheduled Neo4jBackup's name so the
// generated CronJob name ("<name>-backup-cron") stays within Kubernetes'
// 52-character CronJob-name limit. Kubernetes caps CronJob names at 52
// (DNS-1035 label max 63 minus the 11-char "-<timestamp>" suffix the CronJob
// controller appends to child Jobs); "-backup-cron" is 12 chars, so
// 52 - 12 = 40. Without this check a longer name fails only at CronJob
// creation time with an opaque apiserver error and the scheduled backup
// never runs. One-shot backups are unaffected: their Job ("<name>-backup")
// and temp PVC ("<name>-temp-staging") names are bounded by the 253-char
// limit, so they don't hard-fail.
const maxScheduledBackupNameLength = 40

// ValidateScheduledBackupName returns an error if a scheduled Neo4jBackup's
// name is too long for the CronJob the operator generates from it
// ("<name>-backup-cron"). It is called inline from the backup reconciler
// before the CronJob is created (the operator has no admission webhooks), so
// an over-long name fails fast with this clear message rather than at
// CronJob-create time with an opaque apiserver error — at which point the
// scheduled backup would silently never run. One-shot (unscheduled) backups
// don't need this: their Job/PVC names are bounded by the 253-char limit.
func ValidateScheduledBackupName(name string) error {
	if len(name) > maxScheduledBackupNameLength {
		return fmt.Errorf(
			"backup name %q is too long for a scheduled backup (%d chars): the generated CronJob name %q would exceed Kubernetes' 52-character CronJob name limit; use a name of at most %d characters",
			name, len(name), name+"-backup-cron", maxScheduledBackupNameLength)
	}
	return nil
}

// maxAgeShorthand matches the day/hour/minute/second retention shorthand the
// backup controller's parseFindTimeArg understands (e.g. "7d", "30d", "24h").
var maxAgeShorthand = regexp.MustCompile(`^[1-9][0-9]*[dhms]$`)

// isValidMaxAge reports whether a retention maxAge is one the operator can
// actually apply: either the "<n>{d|h|m|s}" shorthand or a value Go's
// time.ParseDuration accepts. (time.ParseDuration alone rejects "7d"/"30d".)
func isValidMaxAge(maxAge string) bool {
	if maxAgeShorthand.MatchString(maxAge) {
		return true
	}
	_, err := time.ParseDuration(maxAge)
	return err == nil
}

// BackupValidator validates Neo4j backup configuration for Neo4j 5.26+ compatibility
type BackupValidator struct{}

// NewBackupValidator creates a new backup validator
func NewBackupValidator() *BackupValidator {
	return &BackupValidator{}
}

// Validate validates the backup configuration for Neo4j 5.26+ compatibility
func (v *BackupValidator) Validate(backup *neo4jv1beta1.Neo4jBackup) field.ErrorList {
	var allErrs field.ErrorList

	// Validate backup target
	allErrs = append(allErrs, v.validateBackupTarget(&backup.Spec.Target, backup.Namespace)...)

	// Validate storage configuration
	allErrs = append(allErrs, v.validateStorageConfiguration(&backup.Spec.Storage, backup.Spec.Cloud)...)

	// Validate schedule if specified
	if backup.Spec.Schedule != "" {
		if err := v.validateSchedule(backup.Spec.Schedule); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "schedule"),
				backup.Spec.Schedule,
				err.Error(),
			))
		}

		// A scheduled backup generates a CronJob named "<name>-backup-cron".
		// Kubernetes caps CronJob names at 52 chars; catch an over-long name
		// here instead of letting the CronJob create fail opaquely at
		// reconcile time (the backup would never run).
		if err := ValidateScheduledBackupName(backup.Name); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("metadata", "name"),
				backup.Name,
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

	// chainFromBackup self-reference check. Cross-CR consistency
	// (target match + storage match) is enforced by the reconciler at
	// reconcile time, because it requires a client lookup.
	if backup.Spec.ChainFromBackup != "" && backup.Spec.ChainFromBackup == backup.Name {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "chainFromBackup"),
			backup.Spec.ChainFromBackup,
			"cannot chain to self",
		))
	}

	return allErrs
}

// validateBackupTarget validates the backup target configuration. backupNamespace
// is the namespace of the parent Neo4jBackup CR; used to enforce same-namespace
// ClusterRef for database-scoped kinds.
func (v *BackupValidator) validateBackupTarget(target *neo4jv1beta1.BackupTarget, backupNamespace string) field.ErrorList {
	var allErrs field.ErrorList
	targetPath := field.NewPath("spec", "target")

	// Validate target kind
	validKinds := []string{
		neo4jv1beta1.BackupTargetKindCluster,
		neo4jv1beta1.BackupTargetKindDatabase,
		neo4jv1beta1.BackupTargetKindShardedDatabase,
	}
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
	} else if !v.isValidResourceName(target.Name) {
		// Validate name format (Kubernetes resource name validation)
		allErrs = append(allErrs, field.Invalid(
			targetPath.Child("name"),
			target.Name,
			"invalid resource name format",
		))
	}

	// Database-scoped kinds (Database, ShardedDatabase) require ClusterRef
	// and only support same-namespace references in v1.
	if neo4jv1beta1.IsDatabaseScopedBackupKind(target.Kind) {
		if target.ClusterRef == "" {
			allErrs = append(allErrs, field.Required(
				targetPath.Child("clusterRef"),
				fmt.Sprintf("clusterRef is required when target.kind=%s", target.Kind),
			))
		}
		if target.Namespace != "" && target.Namespace != backupNamespace {
			allErrs = append(allErrs, field.Invalid(
				targetPath.Child("namespace"),
				target.Namespace,
				fmt.Sprintf("cross-namespace target references are not supported (backup namespace: %s)", backupNamespace),
			))
		}
	}

	return allErrs
}

// validateStorageConfiguration validates storage configuration for Neo4j 5.26+.
// specCloud is the top-level spec.cloud block; the operator resolves the
// effective cloud config as storage.cloud ?? spec.cloud (see getCloudBlock in
// the backup controller), so provider checks must consider both.
func (v *BackupValidator) validateStorageConfiguration(storage *neo4jv1beta1.StorageLocation, specCloud *neo4jv1beta1.CloudBlock) field.ErrorList {
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
	if err := v.validateStorageProvider(storage, specCloud); err != nil {
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

// validateStorageProvider validates provider-specific storage configurations.
// effectiveCloud mirrors the controller's resolution (storage.cloud ?? spec.cloud)
// so cloud-provider requirements are checked against the block the operator
// actually uses — the cloud config commonly lives at spec.cloud, not nested
// under storage.cloud.
func (v *BackupValidator) validateStorageProvider(storage *neo4jv1beta1.StorageLocation, specCloud *neo4jv1beta1.CloudBlock) error {
	storageType := strings.ToLower(storage.Type)

	effectiveCloud := storage.Cloud
	if effectiveCloud == nil {
		effectiveCloud = specCloud
	}

	// For cloud storage providers, validate additional configuration
	switch storageType {
	case "s3":
		// S3 specific validations for Neo4j 5.26+
		if storage.Bucket == "" {
			return fmt.Errorf("S3 storage requires bucket name for Neo4j 5.26+")
		}
		if effectiveCloud == nil || effectiveCloud.Provider != "aws" {
			return fmt.Errorf("S3 storage requires cloud provider 'aws' (set spec.cloud.provider or spec.storage.cloud.provider)")
		}
	case "gcs":
		// GCS specific validations
		if storage.Bucket == "" {
			return fmt.Errorf("Google Cloud Storage requires bucket name for Neo4j 5.26+")
		}
		if effectiveCloud == nil || effectiveCloud.Provider != "gcp" {
			return fmt.Errorf("GCS storage requires cloud provider 'gcp' (set spec.cloud.provider or spec.storage.cloud.provider)")
		}
	case "azure":
		// Azure specific validations
		if storage.Bucket == "" {
			return fmt.Errorf("Azure Blob Storage requires container name for Neo4j 5.26+")
		}
		if effectiveCloud == nil || effectiveCloud.Provider != "azure" {
			return fmt.Errorf("Azure storage requires cloud provider 'azure' (set spec.cloud.provider or spec.storage.cloud.provider)")
		}
	case "pvc":
		// PVC specific validations.
		//
		// `spec.storage.pvc.name` is REQUIRED — backups land in /backup
		// inside the Pod, which is either the named PVC or (if Name is
		// empty) an EmptyDir that evaporates with the Job. The
		// previously-permissive check let `pvc: {}` through and produced
		// silently-discarded artifacts: the backup Job reports Succeeded,
		// status.history records the run, but the .backup file never
		// existed on durable storage so any future restore is impossible.
		if storage.PVC == nil {
			return fmt.Errorf("PVC storage requires spec.storage.pvc to be set")
		}
		// Trim before checking so whitespace-only names like "   " don't
		// slip through. K8s would reject them at PVC lookup time anyway
		// (PVC names follow DNS-label rules), but failing here gives a
		// clear "your CR is wrong" message instead of an opaque
		// MountVolume.SetUp failure on Pod startup.
		if strings.TrimSpace(storage.PVC.Name) == "" {
			return fmt.Errorf("PVC storage requires spec.storage.pvc.name to reference an existing PVC — without it, backup artifacts are written to an EmptyDir and discarded when the Job's TTL elapses")
		}
	}

	return nil
}

// validateSchedule validates the cron schedule using the SAME parser
// Kubernetes uses for CronJob.spec.schedule (github.com/robfig/cron/v3
// ParseStandard). This guarantees the operator accepts exactly what the
// generated CronJob will accept — standard 5-field cron plus lists ("0,30"),
// ranges ("1-5"), steps ("*/15", "1-30/5"), names ("MON-FRI", "JAN"), and
// macros ("@daily"). A hand-rolled check previously rejected several of those
// valid forms and, conversely, accepted 6-field expressions that the CronJob
// then rejected at create time — so we defer to the canonical parser.
func (v *BackupValidator) validateSchedule(schedule string) (err error) {
	// robfig/cron's parser can panic on some malformed inputs (e.g. certain
	// TZ=/CRON_TZ= forms) instead of returning an error. Kubernetes wraps the
	// same parser with a recover; do the same so a bad user-supplied schedule
	// is reported as a validation error rather than crashing the reconcile
	// worker.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid cron schedule %q: %v", schedule, r)
		}
	}()

	// robfig/cron's ParseStandard accepts a leading "TZ="/"CRON_TZ=" prefix,
	// but Kubernetes rejects timezone-embedded strings in CronJob.spec.schedule
	// (the operator does not set CronJob.spec.timeZone). Accepting them here
	// would let the CronJob create fail later with an opaque apiserver error,
	// defeating up-front validation — so reject them with a clear message.
	if t := strings.TrimSpace(schedule); strings.HasPrefix(t, "TZ=") || strings.HasPrefix(t, "CRON_TZ=") {
		return fmt.Errorf("cron schedule %q must not embed a timezone (TZ=/CRON_TZ=) — Kubernetes rejects timezone-prefixed CronJob schedules; use a plain UTC schedule such as \"0 2 * * *\"", schedule)
	}

	if _, perr := cron.ParseStandard(schedule); perr != nil {
		return fmt.Errorf("invalid cron schedule %q: %v (Kubernetes CronJob expects standard 5-field cron, e.g. \"0 2 * * *\", or a macro like \"@daily\")", schedule, perr)
	}
	return nil
}

// validateCloudConfiguration validates cloud-specific backup configuration
func (v *BackupValidator) validateCloudConfiguration(cloud *neo4jv1beta1.CloudBlock) field.ErrorList {
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
func (v *BackupValidator) validateRetentionPolicy(retention *neo4jv1beta1.RetentionPolicy) field.ErrorList {
	var allErrs field.ErrorList
	retentionPath := field.NewPath("spec", "retention")

	// Validate max age format if specified. The runtime (parseFindTimeArg in
	// the backup controller) accepts a day/hour/minute/second shorthand like
	// "7d", "30d", "24h" — which Go's time.ParseDuration rejects (no "d"
	// unit). Accept the shorthand the runtime understands, and also tolerate
	// any value time.ParseDuration accepts, so the validator never rejects a
	// maxAge the operator can actually apply.
	if retention.MaxAge != "" && !isValidMaxAge(retention.MaxAge) {
		allErrs = append(allErrs, field.Invalid(
			retentionPath.Child("maxAge"),
			retention.MaxAge,
			"invalid duration format. Use a value like '7d', '30d', '24h', or '90m'",
		))
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
func (v *BackupValidator) validateBackupOptions(options *neo4jv1beta1.BackupOptions) field.ErrorList {
	var allErrs field.ErrorList
	optionsPath := field.NewPath("spec", "options")

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

// ValidateNeo4jVersion validates that the Neo4j version is 5.26+ or 2025.01+ (calver)
func ValidateNeo4jVersion(imageTag string) error {
	if imageTag == "" {
		return fmt.Errorf("Neo4j image tag is required")
	}

	// Remove any additional tags or suffixes (e.g., "-enterprise")
	version := strings.Split(imageTag, "-")[0]

	// Check for calver format (2025.01.0 and up)
	if matched, _ := regexp.MatchString(`^20\d{2}\.\d{2}(\.\d+)?$`, version); matched {
		return validateCalverVersion(version)
	}

	// Check for semver format (5.26.0 and up)
	if matched, _ := regexp.MatchString(`^\d+\.\d+(\.\d+)?$`, version); matched {
		return validateSemverVersion(version)
	}

	return fmt.Errorf("invalid Neo4j version format: %s. Expected semver (5.26+) or calver (2025.01+)", version)
}

// validateSemverVersion validates semver format versions (5.26.0 and up)
func validateSemverVersion(version string) error {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid semver format: %s", version)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid minor version: %s", parts[1])
	}

	// Check minimum version requirements
	if major < 5 {
		return fmt.Errorf("Neo4j version %s is not supported. Minimum required version is 5.26.0", version)
	}

	if major == 5 && minor < 26 {
		return fmt.Errorf("Neo4j version %s is not supported. Minimum required version is 5.26.0", version)
	}

	return nil
}

// validateCalverVersion validates calver format versions (2025.01.0 and up)
func validateCalverVersion(version string) error {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid calver format: %s", version)
	}

	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid year in calver: %s", parts[0])
	}

	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid month in calver: %s", parts[1])
	}

	// Check minimum version requirements (2025.01 and up)
	if year < 2025 {
		return fmt.Errorf("Neo4j version %s is not supported. Minimum required calver version is 2025.01.0", version)
	}

	if year == 2025 && month < 1 {
		return fmt.Errorf("Neo4j version %s is not supported. Minimum required calver version is 2025.01.0", version)
	}

	return nil
}
