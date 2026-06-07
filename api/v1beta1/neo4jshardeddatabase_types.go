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

// Neo4jShardedDatabaseSpec defines the desired state of Neo4jShardedDatabase
type Neo4jShardedDatabaseSpec struct {
	// +kubebuilder:validation:Required
	// Reference to the Neo4j cluster that will host this sharded database
	ClusterRef string `json:"clusterRef"`

	// +kubebuilder:validation:Required
	// Name of the sharded database to create
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// Default Cypher language version for sharded database (Neo4j 2025.12+ only)
	// +kubebuilder:validation:Enum="25"
	//
	// Property sharding requires Cypher 25 syntax for multi-database operations.
	// This field ensures compatibility with sharded database queries.
	DefaultCypherLanguage string `json:"defaultCypherLanguage"`

	// Property sharding configuration
	// +kubebuilder:validation:Required
	PropertySharding PropertyShardingConfiguration `json:"propertySharding"`

	// Wait for database creation to complete
	// +kubebuilder:default=true
	Wait bool `json:"wait,omitempty"`

	// IfNotExists controls whether the sharded database creation is
	// idempotent. When unset (nil) or true, the operator emits
	// `CREATE DATABASE ... IF NOT EXISTS` which is a no-op if the database
	// already exists. Set explicitly to false to allow `CREATE DATABASE`
	// without the `IF NOT EXISTS` clause — required when paired with
	// `replaceExisting=true` (the destructive recreate path), and disallowed
	// otherwise without manual handling of "database already exists" errors.
	//
	// Pointer type rather than bool with default=true: a `bool` field with
	// `omitempty` would silently revert to the server-side default whenever
	// a user explicitly set it to false, since `false` serializes as the
	// JSON zero value and is dropped from the wire. Using *bool preserves
	// "explicitly false" through Update round-trips.
	// +optional
	IfNotExists *bool `json:"ifNotExists,omitempty"`

	// ReplaceExisting destroys an existing logical sharded database before
	// recreating it from the seed (typically `spec.seedBackupRef`). Intended
	// for the operationally-load-bearing recovery path where shards have
	// become unrecoverable (e.g. all replicas of a property shard severed),
	// and the only restore mechanism is drop-and-recreate from backup.
	//
	// REQUIRES `force: true` as a separate confirmation field, mirroring
	// the safety pattern used by `Neo4jRestore.spec.force`. The validator
	// rejects `replaceExisting: true` without `force: true`.
	//
	// Mutually exclusive with `ifNotExists: true` — those two settings
	// contradict each other (one says "no-op if it exists", the other says
	// "destroy if it exists"). Set `ifNotExists: false` explicitly when
	// using `replaceExisting`.
	//
	// THIS IS DESTRUCTIVE: the operator runs `DROP DATABASE {name}
	// DESTROY DATA WAIT` before CREATE. All existing data in the named
	// sharded DB is lost. The seedBackupRef contents are the only source
	// of data after the operation.
	// +optional
	ReplaceExisting bool `json:"replaceExisting,omitempty"`

	// Force confirms the destructive ReplaceExisting operation. Required as
	// a separate field so an accidental ReplaceExisting flip can't destroy
	// data — the user must consciously set BOTH fields.
	// +optional
	Force bool `json:"force,omitempty"`

	// Seed URI for creating the sharded database from backups or dumps.
	// When provided as a single URI, Neo4j expects backup artifacts to be named
	// using shard suffixes (e.g., <db>-g000, <db>-p000).
	SeedURI string `json:"seedURI,omitempty"`

	// SeedBackupRef names a Neo4jBackup CR (in the same namespace) whose
	// most-recent Succeeded run will be used as the seed for this sharded
	// database. The operator resolves the reference at reconcile time into a
	// concrete seedURI (computed from the backup's storage type + per-run
	// subdirectory). Mutually exclusive with SeedURI and SeedURIs.
	//
	// Currently restricted to backups stored in cloud locations (S3, GCS,
	// Azure Blob): PVC-stored backups would require mounting the backup PVC
	// on cluster pods, which is out of scope for this field. The validator
	// rejects PVC-backed seedBackupRef at reconcile time with an explanatory
	// status message.
	//
	// If the referenced Neo4jBackup has no Succeeded run yet, the sharded
	// database stays in Pending phase and the reconciler requeues — it does
	// NOT route to Failed (mirrors CLAUDE.md rule 72's restore-side semantics).
	SeedBackupRef string `json:"seedBackupRef,omitempty"`

	// Seed URIs keyed by shard name for dump-based seeding or multi-location backups.
	// Keys must match shard names (e.g., <db>-g000, <db>-p000).
	SeedURIs map[string]string `json:"seedURIs,omitempty"`

	// Optional source database name for seed metadata lookup.
	// Used when seeding from backups with a different database name.
	SeedSourceDatabase string `json:"seedSourceDatabase,omitempty"`

	// Seed configuration for advanced initialization
	SeedConfig *SeedConfiguration `json:"seedConfig,omitempty"`

	// Seed credentials for URI access when system-wide auth is not available
	SeedCredentials *SeedCredentials `json:"seedCredentials,omitempty"`

	// Transaction log enrichment for sharded database creation.
	// Valid values are Neo4j-supported txLogEnrichment options (e.g., "FULL").
	TxLogEnrichment string `json:"txLogEnrichment,omitempty"`
}

