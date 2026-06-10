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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Unit tests using standard Go testing for unexported methods

func TestMapPluginName(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "apoc plugin",
			input:    "apoc",
			expected: "apoc",
		},
		{
			name:     "graph data science plugin",
			input:    "graph-data-science",
			expected: "graph-data-science",
		},
		{
			name:     "bloom plugin",
			input:    "bloom",
			expected: "bloom",
		},
		{
			name:     "custom plugin",
			input:    "my-custom-plugin",
			expected: "my-custom-plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.mapPluginName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddPluginToList(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	tests := []struct {
		name      string
		existing  string
		newPlugin string
		expected  string
		wantErr   bool
	}{
		{
			name:      "add to empty list",
			existing:  "[]",
			newPlugin: "apoc",
			expected:  "[\"apoc\"]",
			wantErr:   false,
		},
		{
			name:      "add to existing list",
			existing:  "[\"apoc\"]",
			newPlugin: "graph-data-science",
			expected:  "[\"apoc\",\"graph-data-science\"]",
			wantErr:   false,
		},
		{
			name:      "plugin already exists",
			existing:  "[\"apoc\",\"bloom\"]",
			newPlugin: "apoc",
			expected:  "[\"apoc\",\"bloom\"]", // Should not duplicate
			wantErr:   false,
		},
		{
			name:      "add to list with spaces",
			existing:  "[\"apoc\", \"bloom\"]",
			newPlugin: "graph-data-science",
			expected:  "[\"apoc\",\"bloom\",\"graph-data-science\"]",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.addPluginToList(tt.existing, tt.newPlugin)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGetStatefulSetName(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	tests := []struct {
		name       string
		deployment *DeploymentInfo
		expected   string
	}{
		{
			name: "cluster deployment",
			deployment: &DeploymentInfo{
				Type: "cluster",
				Name: "my-cluster",
			},
			expected: "my-cluster-server",
		},
		{
			name: "standalone deployment",
			deployment: &DeploymentInfo{
				Type: "standalone",
				Name: "my-standalone",
			},
			expected: "my-standalone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.getStatefulSetName(tt.deployment)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPodLabels(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	tests := []struct {
		name       string
		deployment *DeploymentInfo
		expected   map[string]string
	}{
		{
			name: "cluster deployment",
			deployment: &DeploymentInfo{
				Type: "cluster",
				Name: "my-cluster",
			},
			expected: map[string]string{
				"app.kubernetes.io/name":     "neo4j",
				"app.kubernetes.io/instance": "my-cluster",
			},
		},
		{
			name: "standalone deployment",
			deployment: &DeploymentInfo{
				Type: "standalone",
				Name: "my-standalone",
			},
			expected: map[string]string{
				"app": "my-standalone",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.getPodLabels(tt.deployment)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetExpectedReplicas(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	tests := []struct {
		name       string
		deployment *DeploymentInfo
		expected   int
	}{
		{
			name: "cluster with 3 servers",
			deployment: &DeploymentInfo{
				Type: "cluster",
				Object: &neo4jv1beta1.Neo4jEnterpriseCluster{
					Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
						Topology: neo4jv1beta1.TopologyConfiguration{
							Servers: 3,
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "cluster with 5 servers",
			deployment: &DeploymentInfo{
				Type: "cluster",
				Object: &neo4jv1beta1.Neo4jEnterpriseCluster{
					Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
						Topology: neo4jv1beta1.TopologyConfiguration{
							Servers: 5,
						},
					},
				},
			},
			expected: 5,
		},
		{
			name: "standalone always 1",
			deployment: &DeploymentInfo{
				Type:   "standalone",
				Object: &neo4jv1beta1.Neo4jEnterpriseStandalone{},
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.getExpectedReplicas(tt.deployment)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMergeNeo4jPluginList tests the exported package-level function used by both the
// plugin controller and the fleet management reconciler to safely merge plugin names into
// the NEO4J_PLUGINS JSON array without overwriting each other's entries.
func TestMergeNeo4jPluginList(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		newPlugin string
		expected  string
		wantErr   bool
	}{
		{
			name:      "add fleet-management to empty list",
			existing:  "[]",
			newPlugin: "fleet-management",
			expected:  `["fleet-management"]`,
		},
		{
			name:      "add fleet-management when apoc already present",
			existing:  `["apoc"]`,
			newPlugin: "fleet-management",
			expected:  `["apoc","fleet-management"]`,
		},
		{
			name:      "add apoc when fleet-management already present",
			existing:  `["fleet-management"]`,
			newPlugin: "apoc",
			expected:  `["fleet-management","apoc"]`,
		},
		{
			name:      "idempotent — fleet-management already in list",
			existing:  `["apoc","fleet-management"]`,
			newPlugin: "fleet-management",
			expected:  `["apoc","fleet-management"]`,
		},
		{
			name:      "add to multi-plugin list",
			existing:  `["apoc","graph-data-science"]`,
			newPlugin: "fleet-management",
			expected:  `["apoc","graph-data-science","fleet-management"]`,
		},
		{
			name:      "handles spaces in existing list",
			existing:  `["apoc", "bloom"]`,
			newPlugin: "fleet-management",
			expected:  `["apoc","bloom","fleet-management"]`,
		},
		{
			name:      "empty string is treated as empty list",
			existing:  "",
			newPlugin: "apoc",
			expected:  `["apoc"]`,
		},
		{
			name:      "whitespace-only is treated as empty list",
			existing:  "   ",
			newPlugin: "apoc",
			expected:  `["apoc"]`,
		},
		{
			name:      "rejects non-JSON garbage (was silently parsed as a fake plugin previously)",
			existing:  "not a json array",
			newPlugin: "apoc",
			wantErr:   true,
		},
		{
			name:      "rejects malformed JSON",
			existing:  `["apoc",`,
			newPlugin: "fleet-management",
			wantErr:   true,
		},
		{
			name:      "rejects non-string element in JSON array",
			existing:  `["apoc", 123]`,
			newPlugin: "fleet-management",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeNeo4jPluginList(tt.existing, tt.newPlugin)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestIsMergeableCSVKey(t *testing.T) {
	// Iterate the production map directly so any new entry is auto-covered
	// without having to update this test.
	for k := range mergeableCSVKeys {
		t.Run("mergeable: "+k, func(t *testing.T) {
			assert.True(t, isMergeableCSVKey(k))
		})
	}

	notMergeable := []string{
		"my.custom.allowlist",                    // user-supplied with substring "allowlist"
		"NEO4J_MY_ALLOWLIST_SETTING",             // GHAS reviewer's example
		"dbms.security.procedures.allowlist.foo", // dotted suffix
		"some.random.unrestricted",               // user-supplied with substring "unrestricted"
		"dbms.security.procedures",               // prefix
		"",                                       // empty
	}
	for _, k := range notMergeable {
		t.Run("not mergeable: "+k, func(t *testing.T) {
			assert.False(t, isMergeableCSVKey(k))
		})
	}
}

func TestMergeCSV(t *testing.T) {
	cases := []struct {
		name     string
		a, b     string
		expected string
	}{
		{"both empty", "", "", ""},
		{"a empty", "", "x,y", "x,y"},
		{"b empty", "x,y", "", "x,y"},
		{"disjoint", "x,y", "z", "x,y,z"},
		{"overlap is deduped", "x,y", "y,z", "x,y,z"},
		{"identical inputs", "a,b,c", "a,b,c", "a,b,c"},
		{"whitespace trimmed", " x , y ", "y, z ", "x,y,z"},
		{"empty entries dropped", "x,,y", ",,z,,", "x,y,z"},
		{
			// The Bloom failure mode this change fixes: previously,
			// `env.Value + "," + value` would have produced
			// "/,/browser.*,/bloom.*,/,/browser.*,/bloom.*" on the
			// second reconcile. mergeCSV produces the union.
			name:     "bloom http_auth_allowlist roundtrip",
			a:        "/,/browser.*,/bloom.*",
			b:        "/,/browser.*,/bloom.*",
			expected: "/,/browser.*,/bloom.*",
		},
		{
			// Regex-style values (gds.*, apoc.*) appear in
			// `dbms.security.procedures.allowlist` / `unrestricted`.
			// mergeCSV treats `*` and `.` as ordinary bytes, so the
			// dedup is purely string-equality on the trimmed elements.
			name:     "regex-like overlap is deduped",
			a:        "gds.*,apoc.*",
			b:        "gds.*,n10s.*",
			expected: "gds.*,apoc.*,n10s.*",
		},
		{
			// Bloom adds path patterns to `dbms.security.http_auth_allowlist`.
			// Make sure the union is computed correctly when one side
			// brings overlapping path patterns and a bare `/`.
			name:     "path patterns merge with overlap",
			a:        "/browser.*",
			b:        "/bloom.*,/",
			expected: "/browser.*,/bloom.*,/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, mergeCSV(tc.a, tc.b))
		})
	}
}

// TestFleetManagementPluginType verifies that "fleet-management" is recognised as a
// Neo4j config plugin (not environment-variable-only) and gets the correct security settings.
func TestFleetManagementPluginType(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	t.Run("fleet-management is PluginTypeNeo4jConfig", func(t *testing.T) {
		pluginType := r.getPluginType("fleet-management")
		assert.Equal(t, PluginTypeNeo4jConfig, pluginType)
	})

	t.Run("fleet-management needs automatic security settings", func(t *testing.T) {
		assert.True(t, r.requiresAutomaticSecurityConfiguration("fleet-management"),
			"fleet-management should require automatic procedure security settings")
	})

	t.Run("fleet-management security settings are correct", func(t *testing.T) {
		settings := r.getRequiredProcedureSecuritySettings("fleet-management")
		assert.Equal(t, "fleetManagement.*", settings["dbms.security.procedures.unrestricted"])
		assert.Equal(t, "fleetManagement.*", settings["dbms.security.procedures.allowlist"])
	})

	t.Run("fleet-management maps to 'fleet-management' plugin name", func(t *testing.T) {
		assert.Equal(t, "fleet-management", r.mapPluginName("fleet-management"))
	})
}

func TestGetPluginType(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	tests := []struct {
		name     string
		plugin   string
		expected PluginType
	}{
		{"apoc", "apoc", PluginTypeEnvironmentOnly},
		{"apoc-extended", "apoc-extended", PluginTypeEnvironmentOnly},
		{"gds", "graph-data-science", PluginTypeNeo4jConfig},
		{"gds shorthand", "gds", PluginTypeNeo4jConfig},
		{"bloom", "bloom", PluginTypeNeo4jConfig},
		{"genai", "genai", PluginTypeNeo4jConfig},
		{"unknown", "some-custom-plugin", PluginTypeNeo4jConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, r.getPluginType(tt.plugin))
		})
	}
}

func TestIsEnvironmentVariableOnlyPlugin(t *testing.T) {
	r := &Neo4jPluginReconciler{}
	assert.True(t, r.isEnvironmentVariableOnlyPlugin("apoc"))
	assert.True(t, r.isEnvironmentVariableOnlyPlugin("apoc-extended"))
	assert.False(t, r.isEnvironmentVariableOnlyPlugin("gds"))
	assert.False(t, r.isEnvironmentVariableOnlyPlugin("bloom"))
	assert.False(t, r.isEnvironmentVariableOnlyPlugin("unknown"))
}

func TestRequiresAutomaticSecurityConfiguration(t *testing.T) {
	r := &Neo4jPluginReconciler{}
	assert.True(t, r.requiresAutomaticSecurityConfiguration("bloom"))
	assert.True(t, r.requiresAutomaticSecurityConfiguration("graph-data-science"))
	assert.True(t, r.requiresAutomaticSecurityConfiguration("gds"))
	assert.True(t, r.requiresAutomaticSecurityConfiguration("fleet-management"))
	assert.False(t, r.requiresAutomaticSecurityConfiguration("apoc"))
	assert.False(t, r.requiresAutomaticSecurityConfiguration("unknown"))
}

func TestGetAutomaticSecuritySettings(t *testing.T) {
	r := &Neo4jPluginReconciler{}

	t.Run("bloom settings", func(t *testing.T) {
		settings := r.getAutomaticSecuritySettings("bloom")
		assert.Contains(t, settings, "dbms.security.procedures.unrestricted")
		assert.Contains(t, settings["dbms.security.procedures.unrestricted"], "bloom.*")
		assert.Contains(t, settings, "server.unmanaged_extension_classes")
	})

	t.Run("gds settings", func(t *testing.T) {
		settings := r.getAutomaticSecuritySettings("graph-data-science")
		assert.Contains(t, settings, "dbms.security.procedures.unrestricted")
		assert.Contains(t, settings["dbms.security.procedures.unrestricted"], "gds.*")
	})

	t.Run("fleet-management settings", func(t *testing.T) {
		settings := r.getAutomaticSecuritySettings("fleet-management")
		assert.Contains(t, settings, "dbms.security.procedures.unrestricted")
		assert.Contains(t, settings, "dbms.security.procedures.allowlist")
	})

	t.Run("unknown returns empty map", func(t *testing.T) {
		settings := r.getAutomaticSecuritySettings("apoc")
		assert.Empty(t, settings)
	})
}

func TestIsSecuritySetting(t *testing.T) {
	assert.True(t, pluginConfKeyIsSecurity("dbms.security.procedures.unrestricted"))
	assert.True(t, pluginConfKeyIsSecurity("dbms.security.procedures.allowlist"))
	assert.True(t, pluginConfKeyIsSecurity("server.unmanaged_extension_classes"))
	assert.True(t, pluginConfKeyIsSecurity("dbms.bloom.license_file"))
	assert.False(t, pluginConfKeyIsSecurity("server.memory.heap.max_size"))
	assert.False(t, pluginConfKeyIsSecurity("gds.maxConcurrentRequests"))
}

func TestFilterNeo4jClientConfig(t *testing.T) {
	r := &Neo4jPluginReconciler{}
	config := map[string]string{
		"apoc.load.json":                     "true",  // should be filtered (apoc prefix)
		"gds.enterprise.license_file":        "/path", // should be filtered (non-dynamic)
		"dbms.security.procedures.allowlist": "gds.*", // should be filtered (non-dynamic)
		"gds.maxConcurrentRequests":          "4",     // should be kept (dynamic)
	}
	filtered := r.filterNeo4jClientConfig(config)
	assert.NotContains(t, filtered, "apoc.load.json")
	assert.NotContains(t, filtered, "gds.enterprise.license_file")
	assert.NotContains(t, filtered, "dbms.security.procedures.allowlist")
	assert.Contains(t, filtered, "gds.maxConcurrentRequests")
}

func TestHasRuntimeSecurityConfiguration(t *testing.T) {
	r := &Neo4jPluginReconciler{}
	// Currently always returns false per the code comment
	assert.False(t, r.hasRuntimeSecurityConfiguration(nil))
	assert.False(t, r.hasRuntimeSecurityConfiguration(&neo4jv1beta1.PluginSecurity{
		AllowedProcedures: []string{"gds.*"},
	}))
}
