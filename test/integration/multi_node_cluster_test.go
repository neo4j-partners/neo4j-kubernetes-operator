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
		ctx         context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName string
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create test namespace
		namespaceName := createTestNamespace("multi-node")
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

	Context("Two-Node Cluster Formation", func() {
		It("should create a unified cluster with proper coordination", func() {
			By("Creating a 2-primary cluster specification")
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
						Primaries:   2,
						Secondaries: 0,
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

			By("Waiting for ConfigMap to be created with proper cluster formation logic")
			configMapKey := types.NamespacedName{
				Name:      clusterName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				// Verify startup script contains unified bootstrap approach
				startupScript, exists := configMap.Data["startup.sh"]
				if !exists {
					return fmt.Errorf("startup.sh not found in ConfigMap")
				}

				// Check for unified bootstrap approach
				if !containsString(startupScript, "Using unified bootstrap discovery approach") {
					return fmt.Errorf("startup script does not contain unified bootstrap approach")
				}

				// Check for proper minimum primaries setting for 2-node cluster
				if !containsString(startupScript, "MIN_PRIMARIES=2") {
					return fmt.Errorf("startup script does not set MIN_PRIMARIES=2 for 2-node cluster")
				}

				// Check for Kubernetes service discovery
				if !containsString(startupScript, "dbms.cluster.discovery.resolver_type=K8S") {
					return fmt.Errorf("startup script does not contain Kubernetes service discovery")
				}

				// Check for proper label selector
				expectedLabelSelector := fmt.Sprintf("dbms.kubernetes.label_selector=neo4j.com/cluster=%s", clusterName)
				if !containsString(startupScript, expectedLabelSelector) {
					return fmt.Errorf("startup script does not contain proper label selector")
				}

				// CRITICAL: Check for V2_ONLY discovery configuration fix
				// This ensures the fix for Neo4j cluster formation is working
				if !containsString(startupScript, "dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery") {
					return fmt.Errorf("startup script does not contain V2_ONLY discovery fix (tcp-discovery port)")
				}

				if !containsString(startupScript, "dbms.cluster.discovery.version=V2_ONLY") {
					return fmt.Errorf("startup script does not contain V2_ONLY discovery mode")
				}

				// Verify that the wrong port configuration is NOT used
				if containsString(startupScript, "service_port_name=tcp-tx") {
					return fmt.Errorf("startup script incorrectly uses tcp-tx port (should use tcp-discovery)")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for StatefulSet to be created")
			statefulSetKey := types.NamespacedName{
				Name:      clusterName + "-primary",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				statefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, statefulSetKey, statefulSet); err != nil {
					return err
				}

				// Verify StatefulSet is configured for 2 replicas
				if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 2 {
					return fmt.Errorf("StatefulSet should have 2 replicas, got %v", statefulSet.Spec.Replicas)
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for both pods to be created and become ready")
			Eventually(func() error {
				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList, client.InNamespace(namespace.Name),
					client.MatchingLabels{"neo4j.com/cluster": clusterName}); err != nil {
					return err
				}

				if len(podList.Items) != 2 {
					return fmt.Errorf("expected 2 pods, got %d", len(podList.Items))
				}

				readyPods := 0
				for _, pod := range podList.Items {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
							readyPods++
							break
						}
					}
				}

				if readyPods != 2 {
					return fmt.Errorf("expected 2 ready pods, got %d", readyPods)
				}

				return nil
			}, time.Minute*5, time.Second*10).Should(Succeed())

			By("Verifying cluster status is properly reported")
			Eventually(func() error {
				updatedCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, updatedCluster); err != nil {
					return err
				}

				// Check if cluster has proper status conditions
				if len(updatedCluster.Status.Conditions) == 0 {
					return fmt.Errorf("cluster should have status conditions")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})

	Context("Three-Node Cluster Formation", func() {
		It("should create a cluster with quorum-based minimum primaries", func() {
			By("Creating a 3-primary cluster specification")
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
						Secondaries: 0,
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

			By("Waiting for ConfigMap with quorum-based minimum primaries logic")
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

				// Check for quorum logic (3 primaries should require 2 for bootstrap)
				if !containsString(startupScript, "MIN_PRIMARIES=$((TOTAL_PRIMARIES / 2 + 1))") {
					return fmt.Errorf("startup script does not contain quorum logic for 3+ node cluster")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
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

			By("Waiting for ConfigMap with cluster configuration")
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

				// Check for proper cluster configuration
				if !containsString(startupScript, "dbms.cluster.discovery.resolver_type=K8S") {
					return fmt.Errorf("startup script does not contain Kubernetes service discovery")
				}

				// Check for V2_ONLY discovery mode
				if !containsString(startupScript, "dbms.cluster.discovery.version=V2_ONLY") {
					return fmt.Errorf("startup script does not contain V2_ONLY discovery mode")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for both primary and secondary StatefulSets to be created")
			primaryStatefulSetKey := types.NamespacedName{
				Name:      clusterName + "-primary",
				Namespace: namespace.Name,
			}
			secondaryStatefulSetKey := types.NamespacedName{
				Name:      clusterName + "-secondary",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				primaryStatefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, primaryStatefulSetKey, primaryStatefulSet); err != nil {
					return fmt.Errorf("primary StatefulSet not found: %v", err)
				}

				secondaryStatefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, secondaryStatefulSetKey, secondaryStatefulSet); err != nil {
					return fmt.Errorf("secondary StatefulSet not found: %v", err)
				}

				// Verify StatefulSet replicas
				if primaryStatefulSet.Spec.Replicas == nil || *primaryStatefulSet.Spec.Replicas != 1 {
					return fmt.Errorf("primary StatefulSet should have 1 replica, got %v", primaryStatefulSet.Spec.Replicas)
				}

				if secondaryStatefulSet.Spec.Replicas == nil || *secondaryStatefulSet.Spec.Replicas != 1 {
					return fmt.Errorf("secondary StatefulSet should have 1 replica, got %v", secondaryStatefulSet.Spec.Replicas)
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
