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

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// ClusterValidationResult holds validation results including warnings
type ClusterValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// ClusterValidator provides validation for Neo4jEnterpriseCluster resources
type ClusterValidator struct {
	client            client.Client
	editionValidator  *EditionValidator
	topologyValidator *TopologyValidator
	imageValidator    *ImageValidator
	storageValidator  *StorageValidator
	tlsValidator      *TLSValidator
	authValidator     *AuthValidator
	upgradeValidator  *UpgradeValidator
	memoryValidator   *MemoryValidator
	resourceValidator *ResourceValidator
	configValidator   *ConfigValidator
}

// NewClusterValidator creates a new cluster validator
func NewClusterValidator(client client.Client) *ClusterValidator {
	return &ClusterValidator{
		client:            client,
		editionValidator:  NewEditionValidator(),
		topologyValidator: NewTopologyValidator(),
		imageValidator:    NewImageValidator(),
		storageValidator:  NewStorageValidator(),
		tlsValidator:      NewTLSValidator(),
		authValidator:     NewAuthValidator(),
		upgradeValidator:  NewUpgradeValidator(),
		memoryValidator:   NewMemoryValidator(),
		resourceValidator: NewResourceValidator(client),
		configValidator:   NewConfigValidator(),
	}
}

// ValidateCreate validates a Neo4jEnterpriseCluster for creation
func (v *ClusterValidator) ValidateCreate(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	allErrs := v.validateCluster(ctx, cluster)
	if len(allErrs) > 0 {
		return fmt.Errorf("validation failed: %s", allErrs.ToAggregate().Error())
	}
	return nil
}

// ValidateUpdate validates a Neo4jEnterpriseCluster for update
func (v *ClusterValidator) ValidateUpdate(ctx context.Context, oldCluster, newCluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	allErrs := v.validateCluster(ctx, newCluster)
	allErrs = append(allErrs, v.validateClusterUpdate(ctx, oldCluster, newCluster)...)

	if len(allErrs) > 0 {
		return fmt.Errorf("validation failed: %s", allErrs.ToAggregate().Error())
	}
	return nil
}

// ApplyDefaults applies default values to the cluster
func (v *ClusterValidator) ApplyDefaults(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) {
	// Edition field removed - operator only supports enterprise edition

	// Default image pull policy
	if cluster.Spec.Image.PullPolicy == "" {
		cluster.Spec.Image.PullPolicy = "IfNotPresent"
	}

	// Default TLS configuration - disable TLS by default for simplicity
	if cluster.Spec.TLS == nil {
		cluster.Spec.TLS = &neo4jv1beta1.TLSSpec{
			Mode: "disabled",
		}
	} else if cluster.Spec.TLS.Mode == "" {
		cluster.Spec.TLS.Mode = "disabled"
	}

	// Default TLS issuer reference
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" && cluster.Spec.TLS.IssuerRef == nil {
		cluster.Spec.TLS.IssuerRef = &neo4jv1beta1.IssuerRef{
			Kind: "ClusterIssuer",
		}
	}

	// Default auth configuration
	if cluster.Spec.Auth == nil {
		cluster.Spec.Auth = &neo4jv1beta1.AuthSpec{
			AuthenticationProviders: []string{"native"},
			AuthorizationProviders:  []string{"native"},
		}
	} else if len(cluster.Spec.Auth.AuthenticationProviders) == 0 {
		cluster.Spec.Auth.AuthenticationProviders = []string{"native"}
		cluster.Spec.Auth.AuthorizationProviders = []string{"native"}
	}

	// Default service configuration
	if cluster.Spec.Service == nil {
		cluster.Spec.Service = &neo4jv1beta1.ServiceSpec{
			Type: "ClusterIP",
		}
	}

	// Default storage retention policy to Delete
	if cluster.Spec.Storage.RetentionPolicy == "" {
		cluster.Spec.Storage.RetentionPolicy = "Delete"
	}

	// Note: We no longer auto-adjust topology values
	// Topology warnings will be generated during validation instead
}

// validateCluster performs comprehensive validation of the cluster
// maxClusterNameLength is the max name length for clusters.
// Generated resources append "-server" (7 chars), and DNS labels max at 63.
const maxClusterNameLength = 56

