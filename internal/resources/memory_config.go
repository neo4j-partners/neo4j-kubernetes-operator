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
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	// MinHeapSize is the minimum heap size for Neo4j
	MinHeapSize = 256 * 1024 * 1024 // 256MB in bytes
	// MaxHeapSize is the maximum heap size for Neo4j
	MaxHeapSize = 31 * 1024 * 1024 * 1024 // 31GB in bytes (compressed OOPs limit)
	// DefaultHeapPercentage is the default percentage of container memory to allocate to heap
	DefaultHeapPercentage = 0.5 // 50% of container memory
	// DefaultPageCachePercentage is the default percentage of container memory to allocate to page cache
	DefaultPageCachePercentage = 0.25 // 25% of container memory
	// MinPageCacheSize is the minimum page cache size
	MinPageCacheSize = 128 * 1024 * 1024 // 128MB in bytes
	// SystemMemoryReserved is the base amount of memory to reserve for system processes
	SystemMemoryReserved = 512 * 1024 * 1024 // 512MB in bytes
	// MinSystemMemoryReserved is the minimum amount to reserve for system processes
	MinSystemMemoryReserved = 256 * 1024 * 1024 // 256MB in bytes
)

// MemoryConfig represents Neo4j memory configuration
type MemoryConfig struct {
	HeapInitialSize string
	HeapMaxSize     string
	PageCacheSize   string
}

// calculateSystemMemoryReserved calculates system memory reservation based on container size
func calculateSystemMemoryReserved(memoryBytes int64) int64 {
	// For smaller containers, use a smaller system reservation
	if memoryBytes <= 2*1024*1024*1024 { // <= 2GB
		return MinSystemMemoryReserved // 256MB
	}
	return SystemMemoryReserved // 512MB for larger containers
}

// CalculateOptimalMemorySettings calculates optimal memory settings based on container resources
func CalculateOptimalMemorySettings(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) MemoryConfig {
	var memoryLimit resource.Quantity

	// Get memory limit from cluster spec or use default
	if cluster.Spec.Resources != nil && cluster.Spec.Resources.Limits != nil {
		if limit, exists := cluster.Spec.Resources.Limits[corev1.ResourceMemory]; exists {
			memoryLimit = limit
		}
	}

	// If no memory limit specified, use default
	if memoryLimit.IsZero() {
		memoryLimit = resource.MustParse(DefaultMemoryLimit)
	}

	// Convert to bytes
	memoryBytes := memoryLimit.Value()

	// Calculate heap size (50% of container memory, with min/max constraints)
	heapBytes := int64(float64(memoryBytes) * DefaultHeapPercentage)

	// Apply constraints
	if heapBytes < MinHeapSize {
		heapBytes = MinHeapSize
	}
	if heapBytes > MaxHeapSize {
		heapBytes = MaxHeapSize
	}

	// Calculate page cache size (25% of container memory, with minimum)
	pageCacheBytes := int64(float64(memoryBytes) * DefaultPageCachePercentage)
	if pageCacheBytes < MinPageCacheSize {
		pageCacheBytes = MinPageCacheSize
	}

	// Calculate system memory reservation based on container size
	systemReserved := calculateSystemMemoryReserved(memoryBytes)

	// Ensure total memory usage doesn't exceed container limit
	totalAllocated := heapBytes + pageCacheBytes + systemReserved
	if totalAllocated > memoryBytes {
		// For very small containers, prioritize heap over page cache
		availableMemory := memoryBytes - systemReserved
		if availableMemory <= MinHeapSize+MinPageCacheSize {
			// Very constrained memory - use minimum values
			heapBytes = MinHeapSize
			pageCacheBytes = MinPageCacheSize
			// Check if even minimums exceed available memory
			if heapBytes+pageCacheBytes > availableMemory {
				// In extreme cases, reduce page cache to make room for heap
				pageCacheBytes = availableMemory - heapBytes
				if pageCacheBytes <= 0 {
					pageCacheBytes = 64 * 1024 * 1024 // 64MB absolute minimum
					heapBytes = availableMemory - pageCacheBytes
					if heapBytes < 0 {
						heapBytes = MinHeapSize
						pageCacheBytes = 64 * 1024 * 1024 // Keep absolute minimum
					}
				}
			}
		} else {
			// Reduce page cache first
			pageCacheBytes = availableMemory - heapBytes
			if pageCacheBytes < MinPageCacheSize {
				// If still not enough, reduce heap
				heapBytes = availableMemory - MinPageCacheSize
				pageCacheBytes = MinPageCacheSize
			}
		}
	}

	// Ensure all values are non-negative
	if heapBytes < 0 {
		heapBytes = MinHeapSize
	}
	if pageCacheBytes < 0 {
		pageCacheBytes = 64 * 1024 * 1024 // 64MB minimum
	}

	return MemoryConfig{
		HeapInitialSize: formatMemorySize(heapBytes),
		HeapMaxSize:     formatMemorySize(heapBytes),
		PageCacheSize:   formatMemorySize(pageCacheBytes),
	}
}

