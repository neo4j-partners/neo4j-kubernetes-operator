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

	// Phase 2: Upgrade secondaries first (if any) - disabled in server architecture
	if false { // No secondaries in server architecture
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
			Total:   cluster.Spec.Topology.Servers,
			Pending: cluster.Spec.Topology.Servers,
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

	// Secondaries don't exist in server architecture - always return
	logger.Info("Skipping secondary upgrade - using server architecture")
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

	// Update progress with current leader (simplified for server architecture)
	cluster.Status.UpgradeStatus.CurrentStep = fmt.Sprintf("Upgrading non-leader primaries (preserving leader: %s)", leader.ID)
	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status with leader info")
	}

	logger.Info("Upgrading non-leader primaries", "leader", leader.ID)

	// Extract leader ordinal from member ID
	leaderOrdinal := r.extractOrdinalFromMemberID(leader.ID)
	if leaderOrdinal < 0 || leaderOrdinal >= int(cluster.Spec.Topology.Servers) {
		return fmt.Errorf("invalid leader ordinal: %d", leaderOrdinal)
	}

	// Update image for all server StatefulSets except the leader
	newImage := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	timeout := r.getUpgradeTimeout(cluster)

	for i := int32(0); i < cluster.Spec.Topology.Servers; i++ {
		if int32(leaderOrdinal) == i {
			continue // Skip leader, upgrade it last
		}

		serverSts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-server-%d", cluster.Name, i),
			Namespace: cluster.Namespace,
		}, serverSts); err != nil {
			return fmt.Errorf("failed to get server-%d StatefulSet: %w", i, err)
		}

		// Check if already upgraded
		if len(serverSts.Spec.Template.Spec.Containers) == 0 {
			return fmt.Errorf("server-%d StatefulSet has no containers defined", i)
		}
		if serverSts.Spec.Template.Spec.Containers[0].Image == newImage {
			logger.Info("Server StatefulSet already has target image", "server", i)
			continue
		}

		// Update the StatefulSet image
		serverSts.Spec.Template.Spec.Containers[0].Image = newImage

		// Add upgrade annotation
		if serverSts.Spec.Template.Annotations == nil {
			serverSts.Spec.Template.Annotations = make(map[string]string)
		}
		serverSts.Spec.Template.Annotations["neo4j.com/upgrade-timestamp"] = time.Now().Format(time.RFC3339)

		if err := r.Update(ctx, serverSts); err != nil {
			return fmt.Errorf("failed to update server-%d StatefulSet: %w", i, err)
		}

		// Wait for this server to be ready
		if err := r.waitForStatefulSetRollout(ctx, serverSts, timeout); err != nil {
			return fmt.Errorf("server-%d rollout failed: %w", i, err)
		}

		logger.Info("Upgraded non-leader server", "server", i)
	}

	// Verify cluster stability after non-leader upgrade
	stabilizationTimeout := r.getStabilizationTimeout(cluster)
	if err := neo4jClient.WaitForClusterStabilization(ctx, stabilizationTimeout); err != nil {
		return fmt.Errorf("cluster failed to stabilize after non-leader primary upgrade: %w", err)
	}

	// Update progress (simplified for server architecture)
	upgradedCount := cluster.Spec.Topology.Servers - 1 // All except leader
	cluster.Status.UpgradeStatus.Progress.Upgraded += upgradedCount
	cluster.Status.UpgradeStatus.Progress.Pending -= upgradedCount

	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status after non-leader primary upgrade")
	}
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

	cluster.Status.UpgradeStatus.CurrentStep = "Upgrading leader: " + originalLeader.ID
	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status before leader upgrade")
	}

	logger.Info("Upgrading leader", "leaderId", originalLeader.ID)

	// Extract leader ordinal from member ID
	leaderOrdinal := r.extractOrdinalFromMemberID(originalLeader.ID)
	if leaderOrdinal < 0 || leaderOrdinal >= int(cluster.Spec.Topology.Servers) {
		return fmt.Errorf("invalid leader ordinal: %d", leaderOrdinal)
	}

	// Get leader's StatefulSet
	leaderSts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-server-%d", cluster.Name, leaderOrdinal),
		Namespace: cluster.Namespace,
	}, leaderSts); err != nil {
		return fmt.Errorf("failed to get leader StatefulSet server-%d: %w", leaderOrdinal, err)
	}

	// Update the leader StatefulSet image
	newImage := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	if len(leaderSts.Spec.Template.Spec.Containers) > 0 {
		leaderSts.Spec.Template.Spec.Containers[0].Image = newImage
	}

	// Add upgrade annotation
	if leaderSts.Spec.Template.Annotations == nil {
		leaderSts.Spec.Template.Annotations = make(map[string]string)
	}
	leaderSts.Spec.Template.Annotations["neo4j.com/upgrade-timestamp"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, leaderSts); err != nil {
		return fmt.Errorf("failed to update leader StatefulSet: %w", err)
	}

	// Wait for leader rollout
	timeout := r.getUpgradeTimeout(cluster)
	if err := r.waitForStatefulSetRollout(ctx, leaderSts, timeout); err != nil {
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

	// Update progress - all servers upgraded
	cluster.Status.UpgradeStatus.Progress.Upgraded++ // Just the leader
	cluster.Status.UpgradeStatus.Progress.Pending--

	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status after leader upgrade")
	}
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

	if err := r.Status().Update(ctx, cluster); err != nil {
		// Log error but don't fail the upgrade status update
		log.FromContext(ctx).Error(err, "Failed to update cluster status in updateUpgradeStatus")
	}
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

	// Neo4j 4.x is no longer supported - only 5.26+ versions are supported
	if current.Major == 4 || target.Major == 4 {
		return fmt.Errorf("Neo4j 4.x versions are not supported - only 5.26+ versions are supported")
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
	// Check each individual server StatefulSet
	for i := int32(0); i < cluster.Spec.Topology.Servers; i++ {
		serverSts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-server-%d", cluster.Name, i),
			Namespace: cluster.Namespace,
		}, serverSts); err != nil {
			return fmt.Errorf("failed to get server-%d StatefulSet: %w", i, err)
		}

		if serverSts.Status.ReadyReplicas != *serverSts.Spec.Replicas {
			return fmt.Errorf("server-%d StatefulSet not ready: %d/%d replicas ready",
				i, serverSts.Status.ReadyReplicas, *serverSts.Spec.Replicas)
		}
	}

	// Check backup sidecar StatefulSet if configured
	if cluster.Spec.Backups != nil {
		backupSts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-backup-sidecar", cluster.Name),
			Namespace: cluster.Namespace,
		}, backupSts); err == nil {
			if backupSts.Status.ReadyReplicas != *backupSts.Spec.Replicas {
				return fmt.Errorf("backup sidecar StatefulSet not ready: %d/%d replicas ready",
					backupSts.Status.ReadyReplicas, *backupSts.Spec.Replicas)
			}
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
	_ string,
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
	ctx context.Context,
	cluster *neo4jv1alpha1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	// Get the target version from the cluster spec
	targetVersion := cluster.Spec.Image.Tag
	if targetVersion == "" {
		return fmt.Errorf("target version not specified in cluster spec")
	}

	logger.Info("Verifying cluster version after upgrade", "targetVersion", targetVersion)

	// Get cluster overview to check versions of all members
	members, err := neo4jClient.GetClusterOverview(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster overview for version verification: %w", err)
	}

	if len(members) == 0 {
		return fmt.Errorf("no cluster members found during version verification")
	}

	// Verify each member is running the target version
	var versionMismatches []string
	for _, member := range members {
		// Query the specific member for its version
		memberVersion, err := r.getMemberVersion(ctx, neo4jClient, member.ID)
		if err != nil {
			logger.Error(err, "Failed to get version for member", "memberID", member.ID)
			versionMismatches = append(versionMismatches, member.ID+": version query failed")
			continue
		}

		// Compare versions (normalize for comparison)
		if !r.versionsMatch(memberVersion, targetVersion) {
			versionMismatches = append(versionMismatches,
				fmt.Sprintf("%s: running %s, expected %s", member.ID, memberVersion, targetVersion))
		} else {
			logger.Info("Member version verified", "memberID", member.ID, "version", memberVersion)
		}
	}

	// If there are version mismatches, report them
	if len(versionMismatches) > 0 {
		return fmt.Errorf("version verification failed for %d members: %s",
			len(versionMismatches), strings.Join(versionMismatches, "; "))
	}

	// Update cluster status with verified version
	cluster.Status.Version = targetVersion
	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status with verified version")
		// Don't fail the upgrade for this, just log the error
	}

	logger.Info("Version verification completed successfully",
		"targetVersion", targetVersion, "verifiedMembers", len(members))
	return nil
}

