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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestPluginValidator_Validate(t *testing.T) {
	validator := NewPluginValidator()

	tests := []struct {
		name        string
		plugin      *neo4jv1alpha1.Neo4jPlugin
		expectError bool
		errorCount  int
	}{
		{
			name: "valid APOC plugin",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "apoc-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid GDS plugin",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gds-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "graph-data-science",
					Version:    "2.9.0",
					Enabled:    true,
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid plugin version - too old",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "apoc-old",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.25.0", // Too old for Neo4j 5.26+
					Enabled:    true,
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "deprecated plugin",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deprecated-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "neo4j-graph-algorithms",
					Version:    "3.5.0",
					Enabled:    true,
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "unknown plugin",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "unknown-plugin",
					Version:    "1.0.0",
					Enabled:    true,
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "missing plugin name",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "missing-name",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Version:    "5.26.0",
					Enabled:    true,
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "missing plugin version",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "missing-version",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Enabled:    true,
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "valid plugin with custom source",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Source: &neo4jv1alpha1.PluginSource{
						Type:     "url",
						URL:      "https://example.com/plugin.jar",
						Checksum: "sha256:abc123",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid plugin source - missing URL",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-source",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Source: &neo4jv1alpha1.PluginSource{
						Type: "url",
						// Missing URL
					},
				},
			},
			expectError: true,
			errorCount:  2, // Missing URL and checksum
		},
		{
			name: "valid plugin with dependencies",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "plugin-with-deps",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc-extended",
					Version:    "5.26.0",
					Enabled:    true,
					Dependencies: []neo4jv1alpha1.PluginDependency{
						{
							Name:              "apoc",
							VersionConstraint: ">=5.26.0",
							Optional:          false,
						},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid plugin dependency - missing name",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-dep",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Dependencies: []neo4jv1alpha1.PluginDependency{
						{
							// Missing name
							VersionConstraint: ">=5.26.0",
							Optional:          false,
						},
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "valid plugin with security config",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secure-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Security: &neo4jv1alpha1.PluginSecurity{
						AllowedProcedures: []string{"apoc.load.json"},
						SecurityPolicy:    "strict",
						Sandbox:           true,
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid plugin security - conflicting procedures",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "conflicting-security",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Security: &neo4jv1alpha1.PluginSecurity{
						AllowedProcedures: []string{"apoc.load.json"},
						DeniedProcedures:  []string{"apoc.load.json"}, // Conflict
						SecurityPolicy:    "strict",
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "valid plugin with resources",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-plugin",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Resources: &neo4jv1alpha1.PluginResourceRequirements{
						MemoryLimit:    "512Mi",
						CPULimit:       "500m",
						ThreadPoolSize: 10,
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid plugin resources - negative thread pool",
			plugin: &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-resources",
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Resources: &neo4jv1alpha1.PluginResourceRequirements{
						ThreadPoolSize: -1, // Invalid
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.plugin)

			if tt.expectError {
				if len(errors) == 0 {
					t.Errorf("expected validation errors but got none")
				}
				if len(errors) != tt.errorCount {
					t.Errorf("expected %d errors but got %d: %v", tt.errorCount, len(errors), errors)
				}
			} else if len(errors) > 0 {
				t.Errorf("expected no validation errors but got %d: %v", len(errors), errors)
			}
		})
	}
}

func TestPluginValidator_validatePluginCompatibility(t *testing.T) {
	validator := NewPluginValidator()

	tests := []struct {
		name        string
		pluginName  string
		version     string
		expectError bool
	}{
		{
			name:        "valid APOC version",
			pluginName:  "apoc",
			version:     "5.26.0",
			expectError: false,
		},
		{
			name:        "valid GDS version",
			pluginName:  "graph-data-science",
			version:     "2.9.0",
			expectError: false,
		},
		{
			name:        "invalid APOC version - too old",
			pluginName:  "apoc",
			version:     "5.25.0",
			expectError: true,
		},
		{
			name:        "deprecated plugin",
			pluginName:  "neo4j-graph-algorithms",
			version:     "3.5.0",
			expectError: true,
		},
		{
			name:        "unknown plugin",
			pluginName:  "unknown-plugin",
			version:     "1.0.0",
			expectError: true,
		},
		{
			name:        "valid newer version",
			pluginName:  "apoc",
			version:     "5.27.0",
			expectError: false,
		},
		{
			name:        "valid CalVer plugin",
			pluginName:  "neo4j-genai",
			version:     "2025.01.0",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validatePluginCompatibility(tt.pluginName, tt.version)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestPluginValidator_compareVersions(t *testing.T) {
	validator := NewPluginValidator()

	tests := []struct {
		name     string
		version1 string
		version2 string
		expected int
	}{
		{
			name:     "equal versions",
			version1: "5.26.0",
			version2: "5.26.0",
			expected: 0,
		},
		{
			name:     "version1 greater",
			version1: "5.27.0",
			version2: "5.26.0",
			expected: 1,
		},
		{
			name:     "version2 greater",
			version1: "5.25.0",
			version2: "5.26.0",
			expected: -1,
		},
		{
			name:     "different patch versions",
			version1: "5.26.1",
			version2: "5.26.0",
			expected: 1,
		},
		{
			name:     "CalVer comparison",
			version1: "2025.01.0",
			version2: "2024.12.0",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.compareVersions(tt.version1, tt.version2)

			if result != tt.expected {
				t.Errorf("expected %d but got %d", tt.expected, result)
			}
		})
	}
}
