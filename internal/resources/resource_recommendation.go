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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// ResourceRecommendation provides intelligent resource suggestions
type ResourceRecommendation struct {
	RecommendedMemoryLimit   string                       `json:"recommendedMemoryLimit"`
	RecommendedMemoryRequest string                       `json:"recommendedMemoryRequest"`
	RecommendedCPULimit      string                       `json:"recommendedCPULimit"`
	RecommendedCPURequest    string                       `json:"recommendedCPURequest"`
	Neo4jHeapSize            string                       `json:"neo4jHeapSize"`
	Neo4jPageCacheSize       string                       `json:"neo4jPageCacheSize"`
	Reason                   string                       `json:"reason"`
	OptimizationTips         []string                     `json:"optimizationTips,omitempty"`
	ResourceRequirements     *corev1.ResourceRequirements `json:"resourceRequirements"`
	Neo4jConfig              map[string]string            `json:"neo4jConfig"`
}

// ResourceRecommender provides intelligent resource recommendations
type ResourceRecommender struct{}

// NewResourceRecommender creates a new resource recommender
func NewResourceRecommender() *ResourceRecommender {
	return &ResourceRecommender{}
}

// RecommendResourcesForTopology calculates optimal resources for a given cluster topology
func (r *ResourceRecommender) RecommendResourcesForTopology(topology neo4jv1alpha1.TopologyConfiguration, currentResources *corev1.ResourceRequirements) *ResourceRecommendation {
	totalPods := topology.Servers

	// Base recommendations on cluster size and configuration
	recommendation := &ResourceRecommendation{
		OptimizationTips: make([]string, 0),
		Neo4jConfig:      make(map[string]string),
	}

	// Determine memory allocation based on cluster size
	var memoryPerPodGB int
	var cpuLimitStr, cpuRequestStr string
	var reason string

	switch {
	case totalPods >= 7:
		// Large cluster (7+ pods) - conservative per-pod allocation
		memoryPerPodGB = 2
		cpuLimitStr = "1"
		cpuRequestStr = "500m"
		reason = "Large cluster (7+ pods) uses conservative per-pod allocation to fit on typical Kubernetes nodes"
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Consider using dedicated nodes for large Neo4j clusters",
			"Monitor memory pressure and adjust page cache if needed",
			"Use anti-affinity rules to spread pods across nodes")

	case totalPods >= 5:
		// Medium-large cluster (5-6 pods) - moderate allocation
		memoryPerPodGB = 3
		cpuLimitStr = "2"
		cpuRequestStr = "750m"
		reason = "Medium-large cluster (5-6 pods) balances performance with resource efficiency"
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Ensure sufficient node memory for all pods",
			"Consider SSD storage for better performance",
			"Monitor cluster load balancing")

	case totalPods >= 3:
		// Standard cluster (3-4 pods) - good balance
		memoryPerPodGB = 4
		cpuLimitStr = "2"
		cpuRequestStr = "1"
		reason = "Standard cluster (3-4 pods) provides good balance between performance and resource utilization"
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"This is the recommended configuration for most production workloads",
			"Consider read replicas for read-heavy workloads",
			"Monitor query performance and tune as needed")

	case totalPods == 2:
		// Two-node cluster (unusual but possible)
		memoryPerPodGB = 6
		cpuLimitStr = "3"
		cpuRequestStr = "1500m"
		reason = "Two-node cluster allows higher per-pod allocation but lacks high availability"
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Warning: Two-node clusters cannot form quorum - consider 3 nodes",
			"Higher memory allocation compensates for smaller cluster size",
			"Ensure robust backup strategy due to reduced redundancy")

	default:
		// Single-node deployment
		memoryPerPodGB = 8
		cpuLimitStr = "4"
		cpuRequestStr = "2"
		reason = "Single-node deployment maximizes per-pod resources for development or small workloads"
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Single-node provides no high availability",
			"Suitable for development, testing, or small production workloads",
			"Consider clustering for production use")
	}

	// Calculate memory strings
	memoryLimitStr := fmt.Sprintf("%dGi", memoryPerPodGB)
	memoryRequestStr := memoryLimitStr // Requests match limits for Neo4j

	// Calculate Neo4j memory settings
	memoryBytes := int64(memoryPerPodGB) * 1024 * 1024 * 1024
	neo4jMemory := r.calculateOptimalNeo4jMemory(memoryBytes, int(totalPods))

	// Build recommendation
	recommendation.RecommendedMemoryLimit = memoryLimitStr
	recommendation.RecommendedMemoryRequest = memoryRequestStr
	recommendation.RecommendedCPULimit = cpuLimitStr
	recommendation.RecommendedCPURequest = cpuRequestStr
	recommendation.Neo4jHeapSize = neo4jMemory.HeapSize
	recommendation.Neo4jPageCacheSize = neo4jMemory.PageCacheSize
	recommendation.Reason = reason

	// Build Kubernetes ResourceRequirements
	recommendation.ResourceRequirements = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memoryRequestStr),
			corev1.ResourceCPU:    resource.MustParse(cpuRequestStr),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memoryLimitStr),
			corev1.ResourceCPU:    resource.MustParse(cpuLimitStr),
		},
	}

	// Build Neo4j configuration
	recommendation.Neo4jConfig = map[string]string{
		"server.memory.heap.max_size":     neo4jMemory.HeapSize,
		"server.memory.pagecache.size":    neo4jMemory.PageCacheSize,
		"server.memory.heap.initial_size": neo4jMemory.HeapSize, // Set initial = max for consistency
	}

	// Add topology-specific optimizations
	r.addTopologySpecificTips(recommendation, topology)

	// Add resource efficiency analysis
	r.addResourceEfficiencyAnalysis(recommendation, currentResources)

	return recommendation
}

