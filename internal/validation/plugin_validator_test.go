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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestPluginValidator_Validate(t *testing.T) {
	validator := NewPluginValidator()

	tests := []struct {
		name        string
		plugin      *neo4jv1beta1.Neo4jPlugin
		expectError bool
		errorCount  int
	}{
		{
			name: "valid APOC plugin",
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "apoc-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gds-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "apoc-old",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deprecated-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "missing-name",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "missing-version",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Source: &neo4jv1beta1.PluginSource{
						Type: "url",
						URL:  "https://example.com/plugin.jar",
						// 64-char sha256 hex for a representative plugin JAR.
						Checksum: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid plugin source - missing URL",
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-source",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Source: &neo4jv1beta1.PluginSource{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "plugin-with-deps",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc-extended",
					Version:    "5.26.0",
					Enabled:    true,
					Dependencies: []neo4jv1beta1.PluginDependency{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-dep",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Dependencies: []neo4jv1beta1.PluginDependency{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secure-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Security: &neo4jv1beta1.PluginSecurity{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "conflicting-security",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Security: &neo4jv1beta1.PluginSecurity{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-plugin",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Resources: &neo4jv1beta1.PluginResourceRequirements{
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
			plugin: &neo4jv1beta1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-resources",
				},
				Spec: neo4jv1beta1.Neo4jPluginSpec{
					ClusterRef: "test-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Resources: &neo4jv1beta1.PluginResourceRequirements{
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

// TestPluginValidator_ChecksumRules locks in the supply-chain validation
// contract: arbitrary-URL source types must commit to a sha256/sha512
// checksum in the canonical algo-prefixed form. Weaker algorithms (sha1,
// md5) and unprefixed hex are rejected so verification tooling never has
// to guess.
func TestPluginValidator_ChecksumRules(t *testing.T) {
	validator := NewPluginValidator()

	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	const validSHA512 = "sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"

	base := func(src *neo4jv1beta1.PluginSource) *neo4jv1beta1.Neo4jPlugin {
		return &neo4jv1beta1.Neo4jPlugin{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Spec: neo4jv1beta1.Neo4jPluginSpec{
				ClusterRef: "c",
				Name:       "apoc",
				Version:    "5.26.0",
				Enabled:    true,
				Source:     src,
			},
		}
	}

	tests := []struct {
		name    string
		source  *neo4jv1beta1.PluginSource
		wantErr bool
		errSubs string // expected substring in the joined error, if wantErr
	}{
		{
			name:   "url with valid sha256 passes",
			source: &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: validSHA256},
		},
		{
			name:   "url with valid sha512 passes",
			source: &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: validSHA512},
		},
		{
			name:    "custom requires checksum (previously only url did)",
			source:  &neo4jv1beta1.PluginSource{Type: "custom", URL: "https://x/p.jar"},
			wantErr: true, errSubs: "checksum is required",
		},
		{
			name:   "custom with valid sha256 passes",
			source: &neo4jv1beta1.PluginSource{Type: "custom", URL: "https://x/p.jar", Checksum: validSHA256},
		},
		{
			name:   "official does not require checksum",
			source: &neo4jv1beta1.PluginSource{Type: "official"},
		},
		{
			name:   "community does not require checksum",
			source: &neo4jv1beta1.PluginSource{Type: "community"},
		},
		{
			name:    "url with sha1 rejected (collision-prone)",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: "sha1:a94a8fef8c17b933bce8fc1f3e3f6a3b6df8f4dd"},
			wantErr: true, errSubs: "sha256:",
		},
		{
			name:    "url with md5 rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: "md5:d41d8cd98f00b204e9800998ecf8427e"},
			wantErr: true, errSubs: "sha256:",
		},
		{
			name:    "url with unprefixed hex rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
			wantErr: true, errSubs: "sha256:",
		},
		{
			name:    "url with sha256 prefix but wrong length rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: "sha256:abc123"},
			wantErr: true, errSubs: "sha256:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validator.Validate(base(tt.source))
			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatalf("expected an error, got none")
				}
				if tt.errSubs != "" && !strings.Contains(errs.ToAggregate().Error(), tt.errSubs) {
					t.Errorf("expected error to contain %q, got %v", tt.errSubs, errs)
				}
				return
			}
			if len(errs) > 0 {
				t.Fatalf("expected no errors, got %v", errs)
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
			name:        "valid plugin version newer than minimum",
			pluginName:  "apoc",
			version:     "5.26.5", // patch release of APOC for Neo4j 5.26.x
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
			// 5.27.x does not exist in practice; used only for comparison arithmetic
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