// IfNotExistsEffective returns the resolved value of spec.IfNotExists with
// the default applied: nil → true (the kubebuilder default), explicit
// true/false → as set. Use this anywhere the operator needs to decide
// whether to emit `IF NOT EXISTS` in CREATE DATABASE Cypher — callers
// MUST NOT dereference Spec.IfNotExists directly because the pointer is
// nil for unset values.
func (s *Neo4jShardedDatabaseSpec) IfNotExistsEffective() bool {
	if s.IfNotExists == nil {
		return true
	}
	return *s.IfNotExists
}

// PropertyShardingConfiguration defines how property shards are distributed
type PropertyShardingConfiguration struct {
	// Number of property shards to create
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	//
	// Determines how node and relationship properties are distributed.
	// More shards enable better parallelization but increase complexity.
	// Recommended: 4-16 shards for most use cases.
	PropertyShards int32 `json:"propertyShards"`

	// Graph shard topology specification
	//
	// Defines the replication topology for the graph shard database.
	// This database contains nodes, relationships, and labels without properties.
	GraphShard DatabaseTopology `json:"graphShard"`

	// Property shard topology specification
	//
	// Defines the replication topology for each property shard database.
	// All property shards use the same topology configuration.
	PropertyShardTopology PropertyShardTopology `json:"propertyShardTopology"`
}

// PropertyShardTopology defines replica topology for property shards.
type PropertyShardTopology struct {
	// Number of replicas per property shard.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
}

// Neo4jShardedDatabaseStatus defines the observed state of Neo4jShardedDatabase
type Neo4jShardedDatabaseStatus struct {
	// Conditions represent the current state of the sharded database
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the sharded database
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current state
	Message string `json:"message,omitempty"`

	// ObservedGeneration reflects the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ShardingReady indicates whether all shards are created and operational
	ShardingReady *bool `json:"shardingReady,omitempty"`

	// CreationTime shows when the sharded database was created
	CreationTime *metav1.Time `json:"creationTime,omitempty"`

	// Graph shard status
	GraphShard *ShardStatus `json:"graphShard,omitempty"`

	// Property shard statuses
	PropertyShards []ShardStatus `json:"propertyShards,omitempty"`

	// Virtual database status (logical view combining all shards)
	VirtualDatabase *VirtualDatabaseStatus `json:"virtualDatabase,omitempty"`

	// Total size across all shards
	TotalSize string `json:"totalSize,omitempty"`

	// LastBackup records the most recent successful backup that targeted this
	// sharded database. Populated by the backup controller's reverse-lookup
	// when a Neo4jBackup with target.kind=ShardedDatabase and target.name
	// matching this CR's name reaches a Succeeded run. Populated only on
	// Success — Failed/Running runs do not overwrite the field. The reference
	// is informational; operators auditing backup health should also consult
	// the Neo4jBackup CR's status.history for the full chain.
	LastBackup *ShardedDatabaseBackupReference `json:"lastBackup,omitempty"`

	// LastDestructiveRestoreGeneration is the spec.metadata.generation at
	// which the operator last executed a successful destructive restore
	// (replaceExisting+force). Set by the sharded DB controller after the
	// DROP DATABASE … DESTROY DATA WAIT + CREATE DATABASE … OPTIONS { seedURI }
	// cycle finishes. Used to prevent re-triggering the destructive flow on
	// every reconcile: the controller only runs DROP if
	// `LastDestructiveRestoreGeneration < Generation`. To re-trigger after
	// a successful restore (e.g. to re-seed from a newer backup), the user
	// updates spec — which bumps Generation past
	// LastDestructiveRestoreGeneration — and the operator picks up the new
	// request.
	LastDestructiveRestoreGeneration int64 `json:"lastDestructiveRestoreGeneration,omitempty"`
}

