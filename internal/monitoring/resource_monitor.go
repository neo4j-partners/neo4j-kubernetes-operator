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

package monitoring

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// ResourceMonitor monitors cluster resources and provides early warning
type ResourceMonitor struct {
	client   client.Client
	recorder record.EventRecorder
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(client client.Client, recorder record.EventRecorder) *ResourceMonitor {
	return &ResourceMonitor{
		client:   client,
		recorder: recorder,
	}
}

// ResourceUtilization holds resource utilization information
type ResourceUtilization struct {
	MemoryPercentage      float64           `json:"memoryPercentage"`
	CPUPercentage         float64           `json:"cpuPercentage"`
	TotalMemoryUsed       resource.Quantity `json:"totalMemoryUsed"`
	TotalCPUUsed          resource.Quantity `json:"totalCPUUsed"`
	AvailableMemory       resource.Quantity `json:"availableMemory"`
	AvailableCPU          resource.Quantity `json:"availableCPU"`
	PodCount              int               `json:"podCount"`
	NodeCount             int               `json:"nodeCount"`
	Details               []NodeUtilization `json:"details,omitempty"`
	CanScale              bool              `json:"canScale"`
	ScalingRecommendation string            `json:"scalingRecommendation,omitempty"`
}

// NodeUtilization holds utilization information for a specific node
type NodeUtilization struct {
	NodeName          string            `json:"nodeName"`
	MemoryUsed        resource.Quantity `json:"memoryUsed"`
	CPUUsed           resource.Quantity `json:"cpuUsed"`
	MemoryAllocatable resource.Quantity `json:"memoryAllocatable"`
	CPUAllocatable    resource.Quantity `json:"cpuAllocatable"`
	MemoryPercentage  float64           `json:"memoryPercentage"`
	CPUPercentage     float64           `json:"cpuPercentage"`
	PodCount          int               `json:"podCount"`
	Schedulable       bool              `json:"schedulable"`
}

// MonitorClusterResources monitors and analyzes cluster resource utilization
func (rm *ResourceMonitor) MonitorClusterResources(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*ResourceUtilization, error) {
	logger := log.FromContext(ctx)

	// Get cluster nodes
	nodes, err := rm.getClusterNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes found in cluster")
	}

	// Calculate current resource utilization
	utilization, err := rm.calculateResourceUtilization(ctx, nodes, cluster.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate resource utilization: %w", err)
	}

	// Check scaling feasibility
	utilization.CanScale = rm.canScaleUp(utilization, cluster)
	utilization.ScalingRecommendation = rm.generateScalingRecommendation(utilization, cluster)

	// Emit warnings if thresholds are exceeded
	rm.checkResourceThresholds(cluster, utilization)

	logger.Info("Resource monitoring completed",
		"cluster", cluster.Name,
		"memoryUtilization", fmt.Sprintf("%.1f%%", utilization.MemoryPercentage),
		"cpuUtilization", fmt.Sprintf("%.1f%%", utilization.CPUPercentage),
		"canScale", utilization.CanScale)

	return utilization, nil
}

// ValidateScalingCapacity validates if cluster can handle scaling operation
func (rm *ResourceMonitor) ValidateScalingCapacity(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, targetTopology neo4jv1alpha1.TopologyConfiguration) (bool, string, error) {
	utilization, err := rm.MonitorClusterResources(ctx, cluster)
	if err != nil {
		return false, "", err
	}

	// Calculate resources needed for new pods
	currentPods := cluster.Spec.Topology.Servers
	targetPods := targetTopology.Servers
	newPods := targetPods - currentPods

	if newPods <= 0 {
		return true, "No scaling up required", nil
	}

	// Get per-pod resource requirements
	memoryPerPod := resource.MustParse("2Gi") // Default
	cpuPerPod := resource.MustParse("500m")   // Default

	if cluster.Spec.Resources != nil {
		if memory := cluster.Spec.Resources.Requests.Memory(); memory != nil {
			memoryPerPod = *memory
		}
		if cpu := cluster.Spec.Resources.Requests.Cpu(); cpu != nil {
			cpuPerPod = *cpu
		}
	}

	// Calculate total additional resources needed
	totalMemoryNeeded := memoryPerPod.DeepCopy()
	totalMemoryNeeded.Set(memoryPerPod.Value() * int64(newPods))

	totalCPUNeeded := cpuPerPod.DeepCopy()
	totalCPUNeeded.SetMilli(cpuPerPod.MilliValue() * int64(newPods))

	// Check if we have sufficient resources
	memoryAvailable := utilization.AvailableMemory.Value()
	cpuAvailable := utilization.AvailableCPU.MilliValue()

	if totalMemoryNeeded.Value() > memoryAvailable {
		return false, fmt.Sprintf("Insufficient memory: need %s, available %s",
			totalMemoryNeeded.String(), utilization.AvailableMemory.String()), nil
	}

	if totalCPUNeeded.MilliValue() > cpuAvailable {
		return false, fmt.Sprintf("Insufficient CPU: need %s, available %s",
			totalCPUNeeded.String(), utilization.AvailableCPU.String()), nil
	}

	// Check if scaling would result in high utilization
	newMemoryUtilization := float64(utilization.TotalMemoryUsed.Value()+totalMemoryNeeded.Value()) /
		float64(utilization.TotalMemoryUsed.Value()+utilization.AvailableMemory.Value()) * 100

	if newMemoryUtilization > 90 {
		return false, fmt.Sprintf("Scaling would result in high memory utilization (%.1f%%) - consider adding nodes",
			newMemoryUtilization), nil
	}

	return true, fmt.Sprintf("Scaling capacity validated: can add %d pods", newPods), nil
}

