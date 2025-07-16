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
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
)

// MemoryValidator validates Neo4j memory configuration against Kubernetes resource limits
type MemoryValidator struct {
	recommender *resources.ResourceRecommender
}

// NewMemoryValidator creates a new memory validator
func NewMemoryValidator() *MemoryValidator {
	return &MemoryValidator{
		recommender: resources.NewResourceRecommender(),
	}
}

// Validate validates memory configuration consistency
func (v *MemoryValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	if cluster.Spec.Resources == nil {
		return allErrs
	}

	memoryLimit := cluster.Spec.Resources.Limits.Memory()
	if memoryLimit == nil {
		return allErrs
	}

	containerMemoryBytes := memoryLimit.Value()

	// Check Neo4j memory settings against container limits
	neo4jHeap := cluster.Spec.Config["server.memory.heap.max_size"]
	neo4jPageCache := cluster.Spec.Config["server.memory.pagecache.size"]

	if neo4jHeap != "" || neo4jPageCache != "" {
		allErrs = append(allErrs, v.validateNeo4jMemorySettings(cluster, containerMemoryBytes, neo4jHeap, neo4jPageCache)...)
	}

	// Only validate memory allocation and provide recommendations if there are no critical errors
	if len(allErrs) == 0 {
		allErrs = append(allErrs, v.validateMemoryAllocation(cluster, containerMemoryBytes)...)
	}

	return allErrs
}

// validateNeo4jMemorySettings validates explicit Neo4j memory configuration
func (v *MemoryValidator) validateNeo4jMemorySettings(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, containerMemoryBytes int64, neo4jHeap, neo4jPageCache string) field.ErrorList {
	var allErrs field.ErrorList

	var totalNeo4jMemory int64
	var heapBytes, pageCacheBytes int64
	var err error

	if neo4jHeap != "" {
		heapBytes, err = v.parseMemoryToBytes(neo4jHeap)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "config", "server.memory.heap.max_size"),
				neo4jHeap,
				fmt.Sprintf("invalid memory format: %v", err),
			))
			return allErrs
		}
		totalNeo4jMemory += heapBytes
	}

	if neo4jPageCache != "" {
		pageCacheBytes, err = v.parseMemoryToBytes(neo4jPageCache)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "config", "server.memory.pagecache.size"),
				neo4jPageCache,
				fmt.Sprintf("invalid memory format: %v", err),
			))
			return allErrs
		}
		totalNeo4jMemory += pageCacheBytes
	}

	// Add system memory overhead (typically 512MB-1GB)
	systemOverhead := containerMemoryBytes / 4 // 25% for system overhead
	if systemOverhead < 512*1024*1024 {        // Minimum 512MB
		systemOverhead = 512 * 1024 * 1024
	}
	totalNeo4jMemory += systemOverhead

	if totalNeo4jMemory > containerMemoryBytes {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "config"),
			cluster.Spec.Config,
			fmt.Sprintf("Neo4j memory configuration (heap: %s, pagecache: %s) plus system overhead exceeds container memory limit (%s). Consider reducing memory settings or increasing container limits.",
				v.formatMemorySize(heapBytes),
				v.formatMemorySize(pageCacheBytes),
				v.formatMemorySize(containerMemoryBytes),
			),
		))
	}

	// Validate minimum heap size
	minHeapSize := int64(256 * 1024 * 1024) // 256MB
	if heapBytes > 0 && heapBytes < minHeapSize {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "config", "server.memory.heap.max_size"),
			neo4jHeap,
			fmt.Sprintf("heap size must be at least %s", v.formatMemorySize(minHeapSize)),
		))
	}

	// Validate minimum page cache size
	minPageCacheSize := int64(128 * 1024 * 1024) // 128MB
	if pageCacheBytes > 0 && pageCacheBytes < minPageCacheSize {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "config", "server.memory.pagecache.size"),
			neo4jPageCache,
			fmt.Sprintf("page cache size must be at least %s", v.formatMemorySize(minPageCacheSize)),
		))
	}

	return allErrs
}

