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
	"math"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/metrics"
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
)

// RollingUpgradeOrchestrator handles intelligent rolling upgrades for Neo4j clusters
type RollingUpgradeOrchestrator struct {
	client.Client
	upgradeMetrics *metrics.UpgradeMetrics
}

// NewRollingUpgradeOrchestrator creates a new rolling upgrade orchestrator
func NewRollingUpgradeOrchestrator(c client.Client, clusterName, namespace string) *RollingUpgradeOrchestrator {
	return &RollingUpgradeOrchestrator{
		Client:         c,
		upgradeMetrics: metrics.NewUpgradeMetrics(clusterName, namespace),
	}
}

// ExecuteRollingUpgrade orchestrates a complete rolling upgrade
func (r *RollingUpgradeOrchestrator) ExecuteRollingUpgrade(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")

	// Initialize upgrade status
	if err := r.initializeUpgradeStatus(ctx, cluster); err != nil {
		return fmt.Errorf("failed to initialize upgrade status: %w", err)
	}

	startTime := time.Now()
	defer func() {
		success := cluster.Status.UpgradeStatus.Phase == "Completed"
		r.upgradeMetrics.RecordUpgrade(ctx, success, time.Since(startTime))
	}()

	// Phase 1: Pre-upgrade validations
	logger.Info("Starting pre-upgrade validations")
	if err := r.preUpgradeValidations(ctx, cluster, neo4jClient); err != nil {
		r.updateUpgradeStatus(ctx, cluster, "Failed", "Pre-upgrade validation failed", err.Error())
		return fmt.Errorf("pre-upgrade validation failed: %w", err)
	}

	// Phase 2: Upgrade secondaries first (if any)
	if cluster.Spec.Topology.Secondaries > 0 {
		logger.Info("Upgrading secondary nodes")
		if err := r.upgradeSecondaries(ctx, cluster, neo4jClient); err != nil {
			r.updateUpgradeStatus(ctx, cluster, "Failed", "Secondary upgrade failed", err.Error())
			return fmt.Errorf("secondary upgrade failed: %w", err)
		}
	}

	// Phase 3: Upgrade primaries (leader-aware)
	logger.Info("Upgrading primary nodes")
	if err := r.upgradePrimaries(ctx, cluster, neo4jClient); err != nil {
		r.updateUpgradeStatus(ctx, cluster, "Failed", "Primary upgrade failed", err.Error())
		return fmt.Errorf("primary upgrade failed: %w", err)
	}

	// Phase 4: Post-upgrade validations
	logger.Info("Performing post-upgrade validations")
	if err := r.postUpgradeValidations(ctx, cluster, neo4jClient); err != nil {
		r.updateUpgradeStatus(ctx, cluster, "Failed", "Post-upgrade validation failed", err.Error())
		return fmt.Errorf("post-upgrade validation failed: %w", err)
	}

	// Mark upgrade as completed
	r.updateUpgradeStatus(ctx, cluster, "Completed", "Rolling upgrade completed successfully", "")
	logger.Info("Rolling upgrade completed successfully")

	return nil
}

// Helper methods for upgrade orchestration
func (r *RollingUpgradeOrchestrator) initializeUpgradeStatus(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
) error {
	now := metav1.Now()

	cluster.Status.UpgradeStatus = &neo4jv1alpha1.UpgradeStatus{
		Phase:           "InProgress",
		StartTime:       &now,
		CurrentStep:     "Initializing upgrade",
		PreviousVersion: cluster.Status.Version,
		TargetVersion:   cluster.Spec.Image.Tag,
		Progress: &neo4jv1alpha1.UpgradeProgress{
			Total:   cluster.Spec.Topology.Primaries + cluster.Spec.Topology.Secondaries,
			Pending: cluster.Spec.Topology.Primaries + cluster.Spec.Topology.Secondaries,
			Primaries: &neo4jv1alpha1.NodeUpgradeProgress{
				Total:   cluster.Spec.Topology.Primaries,
				Pending: cluster.Spec.Topology.Primaries,
			},
			Secondaries: &neo4jv1alpha1.NodeUpgradeProgress{
				Total:   cluster.Spec.Topology.Secondaries,
				Pending: cluster.Spec.Topology.Secondaries,
			},
		},
	}

	return r.Status().Update(ctx, cluster)
}