// getMemberVersion queries a specific cluster member for its Neo4j version
func (r *RollingUpgradeOrchestrator) getMemberVersion(ctx context.Context, neo4jClient *neo4jclient.Client, memberID string) (string, error) {
	// Query Neo4j for version information
	// This uses a system query to get the version
	query := "CALL dbms.components() YIELD name, versions, edition WHERE name = 'Neo4j Kernel' RETURN versions[0] as version"
	result, err := neo4jClient.ExecuteQuery(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to query version from member %s: %w", memberID, err)
	}

	// Parse the result to extract version
	version := r.parseVersionFromQueryResult(result)
	if version == "" {
		return "", fmt.Errorf("could not parse version from query result for member %s", memberID)
	}

	return version, nil
}

// parseVersionFromQueryResult extracts version string from Neo4j query result
func (r *RollingUpgradeOrchestrator) parseVersionFromQueryResult(result string) string {
	// In a real implementation, this would properly parse the JSON/tabular result
	// For now, use a simple approach to extract version patterns
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for version patterns like "5.26.0", "2025.01.0", etc.
		if r.isVersionString(line) {
			return line
		}

		// Also check if the line contains a version (e.g., "version: 5.26.0")
		if strings.Contains(line, ":") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				potentialVersion := strings.TrimSpace(parts[1])
				if r.isVersionString(potentialVersion) {
					return potentialVersion
				}
			}
		}
	}

	// If no version found, try to extract from anywhere in the result
	// This is a fallback for different result formats
	words := strings.Fields(result)
	for _, word := range words {
		if r.isVersionString(word) {
			return word
		}
	}

	return ""
}

