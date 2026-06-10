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

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Neo4jBackupSpec defines the desired state of Neo4jBackup
type Neo4jBackupSpec struct {
	// +kubebuilder:validation:Required
	// Target defines what to backup
	Target BackupTarget `json:"target"`

	// +kubebuilder:validation:Required
	// Storage defines where to store the backup
	Storage StorageLocation `json:"storage"`

	// Schedule for automated backups (cron format)
	Schedule string `json:"schedule,omitempty"`

	// Cloud configuration for cloud storage
	Cloud *CloudBlock `json:"cloud,omitempty"`

	// Retention policy for backup cleanup
	Retention *RetentionPolicy `json:"retention,omitempty"`

	// Backup options
	Options *BackupOptions `json:"options,omitempty"`

	// Suspend the backup schedule
	Suspend bool `json:"suspend,omitempty"`

	// ChainFromBackup names another Neo4jBackup CR in the same namespace
	// whose `<base>/<cr-name>/` directory this CR should write into INSTEAD
	// of its own. Use this to compose mixed-cadence backup workflows: a
	// daily FULL backup CR plus an hourly DIFF backup CR that chains off
	// the daily's artifacts. `neo4j-admin database backup --type=DIFF`
	// auto-detects the prior full/diff in the shared directory and chains
	// the new diff off it.
	//
	// Constraints (enforced by the validator):
	//   - Must reference a CR in the same namespace.
	//   - Cannot self-reference (`chainFromBackup: <this-cr-name>` is
	//     rejected).
	//   - Target (cluster + database) must match the referenced CR.
	//   - Storage backend (type + bucket/path) must match.
	//
	// Runtime: the operator labels every backup Job with
	// `app.kubernetes.io/part-of: <chain-root-name>` and refuses to
	// submit a new Job while another Job sharing the same `part-of`
	// label is still active — prevents the daily FULL and hourly DIFF
	// from racing against the same artifact directory.
	//
	// Empty (default): write to this CR's own per-name directory.
	ChainFromBackup string `json:"chainFromBackup,omitempty"`
}

// BackupTargetKind values accepted on BackupTarget.Kind. Use these constants
// instead of raw strings at call sites.
const (
	BackupTargetKindCluster         = "Cluster"
	BackupTargetKindDatabase        = "Database"
	BackupTargetKindShardedDatabase = "ShardedDatabase"
)

// IsDatabaseScoped reports whether the kind addresses a single database (or
// logical sharded family) rather than the whole cluster. Database-scoped kinds
// require ClusterRef and carry the database name (not the cluster name) in
// Target.Name.
func IsDatabaseScopedBackupKind(kind string) bool {
	return kind == BackupTargetKindDatabase || kind == BackupTargetKindShardedDatabase
}

// BackupTarget defines what to backup
type BackupTarget struct {
	// +kubebuilder:validation:Enum=Cluster;Database;ShardedDatabase
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	// Name of the target resource. For Kind=ShardedDatabase, this is the logical
	// sharded-database name (e.g. "products"); the operator backs up all shards
	// (products-g000, products-p000, …) in one neo4j-admin invocation via a glob.
	Name string `json:"name"`

	// ClusterRef is the name of the Neo4jEnterpriseCluster (or Neo4jEnterpriseStandalone)
	// that owns the database. Required when Kind=Database or Kind=ShardedDatabase;
	// unused when Kind=Cluster.
	ClusterRef string `json:"clusterRef,omitempty"`

	// Namespace of the target resource (defaults to backup namespace)
	Namespace string `json:"namespace,omitempty"`
}

// RetentionPolicy defines backup retention rules
type RetentionPolicy struct {
	// Maximum age of backups to keep
	MaxAge string `json:"maxAge,omitempty"`

	// Maximum number of backups to keep
	MaxCount int32 `json:"maxCount,omitempty"`

	// Delete policy for expired backups
	// +kubebuilder:validation:Enum=Delete;Archive
	// +kubebuilder:default=Delete
	DeletePolicy string `json:"deletePolicy,omitempty"`
}

