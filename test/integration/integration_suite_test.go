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
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var cfg *rest.Config
var k8sClient client.Client
var ctx context.Context
var cancel context.CancelFunc
var testRunID string
var setupMutex sync.Mutex
var isSetup bool
var namespaceCounter int64
var namespaceMutex sync.Mutex

// Test configuration variables
var timeout = 5 * time.Minute
var interval = 5 * time.Second

// Cluster formation timeout for complex multi-node tests
var clusterTimeout = 5 * time.Minute

// Initialize timeout based on environment
func init() {
	// Increase timeout in CI environments where resources are more constrained
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		timeout = 10 * time.Minute
		clusterTimeout = 20 * time.Minute // Extra long for cluster formation
		GinkgoWriter.Printf("Running in CI environment - using extended timeout of %v (cluster formation: %v)\n", timeout, clusterTimeout)
	}
}

// applyCIOptimizations applies CI-specific optimizations to cluster specs
func applyCIOptimizations(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) {
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		// Reduce cluster size in CI for faster formation
		if cluster.Spec.Topology.Servers > 2 {
			GinkgoWriter.Printf("CI optimization: reducing cluster size from %d to 2 servers\n", cluster.Spec.Topology.Servers)
			cluster.Spec.Topology.Servers = 2
		}

		// Add resource constraints for CI
		if cluster.Spec.Resources == nil {
			cluster.Spec.Resources = &corev1.ResourceRequirements{}
		}
		if cluster.Spec.Resources.Requests == nil {
			cluster.Spec.Resources.Requests = corev1.ResourceList{}
		}
		if cluster.Spec.Resources.Limits == nil {
			cluster.Spec.Resources.Limits = corev1.ResourceList{}
		}

		// Set minimal but sufficient resources for Neo4j Enterprise
		cluster.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse("100m")
		cluster.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse("1.5Gi")
		cluster.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse("500m")
		cluster.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse("1.5Gi") // Neo4j Enterprise REQUIRES ≥ 1.5Gi

		GinkgoWriter.Printf("CI optimization: applied resource constraints (100m-500m CPU, 1.5Gi memory)\n")
	}
}

// List of required CRDs for integration tests
var requiredCRDs = []string{
	"neo4jenterpriseclusters.neo4j.neo4j.com",
	"neo4jenterprisestandalones.neo4j.neo4j.com",
	"neo4jbackups.neo4j.neo4j.com",
	"neo4jrestores.neo4j.neo4j.com",
	"neo4jplugins.neo4j.neo4j.com",
	"neo4jdatabases.neo4j.neo4j.com",
}

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var _ = BeforeSuite(func() {
	setupMutex.Lock()
	defer setupMutex.Unlock()

	if isSetup {
		return
	}

	fmt.Println("=== INTEGRATION TEST BEFORESUITE STARTING ===")

	// Set a timeout for BeforeSuite to prevent hanging
	SetDefaultEventuallyTimeout(30 * time.Second)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	// Generate unique test run ID
	testRunID = fmt.Sprintf("%d", time.Now().UnixNano())
	By(fmt.Sprintf("Generated test run ID: %s", testRunID))

	// Set TEST_MODE for faster test execution
	os.Setenv("TEST_MODE", "true")

	By("connecting to existing cluster")
	// Use existing cluster instead of envtest
	var err error
	cfg, err = ctrl.GetConfig()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	By("Successfully connected to cluster")

	By("registering schemes")
	// Register the scheme
	err = neo4jv1alpha1.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = apiextensionsv1.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	By("Successfully created k8s client")

	// Install CRDs if missing
	By("Installing CRDs if missing")
	installCRDsIfMissing()

	// Wait for operator to be ready
	By("Waiting for operator to be ready")
	waitForOperatorReady()

	isSetup = true
	By("Integration test setup completed successfully")

	// Monitor initial resource state
	monitorResourceUsage("BEFORESUITE_END")
})

var _ = AfterSuite(func() {
	By("Cleaning up test environment")

	// Monitor resource usage before cleanup
	monitorResourceUsage("AFTERSUITE_START")

	// Cancel context
	if cancel != nil {
		cancel()
	}

	// Clean up test namespaces
	cleanupTestNamespaces()

	// Monitor final resource state
	monitorResourceUsage("AFTERSUITE_END")

	By("Test environment cleanup completed")
})

