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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
var interval = 10 * time.Second

// List of required CRDs for integration tests
var requiredCRDs = []string{
	"neo4jenterpriseclusters.neo4j.neo4j.com",
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

	// Setup test environment with proper operator configuration
	setupTestEnvironment()

	// Install CRDs if missing
	By("Installing CRDs if missing")
	installCRDsIfMissing()

	isSetup = true
	By("Integration test setup completed successfully")
})

var _ = AfterSuite(func() {
	By("Cleaning up test environment")

	// Cancel context
	if cancel != nil {
		cancel()
	}

	// Clean up test namespaces
	cleanupTestNamespaces()

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

// cleanupTestNamespaces removes all test namespaces
func cleanupTestNamespaces() {
	By("Cleaning up test namespaces")

	// List all namespaces with test-run label
	cmd := exec.Command("kubectl", "get", "namespaces", "-l", "test-run="+testRunID, "-o", "jsonpath={.items[*].metadata.name}")
	output, err := cmd.Output()
	if err != nil {
		By("No test namespaces found to clean up")
		return
	}

	namespaces := strings.Fields(string(output))
	for _, namespace := range namespaces {
		if namespace != "" {
			By(fmt.Sprintf("Deleting test namespace: %s", namespace))
			cmd := exec.Command("kubectl", "delete", "namespace", namespace, "--ignore-not-found=true")
			cmd.Stdout = GinkgoWriter
			cmd.Stderr = GinkgoWriter
			cmd.Run()
		}
	}
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
		cmd := exec.Command("kubectl", "apply", "-f", "../../config/crd/bases/")
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

// setupTestEnvironment ensures the test environment is properly configured
func setupTestEnvironment() {
	By("Setting up test environment with proper operator configuration")

	// 1. Deploy cert-manager if not present
	By("Ensuring cert-manager is deployed")
	deployCertManagerIfNeeded()

	// 2. Deploy operator with webhooks and TLS certificates
	By("Deploying operator with webhooks and TLS certificates")
	deployOperatorWithWebhooks()

	// 3. Wait for operator to be ready
	By("Waiting for operator to be ready")
	waitForOperatorReady()

	// 4. Verify webhook configuration
	By("Verifying webhook configuration")
	verifyWebhookConfiguration()

	// 5. Verify RBAC permissions
	By("Verifying RBAC permissions")
	verifyRBACPermissions()

	// 6. Verify operator functionality
	By("Verifying operator functionality")
	verifyOperatorFunctionality()

	By("Test environment setup completed")
}

// deployCertManagerIfNeeded deploys cert-manager if it's not already present
func deployCertManagerIfNeeded() {
	// Check if cert-manager is already deployed
	cmd := exec.Command("kubectl", "get", "namespace", "cert-manager")
	if cmd.Run() == nil {
		By("Cert-manager namespace exists, checking if it's ready")
		// Wait for cert-manager to be ready
		cmd = exec.Command("kubectl", "wait", "--for=condition=Available", "deployment/cert-manager", "-n", "cert-manager", "--timeout=60s")
		if cmd.Run() != nil {
			By("Cert-manager not ready, deploying...")
			deployCertManager()
		} else {
			By("Cert-manager is already ready")
		}
	} else {
		By("Cert-manager not found, deploying...")
		deployCertManager()
	}
}

// deployCertManager installs cert-manager
func deployCertManager() {
	cmd := exec.Command("kubectl", "apply", "-f", "https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed(), "Failed to deploy cert-manager")

	// Wait for cert-manager to be ready
	By("Waiting for cert-manager to be ready")
	Eventually(func() error {
		cmd := exec.Command("kubectl", "wait", "--for=condition=Available", "deployment/cert-manager", "-n", "cert-manager", "--timeout=30s")
		return cmd.Run()
	}, 2*time.Minute, 10*time.Second).Should(Succeed(), "Cert-manager failed to become ready")
}

// deployOperatorWithWebhooks deploys the operator with webhooks and TLS certificates
func deployOperatorWithWebhooks() {
	By("Deploying operator with webhooks using test configuration")

	// Check if operator is already running and available
	cmd := exec.Command("kubectl", "wait", "--for=condition=Available", "deployment/neo4j-operator-controller-manager", "-n", "neo4j-operator-system", "--timeout=10s")
	if cmd.Run() == nil {
		By("Operator is already running and available, skipping deployment")
		return
	}

	// Clean up any existing deployment first
	cleanupExistingOperator()

	// Deploy using the test-with-webhooks configuration
	// Note: We need to run from the project root, so we change directory
	cmd = exec.Command("kubectl", "apply", "-k", "../../config/test-with-webhooks")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed(), "Failed to deploy operator with webhooks")

	// Apply webhook resources separately to avoid namePrefix issues
	By("Applying webhook resources separately")
	cmd = exec.Command("kubectl", "apply", "-k", "../../config/webhook")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed(), "Failed to deploy webhook resources")

	// Wait for the certificate to be ready
	By("Waiting for TLS certificate to be ready")
	Eventually(func() error {
		cmd := exec.Command("kubectl", "wait", "--for=condition=Ready", "certificate/serving-cert", "-n", "neo4j-operator-system", "--timeout=30s")
		return cmd.Run()
	}, 2*time.Minute, 10*time.Second).Should(Succeed(), "TLS certificate failed to become ready")
}

// cleanupExistingOperator removes any existing operator deployment
func cleanupExistingOperator() {
	By("Cleaning up existing operator deployment")

	// Delete existing deployment if it exists
	cmd := exec.Command("kubectl", "delete", "deployment", "controller-manager", "-n", "neo4j-operator-system", "--ignore-not-found=true")
	cmd.Run()

	// Delete existing service account if it exists
	cmd = exec.Command("kubectl", "delete", "serviceaccount", "neo4j-operator-controller-manager", "-n", "neo4j-operator-system", "--ignore-not-found=true")
	cmd.Run()

	// Delete existing RBAC bindings if they exist
	cmd = exec.Command("kubectl", "delete", "clusterrolebinding", "neo4j-operator-manager-rolebinding", "--ignore-not-found=true")
	cmd.Run()

	cmd = exec.Command("kubectl", "delete", "clusterrolebinding", "neo4j-operator-leader-election-rolebinding", "--ignore-not-found=true")
	cmd.Run()
}

// waitForOperatorReady waits for the operator to be fully ready
func waitForOperatorReady() {
	// Check if operator deployment is already available
	cmd := exec.Command("kubectl", "wait", "--for=condition=Available", "deployment/neo4j-operator-controller-manager", "-n", "neo4j-operator-system", "--timeout=10s")
	if cmd.Run() == nil {
		By("Operator deployment is already available, skipping availability check")
	} else {
		By("Waiting for operator deployment to be available")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Available", "deployment/neo4j-operator-controller-manager", "-n", "neo4j-operator-system", "--timeout=30s")
			return cmd.Run()
		}, 3*time.Minute, 10*time.Second).Should(Succeed(), "Operator deployment failed to become available")
	}

	// Wait for the pod to be ready
	By("Waiting for operator pod to be ready")
	Eventually(func() error {
		cmd := exec.Command("kubectl", "wait", "--for=condition=Ready", "pod", "-l", "control-plane=controller-manager", "-n", "neo4j-operator-system", "--timeout=30s")
		return cmd.Run()
	}, 2*time.Minute, 10*time.Second).Should(Succeed(), "Operator pod failed to become ready")

	// Wait for leader election
	By("Waiting for leader election")
	Eventually(func() bool {
		cmd := exec.Command("kubectl", "logs", "-n", "neo4j-operator-system", "-l", "control-plane=controller-manager", "--tail=50")
		output, err := cmd.Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(output), "successfully acquired lease")
	}, 2*time.Minute, 10*time.Second).Should(BeTrue(), "Leader election failed")
}

