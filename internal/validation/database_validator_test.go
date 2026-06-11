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

package validation

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestDatabaseValidator_ValidateTopology(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)

	// Create test clusters
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 5, // 5-server cluster
			},
		},
	}

	cluster2Server := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-2server",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 2, // 2-server cluster for CI compatibility test
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, cluster2Server).Build()
	validator := NewDatabaseValidator(client)
	ctx := context.Background()

	tests := []struct {
		name                 string
		database             *neo4jv1beta1.Neo4jDatabase
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name: "valid topology within cluster capacity",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   2,
						Secondaries: 1,
					},
				},
			},
			expectedErrors:   0,
			expectedWarnings: 1, // Warning about utilizing remaining servers
		},
		{
			name: "topology exceeds cluster capacity",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   3,
						Secondaries: 4, // Total: 7 servers, but cluster only has 5
					},
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "database topology requires 7 servers but cluster only has 5 servers available",
		},
		{
			name: "zero primaries",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   0,
						Secondaries: 2,
					},
				},
			},
			expectedErrors:     1,
			expectedWarnings:   2, // excessive ratio warning + remaining servers warning
			shouldContainError: "at least 1 primary is required for database operation",
		},
		{
			name: "negative values",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   -1,
						Secondaries: -2,
					},
				},
			},
			expectedErrors:   2, // negative primaries + negative secondaries (validator stops at negative check)
			expectedWarnings: 0, // no warnings for negative values
		},
		{
			name: "uses all cluster servers",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   3,
						Secondaries: 2, // Total: 5 servers (all available)
					},
				},
			},
			expectedErrors:       0,
			expectedWarnings:     1,
			shouldContainWarning: "Database topology uses all 5 cluster servers",
		},
		{
			name: "uses all servers in 2-server cluster",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db-2server", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster-2server",
					Name:       "testdb2server",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1, // Total: 2 servers (all available in 2-server cluster)
					},
				},
			},
			expectedErrors:       0,
			expectedWarnings:     1,
			shouldContainWarning: "Database topology uses all 2 cluster servers",
		},
		{
			name: "excessive secondaries ratio",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 4, // 4:1 ratio is excessive
					},
				},
			},
			expectedErrors:       0,
			expectedWarnings:     3, // Uses all servers + excessive ratio + single primary bottleneck
			shouldContainWarning: "More than 2:1 secondary-to-primary ratio",
		},
		{
			name: "cluster not found",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "nonexistent-cluster",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   2,
						Secondaries: 1,
					},
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "Referenced cluster nonexistent-cluster not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(ctx, tt.database)

			assert.Equal(t, tt.expectedErrors, len(result.Errors),
				"Expected %d errors, got %d: %v", tt.expectedErrors, len(result.Errors), result.Errors)
			assert.Equal(t, tt.expectedWarnings, len(result.Warnings),
				"Expected %d warnings, got %d: %v", tt.expectedWarnings, len(result.Warnings), result.Warnings)

			if tt.shouldContainError != "" {
				found := false
				for _, err := range result.Errors {
					if strings.Contains(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, tt.shouldContainWarning) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected warning containing '%s' but got: %v", tt.shouldContainWarning, result.Warnings)
			}
		})
	}
}

func TestDatabaseValidator_ValidateCypherLanguage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	validator := NewDatabaseValidator(client)
	ctx := context.Background()

	tests := []struct {
		name                 string
		cypherVersion        string
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name:                 "valid cypher version 5",
			cypherVersion:        "5",
			expectedErrors:       0,
			expectedWarnings:     1,
			shouldContainWarning: "Consider migrating to version '25'",
		},
		{
			name:             "valid cypher version 25",
			cypherVersion:    "25",
			expectedErrors:   0,
			expectedWarnings: 0,
		},
		{
			name:               "invalid cypher version",
			cypherVersion:      "4",
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "supported values: \"5\", \"25\"",
		},
		{
			name:             "empty cypher version",
			cypherVersion:    "",
			expectedErrors:   0,
			expectedWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef:            "test-cluster",
					Name:                  "testdb",
					DefaultCypherLanguage: tt.cypherVersion,
				},
			}

			result := validator.Validate(ctx, database)

			assert.Equal(t, tt.expectedErrors, len(result.Errors),
				"Expected %d errors, got %d: %v", tt.expectedErrors, len(result.Errors), result.Errors)
			assert.Equal(t, tt.expectedWarnings, len(result.Warnings),
				"Expected %d warnings, got %d: %v", tt.expectedWarnings, len(result.Warnings), result.Warnings)

			if tt.shouldContainError != "" {
				found := false
				for _, err := range result.Errors {
					if strings.Contains(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, tt.shouldContainWarning) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected warning containing '%s' but got: %v", tt.shouldContainWarning, result.Warnings)
			}
		})
	}
}

