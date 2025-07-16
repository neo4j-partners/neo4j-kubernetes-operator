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
		// Cleanup will be handled by the test suite cleanup
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
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
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
				if containsString(neo4jConf, "dbms.mode=SINGLE") {
					return fmt.Errorf("ConfigMap should not contain deprecated dbms.mode=SINGLE")
				}

				// Verify basic server configuration is present
				if !containsString(neo4jConf, "# Neo4j Standalone Configuration") {
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

				// Check if standalone has proper status conditions
				if len(updatedStandalone.Status.Conditions) == 0 {
					return fmt.Errorf("standalone should have status conditions")
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
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "2Gi",
					},
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
				if containsString(neo4jConf, "dbms.mode=SINGLE") {
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
					if !containsString(neo4jConf, config) {
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
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "2Gi",
					},
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
				if containsString(neo4jConf, "dbms.ssl.policy") {
					return fmt.Errorf("ConfigMap should not contain SSL policy configuration when TLS is disabled")
				}

				// Verify HTTPS is not explicitly enabled
				if containsString(neo4jConf, "server.https.enabled=true") {
					return fmt.Errorf("ConfigMap should not enable HTTPS when TLS is disabled")
				}

				// Verify Bolt TLS level is not set to REQUIRED
				if containsString(neo4jConf, "server.bolt.tls_level=REQUIRED") {
					return fmt.Errorf("ConfigMap should not require Bolt TLS when TLS is disabled")
				}

				return nil
			}, time.Minute*2, time.Second*5).Should(Succeed())
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
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "2Gi",
					},
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
					if !containsString(neo4jConf, config) {
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
