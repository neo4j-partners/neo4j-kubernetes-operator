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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

// ShardedDatabaseValidator validates Neo4jShardedDatabase resources
type ShardedDatabaseValidator struct {
	client client.Client
}

// NewShardedDatabaseValidator creates a new sharded database validator
func NewShardedDatabaseValidator(client client.Client) *ShardedDatabaseValidator {
	return &ShardedDatabaseValidator{
		client: client,
	}
}

// ShardedDatabaseValidationResult holds validation results including warnings
type ShardedDatabaseValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// ValidateShardedDatabase performs comprehensive validation of a Neo4jShardedDatabase
func (v *ShardedDatabaseValidator) ValidateShardedDatabase(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase) error {
	result := &ShardedDatabaseValidationResult{
		Errors:   field.ErrorList{},
		Warnings: []string{},
	}

	// Validate basic fields
	v.validateBasicFields(shardedDB, result)

	// Validate cluster reference
	if err := v.validateClusterReference(ctx, shardedDB, result); err != nil {
		return err
	}

	// Validate property sharding configuration
	v.validatePropertyShardingConfig(shardedDB, result)

	// Validate topology configuration
	v.validateTopologyConfig(shardedDB, result)

	// Validate backup configuration
	v.validateBackupConfig(shardedDB, result)

	// Validate Cypher language version
	v.validateCypherLanguage(shardedDB, result)

	if len(result.Errors) > 0 {
		return fmt.Errorf("validation failed: %v", result.Errors.ToAggregate().Error())
	}

	return nil
}

// validateBasicFields validates required basic fields
func (v *ShardedDatabaseValidator) validateBasicFields(shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) {
	specPath := field.NewPath("spec")

	if shardedDB.Spec.ClusterRef == "" {
		result.Errors = append(result.Errors, field.Required(specPath.Child("clusterRef"), "cluster reference is required"))
	}

	if shardedDB.Spec.Name == "" {
		result.Errors = append(result.Errors, field.Required(specPath.Child("name"), "database name is required"))
	} else {
		// Validate database name format
		if err := v.validateDatabaseName(shardedDB.Spec.Name); err != nil {
			result.Errors = append(result.Errors, field.Invalid(specPath.Child("name"), shardedDB.Spec.Name, err.Error()))
		}
	}
}

// validateClusterReference validates that the referenced cluster exists and supports property sharding
func (v *ShardedDatabaseValidator) validateClusterReference(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) error {
	specPath := field.NewPath("spec")

	// Get the referenced cluster
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	clusterKey := types.NamespacedName{
		Name:      shardedDB.Spec.ClusterRef,
		Namespace: shardedDB.Namespace,
	}

	if err := v.client.Get(ctx, clusterKey, &cluster); err != nil {
		if errors.IsNotFound(err) {
			result.Errors = append(result.Errors, field.NotFound(specPath.Child("clusterRef"), shardedDB.Spec.ClusterRef))
		}
		return fmt.Errorf("failed to get cluster %s: %w", shardedDB.Spec.ClusterRef, err)
	}

	// Validate cluster supports property sharding
	if cluster.Spec.PropertySharding == nil || !cluster.Spec.PropertySharding.Enabled {
		result.Errors = append(result.Errors, field.Invalid(
			specPath.Child("clusterRef"),
			shardedDB.Spec.ClusterRef,
			"referenced cluster does not have property sharding enabled"))
	}

	// Check if cluster is ready
	if cluster.Status.Phase != "Ready" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("referenced cluster %s is not ready (current phase: %s)", cluster.Name, cluster.Status.Phase))
	}

	// Check if property sharding is ready on the cluster
	if cluster.Status.PropertyShardingReady == nil || !*cluster.Status.PropertyShardingReady {
		result.Warnings = append(result.Warnings, fmt.Sprintf("property sharding is not ready on cluster %s", cluster.Name))
	}

	// Validate that the cluster has sufficient capacity for all shards
	if err := v.validateClusterCapacity(&cluster, shardedDB, result); err != nil {
		return err
	}

	return nil
}