// ShardedDatabaseBackupReference is the reverse-lookup pointer populated by
// the backup controller when a Neo4jBackup of kind=ShardedDatabase succeeds.
// All fields together identify a specific run's artifacts: BackupRef + RunID
// give the exact entry in the Neo4jBackup CR's status.history; BackupsPath
// names the on-disk subdirectory under the backup target storage; Timestamp
// is when the backup Job's Pod reported completion.
type ShardedDatabaseBackupReference struct {
	// BackupRef is the Neo4jBackup CR name in the same namespace.
	BackupRef string `json:"backupRef"`

	// RunID matches BackupRun.RunID in the Neo4jBackup CR (the backup Job's
	// metadata.uid). Stable across status refreshes and unique per run.
	RunID string `json:"runID,omitempty"`

	// BackupsPath is the per-run subdirectory inside the backup storage where
	// the per-shard artifacts were written. Same value as BackupRun.BackupsPath.
	BackupsPath string `json:"backupsPath,omitempty"`

	// Timestamp is the time the backup Job's Pod reported completion.
	Timestamp *metav1.Time `json:"timestamp,omitempty"`
}

// ShardStatus tracks the status of individual shards (graph or property)
type ShardStatus struct {
	// Database name for this shard
	Name string `json:"name,omitempty"`

	// Shard type: graph or property
	// +kubebuilder:validation:Enum=graph;property
	Type string `json:"type,omitempty"`

	// Current state of this shard database
	State string `json:"state,omitempty"`

	// Size of this shard database
	Size string `json:"size,omitempty"`

	// Servers hosting this shard
	Servers []string `json:"servers,omitempty"`

	// Ready indicates if this shard is operational
	Ready bool `json:"ready,omitempty"`

	// Last error encountered for this shard
	LastError string `json:"lastError,omitempty"`

	// Property shard specific fields
	PropertyShardIndex *int32 `json:"propertyShardIndex,omitempty"`
	PropertyCount      *int64 `json:"propertyCount,omitempty"`
}

// VirtualDatabaseStatus tracks the logical view combining all shards
type VirtualDatabaseStatus struct {
	// Name of the virtual database (logical view)
	Name string `json:"name,omitempty"`

	// Ready indicates if the virtual database is operational
	Ready bool `json:"ready,omitempty"`

	// Connection endpoint for virtual database queries
	Endpoint string `json:"endpoint,omitempty"`

	// Performance metrics for virtual database
	Metrics *VirtualDatabaseMetrics `json:"metrics,omitempty"`
}

// VirtualDatabaseMetrics provides performance data for virtual database
type VirtualDatabaseMetrics struct {
	// Total number of nodes across all shards
	TotalNodes int64 `json:"totalNodes,omitempty"`

	// Total number of relationships across all shards
	TotalRelationships int64 `json:"totalRelationships,omitempty"`

	// Total number of properties across all property shards
	TotalProperties int64 `json:"totalProperties,omitempty"`

	// Query performance statistics
	QueryMetrics *QueryPerformanceMetrics `json:"queryMetrics,omitempty"`
}

// QueryPerformanceMetrics tracks query performance across shards
type QueryPerformanceMetrics struct {
	// Average query execution time
	AverageQueryTime string `json:"averageQueryTime,omitempty"`

	// Number of cross-shard queries per second
	CrossShardQueriesPerSecond string `json:"crossShardQueriesPerSecond,omitempty"`

	// Cache hit ratio for property lookups (0.0-1.0 as string)
	PropertyCacheHitRatio string `json:"propertyCacheHitRatio,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Shards",type=integer,JSONPath=`.spec.propertySharding.propertyShards`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jShardedDatabase declaratively manages a property-sharded
// Neo4j database (Neo4j 2025.12+ feature). The operator coordinates
// the graph shard and N property shards, each with its own topology
// (primaries / secondaries), sets the database's default Cypher
// language version (5 or 25), and surfaces shard-level health on
// .status. Use a regular Neo4jDatabase resource for non-sharded
// (single-graph) workloads.
type Neo4jShardedDatabase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jShardedDatabaseSpec   `json:"spec,omitempty"`
	Status Neo4jShardedDatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jShardedDatabaseList contains a list of Neo4jShardedDatabase
type Neo4jShardedDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jShardedDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jShardedDatabase{}, &Neo4jShardedDatabaseList{})
}