// createTestNamespace creates a unique namespace for each test
func createTestNamespace(name string) string {
	namespaceMutex.Lock()
	defer namespaceMutex.Unlock()

	// Use atomic counter for guaranteed uniqueness
	counter := atomic.AddInt64(&namespaceCounter, 1)

	// Use a more unique identifier with timestamp and counter
	timestamp := time.Now().UnixNano()
	randSuffix := fmt.Sprintf("%04x", timestamp%0x10000)
	uniqueName := fmt.Sprintf("%s-%s-%d-%s", name, testRunID[len(testRunID)-8:], counter, randSuffix)

	// Ensure the name is within the 63 character limit
	if len(uniqueName) > 63 {
		uniqueName = uniqueName[:63]
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: uniqueName,
			Labels: map[string]string{
				"test-run": testRunID,
			},
		},
	}

	err := k8sClient.Create(ctx, namespace)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	return uniqueName
}

// waitForOperatorReady waits for the operator deployment to be ready
func waitForOperatorReady() {
	By("Checking if operator deployment exists")

	// Check if the operator is deployed
	deployment := &appsv1.Deployment{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      "neo4j-operator-controller-manager",
		Namespace: "neo4j-operator-system",
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			By("Operator not deployed, skipping wait (assuming running locally)")
			return
		}
		Expect(err).NotTo(HaveOccurred(), "Failed to check operator deployment")
	}

	By("Waiting for operator deployment to be ready")
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      "neo4j-operator-controller-manager",
			Namespace: "neo4j-operator-system",
		}, deployment)
		if err != nil {
			return false
		}

		// Check if deployment is ready
		return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas &&
			deployment.Status.ReadyReplicas > 0
	}, 30*time.Second, 2*time.Second).Should(BeTrue(), "Operator deployment should be ready")

	// Quick verification that pods exist
	By("Verifying operator pod exists")
	podList := &corev1.PodList{}
	err = k8sClient.List(ctx, podList,
		client.InNamespace("neo4j-operator-system"),
		client.MatchingLabels{"control-plane": "controller-manager"})
	if err == nil && len(podList.Items) > 0 {
		By(fmt.Sprintf("Found %d operator pod(s)", len(podList.Items)))
	}

	// Short wait for initialization
	time.Sleep(2 * time.Second)

	By("Operator is ready")
}

// cleanupTestNamespaces removes all test namespaces
func cleanupTestNamespaces() {
	By("Cleaning up test namespaces")

	// List all namespaces with test-run label
	namespaceList := &corev1.NamespaceList{}
	err := k8sClient.List(ctx, namespaceList, client.MatchingLabels{"test-run": testRunID})
	if err != nil {
		By(fmt.Sprintf("Error listing test namespaces: %v", err))
		return
	}

	for _, namespace := range namespaceList.Items {
		By(fmt.Sprintf("Cleaning up namespace: %s", namespace.Name))

		// Clean up custom resources in the namespace first
		cleanupCustomResourcesInNamespace(namespace.Name)

		// Delete the namespace
		By(fmt.Sprintf("Deleting test namespace: %s", namespace.Name))
		err := k8sClient.Delete(ctx, &namespace)
		if err != nil && !errors.IsNotFound(err) {
			By(fmt.Sprintf("Error deleting namespace %s: %v", namespace.Name, err))
		}
	}
}

