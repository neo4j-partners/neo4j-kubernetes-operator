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

// Neo4jDatabaseSpec defines the desired state of Neo4jDatabase
type Neo4jDatabaseSpec struct {
	// +kubebuilder:validation:Required
	// Reference to the Neo4j cluster
	ClusterRef string `json:"clusterRef"`

	// +kubebuilder:validation:Required
	// Database name
	Name string `json:"name"`

	// Database creation options
	Options map[string]string `json:"options,omitempty"`

	// Initial data import configuration
	InitialData *InitialDataSpec `json:"initialData,omitempty"`

	// Wait for database creation to complete
	// +kubebuilder:default=true
	Wait bool `json:"wait,omitempty"`

	// Create database only if it doesn't exist
	// +kubebuilder:default=true
	IfNotExists bool `json:"ifNotExists,omitempty"`

	// Database topology specification for clusters
	//
	// This defines how the database is distributed across the available cluster servers.
	// The cluster provides server infrastructure (e.g., 5 servers), and this topology
	// specifies how many of those servers should act as primaries vs secondaries
	// for this specific database.
	//
	// Multiple databases can have different topologies within the same cluster:
	// - Cluster: 5 servers
	// - Database A: 3 primaries, 1 secondary (uses 4 servers)
	// - Database B: 2 primaries, 0 secondaries (uses 2 servers)
	//
	// Servers self-organize into primary/secondary roles at the database level.
	Topology *DatabaseTopology `json:"topology,omitempty"`

	// Default Cypher language version (Neo4j 2025.x only)
	// +kubebuilder:validation:Enum="5";"25"
	//
	// Specifies the default Cypher language version for this database.
	// - "5": Cypher 5 (backward compatibility)
	// - "25": Cypher 25 (recommended for new databases)
	DefaultCypherLanguage string `json:"defaultCypherLanguage,omitempty"`

	// Seed URI for database creation from existing backup/dump
	//
	// This creates the database from an existing Neo4j backup or dump file
	// located at the specified URI. The seeding occurs during database creation,
	// before any initial data import.
	//
	// Supported URI schemes (via Neo4j CloudSeedProvider):
	// - Amazon S3: s3://bucket/path/backup.backup
	// - Google Cloud Storage: gs://bucket/path/backup.backup
	// - Azure Blob Storage: azb://container/path/backup.backup
	// - HTTP/HTTPS: https://server/path/backup.backup
	// - FTP: ftp://server/path/backup.backup
	//
	// Cloud authentication is configured system-wide via:
	// - S3: IAM roles, AWS credentials, or service account annotations
	// - GCS: Service account keys or workload identity
	// - Azure: Managed identity or service principal
	//
	// Example: "s3://prod-backups/daily-backup-2025-01-15.backup"
	SeedURI string `json:"seedURI,omitempty"`

	// Seed configuration for advanced seeding options
	SeedConfig *SeedConfiguration `json:"seedConfig,omitempty"`

	// Seed credentials for URI access when system-wide auth is not available
	SeedCredentials *SeedCredentials `json:"seedCredentials,omitempty"`
}

// DatabaseTopology defines database distribution in a cluster
//
// IMPORTANT: This is database-level topology, not cluster infrastructure topology.
// The cluster has servers (infrastructure), databases have primaries/secondaries (data distribution).
//
// Example with 5-server cluster:
// - Database "users": 3 primaries, 1 secondary (uses 4 of the 5 servers)
// - Database "logs":  2 primaries, 0 secondaries (uses 2 of the 5 servers)
// - Database "cache": 1 primary, 3 secondaries (uses 4 of the 5 servers)
//
// Constraint: primaries + secondaries <= cluster.spec.topology.servers
type DatabaseTopology struct {
	// Number of primary replicas for this database
	// +kubebuilder:validation:Minimum=1
	//
	// Primaries handle both read and write operations for the database.
	// At least 1 primary is required for database operation.
	Primaries int32 `json:"primaries,omitempty"`

	// Number of secondary replicas for this database
	// +kubebuilder:validation:Minimum=0
	//
	// Secondaries provide read-only access for horizontal scaling.
	// Secondaries are optional but improve read performance and availability.
	Secondaries int32 `json:"secondaries,omitempty"`
}