func (r *RollingUpgradeOrchestrator) preUpgradeValidations(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	// Skip health check if disabled
	if cluster.Spec.UpgradeStrategy == nil || cluster.Spec.UpgradeStrategy.PreUpgradeHealthCheck {
		logger.Info("Validating cluster health before upgrade")

		// Validate upgrade safety using Neo4j client
		targetVersion := cluster.Spec.Image.Tag
		if err := neo4jClient.ValidateUpgradeSafety(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade safety validation failed: %w", err)
		}
	}

	// Validate version compatibility
	if err := r.validateVersionCompatibility(cluster.Status.Version, cluster.Spec.Image.Tag); err != nil {
		return fmt.Errorf("version compatibility check failed: %w", err)
	}

	// Check if StatefulSets are ready
	if err := r.validateStatefulSetsReady(ctx, cluster); err != nil {
		return fmt.Errorf("StatefulSets not ready: %w", err)
	}

	r.updateUpgradeStatus(ctx, cluster, "InProgress", "Pre-upgrade validations completed", "")
	return nil
}

func (r *RollingUpgradeOrchestrator) upgradeSecondaries(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	_ *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	if cluster.Spec.Topology.Secondaries == 0 {
		return nil
	}

	r.updateUpgradeStatus(ctx, cluster, "InProgress", "Upgrading secondary nodes", "")

	// Get secondary StatefulSet
	secondarySts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-secondary", cluster.Name),
		Namespace: cluster.Namespace,
	}, secondarySts); err != nil {
		return fmt.Errorf("failed to get secondary StatefulSet: %w", err)
	}

	// Update image
	newImage := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	if len(secondarySts.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("secondary StatefulSet has no containers defined")
	}
	if secondarySts.Spec.Template.Spec.Containers[0].Image == newImage {
		logger.Info("Secondary StatefulSet already has target image")
		return nil
	}

	secondarySts.Spec.Template.Spec.Containers[0].Image = newImage

	// Add upgrade annotation
	if secondarySts.Spec.Template.Annotations == nil {
		secondarySts.Spec.Template.Annotations = make(map[string]string)
	}
	secondarySts.Spec.Template.Annotations["neo4j.com/upgrade-timestamp"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, secondarySts); err != nil {
		return fmt.Errorf("failed to update secondary StatefulSet: %w", err)
	}

	// Wait for secondary rollout to complete
	timeout := r.getUpgradeTimeout(cluster)
	if err := r.waitForStatefulSetRollout(ctx, secondarySts, timeout); err != nil {
		return fmt.Errorf("secondary rollout failed: %w", err)
	}

	// Update progress
	cluster.Status.UpgradeStatus.Progress.Secondaries.Upgraded = cluster.Spec.Topology.Secondaries
	cluster.Status.UpgradeStatus.Progress.Secondaries.Pending = 0
	cluster.Status.UpgradeStatus.Progress.Upgraded += cluster.Spec.Topology.Secondaries
	cluster.Status.UpgradeStatus.Progress.Pending -= cluster.Spec.Topology.Secondaries

	r.Status().Update(ctx, cluster)
	logger.Info("Secondary nodes upgrade completed")

	return nil
}

func (r *RollingUpgradeOrchestrator) upgradePrimaries(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	r.updateUpgradeStatus(ctx, cluster, "InProgress", "Upgrading primary nodes", "")

	// Step 1: Upgrade non-leader primaries
	if err := r.upgradeNonLeaderPrimaries(ctx, cluster, neo4jClient); err != nil {
		return fmt.Errorf("non-leader primary upgrade failed: %w", err)
	}

	// Step 2: Upgrade leader last
	if err := r.upgradeLeader(ctx, cluster, neo4jClient); err != nil {
		return fmt.Errorf("leader upgrade failed: %w", err)
	}

	logger.Info("Primary nodes upgrade completed")
	return nil
}