// CalculateOptimalMemoryForNeo4j526Plus calculates memory settings optimized for Neo4j 5.26+
func CalculateOptimalMemoryForNeo4j526Plus(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) MemoryConfig {
	baseConfig := CalculateOptimalMemorySettings(cluster)

	// Neo4j 5.26+ optimizations
	var memoryLimit resource.Quantity
	if cluster.Spec.Resources != nil && cluster.Spec.Resources.Limits != nil {
		if limit, exists := cluster.Spec.Resources.Limits[corev1.ResourceMemory]; exists {
			memoryLimit = limit
		}
	}

	if memoryLimit.IsZero() {
		memoryLimit = resource.MustParse(DefaultMemoryLimit)
	}

	memoryBytes := memoryLimit.Value()

	// For Neo4j 5.26+, we can be more aggressive with memory allocation
	// due to improved memory management and garbage collection

	// Check if this is a high-memory deployment (>= 4GB)
	if memoryBytes >= 4*1024*1024*1024 {
		// For high-memory deployments, use 60% for heap and 30% for page cache
		heapBytes := int64(float64(memoryBytes) * 0.6)
		pageCacheBytes := int64(float64(memoryBytes) * 0.3)

		// Apply constraints
		if heapBytes > MaxHeapSize {
			heapBytes = MaxHeapSize
		}
		if heapBytes < MinHeapSize {
			heapBytes = MinHeapSize
		}
		if pageCacheBytes < MinPageCacheSize {
			pageCacheBytes = MinPageCacheSize
		}

		// Calculate system memory reservation based on container size
		systemReserved := calculateSystemMemoryReserved(memoryBytes)

		// Ensure total doesn't exceed limit
		totalAllocated := heapBytes + pageCacheBytes + systemReserved
		if totalAllocated > memoryBytes {
			// Proportionally reduce both heap and page cache
			ratio := float64(memoryBytes-systemReserved) / float64(heapBytes+pageCacheBytes)
			heapBytes = int64(float64(heapBytes) * ratio)
			pageCacheBytes = int64(float64(pageCacheBytes) * ratio)
		}

		baseConfig.HeapInitialSize = formatMemorySize(heapBytes)
		baseConfig.HeapMaxSize = formatMemorySize(heapBytes)
		baseConfig.PageCacheSize = formatMemorySize(pageCacheBytes)
	}

	return baseConfig
}

// formatMemorySize converts bytes to human-readable format with appropriate units
func formatMemorySize(bytes int64) string {
	// Ensure non-negative values
	if bytes < 0 {
		bytes = 0
	}

	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	if bytes >= GB {
		return fmt.Sprintf("%.0fG", float64(bytes)/GB)
	} else if bytes >= MB {
		return fmt.Sprintf("%.0fM", float64(bytes)/MB)
	} else if bytes >= KB {
		return fmt.Sprintf("%.0fK", float64(bytes)/KB)
	}

	return fmt.Sprintf("%d", bytes)
}

// parseMemorySize parses memory size strings like "2Gi", "512Mi", "1G", "256M"
func parseMemorySize(size string) (int64, error) {
	if size == "" {
		return 0, fmt.Errorf("empty memory size")
	}

	// Handle Kubernetes resource quantities
	if strings.HasSuffix(size, "i") {
		quantity, err := resource.ParseQuantity(size)
		if err != nil {
			return 0, err
		}
		return quantity.Value(), nil
	}

	// Handle traditional memory units
	size = strings.ToUpper(size)
	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(size, "G"):
		multiplier = 1024 * 1024 * 1024
		numStr = size[:len(size)-1]
	case strings.HasSuffix(size, "M"):
		multiplier = 1024 * 1024
		numStr = size[:len(size)-1]
	case strings.HasSuffix(size, "K"):
		multiplier = 1024
		numStr = size[:len(size)-1]
	default:
		numStr = size
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, err
	}

	return int64(num * float64(multiplier)), nil
}

// GetMemoryConfigForCluster returns memory configuration for the cluster
func GetMemoryConfigForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) MemoryConfig {
	// Check if custom memory configuration is provided
	if cluster.Spec.Config != nil {
		// Check for custom heap settings
		if heapMax, exists := cluster.Spec.Config["server.memory.heap.max_size"]; exists {
			config := MemoryConfig{
				HeapMaxSize: heapMax,
			}

			// Use heap max as initial size if not specified
			if heapInitial, exists := cluster.Spec.Config["server.memory.heap.initial_size"]; exists {
				config.HeapInitialSize = heapInitial
			} else {
				config.HeapInitialSize = heapMax
			}

			// Use custom page cache if specified, otherwise calculate
			if pageCache, exists := cluster.Spec.Config["server.memory.pagecache.size"]; exists {
				config.PageCacheSize = pageCache
			} else {
				// Calculate page cache based on heap size
				if heapBytes, err := parseMemorySize(heapMax); err == nil {
					pageCacheBytes := heapBytes / 2 // 50% of heap for page cache
					if pageCacheBytes < MinPageCacheSize {
						pageCacheBytes = MinPageCacheSize
					}
					config.PageCacheSize = formatMemorySize(pageCacheBytes)
				} else {
					config.PageCacheSize = "128M"
				}
			}

			return config
		}
	}

	// Use optimized settings for Neo4j 5.26+
	return CalculateOptimalMemoryForNeo4j526Plus(cluster)
}
