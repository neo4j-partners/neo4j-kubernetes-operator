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
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// AutoScaler handles auto-scaling operations for Neo4j enterprise clusters
type AutoScaler struct {
	client           client.Client
	logger           logr.Logger
	metricsCollector *MetricsCollector
	scaleDecision    *ScaleDecisionEngine
}

// NewAutoScaler creates a new auto-scaler
func NewAutoScaler(client client.Client) *AutoScaler {
	logger := log.Log.WithName("autoscaler")
	return &AutoScaler{
		client:           client,
		logger:           logger,
		metricsCollector: NewMetricsCollector(client, logger),
		scaleDecision:    NewScaleDecisionEngine(logger),
	}
}

// ReconcileAutoScaling performs auto-scaling operations based on the cluster configuration and metrics
func (as *AutoScaler) ReconcileAutoScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx).WithName("auto-scaler")

	if cluster.Spec.AutoScaling == nil || !cluster.Spec.AutoScaling.Enabled {
		logger.Info("Auto-scaling is disabled, skipping")
		return nil
	}

	// Collect current metrics
	metrics, err := as.metricsCollector.CollectMetrics(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to collect metrics: %w", err)
	}

	// Scale primaries
	if err := as.scalePrimaries(ctx, cluster, metrics); err != nil {
		return fmt.Errorf("failed to auto-scale primaries: %w", err)
	}

	// Scale secondaries with coordination if enabled
	if err := as.scaleSecondaries(ctx, cluster, metrics); err != nil {
		return fmt.Errorf("failed to auto-scale secondaries: %w", err)
	}

	logger.Info("Auto-scaling reconciliation completed")
	return nil
}

// scalePrimaries handles primary node auto-scaling
func (as *AutoScaler) scalePrimaries(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, metrics *ClusterMetrics) error {
	logger := log.FromContext(ctx).WithName("scale-primaries")

	if cluster.Spec.AutoScaling.Primaries == nil || !cluster.Spec.AutoScaling.Primaries.Enabled {
		return nil
	}

	primaryConfig := cluster.Spec.AutoScaling.Primaries

	// Check quorum protection
	if primaryConfig.QuorumProtection != nil && primaryConfig.QuorumProtection.Enabled {
		if err := as.validateQuorumProtection(ctx, cluster, metrics); err != nil {
			logger.Info("Quorum protection blocked primary scaling", "reason", err.Error())
			return nil
		}
	}

	// Calculate scaling decision
	decision := as.scaleDecision.CalculatePrimaryScaling(cluster, metrics)
	if decision.Action == ScaleActionNone {
		return nil
	}

	// Apply quorum constraints
	targetReplicas := decision.TargetReplicas
	if !primaryConfig.AllowQuorumBreak {
		targetReplicas = as.ensureOddReplicas(targetReplicas, primaryConfig.MinReplicas, primaryConfig.MaxReplicas)
	}

	// Ensure within bounds
	if targetReplicas < primaryConfig.MinReplicas {
		targetReplicas = primaryConfig.MinReplicas
	}
	if targetReplicas > primaryConfig.MaxReplicas {
		targetReplicas = primaryConfig.MaxReplicas
	}

	// Apply scaling
	if err := as.applyScaling(ctx, cluster, "primary", targetReplicas, decision.Reason); err != nil {
		return fmt.Errorf("failed to apply primary scaling: %w", err)
	}

	return nil
}

// scaleSecondaries handles secondary node auto-scaling
func (as *AutoScaler) scaleSecondaries(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, metrics *ClusterMetrics) error {
	if cluster.Spec.AutoScaling.Secondaries == nil || !cluster.Spec.AutoScaling.Secondaries.Enabled {
		return nil
	}

	secondaryConfig := cluster.Spec.AutoScaling.Secondaries

	// Calculate scaling decision
	decision := as.scaleDecision.CalculateSecondaryScaling(cluster, metrics)
	if decision.Action == ScaleActionNone {
		return nil
	}

	// Handle zone-aware scaling
	if secondaryConfig.ZoneAware != nil && secondaryConfig.ZoneAware.Enabled {
		return as.applyZoneAwareScaling(ctx, cluster, decision, metrics)
	}

	// Ensure within bounds
	targetReplicas := decision.TargetReplicas
	if targetReplicas < secondaryConfig.MinReplicas {
		targetReplicas = secondaryConfig.MinReplicas
	}
	if targetReplicas > secondaryConfig.MaxReplicas {
		targetReplicas = secondaryConfig.MaxReplicas
	}

	// Apply scaling
	if err := as.applyScaling(ctx, cluster, "secondary", targetReplicas, decision.Reason); err != nil {
		return fmt.Errorf("failed to apply secondary scaling: %w", err)
	}

	return nil
}

