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

// RestoreSource points at what to restore from (BDR-014 §13): either a Neo4jBackup
// record (backupRef, recommended — the operator resolves the url and walks the chain)
// or a raw artifact url for external/manual artifacts. Exactly one form.
// +kubebuilder:validation:XValidation:rule="has(self.backupRef) != has(self.url)",message="set source.backupRef (a Neo4jBackup) or source.url (raw), not both"
// +kubebuilder:validation:XValidation:rule="!has(self.url) || has(self.type) || self.url.startsWith('file:') || self.url.startsWith('server:')",message="source.type is required with source.url, except credential-free file:/server: seeds (ADR-015)"
// +kubebuilder:validation:XValidation:rule="!has(self.backupRef) || (!has(self.type) && !has(self.pvc) && !has(self.credentials))",message="source.type/pvc/credentials are only valid with source.url, not source.backupRef"
type RestoreSource struct {
	// BackupRef is the name of a Neo4jBackup in the same namespace (recommended).
	BackupRef string `json:"backupRef,omitempty"`
	// Type selects the raw store backend (with url).
	Type BackupDestinationType `json:"type,omitempty"`
	// URL is a raw provider URI to seed from (with type).
	URL string `json:"url,omitempty"`
	// PVC targets an in-cluster volume as a raw source.
	PVC *BackupPVC `json:"pvc,omitempty"`
	// Credentials for a raw source; omit for workload identity.
	Credentials *BackupCredentials `json:"credentials,omitempty"`
}

// Neo4jRestoreSpec is the desired state of a one-shot, immutable restore record.
// Restore covers user databases only; system is rejected (BDR-014 §8).
// +kubebuilder:validation:XValidation:rule="!self.databases.exists(d, d == 'system')",message="system cannot be restored via Neo4jRestore; whole-cluster DR is a manual runbook (BDR-014 §8)"
// +kubebuilder:validation:XValidation:rule="!has(self.forceOffline) || !self.forceOffline || (has(self.overwrite) && self.overwrite)",message="forceOffline requires overwrite: true"
type Neo4jRestoreSpec struct {
	// Neo4jRef is the target cluster (must exist and be formation-stable).
	// +kubebuilder:validation:Required
	Neo4jRef Neo4jRef `json:"neo4jRef"`
	// Databases lists user databases to restore; "*" means all user databases (never system).
	// +kubebuilder:validation:MinItems=1
	Databases []string `json:"databases"`
	// Overwrite allows replacing a database that already exists. Default false refuses
	// (a name clash fails and CREATE OR REPLACE / recreate destroys the current store — BDR-014 §11).
	Overwrite bool `json:"overwrite,omitempty"`
	// ForceOffline stops the database before replacing it to fence in-flight writes,
	// then restarts it. Requires overwrite (§11).
	ForceOffline bool `json:"forceOffline,omitempty"`
	// RestoreMetadata, when true, reapplies the backed-up users, roles, and privileges after the
	// database comes online. Seed-from-URI restores store data only (it never emits Neo4j's
	// restore_metadata.cypher), so the operator runs a post-seed Job that regenerates that script
	// from the artifact and executes it against the system database. Supported only for a
	// PVC-backed source.backupRef the target mounts as its backups volume (the Job needs
	// filesystem access to the artifact); other sources are rejected. Statements that clash with
	// an existing role/user are skipped with a Warning event, and the restore still Succeeds.
	RestoreMetadata bool `json:"restoreMetadata,omitempty"`
	// Source is what to restore from.
	// +kubebuilder:validation:Required
	Source RestoreSource `json:"source"`
}

// RestoreDatabaseStatus is the per-database outcome of a restore.
type RestoreDatabaseStatus struct {
	// Name of the database.
	Name string `json:"name"`
	// Phase of this database's restore.
	Phase RunPhase `json:"phase,omitempty"`
	// Message is a human-readable detail.
	Message string `json:"message,omitempty"`
}

// Neo4jRestoreStatus is the observed state of a restore run.
type Neo4jRestoreStatus struct {
	// Phase is the coarse run state (Failed + reason=DatabaseExists when a target
	// exists and overwrite is false).
	Phase RunPhase `json:"phase,omitempty"`
	// Conditions are Kubernetes-standard signals.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last spec generation reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Reason is a stable machine reason on failure (oracle catalog).
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable detail.
	Message string `json:"message,omitempty"`
	// Databases reports per-database progress.
	Databases []RestoreDatabaseStatus `json:"databases,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=n4jr,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.neo4jRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="Neo4jRestore spec is immutable (it is a run-to-completion record)"
// Neo4jRestore is an immutable, one-shot restore record (BDR-014).
type Neo4jRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jRestoreSpec   `json:"spec,omitempty"`
	Status Neo4jRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Neo4jRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jRestore{}, &Neo4jRestoreList{})
}