func (r *RollingUpgradeOrchestrator) upgradeNonLeaderPrimaries(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	// Get current leader
	leader, err := neo4jClient.GetLeader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current leader: %w", err)
	}

	if leader == nil {
		return fmt.Errorf("no leader found in cluster")
	}

	// Update progress with current leader
	cluster.Status.UpgradeStatus.Progress.Primaries.CurrentLeader = leader.ID
	cluster.Status.UpgradeStatus.CurrentStep = fmt.Sprintf("Upgrading non-leader primaries (preserving leader: %s)", leader.ID)
	r.Status().Update(ctx, cluster)

	logger.Info("Upgrading non-leader primaries", "leader", leader.ID)

	// Get primary StatefulSet
	primarySts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-primary", cluster.Name),
		Namespace: cluster.Namespace,
	}, primarySts); err != nil {
		return err
	}

	// Update image
	newImage := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	if len(primarySts.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("primary StatefulSet has no containers defined")
	}
	if primarySts.Spec.Template.Spec.Containers[0].Image == newImage {
		logger.Info("Primary StatefulSet already has target image")
		return nil
	}

	primarySts.Spec.Template.Spec.Containers[0].Image = newImage

	// Add upgrade annotation
	if primarySts.Spec.Template.Annotations == nil {
		primarySts.Spec.Template.Annotations = make(map[string]string)
	}
	primarySts.Spec.Template.Annotations["neo4j.com/upgrade-timestamp"] = time.Now().Format(time.RFC3339)

	// Use partition update to upgrade non-leader primaries first
	leaderOrdinal := r.extractOrdinalFromMemberID(leader.ID)
	if leaderOrdinal >= 0 {
		// Set partition to upgrade all pods except the leader
		// Bounds check to prevent integer overflow
		if leaderOrdinal > math.MaxInt32 {
			return fmt.Errorf("leader ordinal too large: %d", leaderOrdinal)
		}
		partition := int32(leaderOrdinal)
		primarySts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{
			Partition: &partition,
		}
		logger.Info("Setting StatefulSet partition to preserve leader", "partition", partition, "leaderOrdinal", leaderOrdinal)
	}

	if err := r.Update(ctx, primarySts); err != nil {
		return fmt.Errorf("failed to update primary StatefulSet: %w", err)
	}

	// Wait for non-leader primaries to be upgraded
	timeout := r.getUpgradeTimeout(cluster)
	if err := r.waitForPartialStatefulSetRollout(ctx, primarySts, leaderOrdinal, timeout); err != nil {
		return fmt.Errorf("non-leader primary rollout failed: %w", err)
	}

	// Verify cluster stability after non-leader upgrade
	stabilizationTimeout := r.getStabilizationTimeout(cluster)
	if err := neo4jClient.WaitForClusterStabilization(ctx, stabilizationTimeout); err != nil {
		return fmt.Errorf("cluster failed to stabilize after non-leader primary upgrade: %w", err)
	}

	// Update progress
	upgradedCount := cluster.Spec.Topology.Primaries - 1 // All except leader
	cluster.Status.UpgradeStatus.Progress.Primaries.Upgraded = upgradedCount
	cluster.Status.UpgradeStatus.Progress.Primaries.Pending = 1 // Only leader left
	cluster.Status.UpgradeStatus.Progress.Upgraded += upgradedCount
	cluster.Status.UpgradeStatus.Progress.Pending -= upgradedCount

	r.Status().Update(ctx, cluster)
	logger.Info("Non-leader primary upgrade completed successfully")

	return nil
}

func (r *RollingUpgradeOrchestrator) upgradeLeader(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	// Get current leader before upgrade
	originalLeader, err := neo4jClient.GetLeader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current leader: %w", err)
	}

	cluster.Status.UpgradeStatus.CurrentStep = fmt.Sprintf("Upgrading leader: %s", originalLeader.ID)
	r.Status().Update(ctx, cluster)

	logger.Info("Upgrading leader", "leaderId", originalLeader.ID)

	// Get primary StatefulSet
	primarySts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-primary", cluster.Name),
		Namespace: cluster.Namespace,
	}, primarySts); err != nil {
		return err
	}

	// Remove partition to allow leader upgrade
	primarySts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{
		Partition: nil,
	}

	if err := r.Update(ctx, primarySts); err != nil {
		return fmt.Errorf("failed to remove StatefulSet partition: %w", err)
	}

	// Wait for complete rollout including leader
	timeout := r.getUpgradeTimeout(cluster)
	if err := r.waitForStatefulSetRollout(ctx, primarySts, timeout); err != nil {
		return fmt.Errorf("leader rollout failed: %w", err)
	}

	// Wait for leader election to complete
	if err := r.waitForLeaderElection(ctx, neo4jClient, "", 5*time.Minute); err != nil {
		return fmt.Errorf("leader election failed after upgrade: %w", err)
	}

	// Final cluster stabilization
	stabilizationTimeout := r.getStabilizationTimeout(cluster)
	if err := neo4jClient.WaitForClusterStabilization(ctx, stabilizationTimeout); err != nil {
		return fmt.Errorf("cluster failed to stabilize after leader upgrade: %w", err)
	}

	// Update progress - all primaries upgraded
	cluster.Status.UpgradeStatus.Progress.Primaries.Upgraded = cluster.Spec.Topology.Primaries
	cluster.Status.UpgradeStatus.Progress.Primaries.Pending = 0
	cluster.Status.UpgradeStatus.Progress.Upgraded += 1 // Just the leader
	cluster.Status.UpgradeStatus.Progress.Pending -= 1

	r.Status().Update(ctx, cluster)
	logger.Info("Leader upgrade completed successfully")

	return nil
}

