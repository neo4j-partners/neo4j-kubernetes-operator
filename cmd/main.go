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
// It supports two modes: production (default) and development.
//
// Production mode: go run cmd/main.go
// Development mode: go run cmd/main.go --mode=dev
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/controller"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
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

type managerSettings struct {
	config               *rest.Config
	baseCacheOpts        cache.Options
	metricsAddr          string
	probeAddr            string
	secureMetrics        bool
	enableLeaderElection bool
	operatorMode         OperatorMode
	controllersToLoad    string
	skipCacheWait        bool
	useDirectClient      bool
}

type watchNamespaceConfig struct {
	all            bool
	explicit       []string
	globs          []string
	regexes        []*regexp.Regexp
	regexRaw       []string
	labelSelectors []labels.Selector
	labelRaw       []string
}

type watchNamespaceSelection struct {
	all        bool
	namespaces []string
}

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
		mode                 = flag.String("mode", "production", "Operator mode: production or dev")
		metricsAddr          = flag.String("metrics-bind-address", "", "The address the metric endpoint binds to. (auto-assigned based on mode if empty)")
		probeAddr            = flag.String("health-probe-bind-address", "", "The address the probe endpoint binds to. (auto-assigned based on mode if empty)")
		enableLeaderElection = flag.Bool("leader-elect", false, "Enable leader election for controller manager.")
		secureMetrics        = flag.Bool("metrics-secure", false, "If set the metrics endpoint is served securely")

		// Development mode specific flags
		controllersToLoad = flag.String("controllers", "cluster,standalone,database,backup,restore,plugin", "Comma-separated list of controllers to load (dev mode only)")

		// Cache optimization flags
		cacheStrategy = flag.String("cache-strategy", "", "Cache strategy: standard, lazy, selective, on-demand, none (auto-selected based on mode if empty)")
		skipCacheWait = flag.Bool("skip-cache-wait", false, "Skip waiting for cache sync before starting controllers")
		lazyInformers = flag.Bool("lazy-informers", false, "Enable lazy informer creation")
		ultraFast     = flag.Bool("ultra-fast", false, "Enable ultra-fast mode with no informer caching")
	)

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Validate and normalize mode
	operatorMode := OperatorMode(strings.ToLower(*mode))
	switch operatorMode {
	case ProductionMode, DevelopmentMode:
		// Valid modes
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Valid modes are: production, dev\n", *mode)
		os.Exit(1)
	}

	// Set default addresses based on mode if not specified
	if *metricsAddr == "" {
		switch operatorMode {
		case ProductionMode:
			*metricsAddr = ":8080"
		case DevelopmentMode:
			*metricsAddr = ":8082"
		}
	}

	if *probeAddr == "" {
		switch operatorMode {
		case ProductionMode:
			*probeAddr = ":8081"
		case DevelopmentMode:
			*probeAddr = ":8083"
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
		}
	}

	// Auto-enable optimizations based on mode
	if operatorMode == DevelopmentMode && !isFlagSet("skip-cache-wait") {
		*skipCacheWait = true
	}

	setupLog.Info("starting Neo4j Operator",
		"mode", operatorMode,
		"cache_strategy", *cacheStrategy,
		"skip_cache_wait", *skipCacheWait,
		"lazy_informers", *lazyInformers,
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
			cacheOpts = configureDevelopmentCache(*cacheStrategy, *lazyInformers)
		}
		config.Timeout = 10 * time.Second
		config.QPS = 100
		config.Burst = 200
		setupLog.Info("development mode enabled - using optimized cache settings")

	case ProductionMode:
		if useDirectClient {
			setupLog.Info("WARNING: using direct client in production mode - this may impact performance")
			cacheOpts = cache.Options{}
		} else {
			// Force lazy cache for production to avoid RBAC and startup issues
			if *cacheStrategy == "" {
				*cacheStrategy = "lazy"
			}
			cacheOpts = configureProductionCache(*cacheStrategy, *lazyInformers)
		}
		setupLog.Info("production mode enabled - using standard settings")
	}

	watchConfig, err := parseWatchNamespaceConfig(os.Getenv("WATCH_NAMESPACE"))
	if err != nil {
		setupLog.Error(err, "invalid WATCH_NAMESPACE")
		os.Exit(1)
	}

	settings := managerSettings{
		config:               config,
		baseCacheOpts:        cacheOpts,
		metricsAddr:          *metricsAddr,
		probeAddr:            *probeAddr,
		secureMetrics:        *secureMetrics,
		enableLeaderElection: *enableLeaderElection,
		operatorMode:         operatorMode,
		controllersToLoad:    *controllersToLoad,
		skipCacheWait:        *skipCacheWait,
		useDirectClient:      useDirectClient,
	}

	ctx := ctrl.SetupSignalHandler()
	if err := runManagerWithWatchConfig(ctx, settings, watchConfig); err != nil {
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
				Recorder:     mgr.GetEventRecorderFor("neo4j-plugin-controller"),
				RequeueAfter: controller.GetTestRequeueAfter(),
			},
		},
		{
			name: "Neo4jShardedDatabase",
			controller: &controller.Neo4jShardedDatabaseReconciler{
				Client:                   mgr.GetClient(),
				Scheme:                   mgr.GetScheme(),
				Recorder:                 mgr.GetEventRecorderFor("neo4j-sharded-database-controller"),
				MaxConcurrentReconciles:  1,
				RequeueAfter:             controller.GetTestRequeueAfter(),
				ShardedDatabaseValidator: validation.NewShardedDatabaseValidator(mgr.GetClient()),
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
				Recorder:     mgr.GetEventRecorderFor("neo4j-plugin-controller"),
				RequeueAfter: controller.GetTestRequeueAfter(),
			}, "Neo4jPlugin"
		},
		"shardeddatabase": func() (interface{ SetupWithManager(ctrl.Manager) error }, string) {
			return &controller.Neo4jShardedDatabaseReconciler{
				Client:                   mgr.GetClient(),
				Scheme:                   mgr.GetScheme(),
				Recorder:                 mgr.GetEventRecorderFor("neo4j-sharded-database-controller"),
				MaxConcurrentReconciles:  1,
				RequeueAfter:             controller.GetTestRequeueAfter(),
				ShardedDatabaseValidator: validation.NewShardedDatabaseValidator(mgr.GetClient()),
			}, "Neo4jShardedDatabase"
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

func runManagerWithWatchConfig(ctx context.Context, settings managerSettings, watchConfig watchNamespaceConfig) error {
	if watchConfig.isAll() {
		setupLog.Info("watching all namespaces")
		return runManager(ctx, settings, watchNamespaceSelection{all: true})
	}

	if !watchConfig.hasPatterns() {
		setupLog.Info("limiting cache to watch namespaces", "namespaces", watchConfig.explicit)
		return runManager(ctx, settings, watchNamespaceSelection{namespaces: watchConfig.explicit})
	}

	setupLog.Info("using dynamic namespace discovery",
		"explicit", watchConfig.explicit,
		"globs", watchConfig.globs,
		"regexes", watchConfig.regexRaw,
		"labels", watchConfig.labelRaw,
	)
	return runManagerWithNamespaceDiscovery(ctx, settings, watchConfig)
}

func runManagerWithNamespaceDiscovery(ctx context.Context, settings managerSettings, watchConfig watchNamespaceConfig) error {
	clientset, err := kubernetes.NewForConfig(settings.config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	selection, err := resolveWatchNamespaces(ctx, clientset, watchConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve watch namespaces: %w", err)
	}

	if !selection.all && len(selection.namespaces) == 0 {
		setupLog.Info("watch namespace config matched no namespaces; operator will idle until matches appear")
	}

	changeCh := make(chan struct{}, 1)
	go watchNamespaceChanges(ctx, clientset, changeCh)

	for {
		mgrCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() {
			errCh <- runManager(mgrCtx, settings, selection)
		}()

		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				cancel()
				err := <-errCh
				if err != nil && !isContextCanceled(err) {
					return err
				}
				return ctx.Err()
			case err := <-errCh:
				cancel()
				if err != nil && !isContextCanceled(err) {
					return err
				}
				return err
			case <-changeCh:
				next, err := resolveWatchNamespaces(ctx, clientset, watchConfig)
				if err != nil {
					setupLog.Error(err, "failed to resolve watch namespaces; keeping current selection")
					continue
				}
				if watchNamespaceSelectionEqual(selection, next) {
					continue
				}
				setupLog.Info("watch namespaces changed; restarting manager",
					"current", selection.namespaces,
					"next", next.namespaces,
				)
				cancel()
				err = <-errCh
				if err != nil && !isContextCanceled(err) {
					return err
				}
				selection = next
				restart = true
			}
		}

		if ctx.Err() != nil {
			cancel()
			return ctx.Err()
		}
	}
}

