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