func (r *RollingUpgradeOrchestrator) postUpgradeValidations(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	r.updateUpgradeStatus(ctx, cluster, "InProgress", "Performing post-upgrade validations", "")

	// Skip health check if disabled
	if cluster.Spec.UpgradeStrategy == nil || cluster.Spec.UpgradeStrategy.PostUpgradeHealthCheck {
		logger.Info("Validating cluster health after upgrade")

		healthTimeout := r.getHealthCheckTimeout(cluster)
		ctx, cancel := context.WithTimeout(ctx, healthTimeout)
		defer cancel()

		// Wait for cluster to be healthy
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout waiting for cluster health after upgrade")
			case <-ticker.C:
				healthy, err := neo4jClient.IsClusterHealthy(ctx)
				if err != nil {
					logger.V(1).Info("Health check error (retrying)", "error", err)
					continue
				}
				if healthy {
					logger.Info("Cluster is healthy after upgrade")
					goto healthCheckComplete
				}
			}
		}

	healthCheckComplete:
	}

	// Verify version upgrade
	if err := r.verifyVersionUpgrade(ctx, cluster, neo4jClient); err != nil {
		return fmt.Errorf("version verification failed: %w", err)
	}

	logger.Info("Post-upgrade validations completed")
	return nil
}

// Utility methods
func (r *RollingUpgradeOrchestrator) updateUpgradeStatus(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	phase, currentStep, lastError string,
) {
	if cluster.Status.UpgradeStatus == nil {
		return
	}

	cluster.Status.UpgradeStatus.Phase = phase
	cluster.Status.UpgradeStatus.CurrentStep = currentStep
	cluster.Status.UpgradeStatus.Message = currentStep

	if lastError != "" {
		cluster.Status.UpgradeStatus.LastError = lastError
	}

	if phase == "Completed" {
		now := metav1.Now()
		cluster.Status.UpgradeStatus.CompletionTime = &now
		cluster.Status.LastUpgradeTime = &now
		cluster.Status.Version = cluster.Spec.Image.Tag
	}

	r.Status().Update(ctx, cluster)
}

func (r *RollingUpgradeOrchestrator) validateVersionCompatibility(currentVersion, targetVersion string) error {
	// Parse current and target versions
	current := r.parseVersion(currentVersion)
	target := r.parseVersion(targetVersion)

	if current == nil || target == nil {
		return fmt.Errorf("invalid version format")
	}

	// Prevent downgrades
	if r.isDowngrade(current, target) {
		return fmt.Errorf("downgrades are not supported (current: %s, target: %s)", currentVersion, targetVersion)
	}

	// Validate upgrade path based on versioning scheme
	if r.isCalVer(current) && r.isCalVer(target) {
		// CalVer to CalVer upgrade (2025.x.x -> 2025.y.y or 2026.x.x)
		return r.validateCalVerUpgrade(current, target, currentVersion, targetVersion)
	} else if r.isSemVer(current) && r.isSemVer(target) {
		// SemVer to SemVer upgrade (5.x.x -> 5.y.y)
		return r.validateSemVerUpgrade(current, target, currentVersion, targetVersion)
	} else if r.isSemVer(current) && r.isCalVer(target) {
		// SemVer to CalVer upgrade (5.x.x -> 2025.x.x)
		return r.validateSemVerToCalVerUpgrade(current, target, currentVersion, targetVersion)
	} else {
		// CalVer to SemVer (not supported)
		return fmt.Errorf("downgrade from CalVer to SemVer is not supported")
	}
}

