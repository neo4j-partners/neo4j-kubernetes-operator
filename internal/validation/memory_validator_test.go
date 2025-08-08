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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestMemoryValidator_Validate(t *testing.T) {
	validator := NewMemoryValidator()

	tests := []struct {
		name          string
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
		wantErrorsLen int
		wantErrorMsg  string
	}{
		{
			name: "valid memory configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
					Config: map[string]string{
						"server.memory.heap.max_size":  "2g",
						"server.memory.pagecache.size": "1g",
					},
				},
			},
			wantErrorsLen: 0,
		},
		{
			name: "memory configuration exceeds container limits",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Config: map[string]string{
						"server.memory.heap.max_size":  "4g",
						"server.memory.pagecache.size": "2g",
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "exceeds container memory limit",
		},
		{
			name: "insufficient memory for cluster size",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 7, // 5 + 2
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "insufficient",
		},
		{
			name: "heap size too small",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
					Config: map[string]string{
						"server.memory.heap.max_size": "100m",
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "heap size must be at least",
		},
		{
			name: "no resources specified",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 1, // Invalid - needs at least 2
					},
				},
			},
			wantErrorsLen: 0, // Should not error when no resources specified
		},
		{
			name: "transaction memory exceeds heap size",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
					Config: map[string]string{
						"server.memory.heap.max_size":       "2g",
						"server.memory.pagecache.size":      "1g",
						"dbms.memory.transaction.total.max": "3g", // Exceeds heap size
					},
				},
			},
			wantErrorsLen: 1,
			wantErrorMsg:  "transaction memory", // Should contain transaction memory validation
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
					if strings.Contains(err.Error(), tt.wantErrorMsg) {
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

func TestMemoryValidator_parseMemoryToBytes(t *testing.T) {
	validator := NewMemoryValidator()

	tests := []struct {
		name      string
		input     string
		want      int64
		wantError bool
	}{
		{
			name:      "gigabytes",
			input:     "2g",
			want:      2 * 1024 * 1024 * 1024,
			wantError: false,
		},
		{
			name:      "megabytes",
			input:     "512m",
			want:      512 * 1024 * 1024,
			wantError: false,
		},
		{
			name:      "kubernetes format",
			input:     "2Gi",
			want:      2 * 1024 * 1024 * 1024,
			wantError: false,
		},
		{
			name:      "invalid format",
			input:     "invalid",
			want:      0,
			wantError: true,
		},
		{
			name:      "empty string",
			input:     "",
			want:      0,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validator.parseMemoryToBytes(tt.input)

			if (err != nil) != tt.wantError {
				t.Errorf("parseMemoryToBytes() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if got != tt.want {
				t.Errorf("parseMemoryToBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryValidator_formatMemorySize(t *testing.T) {
	validator := NewMemoryValidator()

	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{
			name:  "gigabytes",
			bytes: 2 * 1024 * 1024 * 1024,
			want:  "2.0Gi",
		},
		{
			name:  "megabytes",
			bytes: 512 * 1024 * 1024,
			want:  "512.0Mi",
		},
		{
			name:  "kilobytes",
			bytes: 1024,
			want:  "1.0Ki",
		},
		{
			name:  "bytes",
			bytes: 500,
			want:  "500b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validator.formatMemorySize(tt.bytes)
			if got != tt.want {
				t.Errorf("formatMemorySize() = %v, want %v", got, tt.want)
			}
		})
	}
}
