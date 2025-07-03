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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// FastCacheStrategy defines ultra-fast caching approaches
type FastCacheStrategy string

const (
	// NoCache - No caching, direct API calls (ultra-fast startup, slower operations)
	NoCache FastCacheStrategy = "none"
	// LazyInformers - Create informers only when first accessed
	LazyInformers FastCacheStrategy = "lazy"
	// SelectiveWatch - Only watch resources that are actually being used
	SelectiveWatch FastCacheStrategy = "selective"
	// OnDemandSync - Sync only when resources are requested
	OnDemandSync FastCacheStrategy = "on-demand"
)

// FastCache implements ultra-fast caching with lazy loading
type FastCache struct {
	config   *rest.Config
	scheme   *runtime.Scheme
	strategy FastCacheStrategy

	// Cache management
	mutex          sync.RWMutex
	caches         map[schema.GroupVersionKind]cache.Cache
	activeWatchers map[schema.GroupVersionKind]bool
	cacheReaders   map[schema.GroupVersionKind]client.Reader

	// Resource usage tracking
	accessCount map[schema.GroupVersionKind]int64
	lastAccess  map[schema.GroupVersionKind]time.Time

	// Cache warmup tracking
	warmedUp    map[schema.GroupVersionKind]bool
	warmupQueue chan schema.GroupVersionKind

	// Direct client for non-cached operations
	directClient client.Client

	// Context for cache lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	logger logr.Logger
}

// NewFastCache creates a new fast cache instance
func NewFastCache(config *rest.Config, scheme *runtime.Scheme, strategy FastCacheStrategy) *FastCache {
	ctx, cancel := context.WithCancel(context.Background())

	fc := &FastCache{
		config:         config,
		scheme:         scheme,
		strategy:       strategy,
		caches:         make(map[schema.GroupVersionKind]cache.Cache),
		activeWatchers: make(map[schema.GroupVersionKind]bool),
		cacheReaders:   make(map[schema.GroupVersionKind]client.Reader),
		accessCount:    make(map[schema.GroupVersionKind]int64),
		lastAccess:     make(map[schema.GroupVersionKind]time.Time),
		warmedUp:       make(map[schema.GroupVersionKind]bool),
		warmupQueue:    make(chan schema.GroupVersionKind, 100),
		ctx:            ctx,
		cancel:         cancel,
		logger:         log.Log.WithName("fast-cache"),
	}

	// Create direct client for non-cached operations
	directClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		fc.logger.Error(err, "failed to create direct client")
		return nil
	}
	fc.directClient = directClient

	return fc
}

// Start begins the fast cache system
func (fc *FastCache) Start(ctx context.Context) error {
	fc.logger.Info("starting fast cache", "strategy", fc.strategy)

	// Start warmup worker
	go fc.warmupWorker(ctx)

	// Start cleanup worker
	go fc.cleanupWorker(ctx)

	// Pre-warm essential resources based on strategy
	if err := fc.prewarmEssentialResources(); err != nil {
		return fmt.Errorf("failed to prewarm essential resources: %w", err)
	}

	return nil
}

// Stop stops the fast cache system
func (fc *FastCache) Stop() {
	fc.logger.Info("stopping fast cache")
	fc.cancel()

	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	// Clear all caches
	fc.caches = make(map[schema.GroupVersionKind]cache.Cache)
	fc.cacheReaders = make(map[schema.GroupVersionKind]client.Reader)
	fc.warmedUp = make(map[schema.GroupVersionKind]bool)
}

// GetClient returns a client that uses the fast cache
func (fc *FastCache) GetClient() client.Client {
	return &FastCacheClient{
		fastCache:    fc,
		directClient: fc.directClient,
	}
}

// prewarmEssentialResources pre-warms only the most critical resources
func (fc *FastCache) prewarmEssentialResources() error {
	essential := []schema.GroupVersionKind{
		neo4jv1alpha1.GroupVersion.WithKind("Neo4jEnterpriseCluster"),
	}

	switch fc.strategy {
	case NoCache:
		// No prewarming for no-cache strategy
		fc.logger.Info("no-cache strategy - skipping prewarming")
		return nil

	case LazyInformers:
		// Only prewarm if explicitly requested
		fc.logger.Info("lazy strategy - minimal prewarming")
		return nil

	case SelectiveWatch:
		// Prewarm only Neo4j cluster resources
		fc.logger.Info("selective strategy - prewarming cluster resources")
		for _, gvk := range essential[:1] { // Only cluster
			fc.requestWarmup(gvk)
		}

	case OnDemandSync:
		// No prewarming, everything on-demand
		fc.logger.Info("on-demand strategy - no prewarming")
		return nil
	}

	return nil
}