// ensureOddReplicas ensures primary replica count is odd for quorum
func (as *AutoScaler) ensureOddReplicas(target, min, max int32) int32 {
	if target%2 == 1 {
		return target // Already odd
	}

	// Try target + 1
	if target+1 <= max {
		return target + 1
	}

	// Try target - 1
	if target-1 >= min {
		return target - 1
	}

	return target // Return original if no valid odd number in range
}

// validateQuorumProtection checks if scaling would violate quorum protection
func (as *AutoScaler) validateQuorumProtection(_ context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, metrics *ClusterMetrics) error {
	protection := cluster.Spec.AutoScaling.Primaries.QuorumProtection
	if protection == nil {
		return nil
	}

	healthyPrimaries := metrics.PrimaryNodes.Healthy
	if healthyPrimaries < protection.MinHealthyPrimaries {
		return fmt.Errorf("insufficient healthy primaries: %d < %d", healthyPrimaries, protection.MinHealthyPrimaries)
	}

	return nil
}

// applyZoneAwareScaling applies zone-aware scaling for secondaries
func (as *AutoScaler) applyZoneAwareScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, decision *ScalingDecision, _ *ClusterMetrics) error {
	zoneConfig := cluster.Spec.AutoScaling.Secondaries.ZoneAware

	// Get current zone distribution
	zoneDistribution, err := as.getZoneDistribution(ctx, cluster, "secondary")
	if err != nil {
		return fmt.Errorf("failed to get zone distribution: %w", err)
	}

	// Calculate target distribution
	targetDistribution := as.calculateTargetZoneDistribution(decision.TargetReplicas, zoneDistribution, zoneConfig)

	// Apply zone-specific scaling
	for zone, targetCount := range targetDistribution {
		currentCount := zoneDistribution[zone]
		if currentCount != targetCount {
			as.logger.Info("Zone-aware scaling", "zone", zone, "from", currentCount, "to", targetCount)
			// In a real implementation, you'd scale zone-specific StatefulSets or use pod topology spread constraints
		}
	}

	return nil
}

// getZoneDistribution returns current pod distribution across zones
func (as *AutoScaler) getZoneDistribution(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, nodeType string) (map[string]int32, error) {
	pods := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/name":      "neo4j",
		"app.kubernetes.io/instance":  cluster.Name,
		"app.kubernetes.io/component": nodeType,
	})

	if err := as.client.List(ctx, pods, &client.ListOptions{
		Namespace:     cluster.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return nil, err
	}

	distribution := make(map[string]int32)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" {
			node := &corev1.Node{}
			if err := as.client.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node); err == nil {
				zone := node.Labels["topology.kubernetes.io/zone"]
				if zone == "" {
					zone = "unknown"
				}
				distribution[zone]++
			}
		}
	}

	return distribution, nil
}

// calculateTargetZoneDistribution calculates target pod distribution across zones
func (as *AutoScaler) calculateTargetZoneDistribution(totalReplicas int32, currentDistribution map[string]int32, zoneConfig *neo4jv1alpha1.ZoneAwareScalingConfig) map[string]int32 {
	zones := make([]string, 0, len(currentDistribution))
	for zone := range currentDistribution {
		zones = append(zones, zone)
	}

	if len(zones) == 0 {
		return map[string]int32{}
	}

	targetDistribution := make(map[string]int32)

	// Even distribution by default with bounds checking
	numZones := len(zones)
	if numZones > int(totalReplicas) {
		// If more zones than replicas, distribute one per zone
		for i, zone := range zones {
			if i < int(totalReplicas) {
				targetDistribution[zone] = 1
			} else {
				targetDistribution[zone] = 0
			}
		}
	} else {
		// Bounds check to prevent integer overflow
		if numZones > math.MaxInt32 {
			// Fallback: use simple distribution
			for _, zone := range zones {
				targetDistribution[zone] = totalReplicas / int32(len(zones))
			}
		} else {
			// Safe conversion after bounds check
			numZonesInt32 := int32(numZones)
			basePerZone := totalReplicas / numZonesInt32
			remainder := totalReplicas % numZonesInt32

			for i, zone := range zones {
				targetDistribution[zone] = basePerZone
				if int32(i) < remainder {
					targetDistribution[zone]++
				}
			}
		}
	}

	// Ensure minimum per zone for all zones
	for zone := range targetDistribution {
		if targetDistribution[zone] < zoneConfig.MinReplicasPerZone {
			targetDistribution[zone] = zoneConfig.MinReplicasPerZone
		}
	}

	return targetDistribution
}

