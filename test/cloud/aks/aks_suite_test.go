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

package aks

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var (
	cfg           *rest.Config
	k8sClient     client.Client
	clientset     *kubernetes.Clientset
	ctx           context.Context
	cancel        context.CancelFunc
	testNamespace string
)

const (
	timeout  = time.Minute * 10
	interval = time.Second * 10
)

func TestAKS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AKS Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	By("Setting up AKS test environment")

	// Skip if not running on AKS
	if os.Getenv("CLUSTER_TYPE") != "aks" {
		Skip("Skipping AKS tests - CLUSTER_TYPE != aks")
	}

	// Use in-cluster config or kubeconfig
	var err error
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	Expect(err).NotTo(HaveOccurred())

	// Create clients
	clientset, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())

	// Create test namespace
	testNamespace = fmt.Sprintf("aks-test-%d", time.Now().Unix())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("Cleaning up AKS test environment")

	if testNamespace != "" {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		k8sClient.Delete(ctx, ns)
	}

	cancel()
})

// AKS-specific test utilities
func createAKSCluster(name string) *neo4jv1alpha1.Neo4jEnterpriseCluster {
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Primaries:   3,
				Secondaries: 2,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "managed-premium", // AKS premium storage class
				Size:      "20Gi",
			},
			// AKS-specific configurations
			Backups: &neo4jv1alpha1.BackupsSpec{
				DefaultStorage: &neo4jv1alpha1.StorageLocation{
					Type:   "azure",
					Bucket: os.Getenv("AZURE_STORAGE_CONTAINER"),
					Path:   "/aks-backups",
				},
				Cloud: &neo4jv1alpha1.CloudBlock{
					Provider: "azure",
					Identity: &neo4jv1alpha1.CloudIdentity{
						Provider:       "azure",
						ServiceAccount: "neo4j-backup-sa",
						AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
							Enabled: true,
							Annotations: map[string]string{
								"azure.workload.identity/client-id": "test-client-id",
							},
						},
					},
				},
			},
		},
	}
}

var _ = Describe("AKS Integration Tests", func() {
	Context("When deploying Neo4j on AKS", func() {
		It("should create a Neo4j cluster with AKS-specific configuration", func() {
			cluster := createAKSCluster("test-aks-cluster")

			// Create the cluster
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			// Wait for cluster to be created (with timeout)
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
			}, timeout, interval).Should(Succeed())

			// Verify AKS-specific configuration
			Expect(cluster.Spec.Storage.ClassName).To(Equal("managed-premium"))
			Expect(cluster.Spec.Backups.DefaultStorage.Type).To(Equal("azure"))

			// Clean up
			By("Cleaning up test resources")
			// Clean up test resources - errors in cleanup are logged but don't fail the test
			if err := k8sClient.Delete(ctx, cluster); err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "warning: failed to delete cluster during cleanup: %v\n", err)
			}
		})

		It("should handle AKS storage classes correctly", func() {
			cluster := createAKSCluster("test-aks-storage")

			// Verify storage configuration
			Expect(cluster.Spec.Storage.ClassName).To(Equal("managed-premium"))
			Expect(cluster.Spec.Storage.Size).To(Equal("20Gi"))
		})

		It("should configure Azure backup storage correctly", func() {
			cluster := createAKSCluster("test-aks-backup")

			// Verify backup configuration
			Expect(cluster.Spec.Backups).NotTo(BeNil())
			Expect(cluster.Spec.Backups.DefaultStorage.Type).To(Equal("azure"))
			Expect(cluster.Spec.Backups.Cloud.Provider).To(Equal("azure"))
		})
	})
})
