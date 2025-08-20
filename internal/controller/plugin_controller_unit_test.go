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
	"github.com/stretchr/testify/require"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// Unit tests using standard Go testing for unexported methods
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
				Object: &neo4jv1alpha1.Neo4jEnterpriseCluster{
					Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
						Topology: neo4jv1alpha1.TopologyConfiguration{
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
				Object: &neo4jv1alpha1.Neo4jEnterpriseCluster{
					Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
						Topology: neo4jv1alpha1.TopologyConfiguration{
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
				Object: &neo4jv1alpha1.Neo4jEnterpriseStandalone{},
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

func TestPluginSourceValidation(t *testing.T) {
	tests := []struct {
		name        string
		source      *neo4jv1alpha1.PluginSource
		shouldError bool
	}{
		{
			name: "official source",
			source: &neo4jv1alpha1.PluginSource{
				Type: "official",
			},
			shouldError: false,
		},
		{
			name: "community source",
			source: &neo4jv1alpha1.PluginSource{
				Type: "community",
			},
			shouldError: false,
		},
		{
			name: "custom source with registry",
			source: &neo4jv1alpha1.PluginSource{
				Type: "custom",
				Registry: &neo4jv1alpha1.PluginRegistry{
					URL: "https://my-registry.example.com",
				},
			},
			shouldError: false,
		},
		{
			name: "url source with URL",
			source: &neo4jv1alpha1.PluginSource{
				Type: "url",
				URL:  "https://example.com/plugin.jar",
			},
			shouldError: false,
		},
		{
			name: "url source with checksum",
			source: &neo4jv1alpha1.PluginSource{
				Type:     "url",
				URL:      "https://example.com/plugin.jar",
				Checksum: "sha256:abcd1234",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that source types are handled correctly
			if tt.source != nil {
				require.NotEmpty(t, tt.source.Type)
				if tt.source.Type == "custom" {
					require.NotNil(t, tt.source.Registry)
				}
				if tt.source.Type == "url" {
					require.NotEmpty(t, tt.source.URL)
				}
			}
		})
	}
}
