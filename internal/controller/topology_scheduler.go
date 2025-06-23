package controller

import (
	"context"
	"fmt"
	"sort"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TopologyScheduler handles topology-aware placement of Neo4j cluster pods
type TopologyScheduler struct {
	client.Client
}

// NewTopologyScheduler creates a new topology scheduler
func NewTopologyScheduler(client client.Client) *TopologyScheduler {
	return &TopologyScheduler{
		Client: client,
	}
}

// CalculateTopologyPlacement determines optimal pod placement based on topology configuration
func (ts *TopologyScheduler) CalculateTopologyPlacement(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*TopologyPlacement, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.Topology.Placement == nil {
		logger.Info("No topology placement configuration specified, using default scheduling")
		placement := &TopologyPlacement{
			UseTopologySpread:   false,
			UseAntiAffinity:     false,
			AvailabilityZones:   cluster.Spec.Topology.AvailabilityZones,
			EnforceDistribution: cluster.Spec.Topology.EnforceDistribution,
		}

		// Still need to validate even with default scheduling
		if err := ts.validateTopologyConfiguration(cluster, placement); err != nil {
			return placement, err
		}

		return placement, nil
	}

	placement := &TopologyPlacement{
		UseTopologySpread:   cluster.Spec.Topology.Placement.TopologySpread != nil && cluster.Spec.Topology.Placement.TopologySpread.Enabled,
		UseAntiAffinity:     cluster.Spec.Topology.Placement.AntiAffinity != nil && cluster.Spec.Topology.Placement.AntiAffinity.Enabled,
		AvailabilityZones:   cluster.Spec.Topology.AvailabilityZones,
		EnforceDistribution: cluster.Spec.Topology.EnforceDistribution,
	}

	// If specific AZs are not provided, discover them from the cluster
	if len(placement.AvailabilityZones) == 0 {
		azs, err := ts.discoverAvailabilityZones(ctx)
		if err != nil {
			logger.Error(err, "Failed to discover availability zones")
			return placement, err
		}
		placement.AvailabilityZones = azs
	}

	// Validate topology configuration
	if err := ts.validateTopologyConfiguration(cluster, placement); err != nil {
		return placement, err
	}

	return placement, nil
}

// TopologyPlacement contains the calculated topology placement strategy
type TopologyPlacement struct {
	UseTopologySpread   bool     `json:"useTopologySpread"`
	UseAntiAffinity     bool     `json:"useAntiAffinity"`
	AvailabilityZones   []string `json:"availabilityZones"`
	EnforceDistribution bool     `json:"enforceDistribution"`
}

// ApplyTopologyConstraints applies topology constraints to a StatefulSet
func (ts *TopologyScheduler) ApplyTopologyConstraints(ctx context.Context, sts *appsv1.StatefulSet, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, placement *TopologyPlacement) error {
	logger := log.FromContext(ctx)

	if !placement.UseTopologySpread && !placement.UseAntiAffinity {
		logger.Info("No topology constraints configured")
		return nil
	}

	podTemplate := &sts.Spec.Template

	// Apply topology spread constraints
	if placement.UseTopologySpread {
		tsc := ts.buildTopologySpreadConstraints(cluster, placement)
		podTemplate.Spec.TopologySpreadConstraints = tsc
		logger.Info("Applied topology spread constraints", "constraints", len(tsc))
	}

	// Apply pod anti-affinity
	if placement.UseAntiAffinity {
		if podTemplate.Spec.Affinity == nil {
			podTemplate.Spec.Affinity = &corev1.Affinity{}
		}
		if podTemplate.Spec.Affinity.PodAntiAffinity == nil {
			podTemplate.Spec.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		antiAffinity := ts.buildPodAntiAffinity(cluster, placement)
		podTemplate.Spec.Affinity.PodAntiAffinity = antiAffinity
		logger.Info("Applied pod anti-affinity constraints")
	}

	return nil
}

// buildTopologySpreadConstraints creates topology spread constraints for the cluster
func (ts *TopologyScheduler) buildTopologySpreadConstraints(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, _ *TopologyPlacement) []corev1.TopologySpreadConstraint {
	config := cluster.Spec.Topology.Placement.TopologySpread
	constraints := []corev1.TopologySpreadConstraint{}

	// Default topology key is zone if not specified
	topologyKey := "topology.kubernetes.io/zone"
	if config.TopologyKey != "" {
		topologyKey = config.TopologyKey
	}

	// Default max skew is 1 for even distribution
	maxSkew := int32(1)
	if config.MaxSkew > 0 {
		maxSkew = config.MaxSkew
	}

	// Default when unsatisfiable is DoNotSchedule for hard constraints
	whenUnsatisfiable := corev1.DoNotSchedule
	if config.WhenUnsatisfiable == "ScheduleAnyway" {
		whenUnsatisfiable = corev1.ScheduleAnyway
	}

	// Primary servers constraint
	if cluster.Spec.Topology.Primaries > 0 {
		constraints = append(constraints, corev1.TopologySpreadConstraint{
			MaxSkew:           maxSkew,
			TopologyKey:       topologyKey,
			WhenUnsatisfiable: whenUnsatisfiable,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "neo4j",
					"app.kubernetes.io/instance":  cluster.Name,
					"app.kubernetes.io/component": "primary",
				},
			},
		})
	}

	// Secondary servers constraint
	if cluster.Spec.Topology.Secondaries > 0 {
		constraints = append(constraints, corev1.TopologySpreadConstraint{
			MaxSkew:           maxSkew,
			TopologyKey:       topologyKey,
			WhenUnsatisfiable: whenUnsatisfiable,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "neo4j",
					"app.kubernetes.io/instance":  cluster.Name,
					"app.kubernetes.io/component": "secondary",
				},
			},
		})
	}

	// Add minimum domains constraint if specified
	if config.MinDomains != nil && *config.MinDomains > 0 {
		for i := range constraints {
			constraints[i].MinDomains = config.MinDomains
		}
	}

	return constraints
}

