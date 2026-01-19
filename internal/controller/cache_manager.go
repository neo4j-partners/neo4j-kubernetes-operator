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
	goruntime "runtime"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	// DefaultMemoryThresholdMB is the default memory threshold in bytes (200 MiB)
	DefaultMemoryThresholdMB = 200 * 1024 * 1024 // 200 MiB in bytes
	// WarningMemoryThresholdMB is the warning memory threshold in bytes (150 MiB)
	WarningMemoryThresholdMB = 150 * 1024 * 1024 // 150 MiB in bytes

	// MaxWatchedNamespaces is the maximum number of namespaces that can be watched simultaneously
	MaxWatchedNamespaces = 500
	// CacheCleanupInterval is the interval between cache cleanup operations
	CacheCleanupInterval = 5 * time.Minute
	// MemoryCheckInterval is the interval between memory usage checks
	MemoryCheckInterval = 30 * time.Second
)

// CacheManager manages dynamic cache filtering and memory monitoring
type CacheManager struct {
	scheme           *runtime.Scheme
	client           client.Client
	watchModePrefix  string
	watchAllModes    bool
	memoryThreshold  int64
	warningThreshold int64

	// Namespace tracking
	mutex             sync.RWMutex
	watchedNamespaces map[string]time.Time
	filteredResources map[string]bool

	// Memory monitoring
	memoryStats   chan MemoryStats
	alertCallback func(MemoryAlert)

	// Cache options
	cacheOptions cache.Options
	setupDone    bool
}

// MemoryStats represents current memory usage
type MemoryStats struct {
	AllocMB      float64
	TotalAllocMB float64
	SysMB        float64
	NumGC        uint32
	Timestamp    time.Time
}

// MemoryAlert represents a memory usage alert
type MemoryAlert struct {
	Level     AlertLevel
	Message   string
	Stats     MemoryStats
	Timestamp time.Time
}

// AlertLevel defines the severity of memory alerts
type AlertLevel int

const (
	// AlertLevelInfo indicates informational alerts
	AlertLevelInfo AlertLevel = iota
	// AlertLevelWarning indicates warning-level alerts
	AlertLevelWarning
	// AlertLevelCritical indicates critical alerts
	AlertLevelCritical
)

// NewCacheManager creates a new cache manager
func NewCacheManager(scheme *runtime.Scheme, watchModePrefix string, watchAllModes bool) *CacheManager {
	cm := &CacheManager{
		scheme:            scheme,
		watchModePrefix:   watchModePrefix,
		watchAllModes:     watchAllModes,
		memoryThreshold:   DefaultMemoryThresholdMB,
		warningThreshold:  WarningMemoryThresholdMB,
		watchedNamespaces: make(map[string]time.Time),
		filteredResources: make(map[string]bool),
		memoryStats:       make(chan MemoryStats, 100),
	}

	cm.setupResourceFilters()
	return cm
}

// SetClient configures the client used for resource checks during cleanup.
func (cm *CacheManager) SetClient(c client.Client) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.client = c
}

// SetMemoryThresholds configures memory monitoring thresholds
func (cm *CacheManager) SetMemoryThresholds(warningMB, criticalMB int64) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.warningThreshold = warningMB * 1024 * 1024
	cm.memoryThreshold = criticalMB * 1024 * 1024
}

// SetAlertCallback sets the callback for memory alerts
func (cm *CacheManager) SetAlertCallback(callback func(MemoryAlert)) {
	cm.alertCallback = callback
}

// GetCacheOptions returns cache options with resource filtering
func (cm *CacheManager) GetCacheOptions() cache.Options {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.setupDone {
		return cm.cacheOptions
	}

	// Configure cache with selective resource watching
	cm.cacheOptions = cache.Options{
		Scheme: cm.scheme,
		ByObject: map[client.Object]cache.ByObject{
			// Neo4j CRDs - always watch these
			&neo4jv1alpha1.Neo4jEnterpriseCluster{}:    {},
			&neo4jv1alpha1.Neo4jEnterpriseStandalone{}: {},
			&neo4jv1alpha1.Neo4jDatabase{}:             {},
			&neo4jv1alpha1.Neo4jBackup{}:               {},
			&neo4jv1alpha1.Neo4jRestore{}:              {},
			&neo4jv1alpha1.Neo4jPlugin{}:               {},
			&neo4jv1alpha1.Neo4jShardedDatabase{}:      {},

			// Core Kubernetes resources - filtered by labels
			&corev1.Secret{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},
			&corev1.Service{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},
			&corev1.ConfigMap{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},
			&corev1.PersistentVolumeClaim{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},

			// Workload resources - filtered by labels
			&appsv1.StatefulSet{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},
			&batchv1.Job{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},
			&batchv1.CronJob{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},

			// Cert-manager resources - filtered by labels
			&certmanagerv1.Certificate{}: {
				Label: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
				}),
			},
		},
	}

	// Configure namespace filtering if not watching all
	if !cm.watchAllModes {
		cm.cacheOptions.DefaultNamespaces = cm.getWatchedNamespaces()
	}

	cm.setupDone = true
	return cm.cacheOptions
}

