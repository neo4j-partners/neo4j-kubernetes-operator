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

// Neo4jRestoreSpec defines the desired state of Neo4jRestore
type Neo4jRestoreSpec struct {
	// +kubebuilder:validation:Required
	// Reference to the Neo4j cluster or standalone to restore to
	ClusterRef string `json:"clusterRef"`

	// +kubebuilder:validation:Required
	// Source backup location
	Source RestoreSource `json:"source"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9.\-]*$`
	// +kubebuilder:validation:MaxLength=65
	// Database to restore to. Must be a valid Neo4j database name (starts with a
	// letter; letters, digits, dots, dashes only) — the name is interpolated
	// into the restore Job's shell command and Cypher.
	DatabaseName string `json:"databaseName"`

	// Restore options
	Options *RestoreOptionsSpec `json:"options,omitempty"`

	// Force restore even if database exists
	Force bool `json:"force,omitempty"`

	// Stop cluster before restore (required for some restore operations)
	StopCluster bool `json:"stopCluster,omitempty"`

	// Timeout bounds the cluster Cypher restore's online-convergence wait
	// (Go duration, e.g. "30m"). Defaults to 5m when unset — increase for
	// large stores seeded from object storage. The standalone Job path is
	// bounded by the Job's own backoff/active-deadline semantics instead.
	Timeout string `json:"timeout,omitempty"`
}

// RestoreSource defines the source of the backup to restore
type RestoreSource struct {
	// +kubebuilder:validation:Enum=backup;storage;pitr
	// +kubebuilder:validation:Required
	// Type of restore source
	Type string `json:"type"`

	// Reference to Neo4jBackup resource (when type=backup)
	BackupRef string `json:"backupRef,omitempty"`

	// Direct storage location (when type=storage)
	Storage *StorageLocation `json:"storage,omitempty"`

	// Specific backup path within storage
	BackupPath string `json:"backupPath,omitempty"`

	// Point in time for restore (when type=pitr or for PITR with backup/storage types)
	PointInTime *metav1.Time `json:"pointInTime,omitempty"`

	// Point-in-time recovery configuration (when type=pitr)
	PITR *PITRConfig `json:"pitr,omitempty"`
}

// PITRConfig defines point-in-time recovery configuration
type PITRConfig struct {
	// Transaction log storage location
	LogStorage *StorageLocation `json:"logStorage,omitempty"`

	// Base backup to restore from before applying transaction logs
	BaseBackup *BaseBackupSource `json:"baseBackup,omitempty"`
}

// BaseBackupSource defines a backup source without PITR config to avoid circular references
type BaseBackupSource struct {
	// +kubebuilder:validation:Enum=backup;storage
	// +kubebuilder:validation:Required
	// Type of backup source (backup or storage, PITR not allowed to avoid circular reference)
	Type string `json:"type"`

	// Reference to Neo4jBackup resource (when type=backup)
	BackupRef string `json:"backupRef,omitempty"`

	// Direct storage location (when type=storage)
	Storage *StorageLocation `json:"storage,omitempty"`

	// Specific backup path within storage
	BackupPath string `json:"backupPath,omitempty"`
}

// RestoreOptionsSpec defines restore-specific options
type RestoreOptionsSpec struct {
	// Resources sets CPU/memory requests + limits on the restore Job's
	// container. When unset, the operator applies a Burstable default
	// (request 100m CPU / 512Mi memory, limit 1 CPU / 2Gi memory).
	//
	// NOTE: only applies to STANDALONE restores. Cluster Neo4jRestore
	// targets use the Cypher path (`dbms.recreateDatabase` /
	// `CREATE DATABASE OPTIONS{seedURI}`) which runs on the cluster's
	// server pods — no Job is spawned, and this field is ignored.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Replace existing database
	ReplaceExisting bool `json:"replaceExisting,omitempty"`

	// VerifyBackup is RESERVED and currently a no-op (#220) — accepted for
	// backward compatibility but not read by the operator. Verify artifacts
	// at backup time via Neo4jBackup.spec.options.validate instead.
	VerifyBackup bool `json:"verifyBackup,omitempty"`

	// Additional neo4j-admin restore arguments
	AdditionalArgs []string `json:"additionalArgs,omitempty"`

	// TempPath is a local directory for temporary files during restore.
	// When TempStorage is configured, this is set automatically to the mount path.
	// Only set manually if you are mounting your own volume via other means.
	TempPath string `json:"tempPath,omitempty"`

	// TempStorage provisions a PVC for temporary staging files during cloud restores.
	// Without this, cloud restores use the container's ephemeral disk which may be
	// too small for large databases. The operator mounts this PVC and passes
	// --temp-path automatically.
	TempStorage *TempStorageSpec `json:"tempStorage,omitempty"`

	// Pre-restore hooks
	PreRestore *RestoreHooks `json:"preRestore,omitempty"`

	// Post-restore hooks
	PostRestore *RestoreHooks `json:"postRestore,omitempty"`
}