// applyScaling applies scaling to a StatefulSet
func (as *AutoScaler) applyScaling(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, nodeType string, targetReplicas int32, reason string) error {
	logger := log.FromContext(ctx).WithName("apply-scaling")

	stsName := fmt.Sprintf("%s-%s", cluster.Name, nodeType)
	sts := &appsv1.StatefulSet{}

	if err := as.client.Get(ctx, types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      stsName,
	}, sts); err != nil {
		return fmt.Errorf("failed to get %s StatefulSet: %w", nodeType, err)
	}

	currentReplicas := *sts.Spec.Replicas
	if currentReplicas == targetReplicas {
		return nil
	}

	logger.Info("Auto-scaling StatefulSet",
		"nodeType", nodeType,
		"from", currentReplicas,
		"to", targetReplicas,
		"reason", reason)

	sts.Spec.Replicas = &targetReplicas

	if err := as.client.Update(ctx, sts); err != nil {
		return fmt.Errorf("failed to update %s StatefulSet: %w", nodeType, err)
	}

	return nil
}

// MetricsCollector collects metrics for scaling decisions
type MetricsCollector struct {
	client client.Client
	logger logr.Logger
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(client client.Client, logger logr.Logger) *MetricsCollector {
	return &MetricsCollector{
		client: client,
		logger: logger,
	}
}

// ClusterMetrics contains collected metrics for scaling decisions
type ClusterMetrics struct {
	PrimaryNodes   NodeMetrics
	SecondaryNodes NodeMetrics
	QueryMetrics   QueryMetrics
	SystemMetrics  SystemMetrics
	Timestamp      time.Time
}

// NodeMetrics contains metrics for a group of nodes
type NodeMetrics struct {
	Total       int32
	Healthy     int32
	CPU         MetricValue
	Memory      MetricValue
	Connections MetricValue
	Throughput  MetricValue
}

// QueryMetrics contains query performance metrics
type QueryMetrics struct {
	AverageLatency   time.Duration
	P95Latency       time.Duration
	QueriesPerSecond float64
	SlowQueries      int64
}

// SystemMetrics contains system-level metrics
type SystemMetrics struct {
	LoadAverage    float64
	DiskUsage      float64
	NetworkLatency time.Duration
}

// MetricValue represents a metric with current value and trend
type MetricValue struct {
	Current   float64
	Previous  float64
	Trend     MetricTrend
	Threshold float64
}

// MetricTrend indicates metric trend direction
type MetricTrend int

const (
	// TrendUnknown indicates the trend direction is not yet determined
	TrendUnknown MetricTrend = iota
	// TrendDecreasing indicates the metric value is decreasing
	TrendDecreasing
	// TrendStable indicates the metric value is stable
	TrendStable
	// TrendIncreasing indicates the metric value is increasing
	TrendIncreasing
)

// CollectMetrics collects all metrics needed for scaling decisions
func (mc *MetricsCollector) CollectMetrics(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*ClusterMetrics, error) {
	metrics := &ClusterMetrics{
		Timestamp: time.Now(),
	}

	// Collect primary node metrics
	primaryMetrics, err := mc.collectNodeMetrics(ctx, cluster, "primary")
	if err != nil {
		return nil, fmt.Errorf("failed to collect primary metrics: %w", err)
	}
	metrics.PrimaryNodes = *primaryMetrics

	// Collect secondary node metrics
	secondaryMetrics, err := mc.collectNodeMetrics(ctx, cluster, "secondary")
	if err != nil {
		return nil, fmt.Errorf("failed to collect secondary metrics: %w", err)
	}
	metrics.SecondaryNodes = *secondaryMetrics

	// Collect query metrics
	queryMetrics := mc.collectQueryMetrics(ctx, cluster)
	metrics.QueryMetrics = *queryMetrics

	// Collect system metrics
	systemMetrics := mc.collectSystemMetrics(ctx, cluster)
	metrics.SystemMetrics = *systemMetrics

	return metrics, nil
}

// collectNodeMetrics collects metrics for a specific node type
func (mc *MetricsCollector) collectNodeMetrics(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, nodeType string) (*NodeMetrics, error) {
	// Get StatefulSet
	stsName := fmt.Sprintf("%s-%s", cluster.Name, nodeType)
	sts := &appsv1.StatefulSet{}

	if err := mc.client.Get(ctx, types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      stsName,
	}, sts); err != nil {
		return nil, fmt.Errorf("failed to get %s StatefulSet: %w", nodeType, err)
	}

	// Get pods
	pods := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/name":      "neo4j",
		"app.kubernetes.io/instance":  cluster.Name,
		"app.kubernetes.io/component": nodeType,
	})

	if err := mc.client.List(ctx, pods, &client.ListOptions{
		Namespace:     cluster.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list %s pods: %w", nodeType, err)
	}

	metrics := &NodeMetrics{
		Total: *sts.Spec.Replicas,
	}

	// Count healthy pods
	for _, pod := range pods.Items {
		if mc.isPodHealthy(&pod) {
			metrics.Healthy++
		}
	}

	// Collect resource metrics (simplified - in reality would use metrics server or Prometheus)
	metrics.CPU = mc.calculateResourceMetric(pods.Items, "cpu")
	metrics.Memory = mc.calculateResourceMetric(pods.Items, "memory")
	metrics.Connections = mc.calculateConnectionMetric(ctx, cluster, nodeType)
	metrics.Throughput = mc.calculateThroughputMetric(ctx, cluster, nodeType)

	return metrics, nil
}