// Neo4jMemoryConfig holds Neo4j memory configuration
type Neo4jMemoryConfig struct {
	HeapSize      string
	PageCacheSize string
}

// calculateOptimalNeo4jMemory calculates optimal Neo4j memory settings
func (r *ResourceRecommender) calculateOptimalNeo4jMemory(containerMemoryBytes int64, totalPods int) Neo4jMemoryConfig {
	// Reserve system memory (minimum 512MB, up to 1GB for large containers)
	systemReserved := containerMemoryBytes / 8
	if systemReserved < 512*1024*1024 {
		systemReserved = 512 * 1024 * 1024
	}
	if systemReserved > 1024*1024*1024 {
		systemReserved = 1024 * 1024 * 1024
	}

	availableMemory := containerMemoryBytes - systemReserved

	var heapBytes, pageCacheBytes int64

	// Adjust allocation strategy based on cluster size
	switch {
	case totalPods >= 5:
		// Larger clusters: prioritize heap for query processing
		heapBytes = int64(float64(availableMemory) * 0.6)      // 60% for heap
		pageCacheBytes = int64(float64(availableMemory) * 0.4) // 40% for page cache

	case totalPods >= 3:
		// Standard clusters: balanced allocation
		heapBytes = int64(float64(availableMemory) * 0.55)      // 55% for heap
		pageCacheBytes = int64(float64(availableMemory) * 0.45) // 45% for page cache

	default:
		// Smaller clusters: favor page cache for data access
		heapBytes = int64(float64(availableMemory) * 0.5)      // 50% for heap
		pageCacheBytes = int64(float64(availableMemory) * 0.5) // 50% for page cache
	}

	// Ensure minimum sizes
	minHeap := int64(256 * 1024 * 1024)      // 256MB
	minPageCache := int64(128 * 1024 * 1024) // 128MB

	if heapBytes < minHeap {
		heapBytes = minHeap
	}
	if pageCacheBytes < minPageCache {
		pageCacheBytes = minPageCache
	}

	return Neo4jMemoryConfig{
		HeapSize:      r.formatMemorySize(heapBytes),
		PageCacheSize: r.formatMemorySize(pageCacheBytes),
	}
}

