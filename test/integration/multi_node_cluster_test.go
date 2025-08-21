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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Multi-Node Cluster Formation Integration Tests", func() {
	var (
		ctx         context.Context
		namespace   *corev1.Namespace
		clusterName string
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Create a new namespace for each test
		namespaceName := createTestNamespace("multinode")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}

		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().Unix())

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
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2, // Minimum cluster topology
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

				// Verify bootstrapping strategy configuration is present
				if !containsString(startupScript, "internal.dbms.cluster.discovery.system_bootstrapping_strategy=$BOOTSTRAP_STRATEGY") {
					return fmt.Errorf("startup script does not contain bootstrapping strategy configuration")
				}

				// Verify server-0 gets "me" strategy and others get "other"
				if !containsString(startupScript, "SERVER_INDEX=\"0\"") && !containsString(startupScript, "BOOTSTRAP_STRATEGY=\"me\"") {
					return fmt.Errorf("startup script does not contain server-0 bootstrapping strategy logic")
				}

				if !containsString(startupScript, "BOOTSTRAP_STRATEGY=\"other\"") {
					return fmt.Errorf("startup script does not contain other servers bootstrapping strategy")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for single server StatefulSet to be created")
			// CURRENT ARCHITECTURE: Single StatefulSet with multiple replicas
			Eventually(func() error {
				// Check single server StatefulSet
				serverKey := types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}
				statefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, serverKey, statefulSet); err != nil {
					return fmt.Errorf("server StatefulSet not found: %v", err)
				}

				if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 2 {
					return fmt.Errorf("server StatefulSet should have 2 replicas, got %v", statefulSet.Spec.Replicas)
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Verifying pods are created for minimal cluster")
			Eventually(func() error {
				// Check server pods (unified architecture)
				serverPods := &corev1.PodList{}
				if err := k8sClient.List(ctx, serverPods, client.InNamespace(namespace.Name),
					client.MatchingLabels{"app.kubernetes.io/name": "neo4j", "app.kubernetes.io/instance": clusterName}); err != nil {
					return err
				}

				if len(serverPods.Items) != 2 {
					return fmt.Errorf("expected 2 server pods, got %d", len(serverPods.Items))
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
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2, // 1 + 1 total servers
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
