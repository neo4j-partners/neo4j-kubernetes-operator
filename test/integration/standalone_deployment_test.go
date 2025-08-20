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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jEnterpriseStandalone Integration Tests", func() {
	var (
		ctx            context.Context
		namespace      *corev1.Namespace
		standalone     *neo4jv1alpha1.Neo4jEnterpriseStandalone
		standaloneName string
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create test namespace
		namespaceName := createTestNamespace("standalone")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}

		standaloneName = fmt.Sprintf("test-standalone-%d", time.Now().Unix())
	})

	AfterEach(func() {
		// Clean up standalone resource if it was created
		if standalone != nil {
			By("Cleaning up standalone resource")
			cleanupResource(standalone, namespace.Name, "Neo4jEnterpriseStandalone")
		}

		// Force cleanup of PVCs to free storage resources
		By("Cleaning up PVCs")
		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := k8sClient.List(ctx, pvcList, client.InNamespace(namespace.Name)); err == nil {
			for _, pvc := range pvcList.Items {
				By(fmt.Sprintf("Deleting PVC: %s", pvc.Name))
				_ = k8sClient.Delete(ctx, &pvc)
			}
		}

		// Give a moment for resources to be freed
		time.Sleep(2 * time.Second)

		// Note: Namespace cleanup is handled by the test suite cleanup
		// which removes all test namespaces and their resources
	})

	Context("Basic Standalone Deployment", func() {
		It("should create a standalone Neo4j instance successfully", func() {
			By("Creating a basic standalone specification")
			standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26-enterprise",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			By("Waiting for ConfigMap to be created with single mode configuration")
			configMapKey := types.NamespacedName{
				Name:      standaloneName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				// Verify Neo4j configuration contains single mode
				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return fmt.Errorf("neo4j.conf not found in ConfigMap")
				}

				// Verify no deprecated dbms.mode configuration
				if strings.Contains(neo4jConf, "dbms.mode=SINGLE") {
					return fmt.Errorf("ConfigMap should not contain deprecated dbms.mode=SINGLE")
				}

				// Verify basic server configuration is present
				if !strings.Contains(neo4jConf, "# Neo4j Standalone Configuration") {
					return fmt.Errorf("ConfigMap should contain configuration header")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for StatefulSet to be created")
			statefulSetKey := types.NamespacedName{
				Name:      standaloneName,
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				statefulSet := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, statefulSetKey, statefulSet); err != nil {
					return err
				}

				// Verify StatefulSet is configured for 1 replica
				if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 {
					return fmt.Errorf("StatefulSet should have 1 replica, got %v", statefulSet.Spec.Replicas)
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for Service to be created")
			serviceKey := types.NamespacedName{
				Name:      standaloneName + "-service",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				service := &corev1.Service{}
				if err := k8sClient.Get(ctx, serviceKey, service); err != nil {
					return err
				}

				// Verify service has proper ports
				expectedPorts := []int32{7687, 7474}
				servicePorts := make([]int32, len(service.Spec.Ports))
				for i, port := range service.Spec.Ports {
					servicePorts[i] = port.Port
				}

				for _, expectedPort := range expectedPorts {
					found := false
					for _, servicePort := range servicePorts {
						if servicePort == expectedPort {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("Service should have port %d", expectedPort)
					}
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for Pod to be created and become ready")
			Eventually(func() error {
				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList, client.InNamespace(namespace.Name),
					client.MatchingLabels{"app": standaloneName}); err != nil {
					return err
				}

				if len(podList.Items) != 1 {
					return fmt.Errorf("expected 1 pod, got %d", len(podList.Items))
				}

				pod := podList.Items[0]

				// Log pod status for debugging in CI
				GinkgoWriter.Printf("Pod %s status: Phase=%s, Reason=%s, Message=%s\n",
					pod.Name, pod.Status.Phase, pod.Status.Reason, pod.Status.Message)

				// Check for pending pods with more details
				if pod.Status.Phase == corev1.PodPending {
					// Check scheduling issues
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
							GinkgoWriter.Printf("Pod not scheduled: %s - %s\n", condition.Reason, condition.Message)
						}
					}

					// Check container statuses
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.State.Waiting != nil {
							GinkgoWriter.Printf("Container %s waiting: %s - %s\n",
								cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
						}
					}

					// Check events
					eventList := &corev1.EventList{}
					if err := k8sClient.List(ctx, eventList, client.InNamespace(namespace.Name)); err == nil {
						for _, event := range eventList.Items {
							if event.InvolvedObject.Name == pod.Name {
								GinkgoWriter.Printf("Pod event: %s - %s\n", event.Reason, event.Message)
							}
						}
					}
				}

				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						return nil
					}
				}

				return fmt.Errorf("pod is not ready")
			}, time.Minute*3, time.Second*10).Should(Succeed())

			By("Verifying standalone status is properly reported")
			Eventually(func() error {
				updatedStandalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, updatedStandalone); err != nil {
					return err
				}

				// Check if standalone has proper status
				if updatedStandalone.Status.Phase == "" {
					return fmt.Errorf("standalone should have phase in status")
				}

				if updatedStandalone.Status.Ready != true {
					return fmt.Errorf("standalone should be ready, current phase: %s", updatedStandalone.Status.Phase)
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})

	Context("Standalone with Custom Configuration", func() {
		It("should merge custom configuration with single mode", func() {
			By("Creating a standalone with custom configuration")
			standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26-enterprise",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Config: map[string]string{
						"server.memory.heap.initial_size": "1G",
						"server.memory.heap.max_size":     "2G",
						"dbms.logs.query.enabled":         "true",
						"dbms.logs.query.threshold":       "1s",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			By("Waiting for ConfigMap with merged configuration")
			configMapKey := types.NamespacedName{
				Name:      standaloneName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return fmt.Errorf("neo4j.conf not found in ConfigMap")
				}

				// Verify no deprecated dbms.mode configuration
				if strings.Contains(neo4jConf, "dbms.mode=SINGLE") {
					return fmt.Errorf("ConfigMap should not contain deprecated dbms.mode=SINGLE")
				}

				// Verify custom configuration is merged
				customConfigs := []string{
					"server.memory.heap.initial_size=1G",
					"server.memory.heap.max_size=2G",
					"dbms.logs.query.enabled=true",
					"dbms.logs.query.threshold=1s",
				}

				for _, config := range customConfigs {
					if !strings.Contains(neo4jConf, config) {
						return fmt.Errorf("ConfigMap does not contain custom configuration: %s", config)
					}
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})

	Context("Standalone with TLS Disabled", func() {
		It("should configure TLS disabled settings properly", func() {
			By("Creating a standalone with TLS disabled")
			standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26-enterprise",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "disabled",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			By("Waiting for ConfigMap with TLS configuration")
			configMapKey := types.NamespacedName{
				Name:      standaloneName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return fmt.Errorf("neo4j.conf not found in ConfigMap")
				}

				// Verify TLS is disabled (no SSL policies configured)
				if strings.Contains(neo4jConf, "dbms.ssl.policy") {
					return fmt.Errorf("ConfigMap should not contain SSL policy configuration when TLS is disabled")
				}

				// Verify HTTPS is not explicitly enabled
				if strings.Contains(neo4jConf, "server.https.enabled=true") {
					return fmt.Errorf("ConfigMap should not enable HTTPS when TLS is disabled")
				}

				// Verify Bolt TLS level is not set to REQUIRED
				if strings.Contains(neo4jConf, "server.bolt.tls_level=REQUIRED") {
					return fmt.Errorf("ConfigMap should not require Bolt TLS when TLS is disabled")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})

	Context("Standalone with Database Creation", func() {
		It("should support creating databases in standalone deployment", func() {
			By("Creating a standalone with authentication secret")

			// Create admin secret first
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "standalone-admin-secret",
					Namespace: namespace.Name,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("admin123"),
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26-enterprise",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "standalone-admin-secret",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			By("Waiting for standalone to become ready")
			Eventually(func() error {
				updatedStandalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, updatedStandalone); err != nil {
					return err
				}

				if updatedStandalone.Status.Ready != true {
					return fmt.Errorf("standalone not ready, current phase: %s", updatedStandalone.Status.Phase)
				}

				return nil
			}, time.Minute*5, time.Second*10).Should(Succeed())

			By("Creating a database resource that references the standalone")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-database-standalone",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  standaloneName, // References standalone resource
					Name:        "teststandalonedb",
					Wait:        true,
					IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database should be accepted and validated for standalone")
			Eventually(func() error {
				updatedDatabase := &neo4jv1alpha1.Neo4jDatabase{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: database.Namespace,
				}, updatedDatabase); err != nil {
					return err
				}

				// Database should exist without validation errors
				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Cleaning up database resource")
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})
	})

	Context("Standalone with TLS Enabled", func() {
		It("should configure TLS enabled settings properly", func() {
			By("Creating a standalone with TLS enabled")
			standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26-enterprise",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}

			By("Creating the standalone resource")
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			By("Waiting for Certificate to be created")
			Eventually(func() error {
				certList := &unstructured.UnstructuredList{}
				certList.SetAPIVersion("cert-manager.io/v1")
				certList.SetKind("Certificate")

				if err := k8sClient.List(ctx, certList, client.InNamespace(namespace.Name)); err != nil {
					return err
				}

				if len(certList.Items) == 0 {
					return fmt.Errorf("no certificates found")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for ConfigMap with TLS configuration")
			configMapKey := types.NamespacedName{
				Name:      standaloneName + "-config",
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return err
				}

				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					return fmt.Errorf("neo4j.conf not found in ConfigMap")
				}

				// Verify TLS is enabled
				expectedTLSConfigs := []string{
					"server.https.enabled=true",
					"server.bolt.tls_level=REQUIRED",
					"dbms.ssl.policy.https.enabled=true",
					"dbms.ssl.policy.bolt.enabled=true",
					"dbms.ssl.policy.https.base_directory=/ssl",
					"dbms.ssl.policy.bolt.base_directory=/ssl",
				}

				for _, config := range expectedTLSConfigs {
					if !strings.Contains(neo4jConf, config) {
						return fmt.Errorf("ConfigMap does not contain TLS configuration: %s", config)
					}
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Waiting for TLS Secret to be created")
			Eventually(func() error {
				secretList := &corev1.SecretList{}
				if err := k8sClient.List(ctx, secretList, client.InNamespace(namespace.Name)); err != nil {
					return err
				}

				// Look for TLS secret
				for _, secret := range secretList.Items {
					if secret.Type == corev1.SecretTypeTLS {
						return nil
					}
				}

				return fmt.Errorf("no TLS secret found")
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})
})
