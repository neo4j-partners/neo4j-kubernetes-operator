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
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/metrics"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
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

// The blocking upgrade loop (ExecuteRollingUpgrade / upgradeServers /
// updatePartitionAndWait) was replaced by the requeue-driven state machine in
// rolling_upgrade_statemachine.go (#174). This file keeps the shared
// orchestration helpers: status writes, version validation, StatefulSet
// access, version verification and timeout configuration.

func (r *RollingUpgradeOrchestrator) updateServerStatefulSet(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	mutate func(*appsv1.StatefulSet),
) (*appsv1.StatefulSet, error) {
	key := types.NamespacedName{
		Name:      fmt.Sprintf("%s-server", cluster.Name),
		Namespace: cluster.Namespace,
	}

	sts := &appsv1.StatefulSet{}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, key, sts); err != nil {
			return err
		}
		mutate(sts)
		return r.Update(ctx, sts)
	}); err != nil {
		return nil, err
	}

	if err := r.Get(ctx, key, sts); err != nil {
		return nil, err
	}

	return sts, nil
}

func (r *RollingUpgradeOrchestrator) getServerStatefulSet(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (*appsv1.StatefulSet, error) {
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-server", cluster.Name),
		Namespace: cluster.Namespace,
	}, sts); err != nil {
		return nil, err
	}

	return sts, nil
}

// Utility methods
func (r *RollingUpgradeOrchestrator) updateUpgradeStatus(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	phase, currentStep, lastError string,
) error {
	if cluster.Status.UpgradeStatus == nil {
		return nil
	}

	// Refetch + RetryOnConflict on every status write. The upgrade runs across
	// minutes, so the reconcile-start `cluster` object's ResourceVersion goes
	// stale; a bare Status().Update then conflicts and was silently swallowed —
	// which could leave even a SUCCESSFUL upgrade stuck in "InProgress" (its
	// final "Completed" write lost), permanently disabling further orchestrated
	// upgrades for the CR. Mirrors updateClusterStatus.
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if getErr := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); getErr != nil {
			return getErr
		}
		if latest.Status.UpgradeStatus == nil {
			return nil
		}
		latest.Status.UpgradeStatus.Phase = phase
		latest.Status.UpgradeStatus.CurrentStep = currentStep
		latest.Status.UpgradeStatus.Message = currentStep
		if lastError != "" {
			latest.Status.UpgradeStatus.LastError = lastError
		}
		if phase == "Completed" {
			now := metav1.Now()
			latest.Status.UpgradeStatus.CompletionTime = &now
			latest.Status.LastUpgradeTime = &now
			latest.Status.Version = cluster.Spec.Image.Tag
		}
		// Keep the in-memory object consistent for callers that read it after.
		cluster.Status.UpgradeStatus = latest.Status.UpgradeStatus
		return r.Status().Update(ctx, latest)
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update cluster status in updateUpgradeStatus")
	}
	return err
}

func (r *RollingUpgradeOrchestrator) validateVersionCompatibility(currentVersion, targetVersion string) error {
	// When Status.Version is empty (e.g. first upgrade after operator deployment or
	// status was cleared), we cannot check the upgrade path but we must not block
	// the upgrade entirely.  Skip compatibility checks and allow it to proceed.
	if currentVersion == "" {
		return nil
	}

	// Parse current and target versions
	current := r.parseVersion(currentVersion)
	target := r.parseVersion(targetVersion)

	if current == nil || target == nil {
		return fmt.Errorf("invalid version format (current=%q, target=%q)", currentVersion, targetVersion)
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

	// Only patch upgrades within 5.26.x are supported (last semver LTS; no 5.27+ exists)
	if current.Major == 5 && target.Major == 5 {
		if current.Minor == 26 && target.Minor == 26 {
			return nil // Allow patch upgrades within 5.26.x
		}
		return fmt.Errorf("only Neo4j 5.26.x (last semver LTS) or 2025.x.x (CalVer) versions are supported")
	}

	// Neo4j 4.x is no longer supported
	if current.Major == 4 || target.Major == 4 {
		return fmt.Errorf("Neo4j 4.x versions are not supported - only 5.26.x (last semver LTS) or 2025.x.x (CalVer) versions are supported")
	}

	return fmt.Errorf("unsupported SemVer upgrade path from %s to %s", currentStr, targetStr)
}

func (r *RollingUpgradeOrchestrator) validateSemVerToCalVerUpgrade(current, _ *VersionInfo, currentStr, targetStr string) error {
	// Only 5.26.x (last semver LTS) may upgrade to CalVer 2025.x.x
	if current.Major == 5 && current.Minor == 26 {
		return nil // 5.26.x -> 2025.x.x is the only supported semver-to-calver path
	}

	return fmt.Errorf("upgrade from %s to CalVer %s requires Neo4j 5.26.x (last semver LTS)", currentStr, targetStr)
}

// VersionInfo represents parsed version information
type VersionInfo struct {
	Major int
	Minor int
	Patch int
}

func (r *RollingUpgradeOrchestrator) validateStatefulSetsReady(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) error {
	serverSts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-server", cluster.Name),
		Namespace: cluster.Namespace,
	}, serverSts); err != nil {
		return fmt.Errorf("failed to get server StatefulSet: %w", err)
	}

	if serverSts.Spec.Replicas == nil {
		return fmt.Errorf("server StatefulSet has no replicas configured")
	}

	if serverSts.Status.ReadyReplicas != *serverSts.Spec.Replicas {
		return fmt.Errorf("server StatefulSet not ready: %d/%d replicas ready",
			serverSts.Status.ReadyReplicas, *serverSts.Spec.Replicas)
	}

	return nil
}