func TestDatabaseValidator_ValidateSeedURI(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
		},
	}

	// Create a test secret with S3 credentials
	s3Secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-credentials", Namespace: "default"},
		Data: map[string][]byte{
			"AWS_ACCESS_KEY_ID":     []byte("test-access-key"),
			"AWS_SECRET_ACCESS_KEY": []byte("test-secret-key"),
			"AWS_REGION":            []byte("us-west-2"),
		},
	}

	// Create a secret missing required keys
	incompleteSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "incomplete-credentials", Namespace: "default"},
		Data: map[string][]byte{
			"AWS_ACCESS_KEY_ID": []byte("test-access-key"),
			// Missing AWS_SECRET_ACCESS_KEY
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, s3Secret, incompleteSecret).Build()
	validator := NewDatabaseValidator(client)
	ctx := context.Background()

	tests := []struct {
		name                 string
		database             *neo4jv1beta1.Neo4jDatabase
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name: "valid S3 seed URI with credentials",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedCredentials: &neo4jv1beta1.SeedCredentials{
						SecretRef: "s3-credentials",
					},
				},
			},
			expectedErrors:   0,
			expectedWarnings: 1, // Warning about missing optional AWS_SESSION_TOKEN
		},
		{
			name: "valid seed URI without credentials (system-wide auth)",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
				},
			},
			expectedErrors:       0,
			expectedWarnings:     1,
			shouldContainWarning: "Using system-wide cloud authentication",
		},
		{
			name: "invalid URI format",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "not-a-valid-uri",
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "supported values:",
		},
		{
			name: "unsupported URI scheme",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "file:///local/path/backup.backup",
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "supported values:",
		},
		{
			name: "URI missing host",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3:///path/backup.backup",
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "URI must specify a host",
		},
		{
			name: "URI missing path",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-bucket/",
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "URI must specify a path to the backup file",
		},
		{
			name: "missing credentials secret",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedCredentials: &neo4jv1beta1.SeedCredentials{
						SecretRef: "nonexistent-secret",
					},
				},
			},
			expectedErrors:     1,
			expectedWarnings:   0,
			shouldContainError: "Secret nonexistent-secret not found",
		},
		{
			name: "incomplete credentials secret",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedCredentials: &neo4jv1beta1.SeedCredentials{
						SecretRef: "incomplete-credentials",
					},
				},
			},
			expectedErrors:     1,
			expectedWarnings:   1, // Warning about missing optional keys
			shouldContainError: "secret must contain required key 'AWS_SECRET_ACCESS_KEY'",
		},
		{
			name: "dump file format warning",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.dump",
				},
			},
			expectedErrors:       0,
			expectedWarnings:     2, // System auth warning + dump format warning
			shouldContainWarning: "Using dump file format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(ctx, tt.database)

			assert.Equal(t, tt.expectedErrors, len(result.Errors),
				"Expected %d errors, got %d: %v", tt.expectedErrors, len(result.Errors), result.Errors)
			assert.Equal(t, tt.expectedWarnings, len(result.Warnings),
				"Expected %d warnings, got %d: %v", tt.expectedWarnings, len(result.Warnings), result.Warnings)

			if tt.shouldContainError != "" {
				found := false
				for _, err := range result.Errors {
					if strings.Contains(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, tt.shouldContainWarning) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected warning containing '%s' but got: %v", tt.shouldContainWarning, result.Warnings)
			}
		})
	}
}