// AddNamespace adds a namespace to be watched dynamically
func (cm *CacheManager) AddNamespace(namespace string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Check if we're at the limit
	if len(cm.watchedNamespaces) >= MaxWatchedNamespaces {
		return fmt.Errorf("maximum watched namespaces limit reached (%d)", MaxWatchedNamespaces)
	}

	// Check memory usage before adding
	if cm.isMemoryAboveThreshold() {
		return fmt.Errorf("memory usage above threshold, cannot add namespace")
	}

	cm.watchedNamespaces[namespace] = time.Now()
	log.Log.Info("Added namespace to watch list", "namespace", namespace, "total", len(cm.watchedNamespaces))

	return nil
}

// RemoveNamespace removes a namespace from being watched
func (cm *CacheManager) RemoveNamespace(namespace string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	delete(cm.watchedNamespaces, namespace)
	log.Log.Info("Removed namespace from watch list", "namespace", namespace, "total", len(cm.watchedNamespaces))
}

// IsNamespaceWatched checks if a namespace is being watched
func (cm *CacheManager) IsNamespaceWatched(namespace string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.watchAllModes {
		return true
	}

	// Check prefix matching if configured
	if cm.watchModePrefix != "" {
		return len(namespace) >= len(cm.watchModePrefix) &&
			namespace[:len(cm.watchModePrefix)] == cm.watchModePrefix
	}

	_, exists := cm.watchedNamespaces[namespace]
	return exists
}

// StartMemoryMonitoring begins memory usage monitoring
func (cm *CacheManager) StartMemoryMonitoring(ctx context.Context) {
	go cm.memoryMonitorLoop(ctx)
	go cm.cacheCleanupLoop(ctx)
	go cm.optimizedGCLoop(ctx)
}

// optimizedGCLoop runs optimized garbage collection based on memory pressure
func (cm *CacheManager) optimizedGCLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Reduced frequency for efficiency
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := cm.GetMemoryStats()

			// Only trigger GC if memory usage is significant
			if stats.AllocMB > float64(cm.warningThreshold/(1024*1024)) {
				goruntime.GC()

				// After GC, check if we need emergency cleanup
				newStats := cm.GetMemoryStats()
				if newStats.AllocMB > float64(cm.memoryThreshold/(1024*1024)) {
					cm.performEmergencyCleanup()
				}
			}
		}
	}
}

// GetMemoryStats returns current memory statistics
func (cm *CacheManager) GetMemoryStats() MemoryStats {
	var m goruntime.MemStats
	goruntime.ReadMemStats(&m)

	return MemoryStats{
		AllocMB:      float64(m.Alloc) / 1024 / 1024,
		TotalAllocMB: float64(m.TotalAlloc) / 1024 / 1024,
		SysMB:        float64(m.Sys) / 1024 / 1024,
		NumGC:        m.NumGC,
		Timestamp:    time.Now(),
	}
}

// ShouldFilterResource determines if a resource should be filtered from cache
func (cm *CacheManager) ShouldFilterResource(obj client.Object) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// Always cache Neo4j CRDs
	switch obj.(type) {
	case *neo4jv1alpha1.Neo4jEnterpriseCluster,
		*neo4jv1alpha1.Neo4jDatabase,
		*neo4jv1alpha1.Neo4jBackup,
		*neo4jv1alpha1.Neo4jRestore:
		return false
	}

	// Check if resource has our management label
	labels := obj.GetLabels()
	if labels != nil {
		if managedBy, exists := labels["app.kubernetes.io/managed-by"]; exists {
			return managedBy != "neo4j-operator"
		}
	}

	// For resources without our label, filter them out
	return true
}

// Private methods

func (cm *CacheManager) setupResourceFilters() {
	cm.filteredResources = map[string]bool{
		"Pod":                true,  // Filter unless labeled
		"Endpoint":           true,  // Filter unless labeled
		"Event":              true,  // Filter unless labeled
		"ReplicaSet":         true,  // Filter unless labeled
		"Deployment":         true,  // Filter unless labeled
		"DaemonSet":          true,  // Filter unless labeled
		"Ingress":            true,  // Filter unless labeled
		"NetworkPolicy":      true,  // Filter unless labeled
		"StorageClass":       false, // Always cache
		"PersistentVolume":   false, // Always cache
		"Node":               false, // Always cache
		"ClusterRole":        false, // Always cache
		"ClusterRoleBinding": false, // Always cache
	}
}