// requestWarmup queues a resource for background warmup
func (fc *FastCache) requestWarmup(gvk schema.GroupVersionKind) {
	select {
	case fc.warmupQueue <- gvk:
		fc.logger.V(1).Info("queued resource for warmup", "gvk", gvk)
	default:
		fc.logger.V(1).Info("warmup queue full, skipping", "gvk", gvk)
	}
}

// warmupWorker processes warmup requests in the background
func (fc *FastCache) warmupWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case gvk := <-fc.warmupQueue:
			if err := fc.warmupResource(ctx, gvk); err != nil {
				fc.logger.Error(err, "failed to warmup resource", "gvk", gvk)
			}
		}
	}
}

// warmupResource creates and starts a cache for a specific resource
func (fc *FastCache) warmupResource(ctx context.Context, gvk schema.GroupVersionKind) error {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	// Check if already warmed up
	if fc.warmedUp[gvk] {
		return nil
	}

	fc.logger.Info("warming up resource", "gvk", gvk)

	// Create object instance for the resource type
	var obj client.Object
	switch gvk {
	case neo4jv1alpha1.GroupVersion.WithKind("Neo4jEnterpriseCluster"):
		obj = &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	case neo4jv1alpha1.GroupVersion.WithKind("Neo4jDatabase"):
		obj = &neo4jv1alpha1.Neo4jDatabase{}
	case neo4jv1alpha1.GroupVersion.WithKind("Neo4jBackup"):
		obj = &neo4jv1alpha1.Neo4jBackup{}
	case neo4jv1alpha1.GroupVersion.WithKind("Neo4jRestore"):
		obj = &neo4jv1alpha1.Neo4jRestore{}
	case neo4jv1alpha1.GroupVersion.WithKind("Neo4jPlugin"):
		obj = &neo4jv1alpha1.Neo4jPlugin{}
	default:
		return fmt.Errorf("unsupported resource type: %v", gvk)
	}

	// Create optimized cache configuration for this resource
	cacheOpts := cache.Options{
		Scheme: fc.scheme,
		ByObject: map[client.Object]cache.ByObject{
			obj: {
				// Cache all namespaces for comprehensive coverage
				Transform: func(obj interface{}) (interface{}, error) {
					// Optional: transform objects to reduce memory usage
					return obj, nil
				},
			},
		},
		// Set reasonable sync period
		SyncPeriod: func() *time.Duration {
			d := 5 * time.Minute
			return &d
		}(),
	}

	// Create the cache
	resourceCache, err := cache.New(fc.config, cacheOpts)
	if err != nil {
		return fmt.Errorf("failed to create cache for %v: %w", gvk, err)
	}

	// Start the cache in background
	go func() {
		if err := resourceCache.Start(fc.ctx); err != nil {
			fc.logger.Error(err, "failed to start cache", "gvk", gvk)
		}
	}()

	// Wait for initial sync with timeout
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if !resourceCache.WaitForCacheSync(syncCtx) {
		fc.logger.Info("cache sync timeout, cache will sync in background", "gvk", gvk)
		// Don't fail - let it sync in background
	}

	// Store the cache and reader
	fc.caches[gvk] = resourceCache
	fc.cacheReaders[gvk] = resourceCache
	fc.warmedUp[gvk] = true
	fc.activeWatchers[gvk] = true

	fc.logger.Info("resource warmed up successfully", "gvk", gvk)
	return nil
}

// cleanupWorker removes unused caches to save memory
func (fc *FastCache) cleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fc.cleanupUnusedCaches()
		}
	}
}

// cleanupUnusedCaches removes caches that haven't been accessed recently
func (fc *FastCache) cleanupUnusedCaches() {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	cleaned := 0

	for gvk, lastAccess := range fc.lastAccess {
		if lastAccess.Before(cutoff) && fc.accessCount[gvk] < 10 {
			// Remove unused cache
			delete(fc.caches, gvk)
			delete(fc.cacheReaders, gvk)
			delete(fc.activeWatchers, gvk)
			delete(fc.warmedUp, gvk)
			delete(fc.accessCount, gvk)
			delete(fc.lastAccess, gvk)
			cleaned++
		}
	}

	if cleaned > 0 {
		fc.logger.Info("cleaned up unused caches", "count", cleaned)
	}
}

// recordAccess tracks resource access for cleanup decisions
func (fc *FastCache) recordAccess(gvk schema.GroupVersionKind) {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	fc.accessCount[gvk]++
	fc.lastAccess[gvk] = time.Now()
}

// getCacheReader returns a cache reader for the given GVK
func (fc *FastCache) getCacheReader(gvk schema.GroupVersionKind) (client.Reader, bool) {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()

	reader, exists := fc.cacheReaders[gvk]
	return reader, exists && fc.warmedUp[gvk]
}

