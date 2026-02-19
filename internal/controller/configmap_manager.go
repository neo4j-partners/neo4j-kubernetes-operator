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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// ConfigMapManager handles ConfigMap updates and pod restarts
type ConfigMapManager struct {
	client.Client
	lastUpdateTime map[string]time.Time
	mu             sync.RWMutex
}

// NewConfigMapManager creates a new ConfigMap manager
func NewConfigMapManager(client client.Client) *ConfigMapManager {
	return &ConfigMapManager{
		Client:         client,
		lastUpdateTime: make(map[string]time.Time),
	}
}

// ReconcileConfigMap handles immediate ConfigMap updates and pod restarts
func (cm *ConfigMapManager) ReconcileConfigMap(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// Generate desired ConfigMap
	desiredConfigMap := resources.BuildConfigMapForEnterprise(cluster)

	// Get existing ConfigMap
	existingConfigMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{
		Name:      fmt.Sprintf("%s-config", cluster.Name),
		Namespace: cluster.Namespace,
	}

	err := cm.Get(ctx, configMapKey, existingConfigMap)
	configMapExists := err == nil

	var configChanged bool
	var oldConfigHash, newConfigHash string

	if configMapExists {
		// Calculate hash of existing config
		oldConfigHash = cm.calculateConfigMapHash(existingConfigMap)
		newConfigHash = cm.calculateConfigMapHash(desiredConfigMap)
		configChanged = oldConfigHash != newConfigHash

		if configChanged {
			// Perform detailed analysis to understand what changed
			changes := cm.analyzeConfigChanges(existingConfigMap, desiredConfigMap)
			needsRestart := cm.requiresRestart(changes)

			logger.Info("ConfigMap configuration changed, updating immediately",
				"cluster", cluster.Name,
				"oldHash", oldConfigHash,
				"newHash", newConfigHash,
				"changes", changes,
				"requiresRestart", needsRestart)

			// Add debug logging for the raw content to understand what's different
			logger.V(1).Info("ConfigMap content comparison",
				"cluster", cluster.Name,
				"existingKeys", getMapKeys(existingConfigMap.Data),
				"desiredKeys", getMapKeys(desiredConfigMap.Data))
		} else {
			logger.V(1).Info("ConfigMap hash unchanged, skipping update",
				"cluster", cluster.Name,
				"hash", newConfigHash)
		}
	} else {
		// ConfigMap doesn't exist, calculate hash for new one
		newConfigHash = cm.calculateConfigMapHash(desiredConfigMap)
		configChanged = true
		logger.Info("Creating new ConfigMap", "cluster", cluster.Name, "hash", newConfigHash)
	}

	// Update ConfigMap immediately if changed and debounce period has passed
	if configChanged {
		clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)

		// Check debounce period to prevent rapid successive updates
		cm.mu.RLock()
		lastUpdate, exists := cm.lastUpdateTime[clusterKey]
		cm.mu.RUnlock()

		minInterval := 1 * time.Second // Disable debounce for testing
		if exists && time.Since(lastUpdate) < minInterval {
			logger.Info("Skipping ConfigMap update due to debounce period",
				"cluster", cluster.Name,
				"timeSinceLastUpdate", time.Since(lastUpdate),
				"minInterval", minInterval)
			return nil
		}

		if err := cm.updateConfigMapImmediate(ctx, cluster, desiredConfigMap); err != nil {
			return fmt.Errorf("failed to update ConfigMap: %w", err)
		}

		// Update last update time
		cm.mu.Lock()
		cm.lastUpdateTime[clusterKey] = time.Now()
		cm.mu.Unlock()

		// Only trigger rolling restart if changes actually require it
		if configMapExists {
			changes := cm.analyzeConfigChanges(existingConfigMap, desiredConfigMap)
			needsRestart := cm.requiresRestart(changes)

			if needsRestart {
				logger.Info("Configuration changes require pod restart, triggering rolling restart",
					"cluster", cluster.Name,
					"changes", changes)
				if err := cm.triggerRollingRestartForConfigChange(ctx, cluster, newConfigHash); err != nil {
					logger.Error(err, "Failed to trigger rolling restart for config change")
					// Don't fail the reconciliation, just log the error
				}
			} else {
				logger.Info("Configuration changes do not require pod restart, skipping rolling restart",
					"cluster", cluster.Name,
					"changes", changes)
			}
		} else {
			// New ConfigMap always requires restart for initial deployment
			logger.Info("New ConfigMap created, triggering rolling restart for initial deployment",
				"cluster", cluster.Name)
			if err := cm.triggerRollingRestartForConfigChange(ctx, cluster, newConfigHash); err != nil {
				logger.Error(err, "Failed to trigger rolling restart for new ConfigMap")
				// Don't fail the reconciliation, just log the error
			}
		}
	}

	return nil
}