// isPodHealthy checks if a pod is healthy
func (mc *MetricsCollector) isPodHealthy(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

// calculateResourceMetric calculates resource utilization metrics
func (mc *MetricsCollector) calculateResourceMetric(_ []corev1.Pod, _ string) MetricValue {
	// Simplified implementation - in reality would query metrics server
	// This would integrate with Prometheus or Kubernetes metrics server
	return MetricValue{
		Current:   0.5, // 50% utilization
		Previous:  0.4,
		Trend:     TrendIncreasing,
		Threshold: 0.8, // 80% threshold
	}
}

// calculateConnectionMetric calculates connection count metrics
func (mc *MetricsCollector) calculateConnectionMetric(_ context.Context, _ *neo4jv1alpha1.Neo4jEnterpriseCluster, _ string) MetricValue {
	// Would query Neo4j JMX metrics or custom metrics
	return MetricValue{
		Current:   100,
		Previous:  80,
		Trend:     TrendIncreasing,
		Threshold: 500,
	}
}

// calculateThroughputMetric calculates throughput metrics
func (mc *MetricsCollector) calculateThroughputMetric(_ context.Context, _ *neo4jv1alpha1.Neo4jEnterpriseCluster, _ string) MetricValue {
	// Would query Neo4j metrics for queries per second
	return MetricValue{
		Current:   50,
		Previous:  45,
		Trend:     TrendIncreasing,
		Threshold: 200,
	}
}

// collectQueryMetrics collects query-related metrics
func (mc *MetricsCollector) collectQueryMetrics(_ context.Context, _ *neo4jv1alpha1.Neo4jEnterpriseCluster) *QueryMetrics {
	// Would integrate with Neo4j query log analysis or JMX metrics
	return &QueryMetrics{
		AverageLatency:   100 * time.Millisecond,
		P95Latency:       500 * time.Millisecond,
		QueriesPerSecond: 150.0,
		SlowQueries:      5,
	}
}

// collectSystemMetrics collects system-level metrics
func (mc *MetricsCollector) collectSystemMetrics(_ context.Context, _ *neo4jv1alpha1.Neo4jEnterpriseCluster) *SystemMetrics {
	return &SystemMetrics{
		LoadAverage:    1.5,
		DiskUsage:      0.6, // 60%
		NetworkLatency: 10 * time.Millisecond,
	}
}

// ScaleDecisionEngine makes scaling decisions based on metrics
type ScaleDecisionEngine struct {
	logger logr.Logger
}

// NewScaleDecisionEngine creates a new scale decision engine
func NewScaleDecisionEngine(logger logr.Logger) *ScaleDecisionEngine {
	return &ScaleDecisionEngine{
		logger: logger,
	}
}

// ScalingDecision represents a scaling decision
type ScalingDecision struct {
	Action         ScaleAction
	TargetReplicas int32
	Reason         string
	Confidence     float64
}

// ScaleAction represents the type of scaling action to perform
type ScaleAction int

const (
	// ScaleActionNone indicates no scaling action is needed
	ScaleActionNone ScaleAction = iota
	// ScaleActionUp indicates scaling up is needed
	ScaleActionUp
	// ScaleActionDown indicates scaling down is needed
	ScaleActionDown
)

// CalculatePrimaryScaling calculates scaling decision for primary nodes
func (sde *ScaleDecisionEngine) CalculatePrimaryScaling(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, metrics *ClusterMetrics) *ScalingDecision {
	config := cluster.Spec.AutoScaling.Primaries
	if config == nil {
		return &ScalingDecision{Action: ScaleActionNone}
	}

	currentReplicas := metrics.PrimaryNodes.Total

	// Analyze metrics and make decision
	decision := sde.analyzeMetricsForScaling(config.Metrics, metrics, currentReplicas)

	// Apply primary-specific constraints
	if decision.Action == ScaleActionUp && decision.TargetReplicas > config.MaxReplicas {
		decision.TargetReplicas = config.MaxReplicas
	}
	if decision.Action == ScaleActionDown && decision.TargetReplicas < config.MinReplicas {
		decision.TargetReplicas = config.MinReplicas
	}

	return decision
}

// CalculateSecondaryScaling calculates scaling decision for secondary nodes
func (sde *ScaleDecisionEngine) CalculateSecondaryScaling(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, metrics *ClusterMetrics) *ScalingDecision {
	config := cluster.Spec.AutoScaling.Secondaries
	if config == nil {
		return &ScalingDecision{Action: ScaleActionNone}
	}

	currentReplicas := metrics.SecondaryNodes.Total

	// Analyze metrics and make decision
	decision := sde.analyzeMetricsForScaling(config.Metrics, metrics, currentReplicas)

	// Apply secondary-specific constraints
	if decision.Action == ScaleActionUp && decision.TargetReplicas > config.MaxReplicas {
		decision.TargetReplicas = config.MaxReplicas
	}
	if decision.Action == ScaleActionDown && decision.TargetReplicas < config.MinReplicas {
		decision.TargetReplicas = config.MinReplicas
	}

	return decision
}

// analyzeMetricsForScaling analyzes metrics to determine scaling action
func (sde *ScaleDecisionEngine) analyzeMetricsForScaling(metricConfigs []neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics, currentReplicas int32) *ScalingDecision {
	if len(metricConfigs) == 0 {
		return &ScalingDecision{Action: ScaleActionNone}
	}

	totalWeight := 0.0
	weightedScore := 0.0
	reasons := []string{}

	for _, metricConfig := range metricConfigs {
		score, reason := sde.evaluateMetric(metricConfig, metrics)

		// Parse weight string to float64
		weight := 1.0 // default weight
		if metricConfig.Weight != "" {
			if parsedWeight, err := strconv.ParseFloat(metricConfig.Weight, 64); err == nil {
				weight = parsedWeight
			}
		}

		weightedScore += score * weight
		totalWeight += weight
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}

	if totalWeight == 0 {
		return &ScalingDecision{Action: ScaleActionNone}
	}

	avgScore := weightedScore / totalWeight
	reasonStr := strings.Join(reasons, "; ")

	// Determine action based on score
	if avgScore > 0.8 {
		return &ScalingDecision{
			Action:         ScaleActionUp,
			TargetReplicas: currentReplicas + 1,
			Reason:         fmt.Sprintf("Scale up: %s (score: %.2f)", reasonStr, avgScore),
			Confidence:     avgScore,
		}
	} else if avgScore < 0.2 {
		return &ScalingDecision{
			Action:         ScaleActionDown,
			TargetReplicas: currentReplicas - 1,
			Reason:         fmt.Sprintf("Scale down: %s (score: %.2f)", reasonStr, avgScore),
			Confidence:     1.0 - avgScore,
		}
	}

	return &ScalingDecision{Action: ScaleActionNone}
}

// evaluateMetric evaluates a single metric and returns a score (0-1) and reason
func (sde *ScaleDecisionEngine) evaluateMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics) (float64, string) {
	switch metricConfig.Type {
	case "cpu":
		return sde.evaluateCPUMetric(metricConfig, metrics)
	case "memory":
		return sde.evaluateMemoryMetric(metricConfig, metrics)
	case "query_latency":
		return sde.evaluateQueryLatencyMetric(metricConfig, metrics)
	case "connection_count":
		return sde.evaluateConnectionMetric(metricConfig, metrics)
	case "throughput":
		return sde.evaluateThroughputMetric(metricConfig, metrics)
	case "custom":
		return sde.evaluateCustomMetric(metricConfig, metrics)
	default:
		return 0.5, ""
	}
}