// FastCacheClient wraps the direct client with fast cache capabilities
type FastCacheClient struct {
	fastCache    *FastCache
	directClient client.Client
}

// Get implements client.Client interface with fast cache logic
func (fcc *FastCacheClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		// Try to determine GVK from the object type
		gvks, _, err := fcc.fastCache.scheme.ObjectKinds(obj)
		if err == nil && len(gvks) > 0 {
			obj.GetObjectKind().SetGroupVersionKind(gvks[0])
			gvk = gvks[0]
		}
	}

	fcc.fastCache.recordAccess(gvk)

	switch fcc.fastCache.strategy {
	case NoCache:
		// Always use direct API calls
		return fcc.directClient.Get(ctx, key, obj, opts...)

	case LazyInformers, SelectiveWatch, OnDemandSync:
		// Try to get from cache first
		if reader, available := fcc.fastCache.getCacheReader(gvk); available {
			if err := reader.Get(ctx, key, obj, opts...); err == nil {
				fcc.fastCache.logger.V(1).Info("cache hit", "gvk", gvk, "key", key)
				return nil
			}
			// Cache miss or error, continue to direct client
			fcc.fastCache.logger.V(1).Info("cache miss, using direct client", "gvk", gvk, "key", key)
		} else {
			// No cache available, request warmup for future requests
			fcc.fastCache.requestWarmup(gvk)
		}

		// Use direct client
		return fcc.directClient.Get(ctx, key, obj, opts...)

	default:
		return fcc.directClient.Get(ctx, key, obj, opts...)
	}
}

// List implements client.Client interface with cache support
func (fcc *FastCacheClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// Extract GVK from the list object
	gvk := list.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		// Try to determine GVK from the list type
		gvks, _, err := fcc.fastCache.scheme.ObjectKinds(list)
		if err == nil && len(gvks) > 0 {
			list.GetObjectKind().SetGroupVersionKind(gvks[0])
			gvk = gvks[0]
		}
	}

	fcc.fastCache.recordAccess(gvk)

	switch fcc.fastCache.strategy {
	case NoCache:
		// Always use direct API calls
		return fcc.directClient.List(ctx, list, opts...)

	case LazyInformers, SelectiveWatch, OnDemandSync:
		// Try to get from cache first
		if reader, available := fcc.fastCache.getCacheReader(gvk); available {
			if err := reader.List(ctx, list, opts...); err == nil {
				fcc.fastCache.logger.V(1).Info("cache list hit", "gvk", gvk)
				return nil
			}
			// Cache miss or error, continue to direct client
			fcc.fastCache.logger.V(1).Info("cache list miss, using direct client", "gvk", gvk)
		} else {
			// No cache available, request warmup for future requests
			fcc.fastCache.requestWarmup(gvk)
		}

		// Use direct client
		return fcc.directClient.List(ctx, list, opts...)

	default:
		return fcc.directClient.List(ctx, list, opts...)
	}
}

// Create implements client.Client interface
func (fcc *FastCacheClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return fcc.directClient.Create(ctx, obj, opts...)
}

// Delete implements client.Client interface
func (fcc *FastCacheClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return fcc.directClient.Delete(ctx, obj, opts...)
}

// Update implements client.Client interface
func (fcc *FastCacheClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return fcc.directClient.Update(ctx, obj, opts...)
}

// Patch implements client.Client interface
func (fcc *FastCacheClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return fcc.directClient.Patch(ctx, obj, patch, opts...)
}

// DeleteAllOf implements client.Client interface
func (fcc *FastCacheClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return fcc.directClient.DeleteAllOf(ctx, obj, opts...)
}

// Status implements client.Client interface
func (fcc *FastCacheClient) Status() client.StatusWriter {
	return fcc.directClient.Status()
}

// Scheme implements client.Client interface
func (fcc *FastCacheClient) Scheme() *runtime.Scheme {
	return fcc.directClient.Scheme()
}

// RESTMapper implements client.Client interface
func (fcc *FastCacheClient) RESTMapper() meta.RESTMapper {
	return fcc.directClient.RESTMapper()
}

// GroupVersionKindFor implements client.Client interface
func (fcc *FastCacheClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return fcc.directClient.GroupVersionKindFor(obj)
}

// IsObjectNamespaced implements client.Client interface
func (fcc *FastCacheClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return fcc.directClient.IsObjectNamespaced(obj)
}

// SubResource implements client.Client interface
func (fcc *FastCacheClient) SubResource(subResource string) client.SubResourceClient {
	return fcc.directClient.SubResource(subResource)
}
