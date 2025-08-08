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

	// Validate servers - enforce minimum clustering requirements
	// Neo4j clusters require at least 2 servers
	if cluster.Spec.Topology.Servers < 2 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("servers"),
			cluster.Spec.Topology.Servers,
			"servers must be at least 2 for clustering. For single-node deployments, use Neo4jEnterpriseStandalone instead",
		))
	}

	// Validate server mode constraint if specified
	if cluster.Spec.Topology.ServerModeConstraint != "" {
		validModes := map[string]bool{"NONE": true, "PRIMARY": true, "SECONDARY": true}
		if !validModes[cluster.Spec.Topology.ServerModeConstraint] {
			allErrs = append(allErrs, field.Invalid(
				topologyPath.Child("serverModeConstraint"),
				cluster.Spec.Topology.ServerModeConstraint,
				"serverModeConstraint must be one of: NONE, PRIMARY, SECONDARY",
			))
		}
	}

	return allErrs
}

// ValidateWithWarnings validates the topology configuration and returns both errors and warnings
func (v *TopologyValidator) ValidateWithWarnings(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) ValidationResult {
	result := ValidationResult{
		Errors:   v.Validate(cluster),
		Warnings: []string{},
	}

	// Check for even number of servers (generate warning for cluster consensus)
	if cluster.Spec.Topology.Servers > 0 && cluster.Spec.Topology.Servers%2 == 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Even number of servers (%d) may reduce fault tolerance when databases specify odd-numbered server allocations. "+
				"Consider using an odd number of servers for optimal fault tolerance.",
				cluster.Spec.Topology.Servers))
	}

	// Check for 2 servers specifically (additional warning)
	if cluster.Spec.Topology.Servers == 2 {
		result.Warnings = append(result.Warnings,
			"2 servers provide limited fault tolerance. "+
				"If one server fails, databases may lose quorum. "+
				"Consider using 3 or more servers for production deployments.")
	}

	// Check for too many servers
	if cluster.Spec.Topology.Servers > 10 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("More than 10 servers (%d) may impact cluster performance "+
				"due to increased coordination overhead. "+
				"Consider the actual database topology needs when scaling servers.",
				cluster.Spec.Topology.Servers))
	}

	// Warn about server mode constraints
	if cluster.Spec.Topology.ServerModeConstraint == "PRIMARY" {
		result.Warnings = append(result.Warnings,
			"All servers are constrained to PRIMARY mode. "+
				"This prevents databases from using secondary replicas for read scaling.")
	} else if cluster.Spec.Topology.ServerModeConstraint == "SECONDARY" {
		result.Warnings = append(result.Warnings,
			"All servers are constrained to SECONDARY mode. "+
				"Ensure other servers in the cluster can host primary database instances.")
	}

	return result
}