// updateConfigMapImmediate immediately updates the ConfigMap
func (cm *ConfigMapManager) updateConfigMapImmediate(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, configMap *corev1.ConfigMap) error {
	logger := log.FromContext(ctx)

	// Set owner reference
	if err := setOwnerReference(cluster, configMap); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Try to get existing ConfigMap
	existingConfigMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{
		Name:      configMap.Name,
		Namespace: configMap.Namespace,
	}

	err := cm.Get(ctx, configMapKey, existingConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			if err := cm.Create(ctx, configMap); err != nil {
				return fmt.Errorf("failed to create ConfigMap: %w", err)
			}
			logger.Info("ConfigMap created successfully", "name", configMap.Name)
		} else {
			return fmt.Errorf("failed to get ConfigMap: %w", err)
		}
	} else {
		// Update existing ConfigMap
		existingConfigMap.Data = configMap.Data
		existingConfigMap.Labels = configMap.Labels
		existingConfigMap.Annotations = configMap.Annotations

		if err := cm.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("failed to update ConfigMap: %w", err)
		}
		logger.Info("ConfigMap updated successfully", "name", configMap.Name)
	}

	return nil
}

// triggerRollingRestartForConfigChange triggers a rolling restart when configuration changes.
// It stamps a config-hash annotation on the pod template of each server StatefulSet, which
// causes Kubernetes to perform a rolling restart without any image change.
func (cm *ConfigMapManager) triggerRollingRestartForConfigChange(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, configHash string) error {
	logger := log.FromContext(ctx)

	// The current architecture uses a single {cluster}-server StatefulSet.
	// Update it so Kubernetes rolls the pods to pick up the new neo4j.conf.
	serverSts := &appsv1.StatefulSet{}
	serverKey := types.NamespacedName{
		Name:      fmt.Sprintf("%s-server", cluster.Name),
		Namespace: cluster.Namespace,
	}

	if err := cm.Get(ctx, serverKey, serverSts); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Server StatefulSet not found, skipping restart", "name", serverKey.Name)
			return nil
		}
		return fmt.Errorf("failed to get server StatefulSet: %w", err)
	}

	if err := cm.updateStatefulSetWithConfigHash(ctx, serverSts, configHash); err != nil {
		return fmt.Errorf("failed to update server StatefulSet: %w", err)
	}

	logger.Info("Rolling restart triggered for configuration change",
		"cluster", cluster.Name,
		"configHash", configHash)

	return nil
}

// updateStatefulSetWithConfigHash updates a StatefulSet with config hash annotation to trigger rolling restart
func (cm *ConfigMapManager) updateStatefulSetWithConfigHash(ctx context.Context, sts *appsv1.StatefulSet, configHash string) error {
	// Initialize annotations if nil
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = make(map[string]string)
	}

	// Set config hash annotation to trigger rolling restart
	sts.Spec.Template.Annotations["neo4j.neo4j.com/config-hash"] = configHash
	sts.Spec.Template.Annotations["neo4j.neo4j.com/config-restart"] = time.Now().Format(time.RFC3339)

	return cm.Update(ctx, sts)
}