// validatePropertyShardingConfig validates property sharding configuration
func (v *ShardedDatabaseValidator) validatePropertyShardingConfig(shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) {
	shardingPath := field.NewPath("spec", "propertySharding")
	config := &shardedDB.Spec.PropertySharding

	// Validate property shards count
	if config.PropertyShards < 1 {
		result.Errors = append(result.Errors, field.Invalid(
			shardingPath.Child("propertyShards"),
			config.PropertyShards,
			"propertyShards must be at least 1"))
	} else if config.PropertyShards > 1000 {
		result.Errors = append(result.Errors, field.Invalid(
			shardingPath.Child("propertyShards"),
			config.PropertyShards,
			"propertyShards cannot exceed 1000"))
	}

	// Performance warnings
	if config.PropertyShards > 16 {
		result.Warnings = append(result.Warnings, "using more than 16 property shards may impact query performance")
	}

	// Validate seed options
	specPath := field.NewPath("spec")
	if shardedDB.Spec.SeedURI != "" && len(shardedDB.Spec.SeedURIs) > 0 {
		result.Errors = append(result.Errors, field.Invalid(
			specPath.Child("seedURI"),
			shardedDB.Spec.SeedURI,
			"seedURI and seedURIs cannot be specified together"))
	}

	if shardedDB.Spec.SeedSourceDatabase != "" && shardedDB.Spec.SeedURI == "" && len(shardedDB.Spec.SeedURIs) == 0 {
		result.Errors = append(result.Errors, field.Invalid(
			specPath.Child("seedSourceDatabase"),
			shardedDB.Spec.SeedSourceDatabase,
			"seedSourceDatabase requires seedURI or seedURIs"))
	}

	if shardedDB.Spec.SeedConfig != nil && shardedDB.Spec.SeedURI == "" && len(shardedDB.Spec.SeedURIs) == 0 {
		result.Errors = append(result.Errors, field.Invalid(
			specPath.Child("seedConfig"),
			"",
			"seedConfig requires seedURI or seedURIs"))
	}

	if shardedDB.Spec.SeedCredentials != nil && shardedDB.Spec.SeedURI == "" && len(shardedDB.Spec.SeedURIs) == 0 {
		result.Errors = append(result.Errors, field.Invalid(
			specPath.Child("seedCredentials"),
			"",
			"seedCredentials requires seedURI or seedURIs"))
	}

	if strings.HasSuffix(shardedDB.Spec.SeedURI, ".dump") {
		result.Warnings = append(result.Warnings,
			"Using dump file format. For better performance with large databases, consider using Neo4j backup format (.backup) instead.")
	}
	for _, uri := range shardedDB.Spec.SeedURIs {
		if strings.HasSuffix(uri, ".dump") {
			result.Warnings = append(result.Warnings,
				"Using dump file format. For better performance with large databases, consider using Neo4j backup format (.backup) instead.")
			break
		}
	}
}

// validateTopologyConfig validates database topology configuration
func (v *ShardedDatabaseValidator) validateTopologyConfig(shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) {
	shardingPath := field.NewPath("spec", "propertySharding")

	// Validate graph shard topology
	graphPath := shardingPath.Child("graphShard")
	if err := v.validateDatabaseTopology(&shardedDB.Spec.PropertySharding.GraphShard, graphPath, result); err != nil {
		// Error already added to result
	}

	// Validate property shard topology
	propertyPath := shardingPath.Child("propertyShardTopology")
	if shardedDB.Spec.PropertySharding.PropertyShardTopology.Replicas < 1 {
		result.Errors = append(result.Errors, field.Invalid(
			propertyPath.Child("replicas"),
			shardedDB.Spec.PropertySharding.PropertyShardTopology.Replicas,
			"replicas must be at least 1"))
	}
}

// validateDatabaseTopology validates a database topology configuration
func (v *ShardedDatabaseValidator) validateDatabaseTopology(topology *neo4jv1alpha1.DatabaseTopology, path *field.Path, result *ShardedDatabaseValidationResult) error {
	if topology.Primaries < 1 {
		result.Errors = append(result.Errors, field.Invalid(
			path.Child("primaries"),
			topology.Primaries,
			"primaries must be at least 1"))
	}

	if topology.Secondaries < 0 {
		result.Errors = append(result.Errors, field.Invalid(
			path.Child("secondaries"),
			topology.Secondaries,
			"secondaries cannot be negative"))
	}

	return nil
}

