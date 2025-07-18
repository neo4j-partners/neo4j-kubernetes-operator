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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Multi-Node Cluster Formation Integration Tests", func() {
	var (
		namespace   *corev1.Namespace
		clusterName string
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		timeout     = time.Second * 120
		interval    = time.Second * 2
	)

	BeforeEach(func() {
		// Create a new namespace for each test
		namespaceName := fmt.Sprintf("multinode-%d", time.Now().Unix())
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}

		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().Unix())
	})

	AfterEach(func() {
		// Cleanup will be handled by the test suite cleanup
		// which removes all test namespaces and their resources
	})

	Context("Minimal Cluster (1 Primary + 1 Secondary)", func() {
		It("should create a minimal cluster with proper coordination", func() {
			By("Creating a minimal cluster specification")
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
						Primaries:   1,
						Secondaries: 1, // Minimum cluster topology
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "2Gi",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}

			By("Creating the cluster resource")
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for ConfigMap to be created")
			configMapKey := types.NamespacedName{
				Name:      clusterName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				startupScript, exists := configMap.Data["startup.sh"]
				if !exists {
					return fmt.Errorf("startup.sh not found in ConfigMap")
				}

				// Verify minimal cluster uses same unified approach as larger clusters
				if !containsString(startupScript, "dbms.cluster.discovery.resolver_type=K8S") {
					return fmt.Errorf("startup script does not contain Kubernetes discovery")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for both StatefulSets to be created")
			// Primary StatefulSet
			primaryStatefulSetKey := types.NamespacedName{
				Name:      clusterName + "-primary",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				statefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, primaryStatefulSetKey, statefulSet); err != nil {
					return err
				}

				if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 {
					return fmt.Errorf("Primary StatefulSet should have 1 replica, got %v", statefulSet.Spec.Replicas)
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			// Secondary StatefulSet
			secondaryStatefulSetKey := types.NamespacedName{
				Name:      clusterName + "-secondary",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				statefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, secondaryStatefulSetKey, statefulSet); err != nil {
					return err
				}

				if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 {
					return fmt.Errorf("Secondary StatefulSet should have 1 replica, got %v", statefulSet.Spec.Replicas)
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Verifying pods are created for minimal cluster")
			Eventually(func() error {
				// Check primary pods
				primaryPods := &corev1.PodList{}
				if err := k8sClient.List(ctx, primaryPods, client.InNamespace(namespace.Name),
					client.MatchingLabels{"neo4j.com/role": "primary"}); err != nil {
					return err
				}

				if len(primaryPods.Items) != 1 {
					return fmt.Errorf("expected 1 primary pod, got %d", len(primaryPods.Items))
				}

				// Check secondary pods
				secondaryPods := &corev1.PodList{}
				if err := k8sClient.List(ctx, secondaryPods, client.InNamespace(namespace.Name),
					client.MatchingLabels{"neo4j.com/role": "secondary"}); err != nil {
					return err
				}

				if len(secondaryPods.Items) != 1 {
					return fmt.Errorf("expected 1 secondary pod, got %d", len(secondaryPods.Items))
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})

	Context("V2_ONLY Discovery Configuration (Critical Fix)", func() {
		It("should use tcp-discovery port for Neo4j 2025.x cluster", func() {
			clusterName = "v2only-2025-test"
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "2025.02.0-enterprise", // Test 2025.x version
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
				},
			}

			By("Creating Neo4j 2025.x cluster")
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for ConfigMap to be created with 2025.x discovery configuration")
			configMapKey := types.NamespacedName{
				Name:      clusterName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				startupScript := configMap.Data["startup.sh"]
				if startupScript == "" {
					return fmt.Errorf("startup script is empty")
				}

				// Check for 2025.x specific discovery configuration
				if !containsString(startupScript, "dbms.kubernetes.discovery.service_port_name=tcp-discovery") {
					return fmt.Errorf("startup script does not contain 2025.x discovery configuration")
				}

				// V2_ONLY should NOT be set explicitly for 2025.x (it's default)
				if containsString(startupScript, "dbms.cluster.discovery.version=V2_ONLY") {
					return fmt.Errorf("startup script incorrectly sets V2_ONLY for 2025.x (should be default)")
				}

				// Verify that the wrong port configuration is NOT used
				if containsString(startupScript, "service_port_name=tcp-tx") {
					return fmt.Errorf("startup script incorrectly uses tcp-tx port (should use tcp-discovery)")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Cleaning up the cluster")
			Expect(k8sClient.Delete(ctx, cluster)).Should(Succeed())
		})
	})
})

// Helper function to check if a string contains a substring
func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i <= len(haystack)-len(needle); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
