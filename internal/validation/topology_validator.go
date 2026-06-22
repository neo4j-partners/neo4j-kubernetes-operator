/*
Copyright 2025 Priyo Lahiri.

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

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
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
func (v *TopologyValidator) Validate(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	topologyPath := field.NewPath("spec", "topology")

	// Validate servers — also enforced by the CRD's Minimum=2 marker, kept
	// here deliberately as defense-in-depth: the validators run in unit
	// tests without an API server, and a CRD regen that lost the marker
	// would otherwise silently drop the floor (no-webhooks architecture
	// makes this layer the only programmatic guard).
	if cluster.Spec.Topology.Servers < 2 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("servers"),
			cluster.Spec.Topology.Servers,
			"servers must be at least 2 for clustering. For single-node deployments, use Neo4jEnterpriseStandalone instead",
		))
	}

	// Validate minSystemPrimaries: must be >= 2 (also enforced by the CRD's
	// Minimum=2 marker — duplicated here as defense-in-depth, same rationale
	// as the servers check above) and must not exceed the server count — a
	// floor higher than the number of servers can never be satisfied, so the
	// cluster would never form. The cross-field ceiling is the part the CRD
	// cannot express.
	if mp := cluster.Spec.Topology.MinSystemPrimaries; mp != nil {
		if *mp > cluster.Spec.Topology.Servers {
			allErrs = append(allErrs, field.Invalid(
				topologyPath.Child("minSystemPrimaries"),
				*mp,
				fmt.Sprintf("must not exceed spec.topology.servers (%d); a system-primary floor larger than the cluster can never be satisfied", cluster.Spec.Topology.Servers),
			))
		}
		if *mp < 2 {
			allErrs = append(allErrs, field.Invalid(
				topologyPath.Child("minSystemPrimaries"),
				*mp,
				"must be at least 2",
			))
		}
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

	// Validate serverRoles indices: must be in range [0, servers-1] with no duplicates
	if len(cluster.Spec.Topology.ServerRoles) > 0 {
		seen := make(map[int32]bool)
		allSecondary := len(cluster.Spec.Topology.ServerRoles) == int(cluster.Spec.Topology.Servers)
		for i, role := range cluster.Spec.Topology.ServerRoles {
			rolePath := topologyPath.Child("serverRoles").Index(i)
			if role.ServerIndex >= cluster.Spec.Topology.Servers {
				allErrs = append(allErrs, field.Invalid(
					rolePath.Child("serverIndex"), role.ServerIndex,
					fmt.Sprintf("must be in range [0, %d]", cluster.Spec.Topology.Servers-1),
				))
			}
			if seen[role.ServerIndex] {
				allErrs = append(allErrs, field.Duplicate(
					rolePath.Child("serverIndex"), role.ServerIndex,
				))
			}
			seen[role.ServerIndex] = true
			if role.ModeConstraint != "SECONDARY" {
				allSecondary = false
			}
		}
		if allSecondary && cluster.Spec.Topology.Servers > 0 {
			allErrs = append(allErrs, field.Invalid(
				topologyPath.Child("serverRoles"),
				"all SECONDARY",
				"cannot set ALL servers to SECONDARY mode; at least one server must be able to host primaries",
			))
		}
	}

	return allErrs
}

// ValidateWithWarnings validates the topology configuration and returns both errors and warnings
func (v *TopologyValidator) ValidateWithWarnings(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) ValidationResult {
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

	// Warn on an even minSystemPrimaries — an even system-DB voting set has no
	// clean write-quorum majority; an odd value (3, 5, …) is recommended.
	if mp := cluster.Spec.Topology.MinSystemPrimaries; mp != nil && *mp%2 == 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("minSystemPrimaries=%d is even; an even system-database voting set has no clean write-quorum majority. "+
				"An odd value (3, 5, …) is recommended.", *mp))
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