func (cm *CacheManager) getWatchedNamespaces() map[string]cache.Config {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	namespaces := make(map[string]cache.Config)
	for ns := range cm.watchedNamespaces {
		namespaces[ns] = cache.Config{}
	}

	return namespaces
}

func (cm *CacheManager) memoryMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(MemoryCheckInterval)
	defer ticker.Stop()

	log := log.FromContext(ctx).WithName("cache-manager").WithValues("component", "memory-monitor")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := cm.GetMemoryStats()

			// Send stats to channel (non-blocking)
			select {
			case cm.memoryStats <- stats:
			default:
				// Channel full, skip this reading
			}

			// Check thresholds and trigger alerts
			allocBytes := int64(stats.AllocMB * 1024 * 1024)

			if allocBytes > cm.memoryThreshold {
				cm.triggerAlert(AlertLevelCritical,
					fmt.Sprintf("Memory usage critical: %.1f MiB", stats.AllocMB), stats)
				cm.performEmergencyCleanup()
			} else if allocBytes > cm.warningThreshold {
				cm.triggerAlert(AlertLevelWarning,
					fmt.Sprintf("Memory usage warning: %.1f MiB", stats.AllocMB), stats)
			}

			// Log periodic status
			if stats.NumGC%10 == 0 { // Every 10 GC cycles
				log.Info("Memory status",
					"alloc_mb", fmt.Sprintf("%.1f", stats.AllocMB),
					"sys_mb", fmt.Sprintf("%.1f", stats.SysMB),
					"num_gc", stats.NumGC,
					"watched_namespaces", len(cm.watchedNamespaces))
			}
		}
	}
}

func (cm *CacheManager) cacheCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(CacheCleanupInterval)
	defer ticker.Stop()

	log := log.FromContext(ctx).WithName("cache-manager").WithValues("component", "cache-cleanup")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.performRoutineCleanup(ctx)
			log.V(1).Info("Performed routine cache cleanup", "watched_namespaces", len(cm.watchedNamespaces))
		}
	}
}

func (cm *CacheManager) performRoutineCleanup(ctx context.Context) {
	if cm.watchAllModes {
		if cm.isMemoryAboveThreshold() {
			goruntime.GC()
		}
		return
	}

	// Remove old namespace entries (older than 24 hours with no activity)
	cutoff := time.Now().Add(-24 * time.Hour)
	var candidates []string

	cm.mutex.RLock()
	for ns, timestamp := range cm.watchedNamespaces {
		if timestamp.Before(cutoff) {
			candidates = append(candidates, ns)
		}
	}
	cm.mutex.RUnlock()

	for _, ns := range candidates {
		if cm.client == nil {
			log.Log.V(1).Info("Cleanup skipped: cache manager client not configured", "namespace", ns)
			continue
		}

		hasResources, err := cm.namespaceHasNeo4jResources(ctx, ns)
		if err != nil {
			log.Log.V(1).Info("Cleanup check failed, keeping namespace", "namespace", ns, "error", err)
			continue
		}

		if hasResources {
			cm.touchNamespace(ns)
			log.Log.V(1).Info("Namespace has active Neo4j resources, keeping watch", "namespace", ns)
			continue
		}

		cm.RemoveNamespace(ns)
	}

	if len(candidates) > 0 {
		log.Log.Info("Processed old namespace watches", "candidates", candidates, "count", len(candidates))
	}

	// Force garbage collection if memory is getting high
	if cm.isMemoryAboveThreshold() {
		goruntime.GC()
	}
}

func (cm *CacheManager) performEmergencyCleanup() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	log.Log.Info("Performing emergency cache cleanup due to high memory usage")

	// Force garbage collection
	goruntime.GC()

	// If still above threshold, remove oldest namespace watches
	if cm.isMemoryAboveThreshold() && len(cm.watchedNamespaces) > 10 {
		// Keep at least 10 namespaces, remove oldest
		oldestTime := time.Now()
		oldestNS := ""

		for ns, timestamp := range cm.watchedNamespaces {
			if timestamp.Before(oldestTime) {
				oldestTime = timestamp
				oldestNS = ns
			}
		}

		if oldestNS != "" {
			delete(cm.watchedNamespaces, oldestNS)
			log.Log.Info("Emergency cleanup: removed oldest namespace", "namespace", oldestNS)
		}
	}
}

