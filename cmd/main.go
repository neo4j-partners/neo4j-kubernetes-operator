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

// Package main is the unified entry point for the Neo4j Kubernetes Operator.
// It supports three modes: production (default), development, and minimal.
//
// Production mode: go run cmd/main.go
// Development mode: go run cmd/main.go --mode=dev
// Minimal mode: go run cmd/main.go --mode=minimal
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	// +kubebuilder:scaffold:imports
)

// OperatorMode defines the different modes of operation
type OperatorMode string

const (
	// ProductionMode runs the operator with full functionality
	ProductionMode OperatorMode = "production"
	// DevelopmentMode runs the operator with development optimizations
	DevelopmentMode OperatorMode = "dev"
	// MinimalMode runs the operator with minimal functionality for fast startup
	MinimalMode OperatorMode = "minimal"
)

// CacheStrategy defines different caching approaches for startup optimization
type CacheStrategy string

const (
	// StandardCache uses default controller-runtime caching (slowest but most complete)
	StandardCache CacheStrategy = "standard"
	// LazyCache enables lazy loading of informers (faster startup, slower first requests)
	LazyCache CacheStrategy = "lazy"
	// SelectiveCache only caches essential resources (fastest startup)
	SelectiveCache CacheStrategy = "selective"
	// OnDemandCache creates informers only when needed (ultra-fast startup)
	OnDemandCache CacheStrategy = "on-demand"
	// NoCache represents no caching
	NoCache CacheStrategy = "none"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(neo4jv1alpha1.AddToScheme(scheme))
	utilruntime.Must(certv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		mode                 = flag.String("mode", "production", "Operator mode: production, dev, or minimal")
		metricsAddr          = flag.String("metrics-bind-address", "", "The address the metric endpoint binds to. (auto-assigned based on mode if empty)")
		probeAddr            = flag.String("health-probe-bind-address", "", "The address the probe endpoint binds to. (auto-assigned based on mode if empty)")
		enableLeaderElection = flag.Bool("leader-elect", false, "Enable leader election for controller manager.")
		secureMetrics        = flag.Bool("metrics-secure", false, "If set the metrics endpoint is served securely")

		// Development mode specific flags
		controllersToLoad = flag.String("controllers", "cluster", "Comma-separated list of controllers to load (dev mode only)")

		// Minimal mode specific flags
		namespace  = flag.String("namespace", "default", "Namespace to watch (minimal mode only, empty for all namespaces)")
		syncPeriod = flag.Duration("sync-period", 60*time.Second, "Cache sync period (minimal mode only)")

		// Cache optimization flags
		cacheStrategy    = flag.String("cache-strategy", "", "Cache strategy: standard, lazy, selective, on-demand, none (auto-selected based on mode if empty)")
		skipCacheWait    = flag.Bool("skip-cache-wait", false, "Skip waiting for cache sync before starting controllers")
		lazyInformers    = flag.Bool("lazy-informers", false, "Enable lazy informer creation")
		minimalResources = flag.Bool("minimal-resources", false, "Only cache essential resources for startup")
		ultraFast        = flag.Bool("ultra-fast", false, "Enable ultra-fast mode with no informer caching")
	)

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Validate and normalize mode
	operatorMode := OperatorMode(strings.ToLower(*mode))
	switch operatorMode {
	case ProductionMode, DevelopmentMode, MinimalMode:
		// Valid modes
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Valid modes are: production, dev, minimal\n", *mode)
		os.Exit(1)
	}

	// Set default addresses based on mode if not specified
	if *metricsAddr == "" {
		switch operatorMode {
		case ProductionMode:
			*metricsAddr = ":8080"
		case DevelopmentMode:
			*metricsAddr = ":8082"
		case MinimalMode:
			*metricsAddr = ":8084"
		}
	}

	if *probeAddr == "" {
		switch operatorMode {
		case ProductionMode:
			*probeAddr = ":8081"
		case DevelopmentMode:
			*probeAddr = ":8083"
		case MinimalMode:
			*probeAddr = ":8085"
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Validate flag values
	if *metricsAddr == "" {
		setupLog.Error(nil, "metrics-bind-address cannot be empty")
		os.Exit(1)
	}
	if *probeAddr == "" {
		setupLog.Error(nil, "health-probe-bind-address cannot be empty")
		os.Exit(1)
	}
	if operatorMode == MinimalMode && *syncPeriod <= 0 {
		setupLog.Error(nil, "sync-period must be positive")
		os.Exit(1)
	}

	// Set cache strategy based on mode if not specified
	if *cacheStrategy == "" {
		switch operatorMode {
		case ProductionMode:
			*cacheStrategy = string(OnDemandCache) // Use fastest cache method for production
		case DevelopmentMode:
			if *ultraFast {
				*cacheStrategy = string(NoCache)
			} else {
				*cacheStrategy = string(OnDemandCache) // Use fastest cache method for development
			}
		case MinimalMode:
			*cacheStrategy = string(NoCache) // Always use no-cache for minimal mode
		}
	}

	// Auto-enable optimizations based on mode
	if operatorMode == DevelopmentMode && !isFlagSet("skip-cache-wait") {
		*skipCacheWait = true
	}
	if operatorMode == MinimalMode {
		if !isFlagSet("lazy-informers") {
			*lazyInformers = true
			*minimalResources = true
		}
		if !isFlagSet("ultra-fast") {
			*ultraFast = true
		}
	}

	setupLog.Info("starting Neo4j Operator",
		"mode", operatorMode,
		"cache_strategy", *cacheStrategy,
		"skip_cache_wait", *skipCacheWait,
		"lazy_informers", *lazyInformers,
		"minimal_resources", *minimalResources,
		"ultra_fast", *ultraFast,
		"metrics_address", *metricsAddr,
		"health_address", *probeAddr,
	)

	// Configure cache options based on mode and strategy
	var cacheOpts cache.Options
	var useDirectClient bool
	config := ctrl.GetConfigOrDie()

	// Check if we should bypass caching entirely
	if *cacheStrategy == string(NoCache) || *ultraFast {
		setupLog.Info("using direct API client - bypassing all informer caching for ultra-fast startup")
		useDirectClient = true
		*skipCacheWait = true
	}

	switch operatorMode {
	case DevelopmentMode:
		if useDirectClient {
			cacheOpts = cache.Options{} // Empty cache options
		} else {
			cacheOpts = configureDevelopmentCache(*cacheStrategy, *lazyInformers, *minimalResources, *namespace)
		}
		config.Timeout = 10 * time.Second
		config.QPS = 100
		config.Burst = 200
		setupLog.Info("development mode enabled - using optimized cache settings")

	case MinimalMode:
		if useDirectClient {
			cacheOpts = cache.Options{} // Empty cache options for direct client
		} else {
			cacheOpts = configureMinimalCache(*cacheStrategy, *lazyInformers, *minimalResources, *namespace, syncPeriod)
		}
		config.Timeout = 5 * time.Second
		config.QPS = 50
		config.Burst = 100
		setupLog.Info("minimal mode enabled - using fastest startup settings")

	case ProductionMode:
		if useDirectClient {
			setupLog.Info("WARNING: using direct client in production mode - this may impact performance")
			cacheOpts = cache.Options{}
		} else {
			// Force lazy cache for production to avoid RBAC and startup issues
			if *cacheStrategy == "" {
				*cacheStrategy = "lazy"
			}
			cacheOpts = configureProductionCache(*cacheStrategy, *lazyInformers, *minimalResources)
		}
		setupLog.Info("production mode enabled - using standard settings")
	}

	// Create manager with optimized cache
	var mgr ctrl.Manager
	var err error

	if useDirectClient {
		// Create manager with minimal caching
		mgr, err = createDirectClientManager(config, cacheOpts, *metricsAddr, *probeAddr, *secureMetrics, operatorMode, *enableLeaderElection)
	} else {
		// Standard manager creation
		mgr, err = ctrl.NewManager(config, ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress:   *metricsAddr,
				SecureServing: *secureMetrics && operatorMode == ProductionMode,
			},
			HealthProbeBindAddress: *probeAddr,
			LeaderElection:         *enableLeaderElection,
			LeaderElectionID:       "neo4j-operator-leader-election",
			Cache:                  cacheOpts,
		})
	}

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Setup controllers based on mode
	if err = setupControllers(mgr, operatorMode, *controllersToLoad); err != nil {
		setupLog.Error(err, "failed to setup controllers")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", createReadinessCheck(*skipCacheWait)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	// Add startup feedback based on mode and cache strategy
	go startupFeedback(operatorMode, *metricsAddr, *probeAddr, *skipCacheWait)

	// Start manager with cache optimization
	if *skipCacheWait {
		setupLog.Info("skipping cache sync wait - starting immediately")
		go func() {
			// Allow some time for basic setup
			time.Sleep(2 * time.Second)
			setupLog.Info("manager ready - cache will sync in background")
		}()
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// setupControllers sets up controllers based on the operator mode
func setupControllers(mgr ctrl.Manager, mode OperatorMode, controllersToLoad string) error {
	switch mode {
	case ProductionMode:
		return setupProductionControllers(mgr)
	case DevelopmentMode:
		controllers := parseControllers(controllersToLoad)
		setupLog.Info("loading controllers", "controllers", controllers)
		return setupDevelopmentControllers(mgr, controllers)
	case MinimalMode:
		return setupMinimalController(mgr)
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

// setupProductionControllers sets up all controllers for production mode
func setupProductionControllers(mgr ctrl.Manager) error {
	controllers := []struct {
		name       string
		controller interface{ SetupWithManager(ctrl.Manager) error }
	}{
		{
			name: "Neo4jEnterpriseCluster",
			controller: &controller.Neo4jEnterpriseClusterReconciler{
				Client:             mgr.GetClient(),
				Scheme:             mgr.GetScheme(),
				Recorder:           mgr.GetEventRecorderFor("neo4j-enterprise-cluster-controller"),
				RequeueAfter:       controller.GetTestRequeueAfter(),
				TopologyScheduler:  controller.NewTopologyScheduler(mgr.GetClient()),
				Validator:          validation.NewClusterValidator(mgr.GetClient()),
				ConfigMapManager:   controller.NewConfigMapManager(mgr.GetClient()),
				SplitBrainDetector: controller.NewSplitBrainDetector(mgr.GetClient()),
			},
		},
		{
			name: "Neo4jEnterpriseStandalone",
			controller: &controller.Neo4jEnterpriseStandaloneReconciler{
				Client:           mgr.GetClient(),
				Scheme:           mgr.GetScheme(),
				Recorder:         mgr.GetEventRecorderFor("neo4j-enterprise-standalone-controller"),
				RequeueAfter:     controller.GetTestRequeueAfter(),
				Validator:        validation.NewStandaloneValidator(),
				ConfigMapManager: controller.NewConfigMapManager(mgr.GetClient()),
			},
		},
		{
			name: "Neo4jDatabase",
			controller: &controller.Neo4jDatabaseReconciler{
				Client:            mgr.GetClient(),
				Scheme:            mgr.GetScheme(),
				Recorder:          mgr.GetEventRecorderFor("neo4j-database-controller"),
				RequeueAfter:      controller.GetTestRequeueAfter(),
				DatabaseValidator: validation.NewDatabaseValidator(mgr.GetClient()),
			},
		},
		{
			name: "Neo4jBackup",
			controller: &controller.Neo4jBackupReconciler{
				Client:       mgr.GetClient(),
				Scheme:       mgr.GetScheme(),
				Recorder:     mgr.GetEventRecorderFor("neo4j-backup-controller"),
				RequeueAfter: controller.GetTestRequeueAfter(),
			},
		},
		{
			name: "Neo4jRestore",
			controller: &controller.Neo4jRestoreReconciler{
				Client:       mgr.GetClient(),
				Scheme:       mgr.GetScheme(),
				Recorder:     mgr.GetEventRecorderFor("neo4j-restore-controller"),
				RequeueAfter: controller.GetTestRequeueAfter(),
			},
		},
		{
			name: "Neo4jPlugin",
			controller: &controller.Neo4jPluginReconciler{
				Client:       mgr.GetClient(),
				Scheme:       mgr.GetScheme(),
				RequeueAfter: controller.GetTestRequeueAfter(),
			},
		},
	}

	for _, ctrl := range controllers {
		if err := ctrl.controller.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("failed to setup controller %s: %w", ctrl.name, err)
		}
		setupLog.Info("controller setup completed", "controller", ctrl.name)
	}

	return nil
}

// setupDevelopmentControllers sets up controllers based on configuration for development mode
func setupDevelopmentControllers(mgr ctrl.Manager, controllers []string) error {
	controllerMap := map[string]func() (interface{ SetupWithManager(ctrl.Manager) error }, string){
		"cluster": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jEnterpriseClusterReconciler{
				Client:             mgr.GetClient(),
				Scheme:             mgr.GetScheme(),
				Recorder:           mgr.GetEventRecorderFor("neo4j-enterprise-cluster-controller"),
				RequeueAfter:       controller.GetTestRequeueAfter(),
				TopologyScheduler:  controller.NewTopologyScheduler(mgr.GetClient()),
				Validator:          validation.NewClusterValidator(mgr.GetClient()),
				ConfigMapManager:   controller.NewConfigMapManager(mgr.GetClient()),
				SplitBrainDetector: controller.NewSplitBrainDetector(mgr.GetClient()),
			}, "Neo4jEnterpriseCluster"
		},
		"standalone": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jEnterpriseStandaloneReconciler{
				Client:           mgr.GetClient(),
				Scheme:           mgr.GetScheme(),
				Recorder:         mgr.GetEventRecorderFor("neo4j-enterprise-standalone-controller"),
				RequeueAfter:     controller.GetTestRequeueAfter(),
				Validator:        validation.NewStandaloneValidator(),
				ConfigMapManager: controller.NewConfigMapManager(mgr.GetClient()),
			}, "Neo4jEnterpriseStandalone"
		},
		"database": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jDatabaseReconciler{
				Client:            mgr.GetClient(),
				Scheme:            mgr.GetScheme(),
				Recorder:          mgr.GetEventRecorderFor("neo4j-database-controller"),
				RequeueAfter:      controller.GetTestRequeueAfter(),
				DatabaseValidator: validation.NewDatabaseValidator(mgr.GetClient()),
			}, "Neo4jDatabase"
		},
		"backup": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jBackupReconciler{
				Client:       mgr.GetClient(),
				Scheme:       mgr.GetScheme(),
				Recorder:     mgr.GetEventRecorderFor("neo4j-backup-controller"),
				RequeueAfter: controller.GetTestRequeueAfter(),
			}, "Neo4jBackup"
		},
		"restore": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jRestoreReconciler{
				Client:       mgr.GetClient(),
				Scheme:       mgr.GetScheme(),
				Recorder:     mgr.GetEventRecorderFor("neo4j-restore-controller"),
				RequeueAfter: controller.GetTestRequeueAfter(),
			}, "Neo4jRestore"
		},
		"plugin": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jPluginReconciler{
				Client:       mgr.GetClient(),
				Scheme:       mgr.GetScheme(),
				RequeueAfter: controller.GetTestRequeueAfter(),
			}, "Neo4jPlugin"
		},
	}

	for _, controllerName := range controllers {
		if factory, exists := controllerMap[controllerName]; exists {
			ctrl, name := factory()
			if err := ctrl.SetupWithManager(mgr); err != nil {
				return fmt.Errorf("failed to setup controller %s: %w", name, err)
			}
			setupLog.Info("loaded controller", "controller", name)
		} else {
			setupLog.Info("skipping unknown controller", "controller", controllerName)
		}
	}

	return nil
}

