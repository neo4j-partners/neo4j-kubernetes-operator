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
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 1,
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

			By("Waiting for StatefulSets to be created")
			primarySts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-primary",
					Namespace: namespace.Name,
				}, primarySts)
			}, timeout, interval).Should(Succeed())

			secondarySts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-secondary",
					Namespace: namespace.Name,
				}, secondarySts)
			}, timeout, interval).Should(Succeed())

			By("Verifying initial replica counts")
			Expect(*primarySts.Spec.Replicas).To(Equal(int32(3)))
			Expect(*secondarySts.Spec.Replicas).To(Equal(int32(1)))

			By("Scaling up secondaries")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return err
				}
				cluster.Spec.Topology.Secondaries = 3
				return k8sClient.Update(ctx, cluster)
			}, timeout, interval).Should(Succeed())

			By("Verifying scaling completed")
			Eventually(func() int32 {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-secondary",
					Namespace: namespace.Name,
				}, secondarySts)
				if err != nil {
					fmt.Printf("Error getting StatefulSet: %v\n", err)
					return 0
				}
				currentReplicas := int32(0)
				if secondarySts.Spec.Replicas != nil {
					currentReplicas = *secondarySts.Spec.Replicas
				}
				fmt.Printf("Current secondary StatefulSet replicas: %d\n", currentReplicas)
				return currentReplicas
			}, 60*time.Second, interval).Should(Equal(int32(3)))

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

			By("Verifying image upgrade")
			Eventually(func() string {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-primary",
					Namespace: namespace.Name,
				}, primarySts)
				if err != nil {
					return ""
				}
				return primarySts.Spec.Template.Spec.Containers[0].Image
			}, timeout, interval).Should(ContainSubstring("5.27-enterprise"))

			By("Verifying cluster status")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return false
				}
				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
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
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 1,
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
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 2,
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
			// Check first cluster
			primarySts1 := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-1-primary",
					Namespace: namespace.Name,
				}, primarySts1)
			}, timeout, interval).Should(Succeed())

			// Check second cluster
			primarySts2 := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-2-primary",
					Namespace: namespace.Name,
				}, primarySts2)
			}, timeout, interval).Should(Succeed())

			secondarySts2 := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-2-secondary",
					Namespace: namespace.Name,
				}, secondarySts2)
			}, timeout, interval).Should(Succeed())

			By("Verifying resource isolation")
			Expect(*primarySts1.Spec.Replicas).To(Equal(int32(3)))
			Expect(*primarySts2.Spec.Replicas).To(Equal(int32(3)))
			Expect(*secondarySts2.Spec.Replicas).To(Equal(int32(2)))

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