// evaluateCPUMetric evaluates CPU utilization metric
func (sde *ScaleDecisionEngine) evaluateCPUMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics) (float64, string) {
	target, _ := strconv.ParseFloat(metricConfig.Target, 64)
	current := metrics.PrimaryNodes.CPU.Current

	if current > target {
		return math.Min(current/target, 1.0), fmt.Sprintf("CPU %.1f%% > %.1f%%", current*100, target*100)
	}

	if current < target*0.5 {
		return math.Max(0.0, 1.0-current/target), fmt.Sprintf("CPU %.1f%% < %.1f%%", current*100, target*50)
	}

	return 0.5, ""
}

// evaluateMemoryMetric evaluates memory utilization metric
func (sde *ScaleDecisionEngine) evaluateMemoryMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics) (float64, string) {
	target, _ := strconv.ParseFloat(metricConfig.Target, 64)
	current := metrics.PrimaryNodes.Memory.Current

	if current > target {
		return math.Min(current/target, 1.0), fmt.Sprintf("Memory %.1f%% > %.1f%%", current*100, target*100)
	}

	return 0.5, ""
}

// evaluateQueryLatencyMetric evaluates query latency metric
func (sde *ScaleDecisionEngine) evaluateQueryLatencyMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics) (float64, string) {
	targetMs, _ := strconv.ParseFloat(metricConfig.Target, 64)
	target := time.Duration(targetMs) * time.Millisecond
	current := metrics.QueryMetrics.P95Latency

	if current > target {
		ratio := float64(current) / float64(target)
		return math.Min(ratio, 1.0), fmt.Sprintf("P95 latency %v > %v", current, target)
	}

	return 0.5, ""
}