// getClusterNodes retrieves schedulable nodes from the cluster
func (rm *ResourceMonitor) getClusterNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := rm.client.List(ctx, nodeList); err != nil {
		return nil, err
	}

	var schedulableNodes []corev1.Node
	for _, node := range nodeList.Items {
		if rm.isNodeSchedulable(&node) {
			schedulableNodes = append(schedulableNodes, node)
		}
	}

	return schedulableNodes, nil
}

// isNodeSchedulable checks if a node is ready and schedulable
func (rm *ResourceMonitor) isNodeSchedulable(node *corev1.Node) bool {
	// Check if node is ready
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return false
		}
	}

	// Check if node is schedulable
	if node.Spec.Unschedulable {
		return false
	}

	// Check for NoSchedule taints
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule {
			return false
		}
	}

	return true
}

// calculateResourceUtilization calculates current resource utilization across all nodes
func (rm *ResourceMonitor) calculateResourceUtilization(ctx context.Context, nodes []corev1.Node, namespace string) (*ResourceUtilization, error) {
	var totalAllocatable, totalUsed ResourceInfo
	var nodeDetails []NodeUtilization

	// Calculate per-node utilization
	for _, node := range nodes {
		nodeUtil, err := rm.calculateNodeUtilization(ctx, &node, namespace)
		if err != nil {
			continue // Skip nodes we can't analyze
		}

		nodeDetails = append(nodeDetails, *nodeUtil)

		// Aggregate totals
		totalAllocatable.Memory.Add(nodeUtil.MemoryAllocatable)
		totalAllocatable.CPU.Add(nodeUtil.CPUAllocatable)
		totalUsed.Memory.Add(nodeUtil.MemoryUsed)
		totalUsed.CPU.Add(nodeUtil.CPUUsed)
	}

	// Calculate percentages
	memoryPercentage := float64(totalUsed.Memory.Value()) / float64(totalAllocatable.Memory.Value()) * 100
	cpuPercentage := float64(totalUsed.CPU.MilliValue()) / float64(totalAllocatable.CPU.MilliValue()) * 100

	// Calculate available resources
	availableMemory := totalAllocatable.Memory.DeepCopy()
	availableMemory.Sub(totalUsed.Memory)

	availableCPU := totalAllocatable.CPU.DeepCopy()
	availableCPU.SetMilli(totalAllocatable.CPU.MilliValue() - totalUsed.CPU.MilliValue())

	// Count pods
	podCount, err := rm.countPodsInNamespace(ctx, namespace)
	if err != nil {
		podCount = 0 // Don't fail, just set to 0
	}

	return &ResourceUtilization{
		MemoryPercentage: memoryPercentage,
		CPUPercentage:    cpuPercentage,
		TotalMemoryUsed:  totalUsed.Memory,
		TotalCPUUsed:     totalUsed.CPU,
		AvailableMemory:  availableMemory,
		AvailableCPU:     availableCPU,
		PodCount:         podCount,
		NodeCount:        len(nodes),
		Details:          nodeDetails,
	}, nil
}