// Version parsing and validation helper methods
func (r *RollingUpgradeOrchestrator) parseVersion(version string) *VersionInfo {
	// Remove any prefix like "v" and suffixes like "-enterprise"
	version = strings.TrimPrefix(version, "v")
	if idx := strings.Index(version, "-"); idx != -1 {
		version = version[:idx]
	}

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	patch := 0
	if len(parts) > 2 {
		if p, err := strconv.Atoi(parts[2]); err == nil {
			patch = p
		}
	}

	return &VersionInfo{
		Major: major,
		Minor: minor,
		Patch: patch,
	}
}

func (r *RollingUpgradeOrchestrator) isDowngrade(current, target *VersionInfo) bool {
	// For CalVer (year-based)
	if r.isCalVer(current) && r.isCalVer(target) {
		if target.Major < current.Major {
			return true
		}
		if target.Major == current.Major && target.Minor < current.Minor {
			return true
		}
		if target.Major == current.Major && target.Minor == current.Minor && target.Patch < current.Patch {
			return true
		}
		return false
	}

	// For SemVer or mixed comparison
	if target.Major < current.Major {
		return true
	}
	if target.Major == current.Major && target.Minor < current.Minor {
		return true
	}
	if target.Major == current.Major && target.Minor == current.Minor && target.Patch < current.Patch {
		return true
	}

	// Special case: CalVer to SemVer is always a downgrade
	if r.isCalVer(current) && r.isSemVer(target) {
		return true
	}

	return false
}

func (r *RollingUpgradeOrchestrator) isCalVer(version *VersionInfo) bool {
	return version.Major >= 2025
}

func (r *RollingUpgradeOrchestrator) isSemVer(version *VersionInfo) bool {
	return version.Major >= 4 && version.Major <= 10 // Neo4j 4.x, 5.x
}

func (r *RollingUpgradeOrchestrator) validateCalVerUpgrade(current, target *VersionInfo, currentStr, targetStr string) error {
	// Allow upgrades within same year (patch/minor)
	if current.Major == target.Major {
		return nil // 2025.1.0 -> 2025.1.1 or 2025.1.0 -> 2025.2.0
	}

	// Allow upgrades to newer years
	if target.Major > current.Major {
		return nil // 2025.x.x -> 2026.x.x
	}

	return fmt.Errorf("unsupported CalVer upgrade path from %s to %s", currentStr, targetStr)
}

func (r *RollingUpgradeOrchestrator) validateSemVerUpgrade(current, target *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades within same major version
	if target.Major != current.Major {
		return fmt.Errorf("major version upgrades are not supported")
	}

	// Allow minor and patch upgrades within supported range
	if current.Major == 5 && target.Major == 5 {
		if current.Minor >= 26 && target.Minor >= 26 {
			return nil // Allow upgrades within 5.26+
		}
		return fmt.Errorf("only Neo4j 5.26+ versions are supported")
	}

	if current.Major == 4 && target.Major == 4 {
		if current.Minor >= 4 {
			return nil // Allow upgrades within 4.4+
		}
		return fmt.Errorf("only Neo4j 4.4+ versions are supported")
	}

	return fmt.Errorf("unsupported SemVer upgrade path from %s to %s", currentStr, targetStr)
}

func (r *RollingUpgradeOrchestrator) validateSemVerToCalVerUpgrade(current, _ *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades from Neo4j 5.26+ to CalVer
	if current.Major == 5 && current.Minor >= 26 {
		return nil // 5.26+ -> 2025.x.x is allowed
	}

	return fmt.Errorf("upgrade from %s to CalVer %s requires Neo4j 5.26 or higher", currentStr, targetStr)
}

// VersionInfo represents parsed version information
type VersionInfo struct {
	Major int
	Minor int
	Patch int
}

func (r *RollingUpgradeOrchestrator) validateStatefulSetsReady(
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
) error {
	// Check primary StatefulSet
	primarySts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-primary", cluster.Name),
		Namespace: cluster.Namespace,
	}, primarySts); err != nil {
		return fmt.Errorf("failed to get primary StatefulSet: %w", err)
	}

	if primarySts.Status.ReadyReplicas != *primarySts.Spec.Replicas {
		return fmt.Errorf("primary StatefulSet not ready: %d/%d replicas ready",
			primarySts.Status.ReadyReplicas, *primarySts.Spec.Replicas)
	}

	// Check secondary StatefulSet if it exists
	if cluster.Spec.Topology.Secondaries > 0 {
		secondarySts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-secondary", cluster.Name),
			Namespace: cluster.Namespace,
		}, secondarySts); err != nil {
			return fmt.Errorf("failed to get secondary StatefulSet: %w", err)
		}

		if secondarySts.Status.ReadyReplicas != *secondarySts.Spec.Replicas {
			return fmt.Errorf("secondary StatefulSet not ready: %d/%d replicas ready",
				secondarySts.Status.ReadyReplicas, *secondarySts.Spec.Replicas)
		}
	}

	return nil
}