// validateClusterCapacity validates that the cluster has sufficient capacity for all shards
func (v *ShardedDatabaseValidator) validateClusterCapacity(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) error {
	clusterServers := cluster.Spec.Topology.Servers
	shardingPath := field.NewPath("spec", "propertySharding")

	// Calculate total required servers for graph shard
	graphShard := &shardedDB.Spec.PropertySharding.GraphShard
	graphServers := graphShard.Primaries + graphShard.Secondaries

	if graphServers > clusterServers {
		result.Errors = append(result.Errors, field.Invalid(
			shardingPath.Child("graphShard"),
			fmt.Sprintf("primaries(%d) + secondaries(%d)", graphShard.Primaries, graphShard.Secondaries),
			fmt.Sprintf("graph shard requires %d servers but cluster only has %d", graphServers, clusterServers)))
	}

	// Calculate total required servers for property shards
	propertyReplicas := shardedDB.Spec.PropertySharding.PropertyShardTopology.Replicas

	if propertyReplicas > clusterServers {
		result.Errors = append(result.Errors, field.Invalid(
			shardingPath.Child("propertyShardTopology"),
			fmt.Sprintf("replicas(%d)", propertyReplicas),
			fmt.Sprintf("property shard requires %d servers but cluster only has %d", propertyReplicas, clusterServers)))
	}

	// Warn about resource utilization
	totalShards := int32(1) + shardedDB.Spec.PropertySharding.PropertyShards // 1 graph + N property shards
	if totalShards > clusterServers {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"total shards (%d) exceeds cluster servers (%d) - shards will share servers",
			totalShards, clusterServers))
	}

	return nil
}

// validateBackupConfig validates backup configuration
func (v *ShardedDatabaseValidator) validateBackupConfig(shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) {
	if shardedDB.Spec.BackupConfig == nil {
		return // Backup is optional
	}

	backupPath := field.NewPath("spec", "backupConfig")
	config := shardedDB.Spec.BackupConfig

	// Validate consistency mode
	validModes := []string{"strict", "eventual"}
	if config.ConsistencyMode != "" && !containsStringItem(validModes, config.ConsistencyMode) {
		result.Errors = append(result.Errors, field.NotSupported(
			backupPath.Child("consistencyMode"),
			config.ConsistencyMode,
			validModes))
	}

	// Validate schedule format (basic check)
	if config.Schedule != "" {
		if !strings.Contains(config.Schedule, " ") {
			result.Errors = append(result.Errors, field.Invalid(
				backupPath.Child("schedule"),
				config.Schedule,
				"schedule must be in cron format (e.g., '0 2 * * *')"))
		}
	}

	// Validate retention format
	if config.Retention != "" && !strings.HasSuffix(config.Retention, "d") && !strings.HasSuffix(config.Retention, "h") {
		result.Warnings = append(result.Warnings, "retention should specify time unit (e.g., '7d', '24h')")
	}
}

// validateCypherLanguage validates Cypher language version
func (v *ShardedDatabaseValidator) validateCypherLanguage(shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, result *ShardedDatabaseValidationResult) {
	specPath := field.NewPath("spec")

	if shardedDB.Spec.DefaultCypherLanguage != "25" {
		result.Errors = append(result.Errors, field.Invalid(
			specPath.Child("defaultCypherLanguage"),
			shardedDB.Spec.DefaultCypherLanguage,
			"property sharding requires Cypher 25 (set to '25')"))
	}
}

// validateDatabaseName validates database name format and conventions
func (v *ShardedDatabaseValidator) validateDatabaseName(name string) error {
	if name == "" {
		return fmt.Errorf("database name cannot be empty")
	}

	if len(name) > 63 {
		return fmt.Errorf("database name cannot exceed 63 characters")
	}

	// Check for reserved names
	reserved := []string{"system", "neo4j"}
	for _, r := range reserved {
		if strings.EqualFold(name, r) {
			return fmt.Errorf("database name '%s' is reserved", name)
		}
	}

	// Check for valid characters (alphanumeric, underscore, hyphen)
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-') {
			return fmt.Errorf("database name contains invalid character '%c'", char)
		}
	}

	return nil
}

// containsStringItem checks if slice contains a value
func containsStringItem(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
