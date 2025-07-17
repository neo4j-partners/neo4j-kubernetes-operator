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

package controller_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jEnterpriseStandalone Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 250
	)

	var (
		ctx            context.Context
		standalone     *neo4jv1alpha1.Neo4jEnterpriseStandalone
		standaloneName string
		namespaceName  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		standaloneName = fmt.Sprintf("test-standalone-%d", time.Now().UnixNano())
		namespaceName = "default"

		// Create basic standalone spec
		standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      standaloneName,
				Namespace: namespaceName,
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
				Env: []corev1.EnvVar{
					{
						Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
						Value: "eval",
					},
				},
			},
		}
	})

	AfterEach(func() {
		if standalone != nil {
			// Clean up the standalone deployment and related resources
			if err := k8sClient.Delete(ctx, standalone, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				// Log the error but don't fail the test cleanup
				fmt.Printf("Warning: Failed to delete standalone during cleanup: %v\n", err)
			}

			// Wait for standalone to be deleted, but don't fail the test if it takes longer
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: standaloneName, Namespace: namespaceName}, standalone)
				if errors.IsNotFound(err) {
					return true
				}
				if err != nil {
					fmt.Printf("Error getting standalone during cleanup: %v\n", err)
					return false
				}

				// If standalone is stuck with finalizers, force remove them
				if standalone.DeletionTimestamp != nil && len(standalone.Finalizers) > 0 {
					fmt.Printf("Standalone is stuck with finalizers: %v, forcing removal\n", standalone.Finalizers)
					standalone.Finalizers = []string{}
					if err := k8sClient.Update(ctx, standalone); err != nil {
						fmt.Printf("Failed to remove finalizers: %v\n", err)
					}
				}

				return false
			}, time.Second*60, interval).Should(BeTrue(), "Standalone should be deleted within 60 seconds")
		}
	})

	Context("When creating a basic Neo4j Enterprise Standalone", func() {
		It("Should create standalone deployment successfully", func() {
			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			By("Waiting for ConfigMap to be created by the controller")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName + "-config",
					Namespace: namespaceName,
				}, configMap)
				if err != nil {
					return false
				}

				// Verify neo4j.conf exists
				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return false
				}

				// Check that basic configuration exists
				return len(neo4jConf) > 0 && containsString(neo4jConf, "server.default_listen_address")
			}, timeout, interval).Should(BeTrue())

			By("Waiting for StatefulSet to be created")
			Eventually(func() bool {
				statefulSet := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespaceName,
				}, statefulSet)
				if err != nil {
					return false
				}

				// Verify StatefulSet has 1 replica
				return statefulSet.Spec.Replicas != nil && *statefulSet.Spec.Replicas == 1
			}, timeout, interval).Should(BeTrue())

			By("Verifying that Service is created")
			Eventually(func() bool {
				service := &corev1.Service{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName + "-service",
					Namespace: namespaceName,
				}, service)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying that ConfigMap contains no clustering configurations")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName + "-config",
					Namespace: namespaceName,
				}, configMap)
				if err != nil {
					return false
				}

				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return false
				}

				// Verify no clustering configurations are present
				return !containsString(neo4jConf, "dbms.cluster.") &&
					!containsString(neo4jConf, "dbms.kubernetes.")
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When creating a standalone with custom configuration", func() {
		It("Should merge custom config with single mode", func() {
			By("Adding custom configuration")
			standalone.Spec.Config = map[string]string{
				"server.memory.heap.initial_size": "1G",
				"server.memory.heap.max_size":     "2G",
				"dbms.logs.query.enabled":         "true",
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			By("Waiting for ConfigMap with merged configuration")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName + "-config",
					Namespace: namespaceName,
				}, configMap)
				if err != nil {
					return false
				}

				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return false
				}

				// Verify custom config is present
				return containsString(neo4jConf, "server.memory.heap.initial_size=1G") &&
					containsString(neo4jConf, "server.memory.heap.max_size=2G") &&
					containsString(neo4jConf, "dbms.logs.query.enabled=true")
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When creating a standalone with TLS configuration", func() {
		It("Should handle TLS configuration properly", func() {
			By("Adding TLS configuration")
			standalone.Spec.TLS = &neo4jv1alpha1.TLSSpec{
				Mode: "disabled",
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			By("Waiting for ConfigMap with TLS configuration")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName + "-config",
					Namespace: namespaceName,
				}, configMap)
				if err != nil {
					return false
				}

				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return false
				}

				// Verify basic configuration exists (TLS is disabled by default)
				return len(neo4jConf) > 0 && containsString(neo4jConf, "server.default_listen_address")
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When creating a standalone with resource limits", func() {
		It("Should respect resource configuration", func() {
			By("Adding resource limits")
			standalone.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    *parseQuantity("500m"),
					corev1.ResourceMemory: *parseQuantity("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    *parseQuantity("2"),
					corev1.ResourceMemory: *parseQuantity("4Gi"),
				},
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			By("Waiting for StatefulSet with resource limits")
			Eventually(func() bool {
				statefulSet := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespaceName,
				}, statefulSet)
				if err != nil {
					return false
				}

				// Verify resource limits are set
				if len(statefulSet.Spec.Template.Spec.Containers) == 0 {
					return false
				}

				container := statefulSet.Spec.Template.Spec.Containers[0]
				return container.Resources.Requests.Cpu().String() == "500m" &&
					container.Resources.Requests.Memory().String() == "2Gi" &&
					container.Resources.Limits.Cpu().String() == "2" &&
					container.Resources.Limits.Memory().String() == "4Gi"
			}, timeout, interval).Should(BeTrue())
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

// Helper function to parse resource quantities
func parseQuantity(value string) *resource.Quantity {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse quantity %s: %v", value, err))
	}
	return &q
}