// cleanupCustomResourcesInNamespace removes all custom resources from a namespace
func cleanupCustomResourcesInNamespace(namespace string) {
	// Clean up Neo4j Backups
	backupList := &neo4jv1alpha1.Neo4jBackupList{}
	if err := k8sClient.List(ctx, backupList, client.InNamespace(namespace)); err == nil {
		for _, item := range backupList.Items {
			cleanupResource(&item, namespace, "Neo4jBackup")
		}
	}

	// Clean up Neo4j Databases
	dbList := &neo4jv1alpha1.Neo4jDatabaseList{}
	if err := k8sClient.List(ctx, dbList, client.InNamespace(namespace)); err == nil {
		for _, item := range dbList.Items {
			cleanupResource(&item, namespace, "Neo4jDatabase")
		}
	}

	// Clean up Neo4j Enterprise Clusters
	clusterList := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
	if err := k8sClient.List(ctx, clusterList, client.InNamespace(namespace)); err == nil {
		for _, item := range clusterList.Items {
			cleanupResource(&item, namespace, "Neo4jEnterpriseCluster")
		}
	}

	// Clean up Neo4j Enterprise Standalones
	standaloneList := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
	if err := k8sClient.List(ctx, standaloneList, client.InNamespace(namespace)); err == nil {
		for _, item := range standaloneList.Items {
			cleanupResource(&item, namespace, "Neo4jEnterpriseStandalone")
		}
	}

	// Clean up Neo4j Restores
	restoreList := &neo4jv1alpha1.Neo4jRestoreList{}
	if err := k8sClient.List(ctx, restoreList, client.InNamespace(namespace)); err == nil {
		for _, item := range restoreList.Items {
			cleanupResource(&item, namespace, "Neo4jRestore")
		}
	}

	// Clean up Neo4j Plugins
	pluginList := &neo4jv1alpha1.Neo4jPluginList{}
	if err := k8sClient.List(ctx, pluginList, client.InNamespace(namespace)); err == nil {
		for _, item := range pluginList.Items {
			cleanupResource(&item, namespace, "Neo4jPlugin")
		}
	}
}

// cleanupResource removes finalizers and deletes a resource
func cleanupResource(obj client.Object, namespace, resourceType string) {
	// Remove finalizers if present
	if len(obj.GetFinalizers()) > 0 {
		By(fmt.Sprintf("Removing finalizers from %s %s/%s", resourceType, namespace, obj.GetName()))
		obj.SetFinalizers([]string{})
		_ = k8sClient.Update(ctx, obj)
	}

	// Delete the resource
	By(fmt.Sprintf("Deleting %s %s/%s", resourceType, namespace, obj.GetName()))
	_ = k8sClient.Delete(ctx, obj)
}

// Helper functions for test utilities

// isCRDAvailable checks if a specific CRD is available and established in the cluster
func isCRDAvailable(crdName string) bool {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)
	if err != nil {
		return false
	}

	// Check if the CRD is established
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			return true
		}
	}

	return false
}

// monitorResourceUsage logs current cluster resource usage for debugging
func monitorResourceUsage(context string) {
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		By(fmt.Sprintf("=== RESOURCE MONITOR: %s ===", context))

		// Count all pods
		podList := &corev1.PodList{}
		err := k8sClient.List(ctx, podList, &client.ListOptions{})
		if err == nil {
			runningPods := 0
			pendingPods := 0
			for _, pod := range podList.Items {
				switch pod.Status.Phase {
				case corev1.PodRunning:
					runningPods++
				case corev1.PodPending:
					pendingPods++
					// Log pending pod details for troubleshooting
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
							By(fmt.Sprintf("UNSCHEDULABLE POD: %s/%s - %s", pod.Namespace, pod.Name, condition.Message))
						}
					}
				}
			}
			By(fmt.Sprintf("PODS: %d total, %d running, %d pending", len(podList.Items), runningPods, pendingPods))
		}

		// Count Neo4j resources
		clusterList := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
		if err := k8sClient.List(ctx, clusterList, &client.ListOptions{}); err == nil {
			By(fmt.Sprintf("NEO4J CLUSTERS: %d", len(clusterList.Items)))
		}

		standaloneList := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
		if err := k8sClient.List(ctx, standaloneList, &client.ListOptions{}); err == nil {
			By(fmt.Sprintf("NEO4J STANDALONES: %d", len(standaloneList.Items)))
		}

		By(fmt.Sprintf("=== END RESOURCE MONITOR: %s ===", context))
	}
}

