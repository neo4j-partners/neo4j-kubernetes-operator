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

// Neo4jRef references a Neo4j workload CR in the same namespace (BDR-001 — day-2
// satellites reference the workload by name; the webhook rejects a missing target).
type Neo4jRef struct {
	// Name of the target Neo4j resource in the same namespace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// RunPhase is the coarse lifecycle of a one-shot backup or restore run (BDR-014).
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type RunPhase string

const (
	RunPhasePending   RunPhase = "Pending"
	RunPhaseRunning   RunPhase = "Running"
	RunPhaseSucceeded RunPhase = "Succeeded"
	RunPhaseFailed    RunPhase = "Failed"
)

// BackupType selects full, incremental, auto, or aggregate (BDR-014 §9). Full/Incremental/Auto map
// to neo4j-admin --type=FULL|DIFF|AUTO. We call it Incremental (not Neo4j's on-disk "differential")
// because the artifacts form a dependent chain. Aggregate is not a live backup: it collapses an
// existing chain (spec.source.backupRef) into a single recovered full via `neo4j-admin backup
// aggregate`, so a restore can seed one artifact instead of replaying the whole chain.
// +kubebuilder:validation:Enum=Full;Incremental;Auto;Aggregate
type BackupType string

const (
	BackupTypeFull        BackupType = "Full"
	BackupTypeIncremental BackupType = "Incremental"
	BackupTypeAuto        BackupType = "Auto"
	BackupTypeAggregate   BackupType = "Aggregate"
)

// BackupSource references an existing backup chain to aggregate (type: Aggregate).
type BackupSource struct {
	// BackupRef is the name of a Succeeded Neo4jBackup whose chain is collapsed into a single
	// recovered full. Any link of the chain (its last incremental, or its full) resolves the whole
	// chain — neo4j-admin walks it from the recorded artifact.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	BackupRef string `json:"backupRef"`
}

// BackupDestinationType selects the artifact store (BDR-014 §4).
// +kubebuilder:validation:Enum=s3;gcs;azure;pvc
type BackupDestinationType string

const (
	BackupDestinationS3    BackupDestinationType = "s3"
	BackupDestinationGCS   BackupDestinationType = "gcs"
	BackupDestinationAzure BackupDestinationType = "azure"
	BackupDestinationPVC   BackupDestinationType = "pvc"
)

// BackupCredentials references a Secret holding cloud credentials. Omit the whole
// block to use workload identity (IRSA / GKE WI / Azure WI) — ADR-015 / ADR-016.
type BackupCredentials struct {
	// SecretName is a Secret in the same namespace with the provider credentials.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName"`
}

// BackupPVC targets an in-cluster volume (dev/local fallback, not a DR path).
// Provide claimName for an existing PVC, or size/storageClassName to have the
// operator provision one — the same Dynamic/Existing model as BDR-005.
// +kubebuilder:validation:XValidation:rule="has(self.claimName) != (has(self.size) || has(self.storageClassName))",message="set pvc.claimName (existing) or pvc.size (provision), not both"
type BackupPVC struct {
	// ClaimName binds an existing PersistentVolumeClaim.
	ClaimName string `json:"claimName,omitempty"`
	// Size provisions a new claim (e.g. 100Gi). Ceiling enforced in admission.
	// +kubebuilder:validation:Pattern=`^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$`
	Size string `json:"size,omitempty"`
	// StorageClassName for a provisioned claim (omit for the cluster default).
	StorageClassName string `json:"storageClassName,omitempty"`
}

// BackupDestination is where artifacts are written (BDR-014 §4). Object stores use
// a provider-neutral url; pvc uses the pvc block.
// +kubebuilder:validation:XValidation:rule="self.type == 'pvc' ? (has(self.pvc) && !has(self.url)) : (has(self.url) && !has(self.pvc))",message="object-store destinations require url (not pvc); pvc destinations require pvc (not url)"
type BackupDestination struct {
	// Type selects the store backend.
	// +kubebuilder:validation:Required
	Type BackupDestinationType `json:"type"`
	// URL is the provider URI for object storage (s3://…, gs://…, azb://…), mapping
	// to neo4j-admin --to-path. Required unless type is pvc.
	URL string `json:"url,omitempty"`
	// PVC targets an in-cluster volume. Required when type is pvc.
	PVC *BackupPVC `json:"pvc,omitempty"`
	// Credentials references cloud credentials; omit for workload identity.
	Credentials *BackupCredentials `json:"credentials,omitempty"`
}

// BackupMetadataScope maps to neo4j-admin --include-metadata (ignored for system).
// +kubebuilder:validation:Enum=none;all;users;roles
type BackupMetadataScope string

// BackupOptions is optional neo4j-admin database backup passthrough (BDR-014 §12).
// Each field maps to a stable flag; omit to inherit Neo4j's default. Operator-owned
// flags (--from, --to-path, --temp-path, --additional-config) are never settable here.
type BackupOptions struct {
	// Compress → --compress (Neo4j default true).
	Compress *bool `json:"compress,omitempty"`
	// KeepFailed → --keep-failed: preserve a failed backup dir for analysis.
	KeepFailed *bool `json:"keepFailed,omitempty"`
	// Verbose → --verbose.
	Verbose *bool `json:"verbose,omitempty"`
	// IncludeMetadata → --include-metadata (default all; ignored for system).
	IncludeMetadata BackupMetadataScope `json:"includeMetadata,omitempty"`
	// ExtraArgs is an allow-listed escape hatch for advanced/version-gated flags.
	// Validated by the webhook against an allowlist and rejected if it collides with
	// an operator-owned flag (BDR-014 §12).
	// +kubebuilder:validation:MaxItems=32
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

// Neo4jBackupSpec is the desired state of a one-shot, immutable backup record.
type Neo4jBackupSpec struct {
	// Neo4jRef is the target workload (same namespace).
	// +kubebuilder:validation:Required
	Neo4jRef Neo4jRef `json:"neo4jRef"`
	// Databases to back up; "*" means all databases including system.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:default={"*"}
	Databases []string `json:"databases,omitempty"`
	// Destination is where artifacts land.
	// +kubebuilder:validation:Required
	Destination BackupDestination `json:"destination"`
	// Type selects Full, Incremental, Auto, or Aggregate (default Auto self-seeds a full when no
	// chain exists). Aggregate collapses spec.source's chain into a recovered full.
	// +kubebuilder:default=Auto
	Type BackupType `json:"type,omitempty"`
	// Source references the chain to aggregate. Required when type is Aggregate and forbidden
	// otherwise. The recovered full is written beside the source chain (in-place) and cataloged as
	// this record's artifact; destination is not used to relocate it.
	Source *BackupSource `json:"source,omitempty"`
	// Options is optional neo4j-admin passthrough (§12).
	Options *BackupOptions `json:"options,omitempty"`
}

// BackupArtifact records one produced backup object.
type BackupArtifact struct {
	// Database this artifact belongs to.
	Database string `json:"database"`
	// Type of this artifact (Full or Incremental; never Auto — Auto is resolved).
	Type BackupType `json:"type,omitempty"`
	// URI is the exact object written (recorded so restore never parses filenames).
	URI string `json:"uri,omitempty"`
	// Path is the real artifact filename neo4j-admin produced for this database, relative to the
	// destination root (e.g. "neo4j-2026-09-01T15-08-49.backup"), recorded by the backup Job.
	// Restore-by-backupRef seeds file:/backups/<path> when the target mounts the destination claim
	// as its backups volume (ADR-015 round-trip); for an incremental this is the chain's last link
	// and Neo4j replays the full chain from the same directory. Empty for wildcard backups or when
	// the Job could not record the name.
	Path string `json:"path,omitempty"`
	// SizeBytes of the artifact.
	SizeBytes int64 `json:"sizeBytes,omitempty"`
	// StartedAt / CompletedAt bracket the run for this database.
	StartedAt   *metav1.Time `json:"startedAt,omitempty"`
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// Neo4jBackupStatus is the observed state of a backup run.
type Neo4jBackupStatus struct {
	// Phase is the coarse run state.
	Phase RunPhase `json:"phase,omitempty"`
	// Conditions are Kubernetes-standard signals.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last spec generation reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Chain is the id of the backup chain this run belongs to (the anchoring full).
	Chain string `json:"chain,omitempty"`
	// Artifacts lists the objects produced, one per database.
	Artifacts []BackupArtifact `json:"artifacts,omitempty"`
	// Reason is a stable machine reason on failure (oracle catalog).
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable detail (e.g. the tail of neo4j-admin output on failure).
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=n4jb,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.neo4jRef.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Chain",type=string,JSONPath=`.status.chain`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="Neo4jBackup spec is immutable (it is a run-to-completion record)"
// +kubebuilder:validation:XValidation:rule="(self.spec.type == 'Aggregate') == has(self.spec.source)",message="type Aggregate requires spec.source.backupRef; spec.source is only valid for Aggregate"
// Neo4jBackup is an immutable, one-shot backup record (BDR-014).
type Neo4jBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jBackupSpec   `json:"spec,omitempty"`
	Status Neo4jBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Neo4jBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jBackup{}, &Neo4jBackupList{})
}
