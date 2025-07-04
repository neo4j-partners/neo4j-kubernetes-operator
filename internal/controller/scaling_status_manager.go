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
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// ScalingStatusManager handles detailed scaling status reporting
type ScalingStatusManager struct {
	client.Client
}

// NewScalingStatusManager creates a new scaling status manager
func NewScalingStatusManager(client client.Client) *ScalingStatusManager {
	return &ScalingStatusManager{
		Client: client,
	}
}

// InitializeScaling initializes scaling status when scaling operation begins
func (ssm *ScalingStatusManager) InitializeScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, targetTopology neo4jv1alpha1.TopologyConfiguration) error {
	logger := log.FromContext(ctx)

	currentPrimaries := cluster.Spec.Topology.Primaries
	currentSecondaries := cluster.Spec.Topology.Secondaries

	// Only initialize if we're actually scaling
	if targetTopology.Primaries == currentPrimaries && targetTopology.Secondaries == currentSecondaries {
		return nil
	}

	now := metav1.Now()

	scalingStatus := &neo4jv1alpha1.ScalingStatus{
		Phase:              "Pending",
		StartTime:          &now,
		CurrentPrimaries:   int(currentPrimaries),
		CurrentSecondaries: int(currentSecondaries),
		TargetPrimaries:    int(targetTopology.Primaries),
		TargetSecondaries:  int(targetTopology.Secondaries),
		Message: fmt.Sprintf("Scaling from %d primaries, %d secondaries to %d primaries, %d secondaries",
			currentPrimaries, currentSecondaries, targetTopology.Primaries, targetTopology.Secondaries),
		Conditions: []neo4jv1alpha1.ScalingCondition{
			{
				Type:               "ResourceValidated",
				Status:             corev1.ConditionUnknown,
				LastTransitionTime: now,
				Reason:             "ValidationPending",
				Message:            "Resource validation has not started yet",
			},
		},
		Progress: &neo4jv1alpha1.ScalingProgress{
			TotalSteps:     ssm.calculateTotalSteps(int(currentPrimaries), int(currentSecondaries), targetTopology),
			CompletedSteps: 0,
			CurrentStep:    "Validating resources",
		},
	}

	// Estimate completion time (rough estimate: 2 minutes per new pod)
	newPods := (targetTopology.Primaries + targetTopology.Secondaries) - (currentPrimaries + currentSecondaries)
	if newPods > 0 {
		estimatedDuration := time.Duration(newPods) * 2 * time.Minute
		estimatedCompletion := metav1.NewTime(now.Add(estimatedDuration))
		scalingStatus.Progress.EstimatedCompletion = &estimatedCompletion
	}

	cluster.Status.ScalingStatus = scalingStatus

	logger.Info("Initialized scaling status",
		"cluster", cluster.Name,
		"from", fmt.Sprintf("%d/%d", currentPrimaries, currentSecondaries),
		"to", fmt.Sprintf("%d/%d", targetTopology.Primaries, targetTopology.Secondaries))

	return ssm.Status().Update(ctx, cluster)
}

// UpdateScalingCondition updates a specific scaling condition
func (ssm *ScalingStatusManager) UpdateScalingCondition(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, conditionType string, status corev1.ConditionStatus, reason, message string) error {
	if cluster.Status.ScalingStatus == nil {
		return nil // No scaling in progress
	}

	logger := log.FromContext(ctx)
	now := metav1.Now()

	// Find existing condition or create new one
	var condition *neo4jv1alpha1.ScalingCondition
	for i := range cluster.Status.ScalingStatus.Conditions {
		if cluster.Status.ScalingStatus.Conditions[i].Type == conditionType {
			condition = &cluster.Status.ScalingStatus.Conditions[i]
			break
		}
	}

	if condition == nil {
		// Add new condition
		cluster.Status.ScalingStatus.Conditions = append(cluster.Status.ScalingStatus.Conditions,
			neo4jv1alpha1.ScalingCondition{
				Type:               conditionType,
				Status:             status,
				LastTransitionTime: now,
				Reason:             reason,
				Message:            message,
				LastProbeTime:      &now,
			})
	} else {
		// Update existing condition
		if condition.Status != status {
			condition.LastTransitionTime = now
		}
		condition.Status = status
		condition.Reason = reason
		condition.Message = message
		condition.LastProbeTime = &now
	}

	// Update phase based on conditions
	ssm.updateScalingPhase(cluster.Status.ScalingStatus)

	logger.Info("Updated scaling condition",
		"cluster", cluster.Name,
		"condition", conditionType,
		"status", status,
		"reason", reason)

	return ssm.Status().Update(ctx, cluster)
}