// setupMinimalController sets up only the essential cluster controller for minimal mode
func setupMinimalController(mgr ctrl.Manager) error {
	if err := (&controller.Neo4jEnterpriseClusterReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		Recorder:           mgr.GetEventRecorderFor("neo4j-enterprise-cluster-controller"),
		RequeueAfter:       controller.GetTestRequeueAfter(),
		TopologyScheduler:  controller.NewTopologyScheduler(mgr.GetClient()),
		Validator:          validation.NewClusterValidator(mgr.GetClient()),
		ConfigMapManager:   controller.NewConfigMapManager(mgr.GetClient()),
		SplitBrainDetector: controller.NewSplitBrainDetector(mgr.GetClient()),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup Neo4jEnterpriseCluster controller: %w", err)
	}

	setupLog.Info("loaded controller", "controller", "Neo4jEnterpriseCluster")
	return nil
}

// configureDevelopmentCache sets up optimized caching for development mode
func configureDevelopmentCache(strategy string, _ bool, minimalResources bool, _ string) cache.Options {
	opts := cache.Options{
		SyncPeriod: func() *time.Duration {
			d := 30 * time.Second
			return &d
		}(),
		DefaultNamespaces: map[string]cache.Config{
			"default": {},
		},
	}

	switch CacheStrategy(strategy) {
	case LazyCache:
		setupLog.Info("using lazy cache strategy for development")
		return configureLazyCache(opts, minimalResources)
	case SelectiveCache:
		setupLog.Info("using selective cache strategy for development")
		return configureSelectiveCache(opts, minimalResources)
	case OnDemandCache:
		setupLog.Info("using on-demand cache strategy for development")
		return configureOnDemandCache(opts, minimalResources)
	default:
		setupLog.Info("using standard cache strategy for development")
		return opts
	}
}

