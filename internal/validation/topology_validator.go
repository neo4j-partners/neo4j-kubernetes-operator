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
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// TopologyValidator validates Neo4j topology configuration
type TopologyValidator struct{}

// NewTopologyValidator creates a new topology validator
func NewTopologyValidator() *TopologyValidator {
	return &TopologyValidator{}
}

// Validate validates the topology configuration
func (v *TopologyValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	topologyPath := field.NewPath("spec", "topology")

	// Check if this is a single-node deployment
	isSingleNode := cluster.Spec.Topology.Primaries == 1 && cluster.Spec.Topology.Secondaries == 0

	// Validate primaries (allow single-node deployment with primaries=1, secondaries=0)
	if !isSingleNode && cluster.Spec.Topology.Primaries < 3 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("primaries"),
			cluster.Spec.Topology.Primaries,
			"primaries must be at least 3 for quorum (or use single-node with primaries=1, secondaries=0)",
		))
	}

	if !isSingleNode && cluster.Spec.Topology.Primaries%2 == 0 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("primaries"),
			cluster.Spec.Topology.Primaries,
			"primaries must be odd to maintain quorum (or use single-node with primaries=1, secondaries=0)",
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

	return allErrs
}