// BackupOptions defines backup-specific options
type BackupOptions struct {
	// Resources sets CPU/memory requests + limits on the backup Job's
	// container. When unset, the operator applies a Burstable default
	// (request 100m CPU / 512Mi memory, limit 1 CPU / 2Gi memory) sized
	// for empty/small databases and CI environments. Tune upward for
	// large production databases (rule of thumb: limit memory should
	// exceed the largest store file by ~30% to give `neo4j-admin`
	// breathing room during compaction).
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Compress the backup (default: true)
	// +kubebuilder:default=true
	Compress bool `json:"compress,omitempty"`

	// Verify backup integrity after creation
	Verify bool `json:"verify,omitempty"`

	// Backup type
	// +kubebuilder:validation:Enum=FULL;DIFF;AUTO
	// +kubebuilder:default=AUTO
	BackupType string `json:"backupType,omitempty"`

	// Page cache size for backup operation (e.g., "4G")
	// +kubebuilder:validation:Pattern="^[0-9]+[KMG]?$"
	PageCache string `json:"pageCache,omitempty"`

	// Enable parallel download for remote backups
	ParallelDownload bool `json:"parallelDownload,omitempty"`

	// Resolve remote addresses for backups via the cluster discovery service
	// (useful in multi-homed environments). When unset and target Kind is
	// ShardedDatabase on Neo4j 2025.09+, the operator defaults this to true to
	// match the canonical upstream sharded-backup invocation. Set explicitly to
	// override the default in either direction.
	RemoteAddressResolution *bool `json:"remoteAddressResolution,omitempty"`

	// Skip recovery step after backup (advanced; use when recovery is handled separately)
	SkipRecovery bool `json:"skipRecovery,omitempty"`

	// Validate runs `neo4j-admin backup validate` against the backup artifacts
	// after the backup itself succeeds, capturing per-shard recoverability
	// status into `BackupRun.Validation`. Opt-in because validate adds
	// ~10-60s of runtime depending on artifact size, and validate failures
	// are recorded but don't fail the Job (the backup itself already
	// succeeded). Pointer type so users explicitly opting in OR out
	// preserves their choice across Update round-trips.
	//
	// When nil (default) or false: validate is not invoked; Validation
	// field stays empty.
	// When true: validate is appended to the backup command with `|| true`
	// (suppress non-zero exit so the Job stays Succeeded). The operator
	// parses validate's stdout into BackupRun.Validation.
	// +optional
	Validate *bool `json:"validate,omitempty"`

	// Additional neo4j-admin backup arguments
	AdditionalArgs []string `json:"additionalArgs,omitempty"`

	// PreferDiffAsParent instructs the backup to use the latest differential
	// backup as the parent instead of the latest full backup when creating a
	// differential backup. Requires CalVer 2025.04+.
	PreferDiffAsParent bool `json:"preferDiffAsParent,omitempty"`

	// TempPath is a local directory for temporary files during the backup.
	// When TempStorage is configured, this is set automatically to the mount path.
	// Only set manually if you are mounting your own volume via other means.
	TempPath string `json:"tempPath,omitempty"`

	// TempStorage provisions a PVC for temporary staging files during cloud backups.
	// Without this, cloud backups stage to the container's ephemeral disk which may
	// be too small for large databases. The operator mounts this PVC and passes
	// --temp-path automatically.
	TempStorage *TempStorageSpec `json:"tempStorage,omitempty"`

	// IncludeMetadata controls which metadata (users, roles) is included in the backup.
	// Supported values: "all" (default), "none", "users", "roles".
	// Requires Neo4j 5.26+.
	// +kubebuilder:validation:Enum=all;none;users;roles
	IncludeMetadata string `json:"includeMetadata,omitempty"`

	// ParallelRecovery enables multi-threaded transaction application during backup.
	ParallelRecovery bool `json:"parallelRecovery,omitempty"`

	// KeepFailed preserves failed backup artifacts for debugging instead of deleting them.
	KeepFailed bool `json:"keepFailed,omitempty"`
}

// TempStorageSpec provisions temporary staging storage for cloud backup/restore.
// The operator creates a PVC, mounts it at /tmp/neo4j-staging in the Job pod,
// and passes --temp-path=/tmp/neo4j-staging to neo4j-admin. The PVC is owned
// by the Job and garbage-collected when the Job's TTL expires.
type TempStorageSpec struct {
	// Size of the temporary PVC (e.g., "50Gi"). Should be at least as large
	// as the expected backup/restore artifact.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^[0-9]+(Ki|Mi|Gi|Ti)?$"
	Size string `json:"size"`

	// StorageClassName for the temporary PVC. If empty, uses the cluster default.
	StorageClassName string `json:"storageClassName,omitempty"`
}