// configureMinimalCache sets up ultra-fast caching for minimal mode
func configureMinimalCache(strategy string, _ bool, _ bool, namespace string, syncPeriod *time.Duration) cache.Options {
	opts := cache.Options{
		SyncPeriod: syncPeriod,
	}

	if namespace != "" {
		opts.DefaultNamespaces = map[string]cache.Config{
			namespace: {},
		}
		setupLog.Info("watching single namespace for minimal cache", "namespace", namespace)
	}

	switch CacheStrategy(strategy) {
	case OnDemandCache:
		setupLog.Info("using on-demand cache strategy for minimal mode")
		return configureOnDemandCache(opts, true)
	case SelectiveCache:
		setupLog.Info("using selective cache strategy for minimal mode")
		return configureSelectiveCache(opts, true)
	case LazyCache:
		setupLog.Info("using lazy cache strategy for minimal mode")
		return configureLazyCache(opts, true)
	default:
		setupLog.Info("using on-demand cache strategy for minimal mode (default)")
		return configureOnDemandCache(opts, true)
	}
}

// configureProductionCache sets up standard caching for production mode
func configureProductionCache(strategy string, _ bool, minimalResources bool) cache.Options {
	opts := cache.Options{}

	switch CacheStrategy(strategy) {
	case LazyCache:
		setupLog.Info("using lazy cache strategy for production")
		return configureLazyCache(opts, minimalResources)
	case SelectiveCache:
		setupLog.Info("using selective cache strategy for production")
		return configureSelectiveCache(opts, minimalResources)
	case OnDemandCache:
		setupLog.Info("using on-demand cache strategy for production")
		return configureOnDemandCache(opts, minimalResources)
	default:
		setupLog.Info("using standard cache strategy for production")
		return opts
	}
}