// buildPodAntiAffinity creates pod anti-affinity rules for the cluster
func (ts *TopologyScheduler) buildPodAntiAffinity(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, placement *TopologyPlacement) *corev1.PodAntiAffinity {
	config := cluster.Spec.Topology.Placement.AntiAffinity

	// Default topology key is zone
	topologyKey := "topology.kubernetes.io/zone"
	if config.TopologyKey != "" {
		topologyKey = config.TopologyKey
	}

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/name":     "neo4j",
			"app.kubernetes.io/instance": cluster.Name,
		},
	}

	antiAffinityTerm := corev1.PodAffinityTerm{
		LabelSelector: labelSelector,
		TopologyKey:   topologyKey,
	}

	antiAffinity := &corev1.PodAntiAffinity{}

	// Use required or preferred anti-affinity based on configuration
	if config.Type == "required" || placement.EnforceDistribution {
		antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = []corev1.PodAffinityTerm{
			antiAffinityTerm,
		}
	} else {
		antiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.WeightedPodAffinityTerm{
			{
				Weight:          100,
				PodAffinityTerm: antiAffinityTerm,
			},
		}
	}

	return antiAffinity
}

// discoverAvailabilityZones discovers available zones in the cluster
func (ts *TopologyScheduler) discoverAvailabilityZones(ctx context.Context) ([]string, error) {
	nodes := &corev1.NodeList{}
	if err := ts.List(ctx, nodes); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	zones := make(map[string]bool)
	for _, node := range nodes.Items {
		if zone, exists := node.Labels["topology.kubernetes.io/zone"]; exists {
			zones[zone] = true
		}
	}

	result := make([]string, 0, len(zones))
	for zone := range zones {
		result = append(result, zone)
	}
	sort.Strings(result)

	return result, nil
}

// validateTopologyConfiguration validates the topology configuration against cluster constraints
func (ts *TopologyScheduler) validateTopologyConfiguration(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, placement *TopologyPlacement) error {
	if cluster == nil {
		return fmt.Errorf("cluster cannot be nil")
	}
	if placement == nil {
		return fmt.Errorf("placement cannot be nil")
	}

	totalReplicas := cluster.Spec.Topology.Primaries + cluster.Spec.Topology.Secondaries
	numZones := int32(len(placement.AvailabilityZones))

	// Validate primary count
	if cluster.Spec.Topology.Primaries < 1 {
		return fmt.Errorf("primaries must be at least 1, got %d", cluster.Spec.Topology.Primaries)
	}

	// Validate odd number of primaries for quorum
	if cluster.Spec.Topology.Primaries%2 == 0 {
		log.Log.Info("Warning: Even number of primaries may cause split-brain scenarios",
			"primaries", cluster.Spec.Topology.Primaries,
			"cluster", cluster.Name)
	}

	// Validate that we have enough zones for distribution
	if placement.EnforceDistribution && cluster.Spec.Topology.Primaries > numZones {
		return fmt.Errorf("cannot enforce distribution: %d primaries require at least %d availability zones, but only %d are available",
			cluster.Spec.Topology.Primaries, numZones, numZones)
	}

	// Validate minimum zones for high availability
	if placement.EnforceDistribution && numZones < 2 {
		return fmt.Errorf("enforced distribution requires at least 2 availability zones, but only %d are available", numZones)
	}

	// Warn if total replicas might not distribute evenly
	if totalReplicas > 0 && numZones > 0 && totalReplicas%numZones != 0 {
		log.Log.Info("Warning: Total replicas may not distribute evenly across availability zones",
			"totalReplicas", totalReplicas,
			"availabilityZones", numZones,
			"cluster", cluster.Name)
	}

	return nil
}