func TestDatabaseValidator_ValidateSeedConfiguration(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	validator := NewDatabaseValidator(client)
	ctx := context.Background()

	tests := []struct {
		name                 string
		seedConfig           *neo4jv1beta1.SeedConfiguration
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name: "valid RFC3339 restoreUntil",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				RestoreUntil: "2025-01-15T10:30:00Z",
			},
			expectedErrors:       0,
			expectedWarnings:     2, // System auth + point-in-time recovery warnings
			shouldContainWarning: "Point-in-time recovery (restoreUntil) is only available with Neo4j 2025.x",
		},
		{
			name: "valid transaction ID restoreUntil",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				RestoreUntil: "txId:12345",
			},
			expectedErrors:   0,
			expectedWarnings: 2, // System auth + point-in-time recovery warnings
		},
		{
			name: "invalid restoreUntil format",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				RestoreUntil: "invalid-format",
			},
			expectedErrors:     1,
			expectedWarnings:   2, // System auth warning + point-in-time recovery warning
			shouldContainError: "restoreUntil must be an RFC3339 timestamp",
		},
		{
			name: "empty transaction ID",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				RestoreUntil: "txId:",
			},
			expectedErrors:     1,
			expectedWarnings:   2, // System auth warning + point-in-time recovery warning
			shouldContainError: "txId: format requires a positive integer",
		},
		{
			name: "valid compression config",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				Config: map[string]string{
					"compression": "gzip",
					"validation":  "strict",
				},
			},
			expectedErrors:   0,
			expectedWarnings: 1, // System auth warning
		},
		{
			name: "invalid compression value",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				Config: map[string]string{
					"compression": "invalid-compression",
				},
			},
			expectedErrors:     1,
			expectedWarnings:   1,
			shouldContainError: "supported values:",
		},
		{
			name: "unknown config option",
			seedConfig: &neo4jv1beta1.SeedConfiguration{
				Config: map[string]string{
					"unknownOption": "someValue",
				},
			},
			expectedErrors:       0,
			expectedWarnings:     2, // System auth + unknown option warnings
			shouldContainWarning: "Unknown seed configuration option 'unknownOption'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedConfig: tt.seedConfig,
				},
			}

			result := validator.Validate(ctx, database)

			assert.Equal(t, tt.expectedErrors, len(result.Errors),
				"Expected %d errors, got %d: %v", tt.expectedErrors, len(result.Errors), result.Errors)
			assert.Equal(t, tt.expectedWarnings, len(result.Warnings),
				"Expected %d warnings, got %d: %v", tt.expectedWarnings, len(result.Warnings), result.Warnings)

			if tt.shouldContainError != "" {
				found := false
				for _, err := range result.Errors {
					if strings.Contains(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, tt.shouldContainWarning) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected warning containing '%s' but got: %v", tt.shouldContainWarning, result.Warnings)
			}
		})
	}
}