// Neo4jBackupStatus defines the observed state of Neo4jBackup
type Neo4jBackupStatus struct {
	// Conditions represent the current state of the backup
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the backup
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current state
	Message string `json:"message,omitempty"`

	// LastRunTime shows when the last backup was started
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`

	// LastSuccessTime shows when the last successful backup completed
	LastSuccessTime *metav1.Time `json:"lastSuccessTime,omitempty"`

	// NextRunTime shows when the next backup is scheduled
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`

	// Backup statistics
	Stats *BackupStats `json:"stats,omitempty"`

	// History of recent backup runs
	History []BackupRun `json:"history,omitempty"`

	// ObservedGeneration reflects the generation most recently observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// BackupStats provides backup statistics
type BackupStats struct {
	// Size of the backup
	Size string `json:"size,omitempty"`

	// Duration of the backup
	Duration string `json:"duration,omitempty"`

	// Throughput of the backup
	Throughput string `json:"throughput,omitempty"`

	// Number of files in the backup
	FileCount int32 `json:"fileCount,omitempty"`
}

// BackupRun represents a single backup execution
type BackupRun struct {
	// RunID uniquely identifies this backup execution. Populated from the
	// backing Job's name (e.g. "my-backup-backup" for a one-shot run,
	// "my-backup-cron-1737028800" for a scheduled child) so a history entry
	// is human-readable and maps directly to the Job found via
	// `kubectl get jobs` — the same value the backup Pod sees as
	// BACKUP_RUN_ID. Unique per run: one-shot Jobs are created once per CR;
	// CronJob children carry a Unix-second suffix.
	RunID string `json:"runID,omitempty"`

	// Start time of the backup run
	StartTime metav1.Time `json:"startTime"`

	// Completion time of the backup run
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Status of the backup run
	Status string `json:"status"`

	// Error message if the backup failed
	Error string `json:"error,omitempty"`

	// Statistics for this backup run
	Stats *BackupStats `json:"stats,omitempty"`

	// BackupsPath is the shared per-CR directory (relative to
	// spec.storage.path) where this run wrote its `.backup` artifact.
	// SAME VALUE FOR EVERY RUN OF ONE CR — all runs accumulate in this
	// directory so neo4j-admin can chain differential backups off the
	// prior full. Use the RunID field for per-run identity.
	// +optional
	BackupsPath string `json:"backupsPath,omitempty"`

	// ArtifactFilename is the filename of the `.backup` artifact produced
	// by this standard-DB run (e.g. "neo4j-2026-06-08T01-18-06.backup").
	// Populated by parsing the Job's Pod log after completion. Empty when
	// the operator couldn't fetch logs or the pattern didn't match.
	//
	// Used by the cluster PVC-restore path: the operator builds a
	// per-restore HTTP proxy URL of the form
	//   http://<proxy>:8080/<backupsPath>/<artifactFilename>
	// which is passed as seedURI to dbms.recreateDatabase /
	// CREATE DATABASE OPTIONS{seedURI}. Sharded backups populate
	// per-shard equivalents in ShardArtifacts[].Filename instead.
	// +optional
	ArtifactFilename string `json:"artifactFilename,omitempty"`

	// ShardArtifacts records the per-shard `.backup` files produced by a
	// sharded backup run (target.kind=ShardedDatabase). Populated by the
	// backup controller after parsing the trailing `ls -la` output appended
	// to the backup command. Empty for non-sharded runs.
	// +optional
	ShardArtifacts []ShardArtifact `json:"shardArtifacts,omitempty"`

	// Validation captures the per-shard outcome of an optional
	// `neo4j-admin backup validate` step run after the backup completes.
	// Populated only when the operator was able to run validate and parse
	// its output (sharded backups on a Neo4j version that exposes the
	// validate subcommand). Absent on older versions or non-sharded runs.
	// +optional
	Validation *BackupValidationResult `json:"validation,omitempty"`
}

// ShardArtifact identifies one `.backup` file produced by a sharded backup.
type ShardArtifact struct {
	// ShardName is the per-shard database name (e.g. "products-g000",
	// "products-p000"). Derived by stripping the timestamp suffix from the
	// neo4j-admin output filename.
	ShardName string `json:"shardName"`

	// Filename is the on-disk filename as written by neo4j-admin (e.g.
	// "products-g000-2025-06-11T21-04-42.backup").
	Filename string `json:"filename,omitempty"`

	// Size is the artifact size in bytes as reported by `ls -la`. Zero if
	// not parseable. Use `humanize.IBytes` or equivalent on the consumer
	// side for display.
	Size int64 `json:"size,omitempty"`
}

// BackupValidationResult captures the output of `neo4j-admin backup validate`
// run after a sharded backup, surfacing per-shard recoverability.
type BackupValidationResult struct {
	// OverallStatus is "OK" if every shard's chain is recoverable to the
	// same transaction or higher; "Degraded" if any shard is ahead/behind
	// beyond the lenient consistency window; "Unknown" if the validate step
	// failed or its output couldn't be parsed.
	// +kubebuilder:validation:Enum=OK;Degraded;Unknown
	OverallStatus string `json:"overallStatus,omitempty"`

	// PerShard holds the validate command's status report for each shard.
	// +optional
	PerShard []ShardValidationStatus `json:"perShard,omitempty"`

	// RawOutput is the truncated stdout of `neo4j-admin backup validate`,
	// kept for operator debugging when the parser couldn't classify a
	// per-shard line. Capped at 2 KiB; longer outputs are truncated with a
	// "…(truncated)" marker.
	// +optional
	RawOutput string `json:"rawOutput,omitempty"`
}

// ShardValidationStatus is one row of validate output.
type ShardValidationStatus struct {
	ShardName string `json:"shardName"`
	// Status is one of OK / Ahead / Behind / Unknown.
	// +kubebuilder:validation:Enum=OK;Ahead;Behind;Unknown
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.kind`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="LastRun",type=string,JSONPath=`.status.lastRunTime`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jBackup declaratively manages a Neo4j backup. The operator runs
// `neo4j-admin database backup` against the referenced cluster or
// standalone target, writes the backup to local PVC storage or to a
// supported cloud bucket (S3 / GCS / Azure Blob / MinIO), records the
// outcome on .status, and (optionally) enforces retention. Schedules
// are expressed via standard cron syntax. See also: Neo4jRestore.
type Neo4jBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jBackupSpec   `json:"spec,omitempty"`
	Status Neo4jBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jBackupList contains a list of Neo4jBackup
type Neo4jBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jBackup{}, &Neo4jBackupList{})
}