// validateMemoryAllocation validates overall memory allocation strategy
func (v *MemoryValidator) validateMemoryAllocation(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, containerMemoryBytes int64) field.ErrorList {
	var allErrs field.ErrorList

	// Validate minimum container memory for Neo4j Enterprise
	minContainerMemory := int64(1024 * 1024 * 1024) // 1GB
	if containerMemoryBytes < minContainerMemory {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "resources", "limits", "memory"),
			v.formatMemorySize(containerMemoryBytes),
			fmt.Sprintf("Neo4j Enterprise requires at least %s of memory", v.formatMemorySize(minContainerMemory)),
		))
	}

	// Check if current memory is critically below minimum operational requirements
	// Only enforce if memory is below absolute minimum (1Gi) rather than recommendation-based
	minOperationalMemory := int64(1024 * 1024 * 1024) // 1GB minimum for basic operation
	if containerMemoryBytes < minOperationalMemory {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "resources", "limits", "memory"),
			v.formatMemorySize(containerMemoryBytes),
			fmt.Sprintf("Memory allocation is below minimum operational requirement. Current: %s, Required minimum: %s",
				v.formatMemorySize(containerMemoryBytes), v.formatMemorySize(minOperationalMemory)),
		))
	}

	// Check if memory is sufficient for cluster size
	totalNodes := cluster.Spec.Topology.Primaries + cluster.Spec.Topology.Secondaries
	if totalNodes > 3 {
		// For larger clusters (>3 nodes), require at least 2GB per node for basic operation
		minMemoryForClusterSize := int64(2 * 1024 * 1024 * 1024) // 2GB minimum for larger clusters
		if containerMemoryBytes < minMemoryForClusterSize {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "resources", "limits", "memory"),
				v.formatMemorySize(containerMemoryBytes),
				fmt.Sprintf("insufficient memory for cluster size (%d nodes). Current: %s, Required minimum: %s",
					totalNodes, v.formatMemorySize(containerMemoryBytes), v.formatMemorySize(minMemoryForClusterSize)),
			))
		}
	}

	// Recommendations are now advisory only - logged as events, not validation errors

	// Log optimization tips as events instead of validation errors
	// We'll emit these as events in the controller rather than blocking validation

	return allErrs
}

// GetOptimizationTips returns optimization tips for the cluster configuration
func (v *MemoryValidator) GetOptimizationTips(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) []string {
	recommendation := v.recommender.RecommendResourcesForTopology(cluster.Spec.Topology, cluster.Spec.Resources)
	return recommendation.OptimizationTips
}

// parseMemoryToBytes converts memory string to bytes
func (v *MemoryValidator) parseMemoryToBytes(memoryStr string) (int64, error) {
	if memoryStr == "" {
		return 0, nil
	}

	// Handle Neo4j format (e.g., "1g", "512m")
	memoryStr = strings.ToLower(strings.TrimSpace(memoryStr))

	var multiplier int64
	var numStr string

	if strings.HasSuffix(memoryStr, "g") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(memoryStr, "g")
	} else if strings.HasSuffix(memoryStr, "m") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(memoryStr, "m")
	} else if strings.HasSuffix(memoryStr, "k") {
		multiplier = 1024
		numStr = strings.TrimSuffix(memoryStr, "k")
	} else if strings.HasSuffix(memoryStr, "gi") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(memoryStr, "gi")
	} else if strings.HasSuffix(memoryStr, "mi") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(memoryStr, "mi")
	} else if strings.HasSuffix(memoryStr, "ki") {
		multiplier = 1024
		numStr = strings.TrimSuffix(memoryStr, "ki")
	} else {
		// Try parsing as Kubernetes resource quantity
		quantity, err := resource.ParseQuantity(memoryStr)
		if err != nil {
			return 0, fmt.Errorf("invalid memory format: %s", memoryStr)
		}
		return quantity.Value(), nil
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory format: %s", memoryStr)
	}

	return int64(num * float64(multiplier)), nil
}

// formatMemorySize formats bytes to human-readable string
func (v *MemoryValidator) formatMemorySize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fMi", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fKi", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%db", bytes)
	}
}
