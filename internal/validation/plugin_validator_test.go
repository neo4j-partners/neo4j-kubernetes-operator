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
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestPluginValidator_Validate(t *testing.T) {
	validator := NewPluginValidator()

	tests := []struct {
		name         string
		plugin       *neo4jv1beta1.Neo4jPlugin
		expectError  bool
		errorCount   int
		warningCount int
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
					Version:    "5.25.0", // Valid APOC version format, but intentionally below the validator's minimum APOC >= 5.26.0 (see plugin_validator.go compatibility matrix)
					Enabled:    true,
				},
			},
			expectError:  false, // compatibility is advisory: warning, not error
			errorCount:   0,
			warningCount: 1,
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
			expectError:  false, // deprecation is advisory: warning, not error
			errorCount:   0,
			warningCount: 1,
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
			expectError:  false, // unknown plugins are advisory: warning, not error
			errorCount:   0,
			warningCount: 1,
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
			result := validator.Validate(tt.plugin)

			if tt.expectError {
				if len(result.Errors) == 0 {
					t.Errorf("expected validation errors but got none")
				}
				if len(result.Errors) != tt.errorCount {
					t.Errorf("expected %d errors but got %d: %v", tt.errorCount, len(result.Errors), result.Errors)
				}
			} else if len(result.Errors) > 0 {
				t.Errorf("expected no validation errors but got %d: %v", len(result.Errors), result.Errors)
			}
			if len(result.Warnings) != tt.warningCount {
				t.Errorf("expected %d warnings but got %d: %v", tt.warningCount, len(result.Warnings), result.Warnings)
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
			errs := validator.Validate(base(tt.source)).Errors
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

func TestPluginValidator_pluginCompatibilityWarning(t *testing.T) {
	validator := NewPluginValidator()

	tests := []struct {
		name          string
		pluginName    string
		version       string
		expectWarning bool
	}{
		{
			name:          "valid APOC version",
			pluginName:    "apoc",
			version:       "5.26.0",
			expectWarning: false,
		},
		{
			name:          "valid GDS version",
			pluginName:    "graph-data-science",
			version:       "2.9.0",
			expectWarning: false,
		},
		{
			name:          "APOC version below recorded minimum warns",
			pluginName:    "apoc",
			version:       "5.25.0",
			expectWarning: true,
		},
		{
			name:          "deprecated plugin warns",
			pluginName:    "neo4j-graph-algorithms",
			version:       "3.5.0",
			expectWarning: true,
		},
		{
			name:          "unknown plugin warns",
			pluginName:    "unknown-plugin",
			version:       "1.0.0",
			expectWarning: true,
		},
		{
			name:          "valid plugin version newer than minimum",
			pluginName:    "apoc",
			version:       "5.26.5", // synthetic fixture to verify version-comparison ordering (> 5.26.0); not a claim about an actual published APOC release
			expectWarning: false,
		},
		{
			name:          "valid CalVer plugin",
			pluginName:    "neo4j-genai",
			version:       "2025.01.0",
			expectWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validator.pluginCompatibilityWarning(tt.pluginName, tt.version)

			if tt.expectWarning {
				if msg == "" {
					t.Errorf("expected a warning but got none")
				}
			} else {
				if msg != "" {
					t.Errorf("expected no warning but got: %q", msg)
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
			version1: "5.27.0", // non-existent in practice; used only to test comparison arithmetic
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

// TestPluginValidator_VerifiedDownloadGates locks in the three
// cross-field gates the VerifiedDownload install mode imposes. Each
// gate exists for a specific reason in the supply-chain story —
// breaking them silently makes the verified-download flow look
// healthy while actually shipping unverified or partially-verified
// JARs.
func TestPluginValidator_VerifiedDownloadGates(t *testing.T) {
	validator := NewPluginValidator()

	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	base := func(src *neo4jv1beta1.PluginSource, deps []neo4jv1beta1.PluginDependency) *neo4jv1beta1.Neo4jPlugin {
		return &neo4jv1beta1.Neo4jPlugin{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Spec: neo4jv1beta1.Neo4jPluginSpec{
				ClusterRef:   "c",
				Name:         "apoc",
				Version:      "5.26.0",
				Enabled:      true,
				InstallMode:  "VerifiedDownload",
				Source:       src,
				Dependencies: deps,
			},
		}
	}

	tests := []struct {
		name    string
		source  *neo4jv1beta1.PluginSource
		deps    []neo4jv1beta1.PluginDependency
		wantErr bool
		errSubs string
	}{
		{
			name:   "valid url+checksum passes",
			source: &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: validSHA256},
		},
		{
			name:    "missing url is rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", Checksum: validSHA256},
			wantErr: true, errSubs: "spec.source.url",
		},
		{
			name:    "missing checksum is rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar"},
			wantErr: true, errSubs: "spec.source.checksum",
		},
		{
			name:    "type=official is rejected — no verifiable URL",
			source:  &neo4jv1beta1.PluginSource{Type: "official", URL: "https://x/p.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "url or source.type=custom",
		},
		{
			name:    "type=community is rejected — no verifiable URL",
			source:  &neo4jv1beta1.PluginSource{Type: "community", URL: "https://x/p.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "url or source.type=custom",
		},
		{
			name:   "type=custom passes with url+checksum",
			source: &neo4jv1beta1.PluginSource{Type: "custom", URL: "https://x/p.jar", Checksum: validSHA256},
		},
		{
			name:   "dependencies are rejected — each must be its own CR",
			source: &neo4jv1beta1.PluginSource{Type: "url", URL: "https://x/p.jar", Checksum: validSHA256},
			deps: []neo4jv1beta1.PluginDependency{
				{Name: "apoc-core", VersionConstraint: ">=5.0.0"},
			},
			wantErr: true, errSubs: "spec.dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validator.Validate(base(tt.source, tt.deps)).Errors
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

func TestPluginValidator_ValidatesConfig(t *testing.T) {
	v := NewPluginValidator()
	base := func(cfg map[string]string) *neo4jv1beta1.Neo4jPlugin {
		return &neo4jv1beta1.Neo4jPlugin{
			Spec: neo4jv1beta1.Neo4jPluginSpec{
				Name:    "apoc",
				Version: "5.26.0",
				Config:  cfg,
			},
		}
	}

	// Newline in a value forges an extra neo4j.conf line.
	res := v.Validate(base(map[string]string{
		"dbms.security.procedures.unrestricted": "apoc.*\ndbms.security.auth_enabled=false",
	}))
	if !hasErrContaining(res.Errors, "newline or carriage-return") {
		t.Errorf("expected newline rejection, got: %v", res.Errors)
	}

	// Malicious key.
	res = v.Validate(base(map[string]string{"bad key$(x)": "v"}))
	if !hasErrContaining(res.Errors, "config key may contain only") {
		t.Errorf("expected key rejection, got: %v", res.Errors)
	}

	// Clean config passes the config checks.
	res = v.Validate(base(map[string]string{"dbms.security.procedures.unrestricted": "apoc.*"}))
	if hasErrContaining(res.Errors, "newline or carriage-return") || hasErrContaining(res.Errors, "config key may contain only") {
		t.Errorf("clean config should not trip config validation: %v", res.Errors)
	}
}

func hasErrContaining(errs field.ErrorList, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), sub) {
			return true
		}
	}
	return false
}

// TestPluginValidator_URLSchemeAllowlist pins the https-only URL scheme rule
// for url/custom plugin sources (#208, audit P0.3). Plugin JARs are fetched
// over the network and the recorded checksum is not enforced at download time
// on the entrypoint path, so a cleartext http URL would let a network attacker
// swap the JAR with no transport AND no content integrity.
func TestPluginValidator_URLSchemeAllowlist(t *testing.T) {
	validator := NewPluginValidator()

	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

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
		errSubs string
	}{
		{
			name:   "https url passes",
			source: &neo4jv1beta1.PluginSource{Type: "url", URL: "https://repo.example.com/apoc.jar", Checksum: validSHA256},
		},
		{
			name:   "https with port and path passes",
			source: &neo4jv1beta1.PluginSource{Type: "custom", URL: "https://mirror.internal:8443/plugins/apoc.jar", Checksum: validSHA256},
		},
		{
			name:   "uppercase HTTPS scheme passes",
			source: &neo4jv1beta1.PluginSource{Type: "url", URL: "HTTPS://repo.example.com/apoc.jar", Checksum: validSHA256},
		},
		{
			name:    "http rejected (cleartext)",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "http://repo.example.com/apoc.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "scheme must be https",
		},
		{
			name:    "http rejected for custom type too",
			source:  &neo4jv1beta1.PluginSource{Type: "custom", URL: "http://repo.example.com/apoc.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "scheme must be https",
		},
		{
			name:    "file scheme rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "file:///plugins/apoc.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "scheme must be https",
		},
		{
			name:    "ftp scheme rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "ftp://repo.example.com/apoc.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "scheme must be https",
		},
		{
			name:    "scheme-relative URL rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "//repo.example.com/apoc.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "scheme must be https",
		},
		{
			name:    "unparseable URL rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https://repo example com/ap oc.jar%zz", Checksum: validSHA256},
			wantErr: true, errSubs: "must be a valid URL",
		},
		{
			name:    "https with empty host rejected",
			source:  &neo4jv1beta1.PluginSource{Type: "url", URL: "https:///apoc.jar", Checksum: validSHA256},
			wantErr: true, errSubs: "must include a host",
		},
		{
			name:   "official source has no URL and is unaffected",
			source: &neo4jv1beta1.PluginSource{Type: "official"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validator.Validate(base(tt.source)).Errors
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