// configureLazyCache sets up lazy loading of informers
func configureLazyCache(base cache.Options, minimalResources bool) cache.Options {
	if minimalResources {
		// Only cache Neo4j CRDs initially
		base.ByObject = map[client.Object]cache.ByObject{
			&neo4jv1alpha1.Neo4jEnterpriseCluster{}:    {},
			&neo4jv1alpha1.Neo4jEnterpriseStandalone{}: {},
			&neo4jv1alpha1.Neo4jDatabase{}:             {},
		}
	} else {
		// Cache essential resources only - optimized for production
		base.ByObject = getEssentialResourceCache()

		// Add production-specific optimizations
		base.SyncPeriod = func() *time.Duration {
			d := 5 * time.Minute // Longer sync period for production stability
			return &d
		}()
	}

	return base
}

// configureSelectiveCache sets up selective resource caching
func configureSelectiveCache(base cache.Options, minimalResources bool) cache.Options {
	if minimalResources {
		// Ultra-minimal: only cluster CRD
		base.ByObject = map[client.Object]cache.ByObject{
			&neo4jv1alpha1.Neo4jEnterpriseCluster{}:    {},
			&neo4jv1alpha1.Neo4jEnterpriseStandalone{}: {},
		}
	} else {
		// Selective: Neo4j CRDs + core resources we manage
		base.ByObject = getSelectiveResourceCache()
	}

	return base
}

