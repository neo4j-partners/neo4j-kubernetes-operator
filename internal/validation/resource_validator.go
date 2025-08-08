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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// ResourceValidator validates cluster resource capacity for scaling operations
type ResourceValidator struct {
	client client.Client
}

// NewResourceValidator creates a new resource validator
func NewResourceValidator(client client.Client) *ResourceValidator {
	return &ResourceValidator{
		client: client,
	}
}

// ValidateScaling validates if cluster has sufficient resources for scaling operation
func (v *ResourceValidator) ValidateScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, targetTopology neo4jv1alpha1.TopologyConfiguration) field.ErrorList {
	var allErrs field.ErrorList

	// Calculate total resource requirements for target topology
	targetPods := targetTopology.Servers
	currentPods := cluster.Spec.Topology.Servers

	// Skip validation if not actually scaling up
	if targetPods <= currentPods {
		return allErrs
	}

	// Get cluster nodes
	nodes, err := v.getClusterNodes(ctx)
	if err != nil {
		allErrs = append(allErrs, field.InternalError(
			field.NewPath("spec", "topology"),
			fmt.Errorf("failed to get cluster nodes: %w", err),
		))
		return allErrs
	}

	if len(nodes) == 0 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "topology"),
			targetTopology,
			"no schedulable nodes found in cluster",
		))
		return allErrs
	}

	// Calculate available resources
	availableResources, err := v.calculateAvailableResources(ctx, nodes, cluster.Namespace)
	if err != nil {
		allErrs = append(allErrs, field.InternalError(
			field.NewPath("spec", "topology"),
			fmt.Errorf("failed to calculate available resources: %w", err),
		))
		return allErrs
	}

	// Calculate required resources for new pods
	newPods := targetPods - currentPods
	requiredResources := v.calculateRequiredResources(cluster, int(newPods))

	// Validate memory capacity
	if requiredResources.Memory.Cmp(availableResources.Memory) > 0 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "topology"),
			targetTopology,
			fmt.Sprintf("insufficient memory for scaling: need %s for %d new pods, but only %s available",
				v.formatResourceQuantity(requiredResources.Memory),
				newPods,
				v.formatResourceQuantity(availableResources.Memory),
			),
		))
	}

	// Validate CPU capacity
	if requiredResources.CPU.Cmp(availableResources.CPU) > 0 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "topology"),
			targetTopology,
			fmt.Sprintf("insufficient CPU for scaling: need %s for %d new pods, but only %s available",
				v.formatResourceQuantity(requiredResources.CPU),
				newPods,
				v.formatResourceQuantity(availableResources.CPU),
			),
		))
	}

	// Validate pod capacity (check if we exceed max pods per node)
	maxPodsPerNode := v.calculateMaxPodsPerNode(nodes)
	if int(newPods) > maxPodsPerNode*len(nodes) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "topology"),
			targetTopology,
			fmt.Sprintf("insufficient pod capacity for scaling: need %d new pods, but cluster supports maximum %d pods per node across %d nodes",
				newPods, maxPodsPerNode, len(nodes)),
		))
	}

	// Warn about resource utilization getting close to limits
	memoryUtilization := float64(requiredResources.Memory.Value()) / float64(availableResources.Memory.Value()) * 100
	if memoryUtilization > 80 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "topology"),
			targetTopology,
			fmt.Sprintf("warning: scaling will result in high memory utilization (%.1f%%), consider adding more nodes", memoryUtilization),
		))
	}

	return allErrs
}

// ResourceInfo holds resource information
type ResourceInfo struct {
	Memory resource.Quantity
	CPU    resource.Quantity
}

// getClusterNodes retrieves schedulable nodes from the cluster
func (v *ResourceValidator) getClusterNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	err := v.client.List(ctx, nodeList)
	if err != nil {
		return nil, err
	}

	var schedulableNodes []corev1.Node
	for _, node := range nodeList.Items {
		// Skip nodes that are not ready or unschedulable
		if v.isNodeSchedulable(&node) {
			schedulableNodes = append(schedulableNodes, node)
		}
	}

	return schedulableNodes, nil
}

// isNodeSchedulable checks if a node is ready and schedulable
func (v *ResourceValidator) isNodeSchedulable(node *corev1.Node) bool {
	// Check if node is ready
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return false
		}
	}

	// Check if node is schedulable (not cordoned)
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

// calculateAvailableResources calculates available resources across all nodes
func (v *ResourceValidator) calculateAvailableResources(ctx context.Context, nodes []corev1.Node, namespace string) (ResourceInfo, error) {
	var totalAllocatable ResourceInfo
	var totalRequested ResourceInfo

	// Sum up allocatable resources from all nodes
	for _, node := range nodes {
		if memory, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			totalAllocatable.Memory.Add(memory)
		}
		if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			totalAllocatable.CPU.Add(cpu)
		}
	}

	// Calculate currently requested resources by existing pods
	podList := &corev1.PodList{}
	err := v.client.List(ctx, podList)
	if err != nil {
		return ResourceInfo{}, err
	}

	for _, pod := range podList.Items {
		// Skip pods that are not running or pending
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}

		// Sum up requests from all containers
		for _, container := range pod.Spec.Containers {
			if memory, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				totalRequested.Memory.Add(memory)
			}
			if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				totalRequested.CPU.Add(cpu)
			}
		}
	}

	// Calculate available = allocatable - requested
	availableMemory := totalAllocatable.Memory.DeepCopy()
	availableMemory.Sub(totalRequested.Memory)

	availableCPU := totalAllocatable.CPU.DeepCopy()
	availableCPU.Sub(totalRequested.CPU)

	return ResourceInfo{
		Memory: availableMemory,
		CPU:    availableCPU,
	}, nil
}

// calculateRequiredResources calculates resources needed for new pods
func (v *ResourceValidator) calculateRequiredResources(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, newPods int) ResourceInfo {
	// Use default resources if not specified
	memoryRequest := resource.MustParse("2Gi") // Default memory request
	cpuRequest := resource.MustParse("500m")   // Default CPU request

	if cluster.Spec.Resources != nil {
		if memory, ok := cluster.Spec.Resources.Requests[corev1.ResourceMemory]; ok {
			memoryRequest = memory
		}
		if cpu, ok := cluster.Spec.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuRequest = cpu
		}
	}

	// Calculate total for new pods
	totalMemory := memoryRequest.DeepCopy()
	totalMemory.Set(memoryRequest.Value() * int64(newPods))

	totalCPU := cpuRequest.DeepCopy()
	totalCPU.SetMilli(cpuRequest.MilliValue() * int64(newPods))

	return ResourceInfo{
		Memory: totalMemory,
		CPU:    totalCPU,
	}
}

// calculateMaxPodsPerNode determines the maximum pods per node based on cluster configuration
func (v *ResourceValidator) calculateMaxPodsPerNode(nodes []corev1.Node) int {
	maxPods := 110 // Default Kubernetes limit

	for _, node := range nodes {
		if nodePods, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
			nodeMaxPods := int(nodePods.Value())
			if nodeMaxPods < maxPods {
				maxPods = nodeMaxPods
			}
		}
	}

	return maxPods
}

// formatResourceQuantity formats a resource quantity for display
func (v *ResourceValidator) formatResourceQuantity(quantity resource.Quantity) string {
	return quantity.String()
}
