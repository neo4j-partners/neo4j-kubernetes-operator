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

	// Additional neo4j-admin backup arguments
	AdditionalArgs []string `json:"additionalArgs,omitempty"`
}

// EncryptionSpec defines backup encryption configuration
type EncryptionSpec struct {
	// Enable encryption
	Enabled bool `json:"enabled,omitempty"`

	// Secret containing encryption key
	KeySecret string `json:"keySecret,omitempty"`

	// Encryption algorithm
	// +kubebuilder:validation:Enum=AES256;ChaCha20
	// +kubebuilder:default=AES256
	Algorithm string `json:"algorithm,omitempty"`
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.kind`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="LastRun",type=string,JSONPath=`.status.lastRunTime`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jBackup is the Schema for the neo4jbackups API
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
