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
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// TopologyValidator validates Neo4j topology configuration
type TopologyValidator struct{}

// NewTopologyValidator creates a new topology validator
func NewTopologyValidator() *TopologyValidator {
	return &TopologyValidator{}
}

// ValidationResult holds validation errors and warnings
type ValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// Validate validates the topology configuration
func (v *TopologyValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	topologyPath := field.NewPath("spec", "topology")

	// Validate primaries - enforce minimum clustering requirements
	if cluster.Spec.Topology.Primaries < 1 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("primaries"),
			cluster.Spec.Topology.Primaries,
			"primaries must be at least 1",
		))
	}

	// Validate secondaries
	if cluster.Spec.Topology.Secondaries < 0 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("secondaries"),
			cluster.Spec.Topology.Secondaries,
			"secondaries cannot be negative",
		))
	}

	// Enforce minimum cluster topology requirements
	// Neo4jEnterpriseCluster must have either:
	// 1. One primary AND at least one secondary (1+1 minimum)
	// 2. Multiple primaries (2+ primaries, any number of secondaries)
	if cluster.Spec.Topology.Primaries == 1 && cluster.Spec.Topology.Secondaries == 0 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath,
			fmt.Sprintf("primaries=%d, secondaries=%d", cluster.Spec.Topology.Primaries, cluster.Spec.Topology.Secondaries),
			"Neo4jEnterpriseCluster requires minimum cluster topology: either 1 primary + 1 secondary, or multiple primaries. For single-node deployments, use Neo4jEnterpriseStandalone instead",
		))
	}

	return allErrs
}

// ValidateWithWarnings validates the topology configuration and returns both errors and warnings
func (v *TopologyValidator) ValidateWithWarnings(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) ValidationResult {
	result := ValidationResult{
		Errors:   v.Validate(cluster),
		Warnings: []string{},
	}

	// Check for even number of primaries (generate warning) - but skip for 0 primaries as that's an error case
	if cluster.Spec.Topology.Primaries > 0 && cluster.Spec.Topology.Primaries%2 == 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Even number of primary nodes (%d) reduces fault tolerance. "+
				"In a split-brain scenario, the cluster may become unavailable. "+
				"Consider using an odd number (3, 5, or 7) for optimal fault tolerance.",
				cluster.Spec.Topology.Primaries))
	}

	// Check for 2 primaries specifically (additional warning)
	if cluster.Spec.Topology.Primaries == 2 {
		result.Warnings = append(result.Warnings,
			"2 primary nodes provide limited fault tolerance. "+
				"If one node fails, the remaining node cannot form quorum. "+
				"Consider using 3 primary nodes for production deployments. "+
				"Note: The operator uses coordinated cluster formation that requires both nodes to start together.")
	}

	// Check for suboptimal primary counts
	if cluster.Spec.Topology.Primaries > 7 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("More than 7 primary nodes (%d) may impact cluster performance "+
				"due to increased consensus overhead. "+
				"Consider using read replicas instead for scaling read capacity.",
				cluster.Spec.Topology.Primaries))
	}

	return result
}