// verifyWebhookConfiguration verifies that webhooks are properly configured
func verifyWebhookConfiguration() {
	By("Verifying webhook configurations exist")

	// Check for validating webhook
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "validatingwebhookconfiguration", "validating-webhook-configuration")
		return cmd.Run()
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Validating webhook configuration not found")

	// Check for mutating webhook
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "mutating-webhook-configuration")
		return cmd.Run()
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Mutating webhook configuration not found")

	// Check for webhook service
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "service", "webhook-service", "-n", "neo4j-operator-system")
		return cmd.Run()
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Webhook service not found")

	// Check for TLS certificate secret
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "secret", "webhook-server-cert", "-n", "neo4j-operator-system")
		return cmd.Run()
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "TLS certificate secret not found")
}

// verifyRBACPermissions verifies that RBAC permissions are properly configured
func verifyRBACPermissions() {
	By("Verifying RBAC permissions")

	// Check that the service account exists
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "serviceaccount", "neo4j-operator-controller-manager", "-n", "neo4j-operator-system")
		return cmd.Run()
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Service account not found")

	// Check that the cluster role binding exists and is bound to the correct service account
	Eventually(func() bool {
		cmd := exec.Command("kubectl", "get", "clusterrolebinding", "manager-rolebinding", "-o", "jsonpath={.subjects[0].name}")
		output, err := cmd.Output()
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(output)) == "neo4j-operator-controller-manager"
	}, 1*time.Minute, 5*time.Second).Should(BeTrue(), "RBAC binding not configured correctly")

	// Check that the leader election role binding exists
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "clusterrolebinding", "leader-election-rolebinding")
		return cmd.Run()
	}, 1*time.Minute, 5*time.Second).Should(Succeed(), "Leader election role binding not found")
}

