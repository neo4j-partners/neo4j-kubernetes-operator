package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
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

// TestValidateReplaceExisting pins the Phase 2c safety gates around the
// destructive drop-and-recreate path. replaceExisting=true must require:
//
//  1. force=true (mirrors Neo4jRestore safety semantics),
//  2. ifNotExists=false (the two contradict each other),
//  3. a seed source (seedURI / seedURIs / seedBackupRef) — recreating
//     without data would leave the database empty.
//
// validatePropertyShardingConfig hosts the check; the test invokes it
// directly to keep the surface narrow.
func TestValidateReplaceExisting(t *testing.T) {
	v := &ShardedDatabaseValidator{}
	baseSpec := func() neo4jv1beta1.Neo4jShardedDatabaseSpec {
		return neo4jv1beta1.Neo4jShardedDatabaseSpec{
			ClusterRef:            "my-cluster",
			Name:                  "products",
			DefaultCypherLanguage: "25",
			PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
				PropertyShards: 2,
				GraphShard:     neo4jv1beta1.DatabaseTopology{Primaries: 1},
				PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{
					Replicas: 1,
				},
			},
		}
	}

	t.Run("replaceExisting without force is rejected", func(t *testing.T) {
		spec := baseSpec()
		spec.ReplaceExisting = true
		spec.Force = false
		spec.SeedBackupRef = "products-backup"
		result := &ShardedDatabaseValidationResult{}
		v.validatePropertyShardingConfig(&neo4jv1beta1.Neo4jShardedDatabase{Spec: spec}, result)
		assert.NotEmpty(t, result.Errors, "expected error for replaceExisting=true without force=true")
	})

	t.Run("replaceExisting with ifNotExists is rejected", func(t *testing.T) {
		spec := baseSpec()
		spec.ReplaceExisting = true
		spec.Force = true
		ifn := true
		spec.IfNotExists = &ifn
		spec.SeedBackupRef = "products-backup"
		result := &ShardedDatabaseValidationResult{}
		v.validatePropertyShardingConfig(&neo4jv1beta1.Neo4jShardedDatabase{Spec: spec}, result)
		errs := 0
		for _, e := range result.Errors {
			if e.Field == "spec.ifNotExists" {
				errs++
			}
		}
		assert.Equal(t, 1, errs, "expected ifNotExists mutex error; got %v", result.Errors)
	})

	t.Run("replaceExisting without seed source is rejected", func(t *testing.T) {
		spec := baseSpec()
		spec.ReplaceExisting = true
		spec.Force = true
		// no seed set
		result := &ShardedDatabaseValidationResult{}
		v.validatePropertyShardingConfig(&neo4jv1beta1.Neo4jShardedDatabase{Spec: spec}, result)
		errs := 0
		for _, e := range result.Errors {
			if e.Field == "spec.replaceExisting" {
				errs++
			}
		}
		assert.GreaterOrEqual(t, errs, 1, "expected error pointing at replaceExisting for missing seed source")
	})

	t.Run("valid: replaceExisting + force + seedBackupRef", func(t *testing.T) {
		spec := baseSpec()
		spec.ReplaceExisting = true
		spec.Force = true
		ifn := false
		spec.IfNotExists = &ifn
		spec.SeedBackupRef = "products-backup"
		result := &ShardedDatabaseValidationResult{}
		v.validatePropertyShardingConfig(&neo4jv1beta1.Neo4jShardedDatabase{Spec: spec}, result)
		assert.Empty(t, result.Errors, "expected no errors for the canonical destructive-restore config: got %v", result.Errors)
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
