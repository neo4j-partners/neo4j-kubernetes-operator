/*
Copyright Neo4j.

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

// BackupRetention bounds how much of a cadence is kept (BDR-014 §10). Set at most
// one of keepLast / keepDays; the schedule prunes chain-aware, never mid-chain links.
// +kubebuilder:validation:XValidation:rule="!(has(self.keepLast) && has(self.keepDays))",message="set keepLast or keepDays, not both"
type BackupRetention struct {
	// KeepLast keeps the last N units (whole chains for full, links for incremental).
	// +kubebuilder:validation:Minimum=1
	KeepLast *int32 `json:"keepLast,omitempty"`
	// KeepDays keeps units younger than N days.
	// +kubebuilder:validation:Minimum=1
	KeepDays *int32 `json:"keepDays,omitempty"`
}

// BackupCadence is one cron schedule plus its retention (BDR-014 §10).
type BackupCadence struct {
	// Schedule is a standard cron expression.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`
	// Retention for artifacts produced by this cadence.
	Retention *BackupRetention `json:"retention,omitempty"`
}

// AggregateCadence optionally collapses a chain into one recovered full (BDR-014 §10),
// bounding restore replay time and chain-loss risk.
type AggregateCadence struct {
	// Enabled turns on periodic aggregation.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// Schedule is a standard cron expression (required when enabled).
	Schedule string `json:"schedule,omitempty"`
}

// BackupTemplate is the inline Neo4jBackup spec the schedule emits, minus neo4jRef
// and type (which the schedule sets per cadence).
type BackupTemplate struct {
	// Databases to back up; "*" means all including system.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:default={"*"}
	Databases []string `json:"databases,omitempty"`
	// Destination is where artifacts land (inherited by every emitted Neo4jBackup).
	// +kubebuilder:validation:Required
	Destination BackupDestination `json:"destination"`
	// Options is neo4j-admin passthrough inherited by every emitted Neo4jBackup.
	Options *BackupOptions `json:"options,omitempty"`
}

// Neo4jBackupScheduleSpec owns cron cadences, retention, and the chain (BDR-014 §10).
// +kubebuilder:validation:XValidation:rule="!has(self.aggregate) || !self.aggregate.enabled || (has(self.aggregate.schedule) && self.aggregate.schedule != '')",message="aggregate.schedule is required when aggregate.enabled is true"
type Neo4jBackupScheduleSpec struct {
	// Neo4jRef is the target workload (same namespace).
	// +kubebuilder:validation:Required
	Neo4jRef Neo4jRef `json:"neo4jRef"`
	// Suspend pauses all cadences without deleting the schedule.
	Suspend bool `json:"suspend,omitempty"`
	// Full is the cron that anchors a new backup chain (--type=FULL).
	// +kubebuilder:validation:Required
	Full BackupCadence `json:"full"`
	// Incremental attaches to the current chain (--type=AUTO). Omit for full-only.
	Incremental *BackupCadence `json:"incremental,omitempty"`
	// Aggregate optionally compacts a chain into one recovered full.
	Aggregate *AggregateCadence `json:"aggregate,omitempty"`
	// BackupTemplate is the inline Neo4jBackup spec each cadence emits.
	// +kubebuilder:validation:Required
	BackupTemplate BackupTemplate `json:"backupTemplate"`
}

// Neo4jBackupScheduleStatus is the observed state of the schedule.
type Neo4jBackupScheduleStatus struct {
	// Conditions are Kubernetes-standard signals.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last spec generation reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Suspended mirrors spec.suspend as observed.
	Suspended bool `json:"suspended,omitempty"`
	// LastFullTime / LastIncrementalTime are the last emission times per cadence.
	LastFullTime        *metav1.Time `json:"lastFullTime,omitempty"`
	LastIncrementalTime *metav1.Time `json:"lastIncrementalTime,omitempty"`
	// CurrentChain is the id of the full anchoring the active chain.
	CurrentChain string `json:"currentChain,omitempty"`
	// LastBackup is the name of the most recent emitted Neo4jBackup.
	LastBackup string `json:"lastBackup,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=n4jbs,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.neo4jRef.name`
// +kubebuilder:printcolumn:name="Full",type=string,JSONPath=`.spec.full.schedule`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Last-Full",type=date,JSONPath=`.status.lastFullTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// Neo4jBackupSchedule is the cron and chain owner that emits Neo4jBackup objects (BDR-014).
type Neo4jBackupSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jBackupScheduleSpec   `json:"spec,omitempty"`
	Status Neo4jBackupScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Neo4jBackupScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jBackupSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jBackupSchedule{}, &Neo4jBackupScheduleList{})
}