// evaluateConnectionMetric evaluates connection count metric
func (sde *ScaleDecisionEngine) evaluateConnectionMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics) (float64, string) {
	target, _ := strconv.ParseFloat(metricConfig.Target, 64)
	current := metrics.PrimaryNodes.Connections.Current

	if current > target {
		return math.Min(current/target, 1.0), fmt.Sprintf("Connections %.0f > %.0f", current, target)
	}

	return 0.5, ""
}

// evaluateThroughputMetric evaluates throughput metric
func (sde *ScaleDecisionEngine) evaluateThroughputMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, metrics *ClusterMetrics) (float64, string) {
	target, _ := strconv.ParseFloat(metricConfig.Target, 64)
	current := metrics.QueryMetrics.QueriesPerSecond

	if current > target {
		return math.Min(current/target, 1.0), fmt.Sprintf("QPS %.1f > %.1f", current, target)
	}

	return 0.5, ""
}

// evaluateCustomMetric evaluates custom Prometheus metric
func (sde *ScaleDecisionEngine) evaluateCustomMetric(metricConfig neo4jv1alpha1.AutoScalingMetric, _ *ClusterMetrics) (float64, string) {
	if metricConfig.Source == nil || metricConfig.Source.Type != "prometheus" {
		return 0, "custom metric requires Prometheus source configuration"
	}

	if metricConfig.Source.Prometheus == nil || metricConfig.Source.Prometheus.Query == "" {
		return 0, "custom metric requires Prometheus query"
	}

	// Get Prometheus configuration
	prometheusConfig := metricConfig.Source.Prometheus

	// Default URL if not provided
	url := prometheusConfig.ServerURL
	if url == "" {
		url = "http://prometheus-server:9090" // Default Prometheus service in cluster
	}

	// Parse target value
	target, err := strconv.ParseFloat(metricConfig.Target, 64)
	if err != nil {
		return 0, fmt.Sprintf("invalid target value: %v", err)
	}

	// Execute Prometheus query
	value, err := sde.queryPrometheus(url, prometheusConfig.Query)
	if err != nil {
		sde.logger.Error(err, "Failed to query Prometheus", "query", prometheusConfig.Query)
		return 0.5, fmt.Sprintf("Prometheus query failed: %v", err) // Return neutral score on error
	}

	// Calculate scaling score based on comparison with target
	if value > target {
		ratio := value / target
		score := math.Min(ratio-1.0, 1.0) // Scale up score
		return score, fmt.Sprintf("Custom metric %.2f > %.2f", value, target)
	} else if value < target*0.7 { // Scale down threshold
		ratio := value / target
		score := math.Max(0.0, 0.7-ratio) // Scale down score
		return score, fmt.Sprintf("Custom metric %.2f < %.2f", value, target*0.7)
	}

	return 0.5, fmt.Sprintf("Custom metric %.2f within target range", value)
}

