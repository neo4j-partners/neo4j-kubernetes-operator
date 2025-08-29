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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Cluster Lifecycle Integration Tests", func() {
	var (
		namespace   *corev1.Namespace
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName string
	)

	BeforeEach(func() {
		By("Starting BeforeEach for cluster lifecycle test")
		// Create test namespace (createTestNamespace already creates it in the cluster)
		namespaceName := createTestNamespace("lifecycle")
		By(fmt.Sprintf("Created namespace: %s", namespaceName))

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		// Note: namespace is already created by createTestNamespace, no need to create again
		By("Successfully set up namespace object")

		clusterName = fmt.Sprintf("lifecycle-cluster-%d", GinkgoRandomSeed())
		By(fmt.Sprintf("Generated cluster name: %s", clusterName))

		// Create admin secret for authentication
		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: namespaceName,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())
		By("Completed BeforeEach setup")
	})

	AfterEach(func() {
		// Clean up cluster resource if it was created
		if cluster != nil {
			By("Cleaning up cluster resource")
			// Remove finalizers if any
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			// Delete the resource
			err := k8sClient.Delete(ctx, cluster)
			if err != nil && !errors.IsNotFound(err) {
				By(fmt.Sprintf("Failed to delete cluster: %v", err))
			}
		}

		// Note: Namespace cleanup is handled by the test suite cleanup
		// which removes all test namespaces and their resources
	})

	Context("End-to-end cluster lifecycle", func() {
		It("Should create, scale, upgrade, and delete cluster successfully", func() {
			// Skip this test if no operator is running (requires full cluster setup)
			if !isOperatorRunning() {
				Skip("End-to-end cluster lifecycle test requires operator to be running")
			}
			By("Starting end-to-end cluster lifecycle test")
			By("Creating a basic cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
				},
			}
			By("About to create Neo4jEnterpriseCluster")
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())
			By("Successfully created Neo4jEnterpriseCluster")

			By("Waiting for single server StatefulSet to be created")
			// CURRENT ARCHITECTURE: Single StatefulSet with multiple replicas
			serverStatefulSet := &appsv1.StatefulSet{}
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-server", clusterName),
					Namespace: namespace.Name,
				}, serverStatefulSet)
				if err != nil {
					return fmt.Errorf("server StatefulSet not found: %v", err)
				}
				if serverStatefulSet.Spec.Replicas == nil || *serverStatefulSet.Spec.Replicas != 3 {
					return fmt.Errorf("server StatefulSet should have 3 replicas, got %v", serverStatefulSet.Spec.Replicas)
				}
				return nil
			}, timeout, interval).Should(Succeed())

			By("Verifying initial server count (single StatefulSet with 3 replicas)")
			// Verify single StatefulSet with correct replica count
			serverSts := &appsv1.StatefulSet{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-server", clusterName),
				Namespace: namespace.Name,
			}, serverSts)
			Expect(err).NotTo(HaveOccurred())
			Expect(*serverSts.Spec.Replicas).To(Equal(int32(3)))

			By("Scaling up servers")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return err
				}
				// Update server count (servers self-organize into primaries/secondaries)
				cluster.Spec.Topology.Servers = 5
				return k8sClient.Update(ctx, cluster)
			}, timeout, interval).Should(Succeed())

			By("Verifying scaling completed - should update StatefulSet to 5 replicas")
			Eventually(func() int32 {
				serverSts := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-server", clusterName),
					Namespace: namespace.Name,
				}, serverSts)
				if err != nil {
					return 0
				}
				if serverSts.Spec.Replicas == nil {
					return 0
				}
				fmt.Printf("Current StatefulSet replica count: %d\n", *serverSts.Spec.Replicas)
				return *serverSts.Spec.Replicas
			}, 60*time.Second, interval).Should(Equal(int32(5)))

			By("Upgrading cluster image")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return err
				}
				cluster.Spec.Image.Tag = "5.27-enterprise"
				return k8sClient.Update(ctx, cluster)
			}, timeout, interval).Should(Succeed())

			By("Verifying image upgrade on server StatefulSet")
			Eventually(func() bool {
				// Check that the server StatefulSet has the upgraded image
				serverSts := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-server", clusterName),
					Namespace: namespace.Name,
				}, serverSts)
				if err != nil {
					return false
				}
				return containsString(serverSts.Spec.Template.Spec.Containers[0].Image, "5.27-enterprise")
			}, timeout, interval).Should(BeTrue())

			By("Verifying cluster status")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					GinkgoWriter.Printf("Failed to get cluster: %v\n", err)
					return false
				}

				// Check if cluster phase is Ready (more reliable than conditions)
				if cluster.Status.Phase == "Ready" {
					GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
						cluster.Status.Phase, cluster.Status.Message)
					return true
				}

				// Log current status for debugging
				GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
					cluster.Status.Phase, cluster.Status.Message)
				return false
			}, timeout, interval).Should(BeTrue())

			By("Deleting the cluster")
			Expect(k8sClient.Delete(ctx, cluster)).Should(Succeed())

			By("Verifying cluster deletion")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				return client.IgnoreNotFound(err) == nil
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Multi-cluster deployment", func() {
		It("Should handle multiple clusters in same namespace", func() {
			// Skip this test if no operator is running (requires full cluster setup)
			if !isOperatorRunning() {
				Skip("Multi-cluster deployment test requires operator to be running")
			}
			cluster1 := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-1",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "5Gi",
					},
				},
			}

			cluster2 := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-2",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "5Gi",
					},
				},
			}

			By("Creating multiple clusters")
			Expect(k8sClient.Create(ctx, cluster1)).Should(Succeed())
			Expect(k8sClient.Create(ctx, cluster2)).Should(Succeed())

			By("Verifying both clusters are processed independently")
			// Check first cluster - should have single StatefulSet with 3 replicas
			serverSts1 := &appsv1.StatefulSet{}
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-1-server", clusterName),
					Namespace: namespace.Name,
				}, serverSts1)
				if err != nil {
					return fmt.Errorf("cluster-1 server StatefulSet not found: %v", err)
				}
				if serverSts1.Spec.Replicas == nil || *serverSts1.Spec.Replicas != 3 {
					return fmt.Errorf("cluster-1 server StatefulSet should have 3 replicas, got %v", serverSts1.Spec.Replicas)
				}
				return nil
			}, timeout, interval).Should(Succeed())

			// Check second cluster - should have single StatefulSet with 3 replicas
			serverSts2 := &appsv1.StatefulSet{}
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-2-server", clusterName),
					Namespace: namespace.Name,
				}, serverSts2)
				if err != nil {
					return fmt.Errorf("cluster-2 server StatefulSet not found: %v", err)
				}
				if serverSts2.Spec.Replicas == nil || *serverSts2.Spec.Replicas != 3 {
					return fmt.Errorf("cluster-2 server StatefulSet should have 3 replicas, got %v", serverSts2.Spec.Replicas)
				}
				return nil
			}, timeout, interval).Should(Succeed())

			By("Verifying resource isolation - each cluster has its own StatefulSet")
			// Verify cluster 1 StatefulSet
			serverSts1Check := &appsv1.StatefulSet{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-1-server", clusterName),
				Namespace: namespace.Name,
			}, serverSts1Check)
			Expect(err).NotTo(HaveOccurred())
			Expect(*serverSts1Check.Spec.Replicas).To(Equal(int32(3)))

			// Verify cluster 2 StatefulSet
			serverSts2Check := &appsv1.StatefulSet{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-2-server", clusterName),
				Namespace: namespace.Name,
			}, serverSts2Check)
			Expect(err).NotTo(HaveOccurred())
			Expect(*serverSts2Check.Spec.Replicas).To(Equal(int32(3)))

			// Verify services are created with unique names
			service1 := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-1-client",
					Namespace: namespace.Name,
				}, service1)
			}, timeout, interval).Should(Succeed())

			service2 := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-2-client",
					Namespace: namespace.Name,
				}, service2)
			}, timeout, interval).Should(Succeed())
		})
	})
})

// Note: containsString helper function is defined in multi_node_cluster_test.go
