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

// Neo4jShardedDatabaseSpec defines the desired state of Neo4jShardedDatabase
type Neo4jShardedDatabaseSpec struct {
	// +kubebuilder:validation:Required
	// Reference to the Neo4j cluster that will host this sharded database
	ClusterRef string `json:"clusterRef"`

	// +kubebuilder:validation:Required
	// Name of the sharded database to create
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// Default Cypher language version for sharded database (Neo4j 2025.10+ only)
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

	// Create database only if it doesn't exist
	// +kubebuilder:default=true
	IfNotExists bool `json:"ifNotExists,omitempty"`

	// Seed URI for creating the sharded database from backups or dumps.
	// When provided as a single URI, Neo4j expects backup artifacts to be named
	// using shard suffixes (e.g., <db>-g000, <db>-p000).
	SeedURI string `json:"seedURI,omitempty"`

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

	// Backup configuration for sharded database
	//
	// Defines how backups are coordinated across graph and property shards.
	// The operator ensures consistent backup snapshots across all shards.
	BackupConfig *ShardedDatabaseBackupConfig `json:"backupConfig,omitempty"`
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

// ShardedDatabaseBackupConfig defines backup coordination across shards
type ShardedDatabaseBackupConfig struct {
	// Enable coordinated backups across all shards
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Backup schedule using cron syntax
	// Applies to all shards simultaneously for consistency
	Schedule string `json:"schedule,omitempty"`

	// Storage location for sharded database backups
	Storage *StorageLocation `json:"storage,omitempty"`

	// Backup retention policy
	// +kubebuilder:default="7d"
	Retention string `json:"retention,omitempty"`

	// Consistency mode for cross-shard backups
	// +kubebuilder:validation:Enum=strict;eventual
	// +kubebuilder:default=strict
	//
	// - strict: All shards backed up simultaneously (consistent point-in-time)
	// - eventual: Shards backed up sequentially (faster, less consistent)
	ConsistencyMode string `json:"consistencyMode,omitempty"`

	// Maximum backup operation timeout
	// +kubebuilder:default="30m"
	Timeout string `json:"timeout,omitempty"`
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

	// Backup status across all shards
	BackupStatus *ShardedBackupStatus `json:"backupStatus,omitempty"`

	// Total size across all shards
	TotalSize string `json:"totalSize,omitempty"`

	// Last successful backup time across all shards
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`
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

// ShardedBackupStatus tracks backup status across all shards
type ShardedBackupStatus struct {
	// Overall backup status
	Status string `json:"status,omitempty"`

	// Last coordinated backup time
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// Individual shard backup statuses
	ShardBackups []ShardBackupStatus `json:"shardBackups,omitempty"`

	// Next scheduled backup time
	NextBackupTime *metav1.Time `json:"nextBackupTime,omitempty"`

	// Backup consistency check status
	ConsistencyCheck string `json:"consistencyCheck,omitempty"`
}

// ShardBackupStatus tracks backup status for individual shards
type ShardBackupStatus struct {
	// Shard name
	ShardName string `json:"shardName,omitempty"`

	// Backup status for this shard
	Status string `json:"status,omitempty"`

	// Last backup time for this shard
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// Backup size for this shard
	BackupSize string `json:"backupSize,omitempty"`

	// Last error during backup
	LastError string `json:"lastError,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Shards",type=integer,JSONPath=`.spec.propertySharding.propertyShards`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jShardedDatabase is the Schema for the neo4jshardeddatabases API
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
