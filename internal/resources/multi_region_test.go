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

package resources

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestBuildServerTagsConfig(t *testing.T) {
	tests := []struct {
		name          string
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectContent []string
	}{
		{
			name: "cluster with server groups and tags",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 6,
						ServerGroups: []neo4jv1alpha1.ServerGroup{
							{
								Name:       "group1",
								Count:      3,
								ServerTags: []string{"region-a", "primary"},
							},
							{
								Name:       "group2",
								Count:      3,
								ServerTags: []string{"region-b", "secondary"},
							},
						},
					},
				},
			},
			expectContent: []string{
				"SERVER_TAGS=\"region-a,primary\"",
				"SERVER_TAGS=\"region-b,secondary\"",
				"server.tags=$SERVER_TAGS",
			},
		},
		{
			name: "cluster without server groups",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			expectContent: []string{},
		},
		{
			name: "cluster with empty server groups",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers:      3,
						ServerGroups: []neo4jv1alpha1.ServerGroup{},
					},
				},
			},
			expectContent: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildServerTagsConfig(tt.cluster)

			if len(tt.expectContent) == 0 {
				if result != "" {
					t.Errorf("Expected empty config but got: %s", result)
				}
				return
			}

			for _, expected := range tt.expectContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected config to contain '%s' but got: %s", expected, result)
				}
			}
		})
	}
}

func TestBuildAntiAffinityForCluster(t *testing.T) {
	tests := []struct {
		name        string
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectNil   bool
		expectHard  bool
		expectSoft  bool
		topologyKey string
	}{
		{
			name: "no anti-affinity configured",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			expectNil: true,
		},
		{
			name: "zone anti-affinity required",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						AntiAffinity: &neo4jv1alpha1.AntiAffinitySpec{
							Type:     "zone",
							Required: true,
						},
					},
				},
			},
			expectHard:  true,
			topologyKey: "topology.kubernetes.io/zone",
		},
		{
			name: "region anti-affinity preferred",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						AntiAffinity: &neo4jv1alpha1.AntiAffinitySpec{
							Type:     "region",
							Required: false,
							Weight:   80,
						},
					},
				},
			},
			expectSoft:  true,
			topologyKey: "topology.kubernetes.io/region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAntiAffinityForCluster(tt.cluster)

			if tt.expectNil {
				if result != nil {
					t.Errorf("Expected nil affinity but got: %v", result)
				}
				return
			}

			if result == nil || result.PodAntiAffinity == nil {
				t.Errorf("Expected anti-affinity configuration but got nil")
				return
			}

			if tt.expectHard {
				if len(result.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) == 0 {
					t.Errorf("Expected hard anti-affinity but got none")
				} else {
					term := result.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0]
					if term.TopologyKey != tt.topologyKey {
						t.Errorf("Expected topology key %s but got %s", tt.topologyKey, term.TopologyKey)
					}
				}
			}

			if tt.expectSoft {
				if len(result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
					t.Errorf("Expected soft anti-affinity but got none")
				} else {
					term := result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0]
					if term.PodAffinityTerm.TopologyKey != tt.topologyKey {
						t.Errorf("Expected topology key %s but got %s", tt.topologyKey, term.PodAffinityTerm.TopologyKey)
					}
				}
			}
		})
	}
}

func TestBuildNeo4jConfigForEnterpriseRouting(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "1Gi",
			},
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
			Routing: &neo4jv1alpha1.RoutingSpec{
				LoadBalancingPolicy: "server_policies",
				Policies: map[string]string{
					"primary-only":  "tags(primary)->min(2);halt();",
					"read-replicas": "tags(secondary);all();",
				},
				DefaultPolicy:   "primary-only",
				CatchupStrategy: "CONNECT_RANDOMLY_TO_PRIMARY_SERVER",
			},
		},
	}

	config := buildNeo4jConfigForEnterprise(cluster)

	expectedContent := []string{
		"dbms.routing.load_balancing.plugin=server_policies",
		"dbms.routing.load_balancing.config.server_policies.primary-only=tags(primary)->min(2);halt();",
		"dbms.routing.load_balancing.config.server_policies.read-replicas=tags(secondary);all();",
		"dbms.routing.default_policy=primary-only",
		"server.cluster.catchup.upstream_strategy=CONNECT_RANDOMLY_TO_PRIMARY_SERVER",
	}

	for _, expected := range expectedContent {
		if !strings.Contains(config, expected) {
			t.Errorf("Expected config to contain '%s' but it was missing", expected)
		}
	}
}