// UpdateScalingProgress updates the detailed progress of scaling
func (ssm *ScalingStatusManager) UpdateScalingProgress(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, currentStep string, completedSteps int) error {
	if cluster.Status.ScalingStatus == nil {
		return nil // No scaling in progress
	}

	logger := log.FromContext(ctx)

	if cluster.Status.ScalingStatus.Progress == nil {
		cluster.Status.ScalingStatus.Progress = &neo4jv1alpha1.ScalingProgress{}
	}

	cluster.Status.ScalingStatus.Progress.CurrentStep = currentStep
	cluster.Status.ScalingStatus.Progress.CompletedSteps = completedSteps

	// Update pod lists
	if err := ssm.updatePodProgress(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update pod progress")
		// Don't fail the update, just log the error
	}

	// Update phase to InProgress if we're making progress
	if cluster.Status.ScalingStatus.Phase == "Pending" {
		cluster.Status.ScalingStatus.Phase = "InProgress"
	}

	logger.Info("Updated scaling progress",
		"cluster", cluster.Name,
		"step", currentStep,
		"progress", fmt.Sprintf("%d/%d", completedSteps, cluster.Status.ScalingStatus.Progress.TotalSteps))

	return ssm.Status().Update(ctx, cluster)
}

// CompleteScaling marks scaling as completed successfully
func (ssm *ScalingStatusManager) CompleteScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	if cluster.Status.ScalingStatus == nil {
		return nil // No scaling in progress
	}

	logger := log.FromContext(ctx)
	now := metav1.Now()

	cluster.Status.ScalingStatus.Phase = "Completed"
	cluster.Status.ScalingStatus.CompletionTime = &now
	cluster.Status.ScalingStatus.LastScaleTime = &now
	cluster.Status.ScalingStatus.Message = "Scaling completed successfully"

	// Update current counts to match targets
	cluster.Status.ScalingStatus.CurrentPrimaries = cluster.Status.ScalingStatus.TargetPrimaries
	cluster.Status.ScalingStatus.CurrentSecondaries = cluster.Status.ScalingStatus.TargetSecondaries

	// Mark final condition as completed
	if err := ssm.UpdateScalingCondition(ctx, cluster, "Completed", corev1.ConditionTrue, "ScalingComplete", "All pods are ready and cluster is formed"); err != nil {
		return err
	}

	logger.Info("Scaling completed successfully",
		"cluster", cluster.Name,
		"primaries", cluster.Status.ScalingStatus.CurrentPrimaries,
		"secondaries", cluster.Status.ScalingStatus.CurrentSecondaries)

	return nil
}

// FailScaling marks scaling as failed
func (ssm *ScalingStatusManager) FailScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, reason, errorMessage string) error {
	if cluster.Status.ScalingStatus == nil {
		return nil // No scaling in progress
	}

	logger := log.FromContext(ctx)
	now := metav1.Now()

	cluster.Status.ScalingStatus.Phase = "Failed"
	cluster.Status.ScalingStatus.CompletionTime = &now
	cluster.Status.ScalingStatus.Message = fmt.Sprintf("Scaling failed: %s", reason)
	cluster.Status.ScalingStatus.LastError = errorMessage

	logger.Error(nil, "Scaling failed",
		"cluster", cluster.Name,
		"reason", reason,
		"error", errorMessage)

	return ssm.Status().Update(ctx, cluster)
}