func (cm *CacheManager) isMemoryAboveThreshold() bool {
	stats := cm.GetMemoryStats()
	allocBytes := int64(stats.AllocMB * 1024 * 1024)
	return allocBytes > cm.warningThreshold
}

func (cm *CacheManager) triggerAlert(level AlertLevel, message string, stats MemoryStats) {
	alert := MemoryAlert{
		Level:     level,
		Message:   message,
		Stats:     stats,
		Timestamp: time.Now(),
	}

	// Log the alert
	switch level {
	case AlertLevelCritical:
		log.Log.Error(nil, alert.Message, "stats", alert.Stats)
	case AlertLevelWarning:
		log.Log.Info(alert.Message, "stats", alert.Stats)
	case AlertLevelInfo:
		log.Log.V(1).Info(alert.Message, "stats", alert.Stats)
	}

	// Call external callback if configured
	if cm.alertCallback != nil {
		go cm.alertCallback(alert)
	}
}

func (cm *CacheManager) touchNamespace(namespace string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if _, exists := cm.watchedNamespaces[namespace]; exists {
		cm.watchedNamespaces[namespace] = time.Now()
	}
}

func (cm *CacheManager) namespaceHasNeo4jResources(ctx context.Context, namespace string) (bool, error) {
	checks := []struct {
		name string
		list client.ObjectList
	}{
		{name: "Neo4jEnterpriseCluster", list: &neo4jv1alpha1.Neo4jEnterpriseClusterList{}},
		{name: "Neo4jEnterpriseStandalone", list: &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}},
		{name: "Neo4jDatabase", list: &neo4jv1alpha1.Neo4jDatabaseList{}},
		{name: "Neo4jPlugin", list: &neo4jv1alpha1.Neo4jPluginList{}},
		{name: "Neo4jBackup", list: &neo4jv1alpha1.Neo4jBackupList{}},
		{name: "Neo4jRestore", list: &neo4jv1alpha1.Neo4jRestoreList{}},
		{name: "Neo4jShardedDatabase", list: &neo4jv1alpha1.Neo4jShardedDatabaseList{}},
	}

	for _, check := range checks {
		if err := cm.client.List(ctx, check.list, client.InNamespace(namespace), client.Limit(1)); err != nil {
			return true, fmt.Errorf("list %s: %w", check.name, err)
		}
		if listHasItems(check.list) {
			return true, nil
		}
	}

	return false, nil
}

func listHasItems(list client.ObjectList) bool {
	switch typed := list.(type) {
	case *neo4jv1alpha1.Neo4jEnterpriseClusterList:
		return len(typed.Items) > 0
	case *neo4jv1alpha1.Neo4jEnterpriseStandaloneList:
		return len(typed.Items) > 0
	case *neo4jv1alpha1.Neo4jDatabaseList:
		return len(typed.Items) > 0
	case *neo4jv1alpha1.Neo4jPluginList:
		return len(typed.Items) > 0
	case *neo4jv1alpha1.Neo4jBackupList:
		return len(typed.Items) > 0
	case *neo4jv1alpha1.Neo4jRestoreList:
		return len(typed.Items) > 0
	case *neo4jv1alpha1.Neo4jShardedDatabaseList:
		return len(typed.Items) > 0
	default:
		return false
	}
}

// NamespaceDiscovery helps discover relevant namespaces based on prefix or labels
type NamespaceDiscovery struct {
	client        client.Client
	prefix        string
	labelSelector labels.Selector
}

// NewNamespaceDiscovery creates a namespace discovery helper
func NewNamespaceDiscovery(client client.Client, prefix string, labelSelector labels.Selector) *NamespaceDiscovery {
	return &NamespaceDiscovery{
		client:        client,
		prefix:        prefix,
		labelSelector: labelSelector,
	}
}

// DiscoverNamespaces finds namespaces that should be watched
func (nd *NamespaceDiscovery) DiscoverNamespaces(ctx context.Context) ([]string, error) {
	namespaceList := &corev1.NamespaceList{}

	listOpts := []client.ListOption{}
	if nd.labelSelector != nil {
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: nd.labelSelector})
	}

	if err := nd.client.List(ctx, namespaceList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var relevant []string
	for _, ns := range namespaceList.Items {
		// Skip system namespaces
		if ns.Name == "kube-system" || ns.Name == "kube-public" || ns.Name == "kube-node-lease" {
			continue
		}

		// Check prefix matching
		if nd.prefix != "" {
			if len(ns.Name) >= len(nd.prefix) && ns.Name[:len(nd.prefix)] == nd.prefix {
				relevant = append(relevant, ns.Name)
			}
		} else {
			relevant = append(relevant, ns.Name)
		}
	}

	return relevant, nil
}