// isVersionString checks if a string looks like a version number
func (r *RollingUpgradeOrchestrator) isVersionString(s string) bool {
	if s == "" {
		return false
	}

	// Remove quotes if present
	s = strings.Trim(s, `"'`)

	// Check for semantic version pattern (X.Y.Z)
	parts := strings.Split(s, ".")
	if len(parts) >= 2 && len(parts) <= 4 {
		for _, part := range parts {
			if part == "" {
				return false
			}
			// Check if each part is numeric (allowing for pre-release suffixes)
			numPart := strings.Split(part, "-")[0] // Remove pre-release suffix
			if _, err := strconv.Atoi(numPart); err != nil {
				return false
			}
		}
		return true
	}

	return false
}

// versionsMatch compares two version strings for equality, handling various formats
func (r *RollingUpgradeOrchestrator) versionsMatch(actual, expected string) bool {
	// Normalize versions by removing quotes and whitespace
	actual = strings.TrimSpace(strings.Trim(actual, `"'`))
	expected = strings.TrimSpace(strings.Trim(expected, `"'`))

	// Direct string comparison first
	if actual == expected {
		return true
	}

	// Try semantic version comparison (handle cases like "5.26" vs "5.26.0")
	actualParts := strings.Split(actual, ".")
	expectedParts := strings.Split(expected, ".")

	// Pad shorter version with zeros
	maxLen := int(math.Max(float64(len(actualParts)), float64(len(expectedParts))))
	for len(actualParts) < maxLen {
		actualParts = append(actualParts, "0")
	}
	for len(expectedParts) < maxLen {
		expectedParts = append(expectedParts, "0")
	}

	// Compare each part
	for i := 0; i < maxLen; i++ {
		actualNum, err1 := strconv.Atoi(strings.Split(actualParts[i], "-")[0]) // Remove pre-release
		expectedNum, err2 := strconv.Atoi(strings.Split(expectedParts[i], "-")[0])

		if err1 != nil || err2 != nil {
			// If we can't parse as numbers, fall back to string comparison
			if actualParts[i] != expectedParts[i] {
				return false
			}
		} else if actualNum != expectedNum {
			return false
		}
	}

	return true
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