// ClearScalingStatus clears the scaling status when scaling is no longer active
func (ssm *ScalingStatusManager) ClearScalingStatus(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	if cluster.Status.ScalingStatus == nil {
		return nil
	}

	// Only clear if scaling is completed or failed
	if cluster.Status.ScalingStatus.Phase == "Completed" || cluster.Status.ScalingStatus.Phase == "Failed" {
		cluster.Status.ScalingStatus = nil
		return ssm.Status().Update(ctx, cluster)
	}

	return nil
}

// updateScalingPhase updates the scaling phase based on current conditions
func (ssm *ScalingStatusManager) updateScalingPhase(scalingStatus *neo4jv1alpha1.ScalingStatus) {
	// Check conditions to determine phase
	resourceValidated := ssm.isConditionTrue(scalingStatus, "ResourceValidated")
	podsScheduled := ssm.isConditionTrue(scalingStatus, "PodsScheduled")
	podsReady := ssm.isConditionTrue(scalingStatus, "PodsReady")
	clusterFormed := ssm.isConditionTrue(scalingStatus, "ClusterFormed")
	completed := ssm.isConditionTrue(scalingStatus, "Completed")

	// Check for any failed conditions
	for _, condition := range scalingStatus.Conditions {
		if condition.Status == corev1.ConditionFalse {
			scalingStatus.Phase = "Failed"
			return
		}
	}

	switch {
	case completed:
		scalingStatus.Phase = "Completed"
	case clusterFormed:
		scalingStatus.Phase = "InProgress"
	case podsReady:
		scalingStatus.Phase = "InProgress"
	case podsScheduled:
		scalingStatus.Phase = "InProgress"
	case resourceValidated:
		scalingStatus.Phase = "InProgress"
	default:
		scalingStatus.Phase = "Pending"
	}
}

// isConditionTrue checks if a condition is true
func (ssm *ScalingStatusManager) isConditionTrue(scalingStatus *neo4jv1alpha1.ScalingStatus, conditionType string) bool {
	for _, condition := range scalingStatus.Conditions {
		if condition.Type == conditionType && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// updatePodProgress updates the pod progress lists
func (ssm *ScalingStatusManager) updatePodProgress(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	// Get all pods for this cluster
	podList := &corev1.PodList{}
	if err := ssm.List(ctx, podList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		"app.kubernetes.io/name": "neo4j",
		"neo4j.com/cluster":      cluster.Name,
	}); err != nil {
		return err
	}

	progress := cluster.Status.ScalingStatus.Progress
	progress.PendingPods = []string{}
	progress.RunningPods = []string{}
	progress.ReadyPods = []string{}
	progress.FailedPods = []string{}

	for _, pod := range podList.Items {
		switch pod.Status.Phase {
		case corev1.PodPending:
			progress.PendingPods = append(progress.PendingPods, pod.Name)
		case corev1.PodRunning:
			if ssm.isPodReady(&pod) {
				progress.ReadyPods = append(progress.ReadyPods, pod.Name)
			} else {
				progress.RunningPods = append(progress.RunningPods, pod.Name)
			}
		case corev1.PodFailed:
			progress.FailedPods = append(progress.FailedPods, pod.Name)
		}
	}

	return nil
}

// isPodReady checks if a pod is ready
func (ssm *ScalingStatusManager) isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// calculateTotalSteps calculates total steps for scaling operation
func (ssm *ScalingStatusManager) calculateTotalSteps(currentPrimaries, currentSecondaries int, targetTopology neo4jv1alpha1.TopologyConfiguration) int {
	steps := 1 // Resource validation

	// Add steps for each new pod
	newPrimaries := int(targetTopology.Primaries) - currentPrimaries
	newSecondaries := int(targetTopology.Secondaries) - currentSecondaries

	if newPrimaries > 0 {
		steps += newPrimaries * 2 // Schedule + Wait for ready
	}
	if newSecondaries > 0 {
		steps += newSecondaries * 2 // Schedule + Wait for ready
	}

	steps += 1 // Cluster formation verification
	steps += 1 // Final validation

	return steps
}