func (v *ClusterValidator) validateCluster(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Preallocate slice with estimated capacity to reduce allocations
	allErrs = make(field.ErrorList, 0, 10)

	// Validate resource name length (generated services add suffixes like "-server")
	if len(cluster.Name) > maxClusterNameLength {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata", "name"),
			cluster.Name,
			fmt.Sprintf("must be no more than %d characters (generated resources append suffixes up to 7 chars, and DNS labels max at 63)", maxClusterNameLength),
		))
	}

	// Edition validation removed - operator only supports enterprise edition

	// Validate topology (second most critical)
	allErrs = append(allErrs, v.topologyValidator.Validate(cluster)...)

	// Validate image (fail fast if image validation fails)
	if imageErrs := v.imageValidator.Validate(cluster); len(imageErrs) > 0 {
		allErrs = append(allErrs, imageErrs...)
		// If image is invalid, other validations are less meaningful
		return allErrs
	}

	// Continue with remaining validations only if critical ones pass
	allErrs = append(allErrs, v.storageValidator.Validate(cluster)...)
	allErrs = append(allErrs, v.tlsValidator.Validate(cluster)...)
	allErrs = append(allErrs, v.authValidator.Validate(cluster)...)

	// spec.config validation — rejects deprecated keys, operator-managed
	// discovery/SSL keys, and per-pod runtime-managed keys (advertised
	// addresses, topology) that would collide with the operator's own
	// startup-appended values. Previously this validator was never wired in.
	allErrs = append(allErrs, v.configValidator.Validate(cluster)...)

	// Property sharding validation (version requirements)
	allErrs = append(allErrs, v.validatePropertySharding(cluster)...)

	// Memory validation (critical for preventing runtime failures)
	allErrs = append(allErrs, v.memoryValidator.Validate(cluster)...)

	// MCP server validation
	allErrs = append(allErrs, validateMCPConfig(cluster.Spec.MCP, field.NewPath("spec", "mcp"))...)

	// Aura Fleet Management validation
	allErrs = append(allErrs, validateAuraFleetManagement(cluster.Spec.AuraFleetManagement, field.NewPath("spec", "auraFleetManagement"))...)

	// Trusted CA Secrets + extra volume mounts (operator-managed path collision check)
	allErrs = append(allErrs, ValidateTrustedCASecrets(cluster.Spec.TrustedCASecrets, field.NewPath("spec", "trustedCASecrets"))...)
	allErrs = append(allErrs, ValidateExtraVolumes(cluster.Spec.ExtraVolumes, field.NewPath("spec", "extraVolumes"))...)
	allErrs = append(allErrs, ValidateExtraVolumeMounts(cluster.Spec.ExtraVolumeMounts, field.NewPath("spec", "extraVolumeMounts"))...)

	return allErrs
}

// validateClusterUpdate performs validation specific to cluster updates
func (v *ClusterValidator) validateClusterUpdate(ctx context.Context, oldCluster, newCluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Validate scaling resource requirements
	if v.isScalingUp(oldCluster, newCluster) {
		allErrs = append(allErrs, v.resourceValidator.ValidateScaling(ctx, newCluster, newCluster.Spec.Topology)...)
	}

	// Prevent downgrading server count below minimum
	if newCluster.Spec.Topology.Servers < oldCluster.Spec.Topology.Servers {
		if newCluster.Spec.Topology.Servers < 2 {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "topology", "servers"),
				newCluster.Spec.Topology.Servers,
				"cannot reduce servers below 2",
			))
		}
	}

	// Validate image upgrades
	if oldCluster.Spec.Image.Tag != newCluster.Spec.Image.Tag {
		if upgradeErrs := v.upgradeValidator.ValidateVersionUpgrade(oldCluster.Spec.Image.Tag, newCluster.Spec.Image.Tag); len(upgradeErrs) > 0 {
			allErrs = append(allErrs, upgradeErrs...)
		}
	}

	// Validate upgrade strategy changes
	if newCluster.Spec.UpgradeStrategy != nil {
		allErrs = append(allErrs, v.upgradeValidator.ValidateUpgradeStrategy(newCluster)...)
	}

	return allErrs
}

// isScalingUp checks if the cluster is scaling up (increasing server count)
func (v *ClusterValidator) isScalingUp(oldCluster, newCluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
	return newCluster.Spec.Topology.Servers > oldCluster.Spec.Topology.Servers
}

// ValidateCreateWithWarnings validates a Neo4jEnterpriseCluster for creation and returns warnings
func (v *ClusterValidator) ValidateCreateWithWarnings(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) ClusterValidationResult {
	result := ClusterValidationResult{
		Errors:   v.validateCluster(ctx, cluster),
		Warnings: []string{},
	}

	// Get topology warnings
	topologyResult := v.topologyValidator.ValidateWithWarnings(cluster)
	result.Warnings = append(result.Warnings, topologyResult.Warnings...)

	return result
}

// ValidateUpdateWithWarnings validates a Neo4jEnterpriseCluster for update and returns warnings
func (v *ClusterValidator) ValidateUpdateWithWarnings(ctx context.Context, oldCluster, newCluster *neo4jv1beta1.Neo4jEnterpriseCluster) ClusterValidationResult {
	result := ClusterValidationResult{
		Errors: v.validateCluster(ctx, newCluster),
	}
	result.Errors = append(result.Errors, v.validateClusterUpdate(ctx, oldCluster, newCluster)...)

	// Get topology warnings
	topologyResult := v.topologyValidator.ValidateWithWarnings(newCluster)
	result.Warnings = append(result.Warnings, topologyResult.Warnings...)

	return result
}

// validatePropertySharding validates property sharding configuration and version requirements.
// Delegates the underlying static check to IsClusterShardingReady so backup/restore
// reconcilers can reuse the same precondition without duplicating the logic.
func (v *ClusterValidator) validatePropertySharding(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Only run the gate when the user opted in. If sharding is disabled the
	// version requirement does not apply — the helper would reject any image
	// tag in that branch, which is wrong for the validator's purpose here.
	if cluster.Spec.PropertySharding == nil || !cluster.Spec.PropertySharding.Enabled {
		return allErrs
	}

	if err := IsClusterShardingReady(cluster); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "image", "tag"),
			cluster.Spec.Image.Tag,
			err.Error()))
	}

	return allErrs
}
