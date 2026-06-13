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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestImageValidator_Validate(t *testing.T) {
	validator := NewImageValidator()

	tests := []struct {
		name          string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		expectedErrs  int
		expectedError string
	}{
		{
			name: "valid 5.26.0 version",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0-enterprise",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs: 0,
		},
		{
			name: "invalid 5.27.0 version (does not exist)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.27.0",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs: 1,
		},
		{
			name: "valid 2025.01.0 CalVer version",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "2025.01.0-enterprise",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs: 0,
		},
		{
			name: "invalid 5.25.0 version (too old)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.25.0-enterprise",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs:  1,
			expectedError: "Neo4j version must be 5.26.x (last semver LTS) or 2025.01.0+ (CalVer) for enterprise operator",
		},
		{
			name: "invalid 4.4.12 version (unsupported major)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "4.4.12-enterprise",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs:  1,
			expectedError: "Neo4j version must be 5.26.x (last semver LTS) or 2025.01.0+ (CalVer) for enterprise operator",
		},
		{
			name: "invalid 5.15.0 version (too old)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.15.0",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs:  1,
			expectedError: "Neo4j version must be 5.26.x (last semver LTS) or 2025.01.0+ (CalVer) for enterprise operator",
		},
		{
			name: "missing repo",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Tag:        "5.26.0",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs: 1,
		},
		{
			name: "missing tag",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs: 1,
		},
		{
			name: "invalid pull policy",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0",
						PullPolicy: "InvalidPolicy",
					},
				},
			},
			expectedErrs: 1,
		},
		{
			// Invariant 3: an explicitly community-tagged image is rejected.
			name: "community image rejected (semver)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0-community",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs:  1,
			expectedError: "community images are not supported",
		},
		{
			name: "community image rejected (calver, case-insensitive)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "2025.01.0-Community",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs:  1,
			expectedError: "community images are not supported",
		},
		{
			// We reject ONLY the explicit community marker — a bare/retagged
			// Enterprise tag with no edition suffix must still pass cleanly.
			name: "bare retagged enterprise tag passes (no false rejection)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "myco.example.com/neo4j",
						Tag:        "5.26.0",
						PullPolicy: "IfNotPresent",
					},
				},
			},
			expectedErrs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validator.Validate(tt.cluster)

			assert.Equal(t, tt.expectedErrs, len(errs), "Expected %d errors, got %d", tt.expectedErrs, len(errs))

			if tt.expectedError != "" && len(errs) > 0 {
				found := false
				for _, err := range errs {
					if err.Type == field.ErrorTypeInvalid && strings.Contains(err.Detail, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s' not found in errors: %v", tt.expectedError, errs)
			}
		})
	}
}

func TestImageValidator_isVersionSupported(t *testing.T) {
	validator := NewImageValidator()

	tests := []struct {
		version   string
		supported bool
	}{
		// Supported SemVer versions
		{"5.26.0", true},
		{"5.27.0", false},
		{"5.30.1", false}, // 5.27+ semver versions do not exist
		{"5.26.0-enterprise", true},
		{"v5.26.0", true},

		// Supported CalVer versions
		{"2025.01.0", true},
		{"2025.06.0", true},
		{"2025.12.0", true},
		{"2025.01.0-enterprise", true},

		// Unsupported versions
		{"5.25.0", false},
		{"5.15.0", false},
		{"4.4.12", false},
		{"4.4.12-enterprise", false},
		{"3.5.0", false},
		{"invalid", false},
		{"", false},
		{"5", false},
		{"5.", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := validator.isVersionSupported(tt.version)
			assert.Equal(t, tt.supported, result, "Version %s support expected: %v, got: %v", tt.version, tt.supported, result)
		})
	}
}