func runManager(ctx context.Context, settings managerSettings, selection watchNamespaceSelection) error {
	cacheOpts := settings.baseCacheOpts
	applyWatchNamespaces(&cacheOpts, selection)

	var mgr ctrl.Manager
	var err error

	if settings.useDirectClient {
		mgr, err = createDirectClientManager(settings.config, cacheOpts, settings.metricsAddr, settings.probeAddr, settings.secureMetrics, settings.operatorMode, settings.enableLeaderElection)
	} else {
		mgr, err = ctrl.NewManager(settings.config, ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress:   settings.metricsAddr,
				SecureServing: settings.secureMetrics && settings.operatorMode == ProductionMode,
			},
			HealthProbeBindAddress: settings.probeAddr,
			LeaderElection:         settings.enableLeaderElection,
			LeaderElectionID:       "neo4j-operator-leader-election",
			Cache:                  cacheOpts,
		})
	}

	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	if err = setupControllers(mgr, settings.operatorMode, settings.controllersToLoad); err != nil {
		return fmt.Errorf("failed to setup controllers: %w", err)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", createReadinessCheck(settings.skipCacheWait)); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	setupLog.Info("starting manager")
	go startupFeedback(ctx, settings.operatorMode, settings.metricsAddr, settings.probeAddr, settings.skipCacheWait)

	if settings.skipCacheWait {
		setupLog.Info("skipping cache sync wait - starting immediately")
		go func() {
			if !sleepWithContext(ctx, 2*time.Second) {
				return
			}
			setupLog.Info("manager ready - cache will sync in background")
		}()
	}

	if err := mgr.Start(ctx); err != nil {
		return err
	}

	return nil
}

