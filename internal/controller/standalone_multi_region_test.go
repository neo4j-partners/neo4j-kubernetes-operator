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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestStandaloneCreateConfigMapWithTags(t *testing.T) {
	reconciler := &Neo4jEnterpriseStandaloneReconciler{}

	standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-standalone",
			Namespace: "test-ns",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
			ServerTags: []string{"edge", "region-a", "standalone"},
		},
	}

	configMap := reconciler.createConfigMap(standalone)

	if configMap == nil {
		t.Fatal("Expected ConfigMap but got nil")
	}

	neo4jConf, exists := configMap.Data["neo4j.conf"]
	if !exists {
		t.Fatal("Expected neo4j.conf in ConfigMap data")
	}

	expectedTags := "server.tags=edge,region-a,standalone"
	if !strings.Contains(neo4jConf, expectedTags) {
		t.Errorf("Expected config to contain '%s' but got: %s", expectedTags, neo4jConf)
	}
}

func TestStandaloneBuildNodeSelector(t *testing.T) {
	reconciler := &Neo4jEnterpriseStandaloneReconciler{}

	tests := []struct {
		name       string
		standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone
		expected   map[string]string
	}{
		{
			name: "with placement zone and region",
			standalone: &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					NodeSelector: map[string]string{
						"custom": "value",
					},
					Placement: &neo4jv1alpha1.StandalonePlacementSpec{
						Zone:   "us-east-1a",
						Region: "us-east-1",
					},
				},
			},
			expected: map[string]string{
				"custom":                        "value",
				"topology.kubernetes.io/zone":   "us-east-1a",
				"topology.kubernetes.io/region": "us-east-1",
			},
		},
		{
			name: "without placement",
			standalone: &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					NodeSelector: map[string]string{
						"custom": "value",
					},
				},
			},
			expected: map[string]string{
				"custom": "value",
			},
		},
		{
			name: "no node selector",
			standalone: &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.buildNodeSelector(tt.standalone)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil but got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d entries but got %d", len(tt.expected), len(result))
				return
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("Expected key %s to have value %s but got %s", k, v, result[k])
				}
			}
		})
	}
}

func TestStandaloneBuildAffinity(t *testing.T) {
	reconciler := &Neo4jEnterpriseStandaloneReconciler{}

	tests := []struct {
		name       string
		standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone
		expectNil  bool
		expectHard bool
		expectSoft bool
	}{
		{
			name: "no anti-affinity configured",
			standalone: &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{},
			},
			expectNil: true,
		},
		{
			name: "hard anti-affinity",
			standalone: &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Placement: &neo4jv1alpha1.StandalonePlacementSpec{
						AntiAffinity: &neo4jv1alpha1.AntiAffinitySpec{
							Type:     "zone",
							Required: true,
						},
					},
				},
			},
			expectHard: true,
		},
		{
			name: "soft anti-affinity",
			standalone: &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Placement: &neo4jv1alpha1.StandalonePlacementSpec{
						AntiAffinity: &neo4jv1alpha1.AntiAffinitySpec{
							Type:     "node",
							Required: false,
							Weight:   50,
						},
					},
				},
			},
			expectSoft: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.buildAffinity(tt.standalone)

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
				}
			}

			if tt.expectSoft {
				if len(result.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
					t.Errorf("Expected soft anti-affinity but got none")
				}
			}
		})
	}
}