// GetTopologyDistribution returns the current topology distribution of cluster pods
func (ts *TopologyScheduler) GetTopologyDistribution(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*TopologyDistribution, error) {
	pods := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(map[string]string{
			"app.kubernetes.io/name":     "neo4j",
			"app.kubernetes.io/instance": cluster.Name,
		}),
	}

	if err := ts.List(ctx, pods, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list cluster pods: %w", err)
	}

	distribution := &TopologyDistribution{
		ZoneDistribution: make(map[string]*ZoneDistribution),
	}

	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" {
			continue // Pod not scheduled yet
		}

		// Get the node to determine its zone
		node := &corev1.Node{}
		if err := ts.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
			continue // Skip if we can't get node info
		}

		zone := node.Labels["topology.kubernetes.io/zone"]
		if zone == "" {
			zone = "unknown"
		}

		if distribution.ZoneDistribution[zone] == nil {
			distribution.ZoneDistribution[zone] = &ZoneDistribution{}
		}

		// Determine pod role based on labels or StatefulSet ordinal
		component := pod.Labels["app.kubernetes.io/component"]
		switch component {
		case "primary":
			distribution.ZoneDistribution[zone].Primaries++
		case "secondary":
			distribution.ZoneDistribution[zone].Secondaries++
		default:
			// Fallback: determine by ordinal (first N are primaries)
			if pod.Labels["statefulset.kubernetes.io/pod-name"] != "" {
				// Extract ordinal from pod name (e.g., cluster-0, cluster-1)
				// For simplicity, assume first N pods are primaries
				distribution.ZoneDistribution[zone].Primaries++
			}
		}
	}

	return distribution, nil
}

// TopologyDistribution represents the current distribution of pods across zones
type TopologyDistribution struct {
	ZoneDistribution map[string]*ZoneDistribution `json:"zoneDistribution"`
}

// ZoneDistribution represents pod distribution within a single zone
type ZoneDistribution struct {
	Primaries   int32 `json:"primaries"`
	Secondaries int32 `json:"secondaries"`
}

// IsBalanced checks if the topology distribution meets the desired balance
func (td *TopologyDistribution) IsBalanced(desiredPrimaries, desiredSecondaries int32, maxSkew int32) bool {
	if len(td.ZoneDistribution) == 0 {
		return false
	}

	zones := make([]string, 0, len(td.ZoneDistribution))
	for zone := range td.ZoneDistribution {
		zones = append(zones, zone)
	}

	// Check primary distribution
	if desiredPrimaries > 0 {
		primaryCounts := make([]int32, len(zones))
		for i, zone := range zones {
			primaryCounts[i] = td.ZoneDistribution[zone].Primaries
		}
		if !isDistributionBalanced(primaryCounts, maxSkew) {
			return false
		}
	}

	// Check secondary distribution
	if desiredSecondaries > 0 {
		secondaryCounts := make([]int32, len(zones))
		for i, zone := range zones {
			secondaryCounts[i] = td.ZoneDistribution[zone].Secondaries
		}
		if !isDistributionBalanced(secondaryCounts, maxSkew) {
			return false
		}
	}

	return true
}

// isDistributionBalanced checks if counts are within maxSkew of each other
func isDistributionBalanced(counts []int32, maxSkew int32) bool {
	if len(counts) <= 1 {
		return true
	}

	var min, max = counts[0], counts[0]
	for _, count := range counts[1:] {
		if count < min {
			min = count
		}
		if count > max {
			max = count
		}
	}

	return max-min <= maxSkew
}