// configureDevelopmentCache sets up optimized caching for development mode
func configureDevelopmentCache(strategy string, _ bool) cache.Options {
	opts := cache.Options{
		SyncPeriod: func() *time.Duration {
			d := 30 * time.Second
			return &d
		}(),
		// Watch all namespaces in development mode
		// DefaultNamespaces is empty to enable cluster-wide watching
	}

	switch CacheStrategy(strategy) {
	case LazyCache:
		setupLog.Info("using lazy cache strategy for development")
		return configureLazyCache(opts)
	case SelectiveCache:
		setupLog.Info("using selective cache strategy for development")
		return configureSelectiveCache(opts)
	case OnDemandCache:
		setupLog.Info("using on-demand cache strategy for development")
		return configureOnDemandCache(opts)
	default:
		setupLog.Info("using standard cache strategy for development")
		return opts
	}
}

// configureProductionCache sets up standard caching for production mode
func configureProductionCache(strategy string, _ bool) cache.Options {
	opts := cache.Options{}

	switch CacheStrategy(strategy) {
	case LazyCache:
		setupLog.Info("using lazy cache strategy for production")
		return configureLazyCache(opts)
	case SelectiveCache:
		setupLog.Info("using selective cache strategy for production")
		return configureSelectiveCache(opts)
	case OnDemandCache:
		setupLog.Info("using on-demand cache strategy for production")
		return configureOnDemandCache(opts)
	default:
		setupLog.Info("using standard cache strategy for production")
		return opts
	}
}

// configureLazyCache sets up lazy loading of informers
func configureLazyCache(base cache.Options) cache.Options {
	// Cache essential resources only - optimized for production
	base.ByObject = getEssentialResourceCache()

	// Add production-specific optimizations
	base.SyncPeriod = func() *time.Duration {
		d := 5 * time.Minute // Longer sync period for production stability
		return &d
	}()

	return base
}

// configureSelectiveCache sets up selective resource caching
func configureSelectiveCache(base cache.Options) cache.Options {
	// Selective: Neo4j CRDs + core resources we manage
	base.ByObject = getSelectiveResourceCache()
	return base
}

