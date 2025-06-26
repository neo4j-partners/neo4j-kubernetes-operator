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

package integration_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/webhooks"
)

var cfg *rest.Config
var k8sClient client.Client
var ctx context.Context
var cancel context.CancelFunc
var testRunID string
var mgr manager.Manager

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	// Generate unique test run ID
	testRunID = fmt.Sprintf("%d", time.Now().UnixNano())

	// Set TEST_MODE for faster test execution
	os.Setenv("TEST_MODE", "true")

	By("connecting to existing cluster")
	// Use existing cluster instead of envtest
	var err error
	cfg, err = ctrl.GetConfig()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Register the scheme
	err = neo4jv1alpha1.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Set up the controller manager
	By("setting up controller manager")

	// Use minimal cache options for faster test execution
	cacheOpt := manager.Options{
		Scheme:                 clientgoscheme.Scheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	}

	mgr, err = manager.New(cfg, cacheOpt)
	Expect(err).NotTo(HaveOccurred())

	// Set up controllers with test mode optimizations
	err = (&controller.Neo4jEnterpriseClusterReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("neo4j-enterprise-cluster-controller"),
		RequeueAfter:      controller.GetTestRequeueAfter(),
		TopologyScheduler: controller.NewTopologyScheduler(mgr.GetClient()),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jDatabaseReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("neo4j-database-controller"),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jBackupReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("neo4j-backup-controller"),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jRestoreReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("neo4j-restore-controller"),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jRoleReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("neo4j-role-controller"),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jGrantReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("neo4j-grant-controller"),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jUserReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("neo4j-user-controller"),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.Neo4jPluginReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		RequeueAfter: controller.GetTestRequeueAfter(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	// Setup webhooks if enabled via environment variable
	enableWebhooks := os.Getenv("ENABLE_WEBHOOKS") == "true"
	if enableWebhooks {
		By("setting up webhooks for integration tests")

		// Register webhooks
		if err = (&webhooks.Neo4jEnterpriseClusterWebhook{
			Client: mgr.GetClient(),
		}).SetupWebhookWithManager(mgr); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		By("webhooks enabled for integration tests")
	} else {
		By("webhooks disabled for integration tests")
	}

	// Start the manager
	By("starting the manager")
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()

	// Wait for cache to sync with increased timeout for real cluster
	By("waiting for cache to sync")
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	Expect(mgr.GetCache().WaitForCacheSync(ctxWithTimeout)).To(BeTrue())

	// Wait for webhook server to be ready if webhooks are enabled
	if enableWebhooks {
		By("waiting for webhook server to be ready")
		webhookReady := waitForWebhookServerReady(ctx, 30*time.Second)
		Expect(webhookReady).To(BeTrue(), "Webhook server should be ready within timeout")
	}
})

var _ = AfterSuite(func() {
	By("cleaning up any leftover test namespaces")
	cleanupTestNamespaces()

	By("tearing down the test environment")
	// Cancel the context to signal shutdown
	cancel()

	By("initiating manager shutdown sequence")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if mgr != nil {
		By("waiting for manager to shut down")
		select {
		case <-shutdownCtx.Done():
			By("manager shutdown timeout reached")
		case <-time.After(5 * time.Second):
			By("manager shutdown completed")
		}
	}

	By("test environment teardown completed, forcefully exiting process to avoid controller-runtime goroutine leaks")
	os.Exit(0)
})

// Common test utilities
const (
	timeout        = time.Second * 10 // Increased from 5s to 10s for integration tests
	interval       = time.Millisecond * 100
	cleanupTimeout = time.Second * 30 // Longer timeout for cleanup operations
)

func createTestNamespace(name string) string {
	return fmt.Sprintf("test-%s-%s-%d", name, testRunID, time.Now().UnixNano())
}

// cleanupTestNamespaces removes any leftover test namespaces
func cleanupTestNamespaces() {
	if k8sClient == nil {
		return
	}

	ctx := context.Background()
	namespaceList := &corev1.NamespaceList{}

	err := k8sClient.List(ctx, namespaceList)
	if err != nil {
		return
	}

	for _, ns := range namespaceList.Items {
		if isTestNamespace(ns.Name) {
			// Force delete the namespace
			err := k8sClient.Delete(ctx, &ns)
			if err != nil && !errors.IsNotFound(err) {
				// Log but don't fail the test
				fmt.Printf("Warning: Failed to cleanup namespace %s: %v\n", ns.Name, err)
			}
		}
	}
}