// installCRDsIfMissing installs required CRDs using kubectl if any are missing
func installCRDsIfMissing() {
	missing := []string{}
	for _, crd := range requiredCRDs {
		if !isCRDAvailable(crd) {
			missing = append(missing, crd)
		}
	}
	if len(missing) > 0 {
		By(fmt.Sprintf("Installing missing CRDs for integration tests: %v", missing))
		cmd := exec.Command("kubectl", "apply", "--validate=false", "-f", "../../config/crd/bases/")
		// Ensure the kubectl command uses the same environment as the test
		cmd.Env = os.Environ()
		cmd.Stdout = GinkgoWriter
		cmd.Stderr = GinkgoWriter
		err := cmd.Run()
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs: %v", err)
		// Wait for CRDs to be established
		for _, crd := range missing {
			By(fmt.Sprintf("Waiting for CRD %s to be established", crd))
			Eventually(func() bool { return isCRDAvailable(crd) }, 30*time.Second, 2*time.Second).Should(BeTrue(), "CRD %s should be available", crd)
		}
	} else {
		By("All required CRDs are already available")
	}
}

// isOperatorRunning checks if the Neo4j operator is deployed and running
func isOperatorRunning() bool {
	deploymentList := &appsv1.DeploymentList{}
	err := k8sClient.List(ctx, deploymentList, client.InNamespace("neo4j-operator-system"))
	if err != nil {
		return false
	}

	for _, deployment := range deploymentList.Items {
		if deployment.Name == "neo4j-operator-controller-manager" {
			return deployment.Status.ReadyReplicas > 0
		}
	}
	return false
}

// randomName generates a random name for test resources
func randomName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
}

// createBasicCluster creates a basic Neo4j cluster for testing with minimum topology
func createBasicCluster(name, namespace string) *neo4jv1alpha1.Neo4jEnterpriseCluster {
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  getNeo4jImageTag(), // Use environment-specified version
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 2, // Minimum cluster topology
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "1Gi",
			},
		},
	}
}

// createBasicStandalone creates a basic Neo4j standalone deployment for testing
func createBasicStandalone(name, namespace string) *neo4jv1alpha1.Neo4jEnterpriseStandalone {
	return &neo4jv1alpha1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  getNeo4jImageTag(), // Use environment-specified version
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "1Gi",
			},
		},
	}
}

// getCIAppropriateResourceRequirements returns resource requirements optimized for CI environments
// getNeo4jImageTag returns the Neo4j image tag to use for tests
// It uses NEO4J_VERSION environment variable if set, otherwise defaults to 5.26-enterprise
func getNeo4jImageTag() string {
	if tag := os.Getenv("NEO4J_VERSION"); tag != "" {
		return tag
	}
	return "5.26-enterprise"
}

// getCIAppropriateClusterSize returns cluster size optimized for CI resources
// Uses minimum viable cluster size in CI to reduce resource pressure
func getCIAppropriateClusterSize(defaultSize int32) int32 {
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		// In CI: Use minimum cluster size (2) to reduce resource usage
		// Neo4j Enterprise clusters require minimum 2 servers
		return 2
	}
	// Local/development: Use the requested size
	return defaultSize
}

// Uses minimal resources while respecting Neo4j Enterprise's 1Gi memory minimum requirement
func getCIAppropriateResourceRequirements() *corev1.ResourceRequirements {
	// Check if running in CI environment
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		// CI environment: Ultra-minimal resources for GitHub Actions standard runners
		// Total available: 2 CPU, 7GB RAM minus system overhead (~2GB) = ~5GB usable
		// Multiple tests may run concurrently, so keep requests very low
		return &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),   // Minimal CPU for scheduling
				corev1.ResourceMemory: resource.MustParse("200Mi"), // Reduced memory request for CI
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),  // Reduced CPU limit for CI
				corev1.ResourceMemory: resource.MustParse("1.5Gi"), // Neo4j Enterprise REQUIRES ≥ 1.5Gi for database operations
			},
		}
	} else {
		// Local/development environment: Use standard Neo4j Enterprise requirements
		return &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),  // Standard CPU request
				corev1.ResourceMemory: resource.MustParse("1.5Gi"), // Neo4j Enterprise recommended minimum
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),  // Standard CPU limit
				corev1.ResourceMemory: resource.MustParse("1.5Gi"), // Neo4j Enterprise recommended minimum
			},
		}
	}
}