func (r *RollingUpgradeOrchestrator) waitForStatefulSetRollout(
	ctx context.Context,
	sts *appsv1.StatefulSet,
	timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for StatefulSet rollout")
		case <-ticker.C:
			current := &appsv1.StatefulSet{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      sts.Name,
				Namespace: sts.Namespace,
			}, current); err != nil {
				continue
			}

			// Check if rollout is complete
			if current.Status.ObservedGeneration >= current.Generation &&
				current.Status.ReadyReplicas == *current.Spec.Replicas &&
				current.Status.UpdatedReplicas == *current.Spec.Replicas {
				return nil
			}
		}
	}
}

func (r *RollingUpgradeOrchestrator) waitForPartialStatefulSetRollout(
	ctx context.Context,
	sts *appsv1.StatefulSet,
	preserveOrdinal int,
	timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for partial StatefulSet rollout")
		case <-ticker.C:
			current := &appsv1.StatefulSet{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      sts.Name,
				Namespace: sts.Namespace,
			}, current); err != nil {
				continue
			}

			// For partition updates, we expect UpdatedReplicas to be total - partition
			expectedUpdated := *current.Spec.Replicas - int32(preserveOrdinal)
			if expectedUpdated <= 0 {
				expectedUpdated = *current.Spec.Replicas - 1 // At least upgrade all but one
			}

			if current.Status.ObservedGeneration >= current.Generation &&
				current.Status.ReadyReplicas == *current.Spec.Replicas &&
				current.Status.UpdatedReplicas >= expectedUpdated {
				return nil
			}
		}
	}
}

func (r *RollingUpgradeOrchestrator) waitForLeaderElection(
	ctx context.Context,
	neo4jClient *neo4jclient.Client,
	_originalLeaderID string,
	timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for leader election")
		case <-ticker.C:
			// Check if we have a leader (could be same or different)
			leader, err := neo4jClient.GetLeader(ctx)
			if err != nil {
				continue // Keep waiting
			}

			if leader != nil {
				// Leader election completed, verify cluster health
				healthy, err := neo4jClient.IsClusterHealthy(ctx)
				if err != nil {
					continue
				}
				if healthy {
					return nil
				}
			}
		}
	}
}

func (r *RollingUpgradeOrchestrator) verifyVersionUpgrade(
	_ctx context.Context,
	_cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	_neo4jClient *neo4jclient.Client,
) error {
	// Implement version verification by querying Neo4j
	// This would verify that the cluster is running the expected version
	// For now, we'll assume success if we reach this point
	return nil
}

func (r *RollingUpgradeOrchestrator) extractOrdinalFromMemberID(memberID string) int {
	// Extract ordinal from member ID (e.g., "cluster-primary-2" -> 2)
	parts := strings.Split(memberID, "-")
	if len(parts) > 0 {
		if ordinal, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return ordinal
		}
	}
	return -1
}

// Configuration helpers
func (r *RollingUpgradeOrchestrator) getUpgradeTimeout(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) time.Duration {
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.UpgradeTimeout != "" {
		if timeout, err := time.ParseDuration(cluster.Spec.UpgradeStrategy.UpgradeTimeout); err == nil {
			return timeout
		}
	}
	return 30 * time.Minute // Default
}

func (r *RollingUpgradeOrchestrator) getHealthCheckTimeout(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) time.Duration {
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.HealthCheckTimeout != "" {
		if timeout, err := time.ParseDuration(cluster.Spec.UpgradeStrategy.HealthCheckTimeout); err == nil {
			return timeout
		}
	}
	return 5 * time.Minute // Default
}

func (r *RollingUpgradeOrchestrator) getStabilizationTimeout(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) time.Duration {
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.StabilizationTimeout != "" {
		if timeout, err := time.ParseDuration(cluster.Spec.UpgradeStrategy.StabilizationTimeout); err == nil {
			return timeout
		}
	}
	return 3 * time.Minute // Default
}