// InitialDataSpec defines initial data import configuration
type InitialDataSpec struct {
	// Source type for initial data
	// +kubebuilder:validation:Enum=cypher;dump;csv
	Source string `json:"source,omitempty"`

	// Cypher statements for initial data
	CypherStatements []string `json:"cypherStatements,omitempty"`

	// Configuration map reference containing data
	ConfigMapRef string `json:"configMapRef,omitempty"`

	// Secret reference containing sensitive data
	SecretRef string `json:"secretRef,omitempty"`

	// Storage location for data files
	Storage *StorageLocation `json:"storage,omitempty"`
}

// SeedConfiguration defines advanced seeding options for Neo4j database creation
type SeedConfiguration struct {
	// Restore until specific point in time (Neo4j 2025.x only)
	//
	// Supports two formats:
	// - RFC3339 timestamp: "2025-01-15T10:30:00Z"
	// - Transaction ID: "txId:12345"
	//
	// This allows point-in-time recovery when creating database from backup.
	// Only available with Neo4j 2025.x and CloudSeedProvider.
	RestoreUntil string `json:"restoreUntil,omitempty"`

	// Additional seed provider configuration options
	//
	// Common configuration options:
	// - "compression": "gzip" | "lz4" | "none"
	// - "validation": "strict" | "lenient"
	// - "bufferSize": Buffer size for seed operations
	//
	// Cloud-specific options:
	// - S3: "region", "endpoint", "pathStyleAccess"
	// - GCS: "project", "location"
	// - Azure: "endpoint", "containerName"
	Config map[string]string `json:"config,omitempty"`
}

// SeedCredentials defines authentication for accessing seed URIs
//
// Used when system-wide cloud authentication (IAM roles, workload identity)
// is not available or when using HTTP/FTP endpoints requiring authentication.
type SeedCredentials struct {
	// Kubernetes secret containing seed URI credentials
	//
	// Expected secret keys by URI scheme:
	//
	// S3 (s3://):
	// - AWS_ACCESS_KEY_ID: AWS access key
	// - AWS_SECRET_ACCESS_KEY: AWS secret key
	// - AWS_SESSION_TOKEN: (optional) session token
	// - AWS_REGION: (optional) AWS region override
	//
	// Google Cloud Storage (gs://):
	// - GOOGLE_APPLICATION_CREDENTIALS: Service account JSON key
	// - GOOGLE_CLOUD_PROJECT: (optional) project ID override
	//
	// Azure Blob Storage (azb://):
	// - AZURE_STORAGE_ACCOUNT: Storage account name
	// - AZURE_STORAGE_KEY: Storage account key
	// - AZURE_STORAGE_SAS_TOKEN: (alternative) SAS token
	//
	// HTTP/HTTPS/FTP:
	// - USERNAME: (optional) authentication username
	// - PASSWORD: (optional) authentication password
	// - AUTH_HEADER: (optional) custom authorization header
	SecretRef string `json:"secretRef,omitempty"`
}

// Neo4jDatabaseStatus defines the observed state of Neo4jDatabase
type Neo4jDatabaseStatus struct {
	// Conditions represent the current state of the database
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the database
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current state
	Message string `json:"message,omitempty"`

	// ObservedGeneration reflects the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// DataImported indicates whether initial data has been imported
	DataImported *bool `json:"dataImported,omitempty"`

	// CreationTime shows when the database was created
	CreationTime *metav1.Time `json:"creationTime,omitempty"`

	// Size shows the current database size
	Size string `json:"size,omitempty"`

	// LastBackupTime shows when the last backup was taken
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// Current state of the database (online, offline, started, stopped)
	State string `json:"state,omitempty"`

	// Servers hosting this database (for topology tracking)
	//
	// Lists which cluster servers are currently hosting this database.
	// This shows the actual distribution of database replicas across
	// the available cluster server infrastructure.
	Servers []string `json:"servers,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jDatabase is the Schema for the neo4jdatabases API
type Neo4jDatabase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jDatabaseSpec   `json:"spec,omitempty"`
	Status Neo4jDatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jDatabaseList contains a list of Neo4jDatabase
type Neo4jDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jDatabase{}, &Neo4jDatabaseList{})
}