// addTopologySpecificTips adds tips specific to the cluster topology
func (r *ResourceRecommender) addTopologySpecificTips(recommendation *ResourceRecommendation, topology neo4jv1alpha1.TopologyConfiguration) {
	if topology.Servers < 2 {
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Single server: Use Neo4jEnterpriseStandalone for single-node deployments")
	}

	if topology.Servers%2 == 0 {
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Even number of servers may reduce fault tolerance when databases specify odd-numbered allocations - consider odd server counts")
	}

	if topology.Servers == 2 {
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Two servers provide limited fault tolerance - consider 3+ servers for production")
	}

	if topology.Servers >= 8 {
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"Large server count increases coordination overhead - ensure fast networking")
	}
}

// addResourceEfficiencyAnalysis compares with current resources and provides efficiency tips
func (r *ResourceRecommender) addResourceEfficiencyAnalysis(recommendation *ResourceRecommendation, currentResources *corev1.ResourceRequirements) {
	if currentResources == nil {
		recommendation.OptimizationTips = append(recommendation.OptimizationTips,
			"No current resources defined - using recommended defaults")
		return
	}

	// Analyze current memory allocation
	if currentMemory := currentResources.Limits.Memory(); currentMemory != nil {
		currentMemoryGB := float64(currentMemory.Value()) / (1024 * 1024 * 1024)
		recommendedMemoryGB := float64(recommendation.ResourceRequirements.Limits.Memory().Value()) / (1024 * 1024 * 1024)

		if currentMemoryGB < recommendedMemoryGB*0.8 {
			recommendation.OptimizationTips = append(recommendation.OptimizationTips,
				fmt.Sprintf("Current memory (%.1fGi) may be insufficient - consider increasing to %.1fGi",
					currentMemoryGB, recommendedMemoryGB))
		} else if currentMemoryGB > recommendedMemoryGB*1.5 {
			recommendation.OptimizationTips = append(recommendation.OptimizationTips,
				fmt.Sprintf("Current memory (%.1fGi) may be overallocated - could reduce to %.1fGi for efficiency",
					currentMemoryGB, recommendedMemoryGB))
		}
	}

	// Analyze current CPU allocation
	if currentCPU := currentResources.Limits.Cpu(); currentCPU != nil {
		currentCPUCores := float64(currentCPU.MilliValue()) / 1000
		recommendedCPUCores := float64(recommendation.ResourceRequirements.Limits.Cpu().MilliValue()) / 1000

		if currentCPUCores < recommendedCPUCores*0.7 {
			recommendation.OptimizationTips = append(recommendation.OptimizationTips,
				fmt.Sprintf("Current CPU (%.1f cores) may limit performance - consider increasing to %.1f cores",
					currentCPUCores, recommendedCPUCores))
		}
	}
}

// formatMemorySize formats bytes to human-readable string
func (r *ResourceRecommender) formatMemorySize(bytes int64) string {
	const (
		GB = 1024 * 1024 * 1024
		MB = 1024 * 1024
	)

	if bytes >= GB && bytes%GB == 0 {
		return fmt.Sprintf("%dg", bytes/GB)
	}
	if bytes >= MB {
		return fmt.Sprintf("%dm", bytes/MB)
	}
	return fmt.Sprintf("%dk", bytes/1024)
}

// GetRecommendationSummary provides a concise summary of the recommendation
func (recommendation *ResourceRecommendation) GetRecommendationSummary() string {
	return fmt.Sprintf("Memory: %s (heap: %s, cache: %s), CPU: %s, Reason: %s",
		recommendation.RecommendedMemoryLimit,
		recommendation.Neo4jHeapSize,
		recommendation.Neo4jPageCacheSize,
		recommendation.RecommendedCPULimit,
		recommendation.Reason)
}