// verifyVersionUpgrade confirms every enabled server reports the target
// version. Source of truth is `SHOW SERVERS YIELD version` (via ListServers):
// it returns one row PER SERVER in a single query — unlike the previous
// dbms.components() approach, which went through the routing driver and
// actually sampled whichever server routing picked, once per member. SHOW
// SERVERS also reports the calendar version for ALL CalVer releases (2025.01
// included), whereas dbms.components() reports the pre-rebrand kernel
// "5.27.0" there; the versionsMatch kernel alias is kept as belt-and-braces.
func (r *RollingUpgradeOrchestrator) verifyVersionUpgrade(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	neo4jClient *neo4jclient.Client,
) error {
	logger := log.FromContext(ctx)

	// Get the target version from the cluster spec
	targetVersion := cluster.Spec.Image.Tag
	if targetVersion == "" {
		return fmt.Errorf("target version not specified in cluster spec")
	}

	logger.Info("Verifying cluster version after upgrade", "targetVersion", targetVersion)

	servers, err := neo4jClient.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers for version verification: %w", err)
	}

	mismatches, verified := r.versionMismatchesFromServers(servers, targetVersion)
	if verified == 0 {
		return fmt.Errorf("no enabled servers found during version verification")
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("version verification failed for %d servers: %s",
			len(mismatches), strings.Join(mismatches, "; "))
	}

	// NOTE: Status.Version is intentionally NOT written here. The Completed
	// transition persists it via refetch+RetryOnConflict (#207) — a bare
	// Status().Update on the reconcile-start object would silently 409.

	logger.Info("Version verification completed successfully",
		"targetVersion", targetVersion, "verifiedServers", verified)
	return nil
}

// versionMismatchesFromServers compares each ENABLED server's self-reported
// SHOW SERVERS version against the target tag. Non-enabled servers (e.g. a
// leftover Cordoned entry from an aborted drain, or a Free server) are not
// the upgrade's concern — the Rolling phase already gated every cluster
// member on Enabled+Available — and must not wedge verification. Returns the
// mismatch descriptions and the number of enabled servers checked.
func (r *RollingUpgradeOrchestrator) versionMismatchesFromServers(
	servers []neo4jclient.ServerInfo,
	targetVersion string,
) (mismatches []string, verified int) {
	for _, s := range servers {
		if s.State != "Enabled" {
			continue
		}
		verified++
		label := s.Name
		if label == "" {
			label = s.Address
		}
		switch {
		case s.Version == "":
			mismatches = append(mismatches, fmt.Sprintf("%s: no version reported by SHOW SERVERS", label))
		case !r.versionsMatch(s.Version, targetVersion):
			mismatches = append(mismatches,
				fmt.Sprintf("%s: running %s, expected %s", label, s.Version, targetVersion))
		}
	}
	return mismatches, verified
}

// normalizeKernelAlias translates the one CalVer release that self-reports a
// SemVer kernel: Neo4j 2025.01.x (the CalVer rebrand release) identifies as
// kernel 5.27.x in dbms.components(). Later CalVers report the calendar
// version directly. Without this alias, post-upgrade version verification for
// a 2025.01.0-enterprise target can never match the servers' reported 5.27.0
// and a SUCCESSFUL upgrade is declared Failed (found live on Kind, #174).
func normalizeKernelAlias(v *VersionInfo) *VersionInfo {
	if v != nil && v.Major == 2025 && v.Minor == 1 {
		return &VersionInfo{Major: 5, Minor: 27, Patch: v.Patch}
	}
	return v
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

	// Kernel-alias comparison: parseVersion strips the "-enterprise" suffix,
	// normalizeKernelAlias maps 2025.01.x <-> 5.27.x (see above).
	if a, e := r.parseVersion(actual), r.parseVersion(expected); a != nil && e != nil {
		na, ne := normalizeKernelAlias(a), normalizeKernelAlias(e)
		if na.Major == ne.Major && na.Minor == ne.Minor && na.Patch == ne.Patch {
			return true
		}
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

// Configuration helpers
func (r *RollingUpgradeOrchestrator) getUpgradeTimeout(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) time.Duration {
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.UpgradeTimeout != "" {
		if timeout, err := time.ParseDuration(cluster.Spec.UpgradeStrategy.UpgradeTimeout); err == nil {
			return timeout
		}
	}
	return 30 * time.Minute // Default
}

func (r *RollingUpgradeOrchestrator) getHealthCheckTimeout(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) time.Duration {
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.HealthCheckTimeout != "" {
		if timeout, err := time.ParseDuration(cluster.Spec.UpgradeStrategy.HealthCheckTimeout); err == nil {
			return timeout
		}
	}
	return 5 * time.Minute // Default
}

func (r *RollingUpgradeOrchestrator) getStabilizationTimeout(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) time.Duration {
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.StabilizationTimeout != "" {
		if timeout, err := time.ParseDuration(cluster.Spec.UpgradeStrategy.StabilizationTimeout); err == nil {
			return timeout
		}
	}
	return 3 * time.Minute // Default
}