// RestoreHooks defines hooks to run before/after restore
type RestoreHooks struct {
	// Job to run as hook
	Job *RestoreHookJob `json:"job,omitempty"`

	// Cypher statements to execute
	CypherStatements []string `json:"cypherStatements,omitempty"`
}

// RestoreHookJob defines a Kubernetes job to run as a hook
type RestoreHookJob struct {
	// Job template
	Template JobTemplateSpec `json:"template"`

	// Timeout for the hook job
	Timeout string `json:"timeout,omitempty"`
}

// JobTemplateSpec defines a job template for hooks
type JobTemplateSpec struct {
	// Container specification
	Container ContainerSpec `json:"container"`

	// Job-level configuration
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`
}

// ContainerSpec defines container specification for hooks
type ContainerSpec struct {
	// Container image
	Image string `json:"image"`

	// Command to execute
	Command []string `json:"command,omitempty"`

	// Arguments to pass to command
	Args []string `json:"args,omitempty"`

	// Environment variables
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// Neo4jRestoreStatus defines the observed state of Neo4jRestore
type Neo4jRestoreStatus struct {
	// Conditions represent the current state of the restore
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the restore
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current state
	Message string `json:"message,omitempty"`

	// Start time of the restore operation
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time of the restore operation
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Restore statistics
	Stats *RestoreStats `json:"stats,omitempty"`

	// Backup information that was restored
	BackupInfo *RestoreBackupInfo `json:"backupInfo,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed Neo4jRestore
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ResolvedSource pins the concrete backup location this restore resolved
	// from a `source.type: backup` reference. Populated the first time the
	// referenced Neo4jBackup is successfully dereferenced, then preferred on
	// every subsequent reconcile — so the restore no longer depends on the
	// Neo4jBackup CR continuing to exist (it may be deleted after resolution).
	// Nil for `source.type: storage` restores, which already carry an explicit
	// location in the spec. See issue #188.
	ResolvedSource *ResolvedRestoreSource `json:"resolvedSource,omitempty"`
}

// ResolvedRestoreSource is the concrete, self-contained backup location a
// `source.type: backup` restore resolved to. Once persisted to
// Neo4jRestore.status it is the source of truth for the restore, so the
// operator never has to re-read the (possibly since-deleted) Neo4jBackup CR.
type ResolvedRestoreSource struct {
	// BackupRef is the Neo4jBackup name this source was resolved from
	// (provenance only).
	BackupRef string `json:"backupRef,omitempty"`

	// Storage is the concrete storage location (PVC or cloud, with creds
	// folded in) of the resolved backup.
	Storage *StorageLocation `json:"storage,omitempty"`

	// BackupPath is the per-CR shared directory (chain root) of the resolved
	// most-recent Succeeded run.
	BackupPath string `json:"backupPath,omitempty"`

	// ArtifactFilename is the exact `.backup` filename of the resolved
	// most-recent Succeeded run. Required by the cluster Cypher restore paths
	// (cloud seedURI + PVC proxy), which seed from a single file; empty for
	// older backups whose Pod-log capture didn't record it (standalone Job
	// restores don't need it — they resolve the file with a shell glob).
	ArtifactFilename string `json:"artifactFilename,omitempty"`

	// ResolvedAt is when the backupRef was first dereferenced.
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`
}

// RestoreStats provides restore operation statistics
type RestoreStats struct {
	// Duration of the restore operation
	Duration string `json:"duration,omitempty"`

	// Size of data restored
	DataSize string `json:"dataSize,omitempty"`

	// Throughput of the restore operation
	Throughput string `json:"throughput,omitempty"`

	// Number of files restored
	FileCount int32 `json:"fileCount,omitempty"`

	// Errors encountered during restore
	ErrorCount int32 `json:"errorCount,omitempty"`
}

// RestoreBackupInfo provides information about the backup that was restored
type RestoreBackupInfo struct {
	// Source backup path
	BackupPath string `json:"backupPath,omitempty"`

	// Original creation time of the backup
	BackupCreatedAt *metav1.Time `json:"backupCreatedAt,omitempty"`

	// Original database name in the backup
	OriginalDatabase string `json:"originalDatabase,omitempty"`

	// Neo4j version of the backup
	Neo4jVersion string `json:"neo4jVersion,omitempty"`

	// Backup size
	BackupSize string `json:"backupSize,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Database",type=string,JSONPath=`.spec.databaseName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Duration",type=string,JSONPath=`.status.stats.duration`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jRestore declaratively restores a Neo4j database from a
// previously-completed Neo4jBackup or directly from a storage
// location (PVC or cloud bucket). The operator launches a Job that
// runs `neo4j-admin database restore` against the referenced
// cluster or standalone target, with optional pre-restore hooks and
// configurable handling of an existing same-named database. The
// referenced cluster/standalone must be Ready before the restore
// proceeds; failed restores do not silently retry without a spec
// change. See also: Neo4jBackup.
type Neo4jRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jRestoreSpec   `json:"spec,omitempty"`
	Status Neo4jRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jRestoreList contains a list of Neo4jRestore
type Neo4jRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jRestore{}, &Neo4jRestoreList{})
}