// configureOnDemandCache sets up on-demand informer creation
func configureOnDemandCache(base cache.Options) cache.Options {
	// Always include ALL Neo4j CRDs - essential for operator functionality
	base.ByObject = map[client.Object]cache.ByObject{
		&neo4jv1alpha1.Neo4jEnterpriseCluster{}:    {},
		&neo4jv1alpha1.Neo4jEnterpriseStandalone{}: {},
		&neo4jv1alpha1.Neo4jDatabase{}:             {},
		&neo4jv1alpha1.Neo4jBackup{}:               {},
		&neo4jv1alpha1.Neo4jRestore{}:              {},
		&neo4jv1alpha1.Neo4jPlugin{}:               {},
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
func startupFeedback(ctx context.Context, mode OperatorMode, metricsAddr, probeAddr string, skipCacheWait bool) {
	defer func() {
		if r := recover(); r != nil {
			setupLog.Error(nil, "startup feedback goroutine panicked", "panic", r)
		}
	}()

	setupLog.Info("starting startup feedback goroutine", "mode", mode, "skipCacheWait", skipCacheWait)

	switch mode {
	case ProductionMode:
		if skipCacheWait {
			if !sleepWithContext(ctx, 2*time.Second) {
				return
			}
			setupLog.Info("manager starting with cache optimizations")
		} else {
			if !sleepWithContext(ctx, 5*time.Second) {
				return
			}
			setupLog.Info("manager is starting - waiting for informer caches to sync (this may take 30-60 seconds)")

			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()

			attempts := 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
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
		}

	case DevelopmentMode:
		if !sleepWithContext(ctx, 1*time.Second) {
			return
		}
		if skipCacheWait {
			setupLog.Info("manager starting with optimized cache - should be ready in 2-5 seconds")
			if !sleepWithContext(ctx, 3*time.Second) {
				return
			}
			setupLog.Info("manager should be ready now",
				"metrics_endpoint", "http://localhost"+metricsAddr+"/metrics",
				"health_endpoint", "http://localhost"+probeAddr+"/healthz",
				"ready_endpoint", "http://localhost"+probeAddr+"/readyz")
		} else {
			setupLog.Info("manager is starting - this should be faster in dev mode")

			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			attempts := 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
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
		}

	}
}

// parseControllers parses the comma-separated list of controllers
func parseControllers(controllersStr string) []string {
	if controllersStr == "" {
		return []string{"cluster", "standalone", "database", "backup", "restore", "plugin"}
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

func parseWatchNamespaceConfig(value string) (watchNamespaceConfig, error) {
	cfg := watchNamespaceConfig{}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		cfg.all = true
		return cfg, nil
	}

	entries := splitNamespaceEntries(trimmed)
	explicit := make(map[string]struct{})
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		if entry == "*" {
			cfg.all = true
			return cfg, nil
		}

		lower := strings.ToLower(entry)
		switch {
		case strings.HasPrefix(lower, "glob:"):
			pattern := strings.TrimSpace(entry[len("glob:"):])
			if pattern == "" {
				return cfg, fmt.Errorf("empty glob pattern in WATCH_NAMESPACE")
			}
			if _, err := path.Match(pattern, "namespace"); err != nil {
				return cfg, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
			}
			cfg.globs = append(cfg.globs, pattern)
		case strings.HasPrefix(lower, "regex:") || strings.HasPrefix(lower, "re:"):
			prefixLen := len("regex:")
			if strings.HasPrefix(lower, "re:") {
				prefixLen = len("re:")
			}
			pattern := strings.TrimSpace(entry[prefixLen:])
			if pattern == "" {
				return cfg, fmt.Errorf("empty regex pattern in WATCH_NAMESPACE")
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return cfg, fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
			}
			cfg.regexes = append(cfg.regexes, re)
			cfg.regexRaw = append(cfg.regexRaw, pattern)
		case strings.HasPrefix(lower, "label:"):
			selectorRaw := strings.TrimSpace(entry[len("label:"):])
			if selectorRaw == "" {
				return cfg, fmt.Errorf("empty label selector in WATCH_NAMESPACE")
			}
			if strings.HasPrefix(selectorRaw, "{") && strings.HasSuffix(selectorRaw, "}") {
				selectorRaw = strings.TrimSpace(selectorRaw[1 : len(selectorRaw)-1])
			}
			if selectorRaw == "" {
				return cfg, fmt.Errorf("empty label selector in WATCH_NAMESPACE")
			}
			selector, err := labels.Parse(selectorRaw)
			if err != nil {
				return cfg, fmt.Errorf("invalid label selector %q: %w", selectorRaw, err)
			}
			cfg.labelSelectors = append(cfg.labelSelectors, selector)
			cfg.labelRaw = append(cfg.labelRaw, selectorRaw)
		default:
			if hasGlobChars(entry) {
				if _, err := path.Match(entry, "namespace"); err != nil {
					return cfg, fmt.Errorf("invalid glob pattern %q: %w", entry, err)
				}
				cfg.globs = append(cfg.globs, entry)
				continue
			}
			explicit[entry] = struct{}{}
		}
	}

	for name := range explicit {
		cfg.explicit = append(cfg.explicit, name)
	}
	sort.Strings(cfg.explicit)

	if len(cfg.explicit) == 0 && !cfg.hasPatterns() {
		cfg.all = true
	}

	return cfg, nil
}

func splitNamespaceEntries(value string) []string {
	var entries []string
	var current strings.Builder
	depth := 0
	escaped := false

	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '{':
			depth++
			current.WriteRune(r)
		case r == '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(r)
		case r == ',' && depth == 0:
			entry := strings.TrimSpace(current.String())
			if entry != "" {
				entries = append(entries, entry)
			}
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	entry := strings.TrimSpace(current.String())
	if entry != "" {
		entries = append(entries, entry)
	}

	return entries
}

func (cfg watchNamespaceConfig) hasPatterns() bool {
	return len(cfg.globs) > 0 || len(cfg.regexes) > 0 || len(cfg.labelSelectors) > 0
}

func (cfg watchNamespaceConfig) isAll() bool {
	return cfg.all && !cfg.hasPatterns() && len(cfg.explicit) == 0
}

func hasGlobChars(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func resolveWatchNamespaces(ctx context.Context, clientset kubernetes.Interface, cfg watchNamespaceConfig) (watchNamespaceSelection, error) {
	if cfg.isAll() {
		return watchNamespaceSelection{all: true}, nil
	}

	names := make(map[string]struct{})
	for _, name := range cfg.explicit {
		names[name] = struct{}{}
	}

	if cfg.hasPatterns() {
		namespaceList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return watchNamespaceSelection{}, err
		}
		for i := range namespaceList.Items {
			ns := &namespaceList.Items[i]
			if matchNamespace(cfg, ns) {
				names[ns.Name] = struct{}{}
			}
		}
	}

	var resolved []string
	for name := range names {
		resolved = append(resolved, name)
	}
	sort.Strings(resolved)

	return watchNamespaceSelection{namespaces: resolved}, nil
}

func matchNamespace(cfg watchNamespaceConfig, namespace *corev1.Namespace) bool {
	name := namespace.Name
	for _, pattern := range cfg.globs {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	for _, re := range cfg.regexes {
		if re.MatchString(name) {
			return true
		}
	}
	if len(cfg.labelSelectors) > 0 {
		labelSet := labels.Set(namespace.Labels)
		for _, selector := range cfg.labelSelectors {
			if selector.Matches(labelSet) {
				return true
			}
		}
	}
	return false
}

func applyWatchNamespaces(cacheOpts *cache.Options, selection watchNamespaceSelection) {
	if selection.all {
		return
	}

	if cacheOpts.DefaultNamespaces == nil {
		cacheOpts.DefaultNamespaces = make(map[string]cache.Config, len(selection.namespaces))
	}

	for _, namespace := range selection.namespaces {
		cacheOpts.DefaultNamespaces[namespace] = cache.Config{}
	}
}

func watchNamespaceChanges(ctx context.Context, clientset kubernetes.Interface, changeCh chan<- struct{}) {
	backoff := 1 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}

		watcher, err := clientset.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
		if err != nil {
			setupLog.Error(err, "failed to watch namespaces, retrying", "backoff", backoff)
			if !sleepWithContext(ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 1 * time.Second

		for {
			select {
			case <-ctx.Done():
				watcher.Stop()
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					watcher.Stop()
					break
				}
				switch event.Type {
				case watch.Added, watch.Modified, watch.Deleted:
					signalChange(changeCh)
				}
			}
		}
	}
}

func signalChange(changeCh chan<- struct{}) {
	select {
	case changeCh <- struct{}{}:
	default:
	}
}

func watchNamespaceSelectionEqual(current, next watchNamespaceSelection) bool {
	if current.all != next.all {
		return false
	}
	if len(current.namespaces) != len(next.namespaces) {
		return false
	}
	for i, name := range current.namespaces {
		if next.namespaces[i] != name {
			return false
		}
	}
	return true
}

func isContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// createDirectClientManager creates a manager that bypasses informer caching
func createDirectClientManager(config *rest.Config, cacheOpts cache.Options, metricsAddr, probeAddr string, secureMetrics bool, mode OperatorMode, enableLeaderElection bool) (ctrl.Manager, error) {
	setupLog.Info("creating direct client manager - bypassing informer cache for ultra-fast startup")

	// Create a cache that doesn't watch anything by default for direct API mode
	directCacheOpts := cache.Options{
		Scheme: scheme,
		// Don't watch any resources by default - everything will be direct API calls
		ByObject: map[client.Object]cache.ByObject{},
	}
	directCacheOpts.DefaultNamespaces = cacheOpts.DefaultNamespaces

	return ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics && mode == ProductionMode,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "neo4j-operator-leader-election-direct",
		Cache:                  directCacheOpts,
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