// calculateConfigMapHash calculates a hash of the ConfigMap data for change detection
// Excludes runtime-only values and normalizes content to prevent false positives
func (cm *ConfigMapManager) calculateConfigMapHash(configMap *corev1.ConfigMap) string {
	hasher := sha256.New()

	// Process each key in deterministic order
	keys := []string{"neo4j.conf", "startup.sh", "health.sh"}
	for _, key := range keys {
		value, exists := configMap.Data[key]
		if !exists {
			continue
		}

		// Normalize the content based on the key
		normalizedValue := cm.normalizeConfigContent(key, value)

		hasher.Write([]byte(key))
		hasher.Write([]byte(normalizedValue))
	}

	return hex.EncodeToString(hasher.Sum(nil))[:16] // Use first 16 chars for brevity
}

// normalizeConfigContent normalizes configuration content to exclude runtime-only values
func (cm *ConfigMapManager) normalizeConfigContent(key, value string) string {
	switch key {
	case "neo4j.conf":
		return cm.normalizeNeo4jConf(value)
	case "startup.sh":
		return cm.normalizeStartupScript(value)
	case "health.sh":
		// Health script is static, no normalization needed
		return value
	default:
		return value
	}
}

// normalizeNeo4jConf normalizes neo4j.conf content to remove duplicates and runtime values
func (cm *ConfigMapManager) normalizeNeo4jConf(content string) string {
	lines := strings.Split(content, "\n")
	seen := make(map[string]bool)
	var normalized []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			normalized = append(normalized, line)
			continue
		}

		// Extract key from property line
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])

				// Skip duplicate keys (keep the first occurrence)
				if seen[key] {
					continue
				}
				seen[key] = true
			}
		}

		normalized = append(normalized, line)
	}

	return strings.Join(normalized, "\n")
}

// normalizeStartupScript normalizes startup script content to exclude variable runtime values
func (cm *ConfigMapManager) normalizeStartupScript(content string) string {
	lines := strings.Split(content, "\n")
	var normalized []string

	for _, line := range lines {
		// Exclude lines that contain runtime environment variables or timestamps
		if strings.Contains(line, "POD_ORDINAL") ||
			strings.Contains(line, "HOSTNAME") ||
			strings.Contains(line, "$(date") ||
			strings.Contains(line, "timestamp") {
			// Replace with placeholder to maintain script structure
			normalized = append(normalized, "# Runtime variable excluded from hash")
			continue
		}

		normalized = append(normalized, line)
	}

	return strings.Join(normalized, "\n")
}

// analyzeConfigChanges provides detailed analysis of what changed between ConfigMaps
func (cm *ConfigMapManager) analyzeConfigChanges(oldConfigMap, newConfigMap *corev1.ConfigMap) []string {
	var changes []string

	// Check each key for changes
	keys := []string{"neo4j.conf", "startup.sh", "health.sh"}
	for _, key := range keys {
		oldValue, oldExists := oldConfigMap.Data[key]
		newValue, newExists := newConfigMap.Data[key]

		if !oldExists && newExists {
			changes = append(changes, fmt.Sprintf("added %s", key))
		} else if oldExists && !newExists {
			changes = append(changes, fmt.Sprintf("removed %s", key))
		} else if oldExists && newExists {
			oldNormalized := cm.normalizeConfigContent(key, oldValue)
			newNormalized := cm.normalizeConfigContent(key, newValue)

			if oldNormalized != newNormalized {
				changes = append(changes, fmt.Sprintf("modified %s", key))

				// For neo4j.conf, identify specific property changes
				if key == "neo4j.conf" {
					propertyChanges := cm.analyzeNeo4jConfChanges(oldNormalized, newNormalized)
					changes = append(changes, propertyChanges...)
				}
			}
		}
	}

	if len(changes) == 0 {
		changes = append(changes, "hash changed but no semantic differences detected")
	}

	return changes
}

