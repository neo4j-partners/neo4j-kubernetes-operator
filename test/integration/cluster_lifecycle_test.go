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

package integration

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
		// Create test namespace
		namespaceName := createTestNamespace("lifecycle")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		clusterName = fmt.Sprintf("lifecycle-cluster-%d", GinkgoRandomSeed())
	})

	AfterEach(func() {
		// Clean up namespace (will cascade delete all resources)
		if namespace != nil {
			Expect(k8sClient.Delete(ctx, namespace)).Should(Succeed())
		}
	})

	Context("End-to-end cluster lifecycle", func() {
		It("Should create, scale, upgrade, and delete cluster successfully", func() {
			By("Creating a basic cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
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
						Size:      "10Gi",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

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
					return 0
				}
				return *secondarySts.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(3)))

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
			cluster1 := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-1",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
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
