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

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Neo4jEnterpriseStandalone Integration Tests", func() {

	// ── Behavior tests: share one standalone that waits for Ready ──
	Context("Standalone Behavior (Pod Ready)", Ordered, func() {
		var (
			ctx            context.Context
			namespaceName  string
			standaloneName string
			standalone     *neo4jv1beta1.Neo4jEnterpriseStandalone
		)

		BeforeAll(func() {
			ctx = context.Background()
			namespaceName = createTestNamespace("standalone-behav")
			standaloneName = fmt.Sprintf("test-standalone-%d", time.Now().Unix())

			By("Creating admin secret")
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "standalone-admin-secret",
					Namespace: namespaceName,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("admin123"),
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			By("Creating standalone resource")
			standalone = &neo4jv1beta1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespaceName,
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        getNeo4jImageTag(),
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: getCIAppropriateResourceRequirements(),
					Auth: &neo4jv1beta1.AuthSpec{
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
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			By("Waiting for standalone to become Ready")
			Eventually(func() bool {
				updated := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: standaloneName, Namespace: namespaceName,
				}, updated); err != nil {
					return false
				}
				if updated.Status.Phase == "Ready" {
					GinkgoWriter.Printf("Standalone is ready. Phase: %s\n", updated.Status.Phase)
					return true
				}
				GinkgoWriter.Printf("Standalone not yet ready. Phase: %s\n", updated.Status.Phase)
				return false
			}, timeout, interval).Should(BeTrue())
		})

		AfterAll(func() {
			if standalone != nil {
				cleanupResource(standalone, namespaceName, "Neo4jEnterpriseStandalone")
			}
			pvcList := &corev1.PersistentVolumeClaimList{}
			if err := k8sClient.List(ctx, pvcList, client.InNamespace(namespaceName)); err == nil {
				for _, pvc := range pvcList.Items {
					_ = k8sClient.Delete(ctx, &pvc)
				}
			}
		})

		It("should create a standalone Neo4j instance successfully", func() {
			By("Verifying ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: standaloneName + "-config", Namespace: namespaceName,
			}, configMap)).To(Succeed())
			neo4jConf := configMap.Data["neo4j.conf"]
			Expect(neo4jConf).To(ContainSubstring("server.default_listen_address"))

			By("Verifying StatefulSet has 1 replica")
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: standaloneName, Namespace: namespaceName,
			}, sts)).To(Succeed())
			Expect(*sts.Spec.Replicas).To(Equal(int32(1)))

			By("Verifying Service was created")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: standaloneName + "-service", Namespace: namespaceName,
			}, svc)).To(Succeed())

			By("Verifying standalone status is Ready")
			updated := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: standaloneName, Namespace: namespaceName,
			}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal("Ready"))
		})

		It("should support creating databases in standalone deployment", func() {
			By("Creating a database resource that references the standalone")
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-database-standalone",
					Namespace: namespaceName,
				},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef:  standaloneName,
					Name:        "teststandalonedb",
					Wait:        true,
					IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database should be accepted and validated for standalone")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: database.Name, Namespace: namespaceName,
				}, &neo4jv1beta1.Neo4jDatabase{})
			}, timeout, interval).Should(Succeed())

			By("Cleaning up database resource")
			cleanupResource(database, namespaceName, "Neo4jDatabase")
		})
	})

	// ── ConfigMap-only tests: fast, don't need pod Ready ──
	Context("Standalone with Custom Configuration", func() {
		It("should merge custom configuration with single mode", func() {
			ctx := context.Background()
			namespaceName := createTestNamespace("standalone-cfg")
			standaloneName := fmt.Sprintf("test-standalone-%d", time.Now().Unix())

			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: namespaceName},
				StringData: map[string]string{"username": "neo4j", "password": "admin123"},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: standaloneName, Namespace: namespaceName},
				Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
					Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag(), PullPolicy: "IfNotPresent"},
					Storage:   neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "500Mi"},
					Resources: getCIAppropriateResourceRequirements(),
					Config: map[string]string{
						"server.memory.heap.initial_size": "1G",
						"server.memory.heap.max_size":     "2G",
						"db.logs.query.enabled":           "true",
					},
					Env: []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
				},
			}
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())
			defer cleanupResource(standalone, namespaceName, "Neo4jEnterpriseStandalone")

			By("Waiting for ConfigMap with merged configuration")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: standaloneName + "-config", Namespace: namespaceName,
				}, configMap); err != nil {
					return err
				}
				neo4jConf := configMap.Data["neo4j.conf"]
				if neo4jConf == "" {
					return fmt.Errorf("neo4j.conf not found")
				}
				for _, s := range []string{
					"server.memory.heap.initial_size=1G",
					"server.memory.heap.max_size=2G",
					"db.logs.query.enabled=true",
				} {
					if !strings.Contains(neo4jConf, s) {
						return fmt.Errorf("missing config: %s", s)
					}
				}
				if strings.Contains(neo4jConf, "dbms.mode=SINGLE") {
					return fmt.Errorf("should not contain deprecated dbms.mode=SINGLE")
				}
				return nil
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("Standalone with TLS Disabled", func() {
		It("should configure TLS disabled settings properly", func() {
			ctx := context.Background()
			namespaceName := createTestNamespace("standalone-notls")
			standaloneName := fmt.Sprintf("test-standalone-%d", time.Now().Unix())

			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: namespaceName},
				StringData: map[string]string{"username": "neo4j", "password": "admin123"},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: standaloneName, Namespace: namespaceName},
				Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
					Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag(), PullPolicy: "IfNotPresent"},
					Storage:   neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "500Mi"},
					Resources: getCIAppropriateResourceRequirements(),
					TLS:       &neo4jv1beta1.TLSSpec{Mode: "disabled"},
					Env:       []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
				},
			}
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())
			defer cleanupResource(standalone, namespaceName, "Neo4jEnterpriseStandalone")

			By("Waiting for ConfigMap with TLS disabled configuration")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: standaloneName + "-config", Namespace: namespaceName,
				}, configMap); err != nil {
					return err
				}
				neo4jConf := configMap.Data["neo4j.conf"]
				if strings.Contains(neo4jConf, "dbms.ssl.policy") {
					return fmt.Errorf("should not contain SSL policy when TLS disabled")
				}
				if strings.Contains(neo4jConf, "server.https.enabled=true") {
					return fmt.Errorf("should not enable HTTPS when TLS disabled")
				}
				if strings.Contains(neo4jConf, "server.bolt.tls_level=REQUIRED") {
					return fmt.Errorf("should not require bolt TLS when TLS disabled")
				}
				return nil
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("Standalone with TLS Enabled", func() {
		It("should configure TLS enabled settings properly", func() {
			ctx := context.Background()
			namespaceName := createTestNamespace("standalone-tls")
			standaloneName := fmt.Sprintf("test-standalone-%d", time.Now().Unix())

			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: namespaceName},
				StringData: map[string]string{"username": "neo4j", "password": "admin123"},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: standaloneName, Namespace: namespaceName},
				Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
					Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag(), PullPolicy: "IfNotPresent"},
					Storage:   neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "500Mi"},
					Resources: getCIAppropriateResourceRequirements(),
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
					},
					Env: []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
				},
			}
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())
			defer cleanupResource(standalone, namespaceName, "Neo4jEnterpriseStandalone")

			By("Waiting for Certificate to be created")
			Eventually(func() error {
				certList := &unstructured.UnstructuredList{}
				certList.SetAPIVersion("cert-manager.io/v1")
				certList.SetKind("Certificate")
				if err := k8sClient.List(ctx, certList, client.InNamespace(namespaceName)); err != nil {
					return err
				}
				if len(certList.Items) == 0 {
					return fmt.Errorf("no certificates found")
				}
				return nil
			}, timeout, interval).Should(Succeed())

			By("Waiting for ConfigMap with TLS configuration")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: standaloneName + "-config", Namespace: namespaceName,
				}, configMap); err != nil {
					return err
				}
				neo4jConf := configMap.Data["neo4j.conf"]
				for _, s := range []string{
					"server.https.enabled=true",
					"server.bolt.tls_level=REQUIRED",
					"dbms.ssl.policy.https.enabled=true",
					"dbms.ssl.policy.bolt.enabled=true",
					"dbms.ssl.policy.https.base_directory=/ssl",
					"dbms.ssl.policy.bolt.base_directory=/ssl",
				} {
					if !strings.Contains(neo4jConf, s) {
						return fmt.Errorf("missing TLS config: %s", s)
					}
				}
				return nil
			}, timeout, interval).Should(Succeed())

			By("Waiting for TLS Secret to be created")
			Eventually(func() error {
				secretList := &corev1.SecretList{}
				if err := k8sClient.List(ctx, secretList, client.InNamespace(namespaceName)); err != nil {
					return err
				}
				for _, secret := range secretList.Items {
					if secret.Type == corev1.SecretTypeTLS {
						return nil
					}
				}
				return fmt.Errorf("no TLS secret found")
			}, timeout, interval).Should(Succeed())
		})
	})
})