// analyzeNeo4jConfChanges identifies specific property changes in neo4j.conf
func (cm *ConfigMapManager) analyzeNeo4jConfChanges(oldConf, newConf string) []string {
	oldProps := cm.parseNeo4jProperties(oldConf)
	newProps := cm.parseNeo4jProperties(newConf)

	var changes []string

	// Check for added properties
	for key, value := range newProps {
		if oldValue, exists := oldProps[key]; !exists {
			changes = append(changes, fmt.Sprintf("added property %s=%s", key, value))
		} else if oldValue != value {
			changes = append(changes, fmt.Sprintf("changed property %s: %s -> %s", key, oldValue, value))
		}
	}

	// Check for removed properties
	for key := range oldProps {
		if _, exists := newProps[key]; !exists {
			changes = append(changes, fmt.Sprintf("removed property %s", key))
		}
	}

	return changes
}

// parseNeo4jProperties parses neo4j.conf content into a map of properties
func (cm *ConfigMapManager) parseNeo4jProperties(content string) map[string]string {
	props := make(map[string]string)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse property line
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				props[key] = value
			}
		}
	}

	return props
}

// hasMemoryConfigChanged checks if memory-related configuration has changed
func (cm *ConfigMapManager) HasMemoryConfigChanged(oldCluster, newCluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	if oldCluster == nil || newCluster == nil {
		return true
	}

	// Check resource limits/requests changes
	if !reflect.DeepEqual(oldCluster.Spec.Resources, newCluster.Spec.Resources) {
		return true
	}

	// Check Neo4j memory config changes
	oldHeap := ""
	newHeap := ""
	oldPageCache := ""
	newPageCache := ""

	if oldCluster.Spec.Config != nil {
		oldHeap = oldCluster.Spec.Config["server.memory.heap.max_size"]
		oldPageCache = oldCluster.Spec.Config["server.memory.pagecache.size"]
	}

	if newCluster.Spec.Config != nil {
		newHeap = newCluster.Spec.Config["server.memory.heap.max_size"]
		newPageCache = newCluster.Spec.Config["server.memory.pagecache.size"]
	}

	return oldHeap != newHeap || oldPageCache != newPageCache
}

// hasTopologyChanged checks if cluster topology has changed
func (cm *ConfigMapManager) hasTopologyChanged(oldCluster, newCluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	if oldCluster == nil || newCluster == nil {
		return true
	}

	return oldCluster.Spec.Topology.Servers != newCluster.Spec.Topology.Servers
}

// setOwnerReference sets the owner reference for the ConfigMap
func setOwnerReference(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, configMap *corev1.ConfigMap) error {
	// Set owner reference to ensure garbage collection
	configMap.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         cluster.APIVersion,
			Kind:               cluster.Kind,
			Name:               cluster.Name,
			UID:                cluster.UID,
			Controller:         boolPtr(true),
			BlockOwnerDeletion: boolPtr(true),
		},
	}
	return nil
}

// boolPtr returns a pointer to a boolean value
func boolPtr(b bool) *bool {
	return &b
}

// getMapKeys returns the keys of a map as a slice
func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// requiresRestart determines if the changes require a pod restart
func (cm *ConfigMapManager) requiresRestart(changes []string) bool {
	// Changes that don't require restart
	nonRestartPatterns := []string{
		"hash changed but no semantic differences detected",
		"Runtime variable excluded from hash",
	}

	// If all changes are non-restart changes, don't restart
	if len(changes) == 0 {
		return false
	}

	for _, change := range changes {
		isNonRestart := false
		for _, pattern := range nonRestartPatterns {
			if strings.Contains(change, pattern) {
				isNonRestart = true
				break
			}
		}
		// If we find any change that requires restart, return true
		if !isNonRestart {
			return true
		}
	}

	return false
}
