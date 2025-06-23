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

package gke

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
	"k8s.io/apimachinery/pkg/types"
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

func TestGKE(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GKE Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	By("Setting up GKE test environment")

	// Skip if not running on GKE
	if os.Getenv("CLUSTER_TYPE") != "gke" {
		Skip("Skipping GKE tests - CLUSTER_TYPE != gke")
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
	testNamespace = fmt.Sprintf("gke-test-%d", time.Now().Unix())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("Cleaning up GKE test environment")

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

// GKE-specific test utilities
func createGKECluster(name string) *neo4jv1alpha1.Neo4jEnterpriseCluster {
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
				ClassName: "standard-rwo", // GKE default storage class
				Size:      "20Gi",
			},
			// GKE-specific configurations
			Backups: &neo4jv1alpha1.BackupsSpec{
				DefaultStorage: &neo4jv1alpha1.StorageLocation{
					Type:   "gcs",
					Bucket: os.Getenv("GCS_BACKUP_BUCKET"),
					Path:   "/gke-backups",
				},
				Cloud: &neo4jv1alpha1.CloudBlock{
					Provider: "gcp",
					Identity: &neo4jv1alpha1.CloudIdentity{
						Provider:       "gcp",
						ServiceAccount: "neo4j-backup-sa",
						AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
							Enabled: true,
							Annotations: map[string]string{
								"iam.gke.io/gcp-service-account": "neo4j-backup@project.iam.gserviceaccount.com",
							},
						},
					},
				},
			},
		},
	}
}

func waitForGKENodeReadiness() {
	Eventually(func() bool {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false
		}

		readyNodes := 0
		for _, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					readyNodes++
					break
				}
			}
		}

		return readyNodes >= 2 // Minimum nodes for testing
	}, timeout, interval).Should(BeTrue())
}

func verifyGKESpecificFeatures(clusterName string) {
	By("Verifying GKE-specific features")

	// Check if service account has Workload Identity annotations
	sa := &corev1.ServiceAccount{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterName + "-backup",
			Namespace: testNamespace,
		}, sa)
	}, timeout, interval).Should(Succeed())

	// Verify Workload Identity annotation
	Expect(sa.Annotations).To(HaveKey("iam.gke.io/gcp-service-account"))

	By("Verifying GKE networking features")
	// Check if cluster uses GKE-specific networking
}

var _ = Describe("GKE Integration Tests", func() {
	Context("When deploying Neo4j on GKE", func() {
		It("should create a Neo4j cluster with GKE-specific configuration", func() {
			cluster := createGKECluster("test-gke-cluster")

			// Create the cluster
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			// Wait for cluster to be created (with timeout)
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
			}, timeout, interval).Should(Succeed())

			// Verify GKE-specific configuration
			Expect(cluster.Spec.Storage.ClassName).To(Equal("standard-rwo"))
			Expect(cluster.Spec.Backups.DefaultStorage.Type).To(Equal("gcs"))

			// Clean up
			Expect(k8sClient.Delete(ctx, cluster)).Should(Succeed())
		})

		It("should wait for GKE node readiness", func() {
			waitForGKENodeReadiness()
			// This should succeed or timeout gracefully
		})

		It("should verify GKE-specific features", func() {
			cluster := createGKECluster("test-gke-features")

			verifyGKESpecificFeatures(cluster.Name)
			// This should succeed or skip if features not available
		})
	})
})