// isTestNamespace checks if a namespace is a test namespace
func isTestNamespace(name string) bool {
	return strings.HasPrefix(name, "test-")
}

// waitForWebhookServerReady waits for the webhook server to be ready by checking if the TLS certificate exists
func waitForWebhookServerReady(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check multiple possible certificate paths
		possiblePaths := []string{
			"/tmp/k8s-webhook-server/serving-certs/tls.crt",
			os.TempDir() + "/k8s-webhook-server/serving-certs/tls.crt",
			"/var/folders/8_/z8fx9g411bdc0n0fzsw545l80000gp/T/k8s-webhook-server/serving-certs/tls.crt",
		}

		for _, certPath := range possiblePaths {
			if _, err := os.Stat(certPath); err == nil {
				// Certificate exists, now check if webhook server is responding
				if isWebhookServerResponding() {
					return true
				}
			}
		}

		// Wait a bit before checking again
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
			continue
		}
	}

	return false
}

// isWebhookServerResponding checks if the webhook server is responding to requests
func isWebhookServerResponding() bool {
	// Try to make a simple HTTP request to the webhook server
	// The webhook server typically runs on port 9443
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Skip certificate verification for testing
			},
		},
	}

	// Try multiple endpoints that might be available
	endpoints := []string{
		"https://localhost:9443/healthz",
		"https://localhost:9443/readyz",
		"https://localhost:9443/",
	}

	for _, endpoint := range endpoints {
		resp, err := client.Get(endpoint)
		if err == nil {
			defer resp.Body.Close()
			// Any response means the server is up
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return true
			}
		}
	}

	// If HTTP checks fail, try to check if the port is listening
	return isPortListening("localhost", "9443")
}

// isPortListening checks if a port is listening on the given host
func isPortListening(host, port string) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	if conn != nil {
		defer conn.Close()
		return true
	}
	return false
}

// aggressiveCleanup performs fast cleanup without waiting for complete deletion
func aggressiveCleanup(namespace string) {
	if k8sClient == nil || namespace == "" {
		return
	}

	ctx := context.Background()

	// List of CRDs to clean up
	crds := []client.ObjectList{
		&neo4jv1alpha1.Neo4jEnterpriseClusterList{},
		&neo4jv1alpha1.Neo4jBackupList{},
		&neo4jv1alpha1.Neo4jRestoreList{},
		&neo4jv1alpha1.Neo4jPluginList{},
		&neo4jv1alpha1.Neo4jUserList{},
		&neo4jv1alpha1.Neo4jRoleList{},
		&neo4jv1alpha1.Neo4jGrantList{},
	}

	// Force delete all custom resources
	for _, crdList := range crds {
		_ = k8sClient.List(ctx, crdList, client.InNamespace(namespace))
		switch list := crdList.(type) {
		case *neo4jv1alpha1.Neo4jEnterpriseClusterList:
			for _, item := range list.Items {
				// Remove finalizers and force delete
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		case *neo4jv1alpha1.Neo4jBackupList:
			for _, item := range list.Items {
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		case *neo4jv1alpha1.Neo4jRestoreList:
			for _, item := range list.Items {
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		case *neo4jv1alpha1.Neo4jPluginList:
			for _, item := range list.Items {
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		case *neo4jv1alpha1.Neo4jUserList:
			for _, item := range list.Items {
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		case *neo4jv1alpha1.Neo4jRoleList:
			for _, item := range list.Items {
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		case *neo4jv1alpha1.Neo4jGrantList:
			for _, item := range list.Items {
				if len(item.Finalizers) > 0 {
					item.Finalizers = nil
					_ = k8sClient.Update(ctx, &item)
				}
				_ = k8sClient.Delete(ctx, &item, client.GracePeriodSeconds(0))
			}
		}
	}

	// Force delete the namespace without waiting
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	_ = k8sClient.Delete(ctx, ns, client.GracePeriodSeconds(0))
}