func TestDatabaseValidator_ValidateStandalone(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create test standalone
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-standalone",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Auth: &neo4jv1beta1.AuthSpec{
				AdminSecret: "admin-secret",
			},
		},
	}

	// Create admin secret
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "admin-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"username": []byte("neo4j"),
			"password": []byte("test123"),
		},
	}

	tests := []struct {
		name            string
		database        *neo4jv1beta1.Neo4jDatabase
		expectErrors    int
		expectWarnings  int
		errorMessages   []string
		warningMessages []string
	}{
		{
			name: "valid standalone database without topology",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-standalone",
					Name:       "testdb",
					Wait:       true,
				},
			},
			expectErrors:   0,
			expectWarnings: 0,
		},
		{
			name: "standalone database with topology (should warn)",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-with-topology",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-standalone",
					Name:       "testdb",
					Wait:       true,
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 2,
					},
				},
			},
			expectErrors:   0,
			expectWarnings: 2, // topology not required + secondaries warning
			warningMessages: []string{
				"Database topology specification is not required for standalone deployments",
				"Database topology specifies 2 secondaries, but standalone deployments cannot provide read replicas",
			},
		},
		{
			name: "standalone database with invalid topology",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-invalid",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-standalone",
					Name:       "testdb",
					Wait:       true,
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   0, // Invalid - need at least 1 primary
						Secondaries: 1,
					},
				},
			},
			expectErrors:   1,
			expectWarnings: 2,
			errorMessages:  []string{"at least 1 primary is required"},
			warningMessages: []string{
				"Database topology specification is not required for standalone deployments",
				"Database topology specifies 1 secondaries, but standalone deployments cannot provide read replicas",
			},
		},
		{
			name: "standalone not found",
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-notfound",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "nonexistent-standalone",
					Name:       "testdb",
					Wait:       true,
				},
			},
			expectErrors:   1,
			expectWarnings: 0,
			errorMessages:  []string{"Referenced cluster nonexistent-standalone not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with standalone and secret
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(standalone, adminSecret).
				Build()

			validator := NewDatabaseValidator(fakeClient)

			result := validator.Validate(context.Background(), tt.database)

			assert.Equal(t, tt.expectErrors, len(result.Errors),
				"Expected %d errors, got %d. Errors: %v", tt.expectErrors, len(result.Errors), result.Errors)
			assert.Equal(t, tt.expectWarnings, len(result.Warnings),
				"Expected %d warnings, got %d. Warnings: %v", tt.expectWarnings, len(result.Warnings), result.Warnings)

			// Check specific error messages
			for _, expectedErr := range tt.errorMessages {
				found := false
				for _, actualErr := range result.Errors {
					if strings.Contains(actualErr.Error(), expectedErr) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error message containing '%s' not found in: %v", expectedErr, result.Errors)
			}

			// Check specific warning messages
			for _, expectedWarn := range tt.warningMessages {
				found := false
				for _, actualWarn := range result.Warnings {
					if strings.Contains(actualWarn, expectedWarn) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected warning message containing '%s' not found in: %v", expectedWarn, result.Warnings)
			}
		})
	}
}

func TestDatabaseValidator_ValidateClusterAndStandaloneFallback(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create both cluster and standalone with same name
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "neo4j-resource",
			Namespace: "default",
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}

	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "neo4j-resource", // Same name as cluster
			Namespace: "default",
		},
	}

	tests := []struct {
		name         string
		objects      []client.Object
		database     *neo4jv1beta1.Neo4jDatabase
		expectErrors int
		description  string
	}{
		{
			name:    "cluster found first (takes precedence)",
			objects: []client.Object{cluster},
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "neo4j-resource",
					Name:       "testdb",
					Topology: &neo4jv1beta1.DatabaseTopology{
						Primaries:   2,
						Secondaries: 1,
					},
				},
			},
			expectErrors: 0,
			description:  "Should validate as cluster (no warnings about topology not required)",
		},
		{
			name:    "only standalone exists (fallback works)",
			objects: []client.Object{standalone},
			database: &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "neo4j-resource",
					Name:       "testdb",
				},
			},
			expectErrors: 0,
			description:  "Should validate as standalone successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			validator := NewDatabaseValidator(fakeClient)
			result := validator.Validate(context.Background(), tt.database)

			assert.Equal(t, tt.expectErrors, len(result.Errors),
				"Test: %s. Expected %d errors, got %d. Errors: %v", tt.description, tt.expectErrors, len(result.Errors), result.Errors)
		})
	}
}

