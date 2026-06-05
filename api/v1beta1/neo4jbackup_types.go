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
}

// BackupTarget defines what to backup
type BackupTarget struct {
	// +kubebuilder:validation:Enum=Cluster;Database
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	// Name of the target resource
	Name string `json:"name"`

	// ClusterRef is the name of the Neo4jEnterpriseCluster (or Neo4jEnterpriseStandalone)
	// that owns the database. Required when Kind=Database; unused when Kind=Cluster.
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
	// Compress the backup (default: true)
	// +kubebuilder:default=true
	Compress bool `json:"compress,omitempty"`

	// Encryption configuration
	Encryption *EncryptionSpec `json:"encryption,omitempty"`

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

	// Resolve remote addresses for backups (useful in multi-homed environments)
	RemoteAddressResolution bool `json:"remoteAddressResolution,omitempty"`

	// Skip recovery step after backup (advanced; use when recovery is handled separately)
	SkipRecovery bool `json:"skipRecovery,omitempty"`

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

// EncryptionSpec defines encryption configuration for backup and restore operations
type EncryptionSpec struct {
	// Enable encryption
	Enabled bool `json:"enabled,omitempty"`

	// Secret containing encryption key
	KeySecret string `json:"keySecret,omitempty"`

	// Key within the secret containing the encryption key
	// +kubebuilder:default=key
	KeySecretKey string `json:"keySecretKey,omitempty"`

	// Encryption algorithm
	// +kubebuilder:validation:Enum=AES256;ChaCha20Poly1305
	// +kubebuilder:default=AES256
	Algorithm string `json:"algorithm,omitempty"`
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
	// backing Job's metadata.uid so a history entry can be correlated with
	// the actual Job artifact (or audit log) that produced it. Stable for
	// the lifetime of the Job; new for every retry.
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

	// BackupsPath is the run-specific subfolder under spec.storage.path
	// where this run's `.backup` artifact(s) were written. To restore from
	// this run, set Neo4jRestore.spec.source.backupPath to
	// "<spec.storage.path>/<BackupsPath>" — neo4j-admin's --from-path
	// accepts directories and auto-discovers .backup files inside.
	//
	// The subfolder name is the backing Job's name: for one-shot
	// Neo4jBackup CRs it is "<backup-name>-backup"; for scheduled
	// (CronJob) backups Kubernetes auto-suffixes it with the run's
	// scheduled Unix timestamp ("<cronjob-name>-<unix-seconds>"), so
	// every run lands in its own directory.
	//
	// Older runs predating the per-run-subfolder change have an empty
	// BackupsPath — those artifacts live flat in spec.storage.path.
	// +optional
	BackupsPath string `json:"backupsPath,omitempty"`
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
