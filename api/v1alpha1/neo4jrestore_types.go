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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4jRestoreSpec defines the desired state of Neo4jRestore
type Neo4jRestoreSpec struct {
	// +kubebuilder:validation:Required
	// Target cluster to restore to
	TargetCluster string `json:"targetCluster"`

	// +kubebuilder:validation:Required
	// Source backup location
	Source RestoreSource `json:"source"`

	// +kubebuilder:validation:Required
	// Database to restore to
	DatabaseName string `json:"databaseName"`

	// Restore options
	Options *RestoreOptionsSpec `json:"options,omitempty"`

	// Force restore even if database exists
	Force bool `json:"force,omitempty"`

	// Stop cluster before restore (required for some restore operations)
	StopCluster bool `json:"stopCluster,omitempty"`

	// Timeout for restore operation
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

	// Transaction log retention period
	// +kubebuilder:default="7d"
	LogRetention string `json:"logRetention,omitempty"`

	// Recovery point objective
	// +kubebuilder:default="1m"
	RecoveryPointObjective string `json:"recoveryPointObjective,omitempty"`

	// Base backup to restore from before applying transaction logs
	BaseBackup *BaseBackupSource `json:"baseBackup,omitempty"`

	// Validate transaction log integrity before restore
	// +kubebuilder:default=true
	ValidateLogIntegrity bool `json:"validateLogIntegrity,omitempty"`

	// Compression settings for transaction logs
	Compression *CompressionConfig `json:"compression,omitempty"`

	// Encryption settings for transaction logs
	Encryption *EncryptionConfig `json:"encryption,omitempty"`
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

// CompressionConfig defines compression settings
type CompressionConfig struct {
	// Enable compression
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Compression algorithm (gzip, lz4, zstd)
	// +kubebuilder:validation:Enum=gzip;lz4;zstd
	// +kubebuilder:default=gzip
	Algorithm string `json:"algorithm,omitempty"`

	// Compression level (1-9 for gzip, 1-12 for lz4, 1-22 for zstd)
	Level int32 `json:"level,omitempty"`
}

// EncryptionConfig defines encryption settings
type EncryptionConfig struct {
	// Enable encryption
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Encryption algorithm (AES256, ChaCha20Poly1305)
	// +kubebuilder:validation:Enum=AES256;ChaCha20Poly1305
	// +kubebuilder:default=AES256
	Algorithm string `json:"algorithm,omitempty"`

	// Secret containing encryption key
	KeySecret string `json:"keySecret,omitempty"`

	// Key within the secret
	// +kubebuilder:default=key
	KeySecretKey string `json:"keySecretKey,omitempty"`
}

// RestoreOptionsSpec defines restore-specific options
type RestoreOptionsSpec struct {
	// Replace existing database
	ReplaceExisting bool `json:"replaceExisting,omitempty"`

	// Verify backup before restore
	VerifyBackup bool `json:"verifyBackup,omitempty"`

	// Additional neo4j-admin restore arguments
	AdditionalArgs []string `json:"additionalArgs,omitempty"`

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
	Env []EnvVar `json:"env,omitempty"`
}

// EnvVar represents an environment variable
type EnvVar struct {
	// Name of the environment variable
	Name string `json:"name"`

	// Value of the environment variable
	Value string `json:"value,omitempty"`

	// ValueFrom specifies a source for the value
	ValueFrom *EnvVarSource `json:"valueFrom,omitempty"`
}

// EnvVarSource represents a source for the value of an EnvVar
type EnvVarSource struct {
	// Secret key reference
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef,omitempty"`

	// ConfigMap key reference
	ConfigMapKeyRef *ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
}

// SecretKeySelector selects a key of a Secret
type SecretKeySelector struct {
	// Name of the secret
	Name string `json:"name"`

	// Key within the secret
	Key string `json:"key"`
}

// ConfigMapKeySelector selects a key of a ConfigMap
type ConfigMapKeySelector struct {
	// Name of the ConfigMap
	Name string `json:"name"`

	// Key within the ConfigMap
	Key string `json:"key"`
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
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetCluster`
// +kubebuilder:printcolumn:name="Database",type=string,JSONPath=`.spec.databaseName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Duration",type=string,JSONPath=`.status.stats.duration`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jRestore is the Schema for the neo4jrestores API
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
