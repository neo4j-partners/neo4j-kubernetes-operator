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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestDatabaseValidator_ValidateTopology(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1alpha1.AddToScheme(scheme)

	// Create test clusters
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 5, // 5-server cluster
			},
		},
	}

	cluster2Server := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-2server",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 2, // 2-server cluster for CI compatibility test
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, cluster2Server).Build()
	validator := NewDatabaseValidator(client)
	ctx := context.Background()

	tests := []struct {
		name                 string
		database             *neo4jv1alpha1.Neo4jDatabase
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name: "valid topology within cluster capacity",
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   0,
						Secondaries: 2,
					},
				},
			},
			expectedErrors:     1,
			expectedWarnings:   2, // Zero primary error + excessive ratio warning + remaining servers warning
			shouldContainError: "at least 1 primary is required for database operation",
		},
		{
			name: "negative values",
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db-2server", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster-2server",
					Name:       "testdb2server",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "nonexistent-cluster",
					Name:       "testdb",
					Topology: &neo4jv1alpha1.DatabaseTopology{
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
					if containsString(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if containsString(warning, tt.shouldContainWarning) {
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
	_ = neo4jv1alpha1.AddToScheme(scheme)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
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
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
					if containsString(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if containsString(warning, tt.shouldContainWarning) {
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
	_ = neo4jv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
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
		database             *neo4jv1alpha1.Neo4jDatabase
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name: "valid S3 seed URI with credentials",
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
						SecretRef: "s3-credentials",
					},
				},
			},
			expectedErrors:   0,
			expectedWarnings: 1, // Warning about missing optional AWS_SESSION_TOKEN
		},
		{
			name: "valid seed URI without credentials (system-wide auth)",
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "testdb",
					SeedURI:    "s3://my-backups/database-backup.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
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
			database: &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
					if containsString(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if containsString(warning, tt.shouldContainWarning) {
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
	_ = neo4jv1alpha1.AddToScheme(scheme)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	validator := NewDatabaseValidator(client)
	ctx := context.Background()

	tests := []struct {
		name                 string
		seedConfig           *neo4jv1alpha1.SeedConfiguration
		expectedErrors       int
		expectedWarnings     int
		shouldContainError   string
		shouldContainWarning string
	}{
		{
			name: "valid RFC3339 restoreUntil",
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
				RestoreUntil: "2025-01-15T10:30:00Z",
			},
			expectedErrors:       0,
			expectedWarnings:     2, // System auth + point-in-time recovery warnings
			shouldContainWarning: "Point-in-time recovery (restoreUntil) is only available with Neo4j 2025.x",
		},
		{
			name: "valid transaction ID restoreUntil",
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
				RestoreUntil: "txId:12345",
			},
			expectedErrors:   0,
			expectedWarnings: 2, // System auth + point-in-time recovery warnings
		},
		{
			name: "invalid restoreUntil format",
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
				RestoreUntil: "invalid-format",
			},
			expectedErrors:     1,
			expectedWarnings:   2, // System auth warning + point-in-time recovery warning
			shouldContainError: "restoreUntil must be RFC3339 timestamp",
		},
		{
			name: "empty transaction ID",
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
				RestoreUntil: "txId:",
			},
			expectedErrors:     1,
			expectedWarnings:   2, // System auth warning + point-in-time recovery warning
			shouldContainError: "transaction ID cannot be empty when using txId: format",
		},
		{
			name: "valid compression config",
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
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
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
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
			seedConfig: &neo4jv1alpha1.SeedConfiguration{
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
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: "default"},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
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
					if containsString(err.Error(), tt.shouldContainError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' but got: %v", tt.shouldContainError, result.Errors)
			}

			if tt.shouldContainWarning != "" {
				found := false
				for _, warning := range result.Warnings {
					if containsString(warning, tt.shouldContainWarning) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected warning containing '%s' but got: %v", tt.shouldContainWarning, result.Warnings)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i <= len(haystack)-len(needle); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
