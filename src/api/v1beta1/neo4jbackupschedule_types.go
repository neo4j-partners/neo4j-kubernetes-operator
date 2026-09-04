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

// BackupRetention bounds how many whole backup chains full.retention keeps (BDR-014 §10). Set at
// most one of keepLast / keepDays; the schedule prunes whole chains, never mid-chain links.
// +kubebuilder:validation:XValidation:rule="!(has(self.keepLast) && has(self.keepDays))",message="set keepLast or keepDays, not both"
type BackupRetention struct {
	// KeepLast keeps the last N whole chains; older chains are pruned entirely.
	// +kubebuilder:validation:Minimum=1
	KeepLast *int32 `json:"keepLast,omitempty"`
	// KeepDays keeps chains whose anchoring full is younger than N days.
	// +kubebuilder:validation:Minimum=1
	KeepDays *int32 `json:"keepDays,omitempty"`
}

// FullCadence anchors a new backup chain each tick (--type=FULL) and owns whole-chain retention
// (BDR-014 §10).
type FullCadence struct {
	// Schedule is a standard cron expression.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`
	// Retention bounds how many whole chains are kept; older chains are pruned entirely.
	Retention *BackupRetention `json:"retention,omitempty"`
}

// IncrementalCadence attaches differentials to the current chain (--type=AUTO). It has no retention:
// a mid-chain link cannot be deleted without breaking every later link's restore, so within-chain
// bounding is done by full.retention (whole chains) and aggregate compaction, not per link.
type IncrementalCadence struct {
	// Schedule is a standard cron expression.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule"`
}

// AggregateCadence optionally collapses each closed chain into one recovered full (BDR-014 §10),
// bounding restore replay time and chain-loss risk. Aggregation is triggered at the chain boundary
// (when the next full anchors a new chain, the one that just closed is compacted) — not on its own
// cron — so it is always relative to the full cadence.
type AggregateCadence struct {
	// Enabled compacts each closed chain: once every link has Succeeded, the schedule emits an
	// aggregate Neo4jBackup for it and, once that recovered full is cataloged, prunes the chain's
	// original links (keeping the recovered full). Never touches the active chain.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
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
type Neo4jBackupScheduleSpec struct {
	// Neo4jRef is the target workload (same namespace).
	// +kubebuilder:validation:Required
	Neo4jRef Neo4jRef `json:"neo4jRef"`
	// Suspend pauses all cadences without deleting the schedule.
	Suspend bool `json:"suspend,omitempty"`
	// Full is the cron that anchors a new backup chain (--type=FULL).
	// +kubebuilder:validation:Required
	Full FullCadence `json:"full"`
	// Incremental attaches to the current chain (--type=AUTO). Omit for full-only.
	Incremental *IncrementalCadence `json:"incremental,omitempty"`
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