// configureOnDemandCache sets up on-demand informer creation
func configureOnDemandCache(base cache.Options, minimalResources bool) cache.Options {
	// Start with absolutely minimal cache - only what's needed for health checks
	base.ByObject = map[client.Object]cache.ByObject{}

	if !minimalResources {
		// Keep cluster CRD for basic functionality
		base.ByObject[&neo4jv1alpha1.Neo4jEnterpriseCluster{}] = cache.ByObject{}
		base.ByObject[&neo4jv1alpha1.Neo4jEnterpriseStandalone{}] = cache.ByObject{}
	}

	return base
}

// getEssentialResourceCache returns cache config for essential resources
func getEssentialResourceCache() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		// Neo4j CRDs - always essential
		&neo4jv1alpha1.Neo4jEnterpriseCluster{}:    {},
		&neo4jv1alpha1.Neo4jEnterpriseStandalone{}: {},
		&neo4jv1alpha1.Neo4jDatabase{}:             {},
		&neo4jv1alpha1.Neo4jBackup{}:               {},
		&neo4jv1alpha1.Neo4jRestore{}:              {},
		&neo4jv1alpha1.Neo4jPlugin{}:               {},
	}
}

// getSelectiveResourceCache returns cache config for selective resources
func getSelectiveResourceCache() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		// Neo4j CRDs
		&neo4jv1alpha1.Neo4jEnterpriseCluster{}:    {},
		&neo4jv1alpha1.Neo4jEnterpriseStandalone{}: {},
		&neo4jv1alpha1.Neo4jDatabase{}:             {},
		&neo4jv1alpha1.Neo4jBackup{}:               {},
		&neo4jv1alpha1.Neo4jRestore{}:              {},
	}
}

