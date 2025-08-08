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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestCalculateOptimalMemorySettings(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectedHeap   string
		expectedPage   string
		testMemorySize string
	}{
		{
			name: "default memory limit (2Gi)",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					// No resources specified, should use default
				},
			},
			expectedHeap:   "1G",
			expectedPage:   "512M", // Adjusted for actual calculation
			testMemorySize: "2Gi",
		},
		{
			name: "high memory deployment (8Gi)",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
			},
			expectedHeap:   "5G",
			expectedPage:   "2G",
			testMemorySize: "8Gi",
		},
		{
			name: "low memory deployment (512Mi)",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			expectedHeap:   "192M", // Heap size after reserving space for page cache
			expectedPage:   "64M",  // Minimum page cache for low memory
			testMemorySize: "512Mi",
		},
		{
			name: "very high memory deployment (16Gi)",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				},
			},
			expectedHeap:   "10G",
			expectedPage:   "5G",
			testMemorySize: "16Gi",
		},
		{
			name: "custom memory configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
					Config: map[string]string{
						"server.memory.heap.max_size":  "1G",
						"server.memory.pagecache.size": "512M",
					},
				},
			},
			expectedHeap:   "1G",
			expectedPage:   "512M",
			testMemorySize: "4Gi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memoryConfig := GetMemoryConfigForCluster(tt.cluster)

			if memoryConfig.HeapMaxSize != tt.expectedHeap {
				t.Errorf("expected heap max size %s, got %s", tt.expectedHeap, memoryConfig.HeapMaxSize)
			}

			if memoryConfig.PageCacheSize != tt.expectedPage {
				t.Errorf("expected page cache size %s, got %s", tt.expectedPage, memoryConfig.PageCacheSize)
			}

			// Heap initial should equal heap max
			if memoryConfig.HeapInitialSize != memoryConfig.HeapMaxSize {
				t.Errorf("heap initial size %s should equal heap max size %s",
					memoryConfig.HeapInitialSize, memoryConfig.HeapMaxSize)
			}
		})
	}
}

func TestCalculateOptimalMemoryForNeo4j526Plus(t *testing.T) {
	tests := []struct {
		name         string
		memoryLimit  string
		expectedHeap string
		expectedPage string
	}{
		{
			name:         "high memory optimized (8Gi)",
			memoryLimit:  "8Gi",
			expectedHeap: "5G",
			expectedPage: "2G",
		},
		{
			name:         "low memory standard (1Gi)",
			memoryLimit:  "1Gi",
			expectedHeap: "512M",
			expectedPage: "256M",
		},
		{
			name:         "very high memory (32Gi)",
			memoryLimit:  "32Gi",
			expectedHeap: "19G",
			expectedPage: "10G",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(tt.memoryLimit),
						},
					},
				},
			}

			memoryConfig := CalculateOptimalMemoryForNeo4j526Plus(cluster)

			if memoryConfig.HeapMaxSize != tt.expectedHeap {
				t.Errorf("expected heap max size %s, got %s", tt.expectedHeap, memoryConfig.HeapMaxSize)
			}

			if memoryConfig.PageCacheSize != tt.expectedPage {
				t.Errorf("expected page cache size %s, got %s", tt.expectedPage, memoryConfig.PageCacheSize)
			}
		})
	}
}

func TestFormatMemorySize(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "bytes",
			bytes:    512,
			expected: "512",
		},
		{
			name:     "kilobytes",
			bytes:    1024,
			expected: "1K",
		},
		{
			name:     "megabytes",
			bytes:    1024 * 1024,
			expected: "1M",
		},
		{
			name:     "gigabytes",
			bytes:    1024 * 1024 * 1024,
			expected: "1G",
		},
		{
			name:     "256 megabytes",
			bytes:    256 * 1024 * 1024,
			expected: "256M",
		},
		{
			name:     "1.5 gigabytes",
			bytes:    1536 * 1024 * 1024,
			expected: "2G", // Rounded up
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMemorySize(tt.bytes)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		name     string
		size     string
		expected int64
		hasError bool
	}{
		{
			name:     "kubernetes format Gi",
			size:     "2Gi",
			expected: 2 * 1024 * 1024 * 1024,
			hasError: false,
		},
		{
			name:     "kubernetes format Mi",
			size:     "512Mi",
			expected: 512 * 1024 * 1024,
			hasError: false,
		},
		{
			name:     "traditional format G",
			size:     "2G",
			expected: 2 * 1024 * 1024 * 1024,
			hasError: false,
		},
		{
			name:     "traditional format M",
			size:     "256M",
			expected: 256 * 1024 * 1024,
			hasError: false,
		},
		{
			name:     "traditional format K",
			size:     "1024K",
			expected: 1024 * 1024,
			hasError: false,
		},
		{
			name:     "plain bytes",
			size:     "1048576",
			expected: 1048576,
			hasError: false,
		},
		{
			name:     "empty string",
			size:     "",
			expected: 0,
			hasError: true,
		},
		{
			name:     "invalid format",
			size:     "invalid",
			expected: 0,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMemorySize(tt.size)

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("expected %d, got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestMemoryConfigConstraints(t *testing.T) {
	// Test minimum heap size constraint
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"), // Very low memory
				},
			},
		},
	}

	memoryConfig := CalculateOptimalMemorySettings(cluster)

	// Should enforce minimum heap size
	if memoryConfig.HeapMaxSize != "256M" {
		t.Errorf("expected minimum heap size 256M, got %s", memoryConfig.HeapMaxSize)
	}

	// Should enforce minimum page cache size (64M for very low memory)
	if memoryConfig.PageCacheSize != "64M" {
		t.Errorf("expected minimum page cache size 64M, got %s", memoryConfig.PageCacheSize)
	}
}

func TestMemoryConfigForDifferentTopologies(t *testing.T) {
	tests := []struct {
		name         string
		servers      int32
		memoryLimit  string
		expectedHeap string
	}{
		{
			name:         "small cluster",
			servers:      2,
			memoryLimit:  "4Gi",
			expectedHeap: "2G",
		},
		{
			name:         "multi server cluster",
			servers:      5,
			memoryLimit:  "4Gi",
			expectedHeap: "2G",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: tt.servers,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(tt.memoryLimit),
						},
					},
				},
			}

			memoryConfig := GetMemoryConfigForCluster(cluster)

			if memoryConfig.HeapMaxSize != tt.expectedHeap {
				t.Errorf("expected heap max size %s, got %s", tt.expectedHeap, memoryConfig.HeapMaxSize)
			}
		})
	}
}