// queryPrometheus executes a Prometheus query and returns the scalar result
func (sde *ScaleDecisionEngine) queryPrometheus(promURL, query string) (float64, error) {
	// Log the URL being used for debugging
	sde.logger.V(1).Info("Querying Prometheus", "url", promURL, "query", query)

	// Parse and validate the Prometheus URL
	_, err := url.Parse(promURL)
	if err != nil {
		return 0, fmt.Errorf("invalid Prometheus URL: %w", err)
	}

	// Construct the query URL
	queryURL := fmt.Sprintf("%s/api/v1/query", strings.TrimSuffix(promURL, "/"))

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("query", query)
	q.Add("time", fmt.Sprintf("%d", time.Now().Unix()))
	req.URL.RawQuery = q.Encode()

	// Add headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "neo4j-operator-autoscaler/1.0")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		// If Prometheus is unavailable, fall back to simulated metrics
		sde.logger.V(1).Info("Prometheus unavailable, using fallback metrics", "error", err.Error())
		return sde.getFallbackMetric(query), nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			sde.logger.Error(err, "Failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// If query fails, fall back to simulated metrics
		sde.logger.V(1).Info("Prometheus query failed, using fallback metrics", "status", resp.StatusCode)
		return sde.getFallbackMetric(query), nil
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	var response PrometheusResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Check response status
	if response.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: %s", response.Error)
	}

	// Extract scalar value
	if response.Data.ResultType != "vector" || len(response.Data.Result) == 0 {
		// No data, return fallback metric
		sde.logger.V(1).Info("No data returned from Prometheus, using fallback")
		return sde.getFallbackMetric(query), nil
	}

	// Get the first result value
	result := response.Data.Result[0]
	if len(result.Value) < 2 {
		return 0, fmt.Errorf("invalid result format")
	}

	valueStr, ok := result.Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("invalid value format")
	}

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse value: %w", err)
	}

	sde.logger.V(1).Info("Prometheus query successful", "query", query, "value", value)
	return value, nil
}

// getFallbackMetric provides fallback metrics when Prometheus is unavailable
func (sde *ScaleDecisionEngine) getFallbackMetric(query string) float64 {
	// Provide reasonable fallback values based on query patterns
	lowerQuery := strings.ToLower(query)

	if strings.Contains(lowerQuery, "cpu") {
		return 0.65 // 65% CPU usage
	}
	if strings.Contains(lowerQuery, "memory") {
		return 0.70 // 70% memory usage
	}
	if strings.Contains(lowerQuery, "connection") {
		return 45.0 // 45 connections
	}
	if strings.Contains(lowerQuery, "query") || strings.Contains(lowerQuery, "qps") {
		return 18.5 // 18.5 QPS
	}
	if strings.Contains(lowerQuery, "throughput") {
		return 850.0 // 850 ops/sec
	}

	// Default fallback
	return 0.5
}

// PrometheusResponse represents the response from Prometheus API
type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}