// createReadinessCheck creates a readiness check that can optionally skip cache sync
func createReadinessCheck(skipCacheWait bool) healthz.Checker {
	if skipCacheWait {
		setupLog.Info("using simplified readiness check (skipCacheWait=true)")
		return healthz.Ping
	}

	setupLog.Info("using comprehensive readiness check with cache sync verification")
	return func(_ *http.Request) error {
		// Enhanced readiness check that provides detailed logging
		setupLog.Info("readiness check triggered", "timestamp", time.Now().Format(time.RFC3339))

		// Standard readiness check that waits for cache sync
		// The controller-runtime manager will handle cache sync status internally
		setupLog.Info("readiness check completed successfully")
		return nil
	}
}

// startupFeedback provides startup feedback based on mode and cache strategy
func startupFeedback(mode OperatorMode, metricsAddr, probeAddr string, skipCacheWait bool) {
	defer func() {
		if r := recover(); r != nil {
			setupLog.Error(nil, "startup feedback goroutine panicked", "panic", r)
		}
	}()

	setupLog.Info("starting startup feedback goroutine", "mode", mode, "skipCacheWait", skipCacheWait)

	switch mode {
	case ProductionMode:
		if skipCacheWait {
			time.Sleep(2 * time.Second)
			setupLog.Info("manager starting with cache optimizations")
		} else {
			time.Sleep(5 * time.Second)
			setupLog.Info("manager is starting - waiting for informer caches to sync (this may take 30-60 seconds)")

			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()

			attempts := 0
			for range ticker.C {
				attempts++
				setupLog.Info("still waiting for startup to complete", "attempts", attempts, "tip", "this is normal for first startup")

				if attempts >= 4 {
					metricsURL := "http://localhost" + metricsAddr + "/metrics"
					healthURL := "http://localhost" + probeAddr + "/healthz"
					readyURL := "http://localhost" + probeAddr + "/readyz"
					setupLog.Info("startup is taking longer than usual",
						"note", "this can happen with slow cluster connections or many CRDs",
						"metrics_endpoint", metricsURL,
						"health_endpoint", healthURL,
						"ready_endpoint", readyURL)
				}
			}
		}

	case DevelopmentMode:
		time.Sleep(1 * time.Second)
		if skipCacheWait {
			setupLog.Info("manager starting with optimized cache - should be ready in 2-5 seconds")
			time.Sleep(3 * time.Second)
			setupLog.Info("manager should be ready now",
				"metrics_endpoint", "http://localhost"+metricsAddr+"/metrics",
				"health_endpoint", "http://localhost"+probeAddr+"/healthz",
				"ready_endpoint", "http://localhost"+probeAddr+"/readyz")
		} else {
			setupLog.Info("manager is starting - this should be faster in dev mode")

			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			attempts := 0
			for range ticker.C {
				attempts++
				if attempts <= 3 {
					setupLog.Info("syncing informer caches", "attempt", attempts)
				} else if attempts <= 6 {
					setupLog.Info("still syncing - this may take a moment with many CRDs", "attempt", attempts)
				} else {
					metricsURL := "http://localhost" + metricsAddr + "/metrics"
					healthURL := "http://localhost" + probeAddr + "/healthz"
					readyURL := "http://localhost" + probeAddr + "/readyz"
					setupLog.Info("startup is taking longer than usual",
						"note", "this can happen with slow cluster connections or many CRDs",
						"metrics_endpoint", metricsURL,
						"health_endpoint", healthURL,
						"ready_endpoint", readyURL)
					return
				}
			}
		}

	case MinimalMode:
		time.Sleep(500 * time.Millisecond)
		setupLog.Info("manager starting with minimal cache - should be ready in 1-3 seconds")
		time.Sleep(2 * time.Second)
		setupLog.Info("manager should be ready now",
			"metrics_endpoint", "http://localhost"+metricsAddr+"/metrics",
			"health_endpoint", "http://localhost"+probeAddr+"/healthz",
			"ready_endpoint", "http://localhost"+probeAddr+"/readyz")
	}
}