// calculateNodeUtilization calculates utilization for a specific node
func (rm *ResourceMonitor) calculateNodeUtilization(ctx context.Context, node *corev1.Node, namespace string) (*NodeUtilization, error) {
	// Get allocatable resources
	memoryAllocatable := node.Status.Allocatable[corev1.ResourceMemory]
	cpuAllocatable := node.Status.Allocatable[corev1.ResourceCPU]

	// Get pods on this node
	podList := &corev1.PodList{}
	if err := rm.client.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
		return nil, err
	}

	// Calculate used resources from pod requests
	var memoryUsed, cpuUsed resource.Quantity
	podCount := 0

	for _, pod := range podList.Items {
		// Skip pods not in running/pending state
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}

		podCount++
		for _, container := range pod.Spec.Containers {
			if memory := container.Resources.Requests.Memory(); memory != nil {
				memoryUsed.Add(*memory)
			}
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				cpuUsed.Add(*cpu)
			}
		}
	}

	// Calculate percentages
	memoryPercentage := float64(memoryUsed.Value()) / float64(memoryAllocatable.Value()) * 100
	cpuPercentage := float64(cpuUsed.MilliValue()) / float64(cpuAllocatable.MilliValue()) * 100

	return &NodeUtilization{
		NodeName:          node.Name,
		MemoryUsed:        memoryUsed,
		CPUUsed:           cpuUsed,
		MemoryAllocatable: memoryAllocatable,
		CPUAllocatable:    cpuAllocatable,
		MemoryPercentage:  memoryPercentage,
		CPUPercentage:     cpuPercentage,
		PodCount:          podCount,
		Schedulable:       rm.isNodeSchedulable(node),
	}, nil
}

// countPodsInNamespace counts pods in a specific namespace
func (rm *ResourceMonitor) countPodsInNamespace(ctx context.Context, namespace string) (int, error) {
	podList := &corev1.PodList{}
	if err := rm.client.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		return 0, err
	}

	count := 0
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			count++
		}
	}

	return count, nil
}

// canScaleUp determines if the cluster can scale up
func (rm *ResourceMonitor) canScaleUp(utilization *ResourceUtilization, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	// Check if memory utilization is too high
	if utilization.MemoryPercentage > 85 {
		return false
	}

	// Check if CPU utilization is too high
	if utilization.CPUPercentage > 80 {
		return false
	}

	// Check if we have enough available resources for at least one more pod
	memoryPerPod := resource.MustParse("2Gi") // Default
	if cluster.Spec.Resources != nil {
		if memory := cluster.Spec.Resources.Requests.Memory(); memory != nil {
			memoryPerPod = *memory
		}
	}

	return utilization.AvailableMemory.Value() >= memoryPerPod.Value()
}

// generateScalingRecommendation provides scaling recommendations
func (rm *ResourceMonitor) generateScalingRecommendation(utilization *ResourceUtilization, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	if !utilization.CanScale {
		if utilization.MemoryPercentage > 90 {
			return "Cannot scale: memory utilization too high (>90%). Consider adding nodes or reducing resource requests."
		}
		if utilization.CPUPercentage > 85 {
			return "Cannot scale: CPU utilization too high (>85%). Consider adding nodes or reducing resource requests."
		}
		return "Cannot scale: insufficient available resources. Consider adding nodes."
	}

	if utilization.MemoryPercentage > 70 {
		return "Scaling possible but will result in high resource utilization. Monitor closely."
	}

	if utilization.MemoryPercentage < 50 {
		return "Cluster has good resource headroom for scaling."
	}

	return "Cluster can scale with moderate resource utilization."
}

// checkResourceThresholds checks resource thresholds and emits warnings
func (rm *ResourceMonitor) checkResourceThresholds(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, utilization *ResourceUtilization) {
	// High memory utilization warning
	if utilization.MemoryPercentage > 85 {
		rm.recorder.Eventf(cluster, corev1.EventTypeWarning, "HighMemoryUtilization",
			"Cluster memory utilization is high (%.1f%%). Scaling may not be possible.", utilization.MemoryPercentage)
	}

	// High CPU utilization warning
	if utilization.CPUPercentage > 80 {
		rm.recorder.Eventf(cluster, corev1.EventTypeWarning, "HighCPUUtilization",
			"Cluster CPU utilization is high (%.1f%%). Scaling may not be possible.", utilization.CPUPercentage)
	}

	// Low available resources warning
	if utilization.AvailableMemory.Value() < 2*1024*1024*1024 { // Less than 2GB available
		rm.recorder.Eventf(cluster, corev1.EventTypeWarning, "LowAvailableMemory",
			"Low available memory (%s). Consider adding nodes before scaling.", utilization.AvailableMemory.String())
	}

	// Node count warning
	if utilization.NodeCount < 3 {
		rm.recorder.Eventf(cluster, corev1.EventTypeWarning, "LowNodeCount",
			"Only %d schedulable nodes available. Consider adding nodes for better resource distribution.", utilization.NodeCount)
	}
}

// ResourceInfo holds resource information
type ResourceInfo struct {
	Memory resource.Quantity
	CPU    resource.Quantity
}
