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
	appsv1 "k8s.io/api/apps/v1"
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
			Edition: "enterprise",
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Primaries:   1,
				Secondaries: 1, // Minimum cluster topology
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "10Gi",
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
			Edition: "enterprise",
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "10Gi",
			},
		},
	}
}
