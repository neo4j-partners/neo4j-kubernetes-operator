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
	"testing"

	"github.com/stretchr/testify/assert"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestTopologyValidator_Validate(t *testing.T) {
	validator := NewTopologyValidator()

	tests := []struct {
		name         string
		cluster      *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectErrors bool
	}{
		{
			name: "invalid single node cluster - should now error",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 0,
					},
				},
			},
			expectErrors: true,
		},
		{
			name: "valid minimal cluster with 1 primary + 1 secondary",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
				},
			},
			expectErrors: false,
		},
		{
			name: "valid multi-primary cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   2,
						Secondaries: 0,
					},
				},
			},
			expectErrors: false,
		},
		{
			name: "valid odd primaries",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 2,
					},
				},
			},
			expectErrors: false,
		},
		{
			name: "valid even primaries (no errors, only warnings)",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   4,
						Secondaries: 1,
					},
				},
			},
			expectErrors: false,
		},
		{
			name: "zero primaries should error",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   0,
						Secondaries: 1,
					},
				},
			},
			expectErrors: true,
		},
		{
			name: "negative secondaries should error",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: -1,
					},
				},
			},
			expectErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.cluster)
			hasErrors := len(errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Error expectation mismatch: %v", errors)
		})
	}
}

func TestTopologyValidator_ValidateWithWarnings(t *testing.T) {
	validator := NewTopologyValidator()

	tests := []struct {
		name            string
		cluster         *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectErrors    bool
		expectWarnings  bool
		warningKeywords []string
	}{
		{
			name: "single node - now should error",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 0,
					},
				},
			},
			expectErrors:   true,
			expectWarnings: false,
		},
		{
			name: "minimal cluster (1 primary + 1 secondary) - no warnings",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
				},
			},
			expectErrors:   false,
			expectWarnings: false,
		},
		{
			name: "odd primaries - no warnings",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 2,
					},
				},
			},
			expectErrors:   false,
			expectWarnings: false,
		},
		{
			name: "even primaries - warning about fault tolerance",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   4,
						Secondaries: 1,
					},
				},
			},
			expectErrors:    false,
			expectWarnings:  true,
			warningKeywords: []string{"Even number", "fault tolerance", "split-brain"},
		},
		{
			name: "two primaries - specific warning",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   2,
						Secondaries: 1,
					},
				},
			},
			expectErrors:    false,
			expectWarnings:  true,
			warningKeywords: []string{"2 primary nodes", "cannot form quorum", "3 primary nodes"},
		},
		{
			name: "excessive primaries - performance warning",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   9,
						Secondaries: 0,
					},
				},
			},
			expectErrors:    false,
			expectWarnings:  true,
			warningKeywords: []string{"More than 7", "consensus overhead", "read replicas"},
		},
		{
			name: "zero primaries - error, no warnings",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   0,
						Secondaries: 1,
					},
				},
			},
			expectErrors:   true,
			expectWarnings: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateWithWarnings(tt.cluster)

			hasErrors := len(result.Errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Error expectation mismatch: %v", result.Errors)

			hasWarnings := len(result.Warnings) > 0
			assert.Equal(t, tt.expectWarnings, hasWarnings, "Warning expectation mismatch: %v", result.Warnings)

			// Check that warning messages contain expected keywords
			if tt.expectWarnings && len(tt.warningKeywords) > 0 {
				assert.Greater(t, len(result.Warnings), 0, "Expected warnings but got none")
				warningText := ""
				for _, warning := range result.Warnings {
					warningText += warning + " "
				}

				for _, keyword := range tt.warningKeywords {
					assert.Contains(t, warningText, keyword, "Warning should contain keyword: %s", keyword)
				}
			}
		})
	}
}