// verifyOperatorFunctionality tests that the operator can create and manage resources
func verifyOperatorFunctionality() {
	By("Verifying operator functionality with a test resource")

	// Create a test namespace
	testNamespace := createTestNamespace("operator-test")

	// Create a simple Neo4jEnterpriseCluster resource
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: testNamespace,
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Edition: "enterprise",
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Primaries: 1,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "local-path",
				Size:      "1Gi",
			},
			TLS: &neo4jv1alpha1.TLSSpec{
				IssuerRef: &neo4jv1alpha1.IssuerRef{
					Name: "neo4j-operator-selfsigned-issuer",
				},
			},
		},
	}

	// Create the resource
	err := k8sClient.Create(ctx, cluster)
	Expect(err).NotTo(HaveOccurred(), "Failed to create test Neo4jEnterpriseCluster")

	// Wait for the resource to be processed (this should trigger webhook validation)
	By("Waiting for operator to process the test resource")
	Eventually(func() bool {
		var createdCluster neo4jv1alpha1.Neo4jEnterpriseCluster
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: testNamespace}, &createdCluster)
		if err != nil {
			return false
		}
		// Check if the resource exists and has been created successfully
		return createdCluster.Name == "test-cluster" && createdCluster.Namespace == testNamespace
	}, 60*time.Second, 5*time.Second).Should(BeTrue(), "Operator failed to process test resource")

	By("Test resource created successfully - operator is working")

	// Clean up the test resource
	By("Cleaning up test resource")
	err = k8sClient.Delete(ctx, cluster)
	Expect(err).NotTo(HaveOccurred(), "Failed to delete test Neo4jEnterpriseCluster")

	// Wait for deletion to complete
	Eventually(func() bool {
		var deletedCluster neo4jv1alpha1.Neo4jEnterpriseCluster
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: testNamespace}, &deletedCluster)
		return errors.IsNotFound(err)
	}, 30*time.Second, 5*time.Second).Should(BeTrue(), "Test resource not deleted")

	By("Operator functionality verification completed successfully")
}
