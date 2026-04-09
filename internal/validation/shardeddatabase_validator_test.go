package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestValidateShardedDatabaseName(t *testing.T) {
	v := &ShardedDatabaseValidator{}

	tests := []struct {
		name      string
		dbName    string
		expectErr bool
	}{
		{"valid simple name", "mydb", false},
		{"valid with hyphen and underscore", "my-db_123", false},
		{"empty name", "", true},
		{"reserved system", "system", true},
		{"reserved System case", "System", true},
		{"reserved neo4j", "neo4j", true},
		{"reserved NEO4J case", "NEO4J", true},
		{"invalid char @", "my@db", true},
		{"invalid char space", "my db", true},
		{"max length 63", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"too long 64", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.validateDatabaseName(tt.dbName)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCypherLanguage(t *testing.T) {
	v := &ShardedDatabaseValidator{}

	t.Run("valid 25", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{}
		db.Spec.DefaultCypherLanguage = "25"
		v.validateCypherLanguage(db, result)
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid 5", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{}
		db.Spec.DefaultCypherLanguage = "5"
		v.validateCypherLanguage(db, result)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("empty", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{}
		db.Spec.DefaultCypherLanguage = ""
		v.validateCypherLanguage(db, result)
		assert.NotEmpty(t, result.Errors)
	})
}

func TestValidateBackupConfig(t *testing.T) {
	v := &ShardedDatabaseValidator{}

	t.Run("nil config is valid", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{}
		v.validateBackupConfig(db, result)
		assert.Empty(t, result.Errors)
		assert.Empty(t, result.Warnings)
	})

	t.Run("valid consistency mode", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				BackupConfig: &neo4jv1beta1.ShardedDatabaseBackupConfig{
					ConsistencyMode: "strict",
				},
			},
		}
		v.validateBackupConfig(db, result)
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid consistency mode", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				BackupConfig: &neo4jv1beta1.ShardedDatabaseBackupConfig{
					ConsistencyMode: "invalid",
				},
			},
		}
		v.validateBackupConfig(db, result)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("valid cron schedule", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				BackupConfig: &neo4jv1beta1.ShardedDatabaseBackupConfig{
					Schedule: "0 2 * * *",
				},
			},
		}
		v.validateBackupConfig(db, result)
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid schedule no space", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				BackupConfig: &neo4jv1beta1.ShardedDatabaseBackupConfig{
					Schedule: "invalid",
				},
			},
		}
		v.validateBackupConfig(db, result)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("retention warning without unit", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				BackupConfig: &neo4jv1beta1.ShardedDatabaseBackupConfig{
					Retention: "7",
				},
			},
		}
		v.validateBackupConfig(db, result)
		assert.NotEmpty(t, result.Warnings)
	})

	t.Run("retention valid with unit", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				BackupConfig: &neo4jv1beta1.ShardedDatabaseBackupConfig{
					Retention: "7d",
				},
			},
		}
		v.validateBackupConfig(db, result)
		assert.Empty(t, result.Warnings)
	})
}

func TestValidateBasicFields(t *testing.T) {
	v := &ShardedDatabaseValidator{}

	t.Run("missing clusterRef", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				Name: "mydb",
			},
		}
		v.validateBasicFields(db, result)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("missing name", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef: "my-cluster",
			},
		}
		v.validateBasicFields(db, result)
		assert.NotEmpty(t, result.Errors)
	})

	t.Run("valid basic fields", func(t *testing.T) {
		result := &ShardedDatabaseValidationResult{}
		db := &neo4jv1beta1.Neo4jShardedDatabase{
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef: "my-cluster",
				Name:       "valid-db",
			},
		}
		v.validateBasicFields(db, result)
		assert.Empty(t, result.Errors)
	})
}

func TestContainsStringItem(t *testing.T) {
	assert.True(t, containsStringItem([]string{"a", "b", "c"}, "b"))
	assert.False(t, containsStringItem([]string{"a", "b", "c"}, "d"))
	assert.False(t, containsStringItem([]string{}, "a"))
	assert.False(t, containsStringItem(nil, "a"))
}