func TestValidateDatabaseName(t *testing.T) {
	tests := []struct {
		name         string
		dbName       string
		wantErrors   int
		wantWarnings int
		wantMsg      string
	}{
		{
			name:       "valid name",
			dbName:     "mydb",
			wantErrors: 0,
		},
		{
			name:       "valid name with dashes",
			dbName:     "my-database",
			wantErrors: 0,
		},
		{
			name:       "valid name with dots",
			dbName:     "my.database.v2",
			wantErrors: 0,
		},
		{
			name:       "valid name with dots and dashes",
			dbName:     "my-db.v2",
			wantErrors: 0,
		},
		{
			name:       "invalid name with underscore",
			dbName:     "my_database",
			wantErrors: 1,
			wantMsg:    "must start with a letter",
		},
		{
			name:       "invalid name starts with underscore",
			dbName:     "_internal",
			wantErrors: 1,
			wantMsg:    "must start with a letter",
		},
		{
			name:       "empty name",
			dbName:     "",
			wantErrors: 1,
			wantMsg:    "Required",
		},
		{
			name:       "starts with number",
			dbName:     "1badname",
			wantErrors: 1,
			wantMsg:    "must start with a letter",
		},
		{
			name:       "starts with dash",
			dbName:     "-badname",
			wantErrors: 1,
			wantMsg:    "must start with a letter",
		},
		{
			name:       "reserved name system",
			dbName:     "system",
			wantErrors: 1,
			wantMsg:    "reserved",
		},
		{
			name:         "default name neo4j warns",
			dbName:       "neo4j",
			wantErrors:   0,
			wantWarnings: 1,
		},
		{
			name:       "too long name",
			dbName:     "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmn",
			wantErrors: 1,
			wantMsg:    "no more than 65",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, warnings := validateDatabaseName(tt.dbName, field.NewPath("spec", "name"))
			assert.Equal(t, tt.wantErrors, len(errs), "errors: %v", errs)
			if tt.wantWarnings > 0 {
				assert.Equal(t, tt.wantWarnings, len(warnings), "warnings: %v", warnings)
			}
			if tt.wantMsg != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if strings.Contains(e.Error(), tt.wantMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error containing '%s', got: %v", tt.wantMsg, errs)
				}
			}
		})
	}
}

// TestIsValidDatabaseName covers the exported helper the Neo4jRestore validator
// uses to reject shell/Cypher-injecting database names.
func TestIsValidDatabaseName(t *testing.T) {
	valid := []string{"neo4j", "mydb", "my.db", "my-db", "Db123"}
	for _, n := range valid {
		if !IsValidDatabaseName(n) {
			t.Errorf("expected %q to be valid", n)
		}
	}
	invalid := []string{
		"",                 // empty
		"1db",              // leading digit
		"db; rm -rf /data", // shell injection
		"db' OR '1'='1",    // quote
		"db`whoami`",       // backtick
		"db\nmore",         // newline
		"db$(curl evil)",   // command substitution
		"db with space",
	}
	for _, n := range invalid {
		if IsValidDatabaseName(n) {
			t.Errorf("expected %q to be rejected", n)
		}
	}
	// Over-length is rejected.
	long := "a"
	for i := 0; i < MaxDatabaseNameLength; i++ {
		long += "a"
	}
	if IsValidDatabaseName(long) {
		t.Errorf("expected over-length name to be rejected")
	}
}

// TestSeedConfigInjectionGuards covers the helpers that protect the legacy
// string-interpolated SEED CONFIG clause (issue #169) from Cypher injection.
func TestSeedConfigInjectionGuards(t *testing.T) {
	t.Run("cypherLiteralUnsafe rejects break-out chars", func(t *testing.T) {
		for _, s := range []string{"x'", "a`b", "line\nbreak", "carriage\rreturn", "use' } DROP DATABASE neo4j //"} {
			if !cypherLiteralUnsafe(s) {
				t.Errorf("expected %q to be flagged unsafe", s)
			}
		}
		for _, s := range []string{"use", "region=eu-west-1", "block", ""} {
			if cypherLiteralUnsafe(s) {
				t.Errorf("expected %q to be safe", s)
			}
		}
	})

	t.Run("restoreUntil txId requires positive integer", func(t *testing.T) {
		if !restoreUntilTxIDPattern.MatchString("12345") {
			t.Error("expected 12345 to match")
		}
		for _, bad := range []string{"", "abc", "12345} DROP", "1.5", "-1"} {
			if restoreUntilTxIDPattern.MatchString(bad) {
				t.Errorf("expected %q to be rejected", bad)
			}
		}
	})

	t.Run("isRFC3339Timestamp", func(t *testing.T) {
		if !isRFC3339Timestamp("2025-01-15T10:30:00Z") {
			t.Error("expected valid RFC3339 to pass")
		}
		for _, bad := range []string{"2025-01-15", "not-a-time", "2025'T:}"} {
			if isRFC3339Timestamp(bad) {
				t.Errorf("expected %q to be rejected", bad)
			}
		}
	})
}