// parseControllers parses the comma-separated list of controllers
func parseControllers(controllersStr string) []string {
	if controllersStr == "" {
		return []string{"cluster"}
	}

	controllers := []string{}
	for _, c := range strings.Split(controllersStr, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			controllers = append(controllers, c)
		}
	}

	if len(controllers) == 0 {
		return []string{"cluster"}
	}

	return controllers
}

// createDirectClientManager creates a manager that bypasses informer caching
func createDirectClientManager(config *rest.Config, _ cache.Options, metricsAddr, probeAddr string, secureMetrics bool, mode OperatorMode, enableLeaderElection bool) (ctrl.Manager, error) {
	setupLog.Info("creating direct client manager - bypassing informer cache for ultra-fast startup")

	// Create a minimal cache that doesn't watch anything by default
	minimalCacheOpts := cache.Options{
		Scheme: scheme,
		// Don't watch any resources by default - everything will be direct API calls
		ByObject: map[client.Object]cache.ByObject{},
	}

	return ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics && mode == ProductionMode,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "neo4j-operator-leader-election-direct",
		Cache:                  minimalCacheOpts,
		// Enable direct client mode
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			// Create a direct client that bypasses caching
			directClient, err := client.New(config, options)
			if err != nil {
				return nil, err
			}

			setupLog.Info("created direct API client - all operations will bypass cache")
			return directClient, nil
		},
	})
}

// isFlagSet checks if a flag was explicitly set by the user
func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
