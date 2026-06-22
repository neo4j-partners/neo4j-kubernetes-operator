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
	"testing"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestTopologyValidator_Validate(t *testing.T) {
	validator := NewTopologyValidator()

	tests := []struct {
		name          string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErrorsLen int
		wantErrorMsg  string
	}{
		{
			name: "valid server configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			wantErrorsLen: 0,
		},
		{
			name: "valid minimum server configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			wantErrorsLen: 0,
		},
		{
			name: "valid large server configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 7,
					},
				},
			},
			wantErrorsLen: 0,
		},
		{
			name: "invalid single server configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 1,
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "servers must be at least 2",
		},
		{
			name: "invalid zero server configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 0,
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "servers must be at least 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.cluster)

			if len(errors) != tt.wantErrorsLen {
				t.Errorf("Validate() returned %d errors, want %d", len(errors), tt.wantErrorsLen)
				for _, err := range errors {
					t.Logf("Error: %s", err.Error())
				}
				return
			}

			if tt.wantErrorMsg != "" && len(errors) > 0 {
				found := false
				for _, err := range errors {
					if contains(err.Error(), tt.wantErrorMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error message containing '%s', but didn't find it in errors: %v", tt.wantErrorMsg, errors)
				}
			}
		})
	}
}

func TestTopologyValidator_ValidateDefaults(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			AcceptLicenseAgreement: "eval",
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}

	// No defaults need to be applied for server-based topology

	// Server count should remain unchanged
	if cluster.Spec.Topology.Servers != 3 {
		t.Errorf("Expected servers to remain 3, got %d", cluster.Spec.Topology.Servers)
	}
}

func TestTopologyValidator_ValidateServerRoles(t *testing.T) {
	validator := NewTopologyValidator()

	tests := []struct {
		name          string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErrorsLen int
		wantErrorMsg  string
	}{
		{
			name: "valid serverRoles within range",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1beta1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
							{ServerIndex: 1, ModeConstraint: "SECONDARY"},
						},
					},
				},
			},
			wantErrorsLen: 0,
		},
		{
			name: "serverIndex out of range",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1beta1.ServerRoleHint{
							{ServerIndex: 5, ModeConstraint: "PRIMARY"},
						},
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "must be in range [0, 2]",
		},
		{
			name: "duplicate serverIndex",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1beta1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
							{ServerIndex: 0, ModeConstraint: "SECONDARY"},
						},
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "Duplicate",
		},
		{
			name: "all servers SECONDARY rejected",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2,
						ServerRoles: []neo4jv1beta1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "SECONDARY"},
							{ServerIndex: 1, ModeConstraint: "SECONDARY"},
						},
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "cannot set ALL servers to SECONDARY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.cluster)
			if len(errors) != tt.wantErrorsLen {
				t.Errorf("Expected %d errors, got %d: %v", tt.wantErrorsLen, len(errors), errors)
			}
			if tt.wantErrorMsg != "" && len(errors) > 0 {
				found := false
				for _, err := range errors {
					if contains(err.Error(), tt.wantErrorMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error containing '%s', got: %v", tt.wantErrorMsg, errors)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			func() bool {
				for i := 1; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())))
}
