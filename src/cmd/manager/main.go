/*
Copyright Neo4j.

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

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	neo4jctrl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/controller/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/imagepolicy"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/logging"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/validation"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(neo4jv1beta1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableWebhooks bool
	var webhookCertDir string
	var allowedImageRepos string
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false, "If set, the metrics endpoint is served securely.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false, "Register the Neo4j validating admission webhook (requires TLS certs).")
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory with tls.crt and tls.key for the webhook server.")
	flag.StringVar(&allowedImageRepos, "allowed-image-repositories", "",
		"Comma-separated image repository prefixes allowed in Neo4j CRs (NEO-012). Empty uses defaults (neo4j, docker.io/neo4j). Use * to allow any (lab only).")
	// Production JSON by default (ADR-014); --zap-devel for console. Optional --log-file tees verbose logs.
	logOpts := logging.Options{}
	logOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	if allowedImageRepos == "" {
		allowedImageRepos = os.Getenv("ALLOWED_IMAGE_REPOSITORIES")
	}
	imagepolicy.SetAllowedRepositories(allowedImageRepos)

	rootLog, closeLog, err := logging.New(logOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid logging config: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()
	ctrl.SetLogger(rootLog)

	namespaces, err := watchNamespaces()
	if err != nil {
		setupLog.Error(err, "invalid watch scope")
		os.Exit(1)
	}
	defaultNamespaces := make(map[string]cache.Config, len(namespaces))
	for _, ns := range namespaces {
		defaultNamespaces[ns] = cache.Config{}
	}
	setupLog.Info("watching namespaces", "namespaces", namespaces)
	if allow := imagepolicy.AllowedRepositories(); allow == nil {
		setupLog.Info("image repository allowlist", "allowed", "*")
	} else {
		setupLog.Info("image repository allowlist", "allowed", allow)
	}

	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts: []func(*tls.Config){
				func(cfg *tls.Config) { cfg.MinVersion = tls.VersionTLS12 },
			},
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "neo4j.com.neo4j-operator",
		Cache:                  cache.Options{DefaultNamespaces: defaultNamespaces},
	}
	if enableWebhooks {
		mgrOpts.WebhookServer = webhook.NewServer(webhook.Options{
			Port:    9443,
			CertDir: webhookCertDir,
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := neo4jctrl.NewReconciler(mgr).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Neo4j")
		os.Exit(1)
	}

	if enableWebhooks {
		if err := ctrl.NewWebhookManagedBy(mgr).
			For(&neo4jv1beta1.Neo4j{}).
			WithValidator(&validation.Neo4jValidator{Client: mgr.GetClient()}).
			Complete(); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Neo4j")
			os.Exit(1)
		}
		setupLog.Info("neo4j validating webhook registered", "certDir", webhookCertDir)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
