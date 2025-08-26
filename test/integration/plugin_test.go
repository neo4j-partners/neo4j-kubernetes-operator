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
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jPlugin Integration Tests", func() {
	const (
		timeout  = time.Second * 900 // 15 minutes for CI cluster provisioning + plugin installation
		interval = time.Second * 5
	)

	Context("Plugin Installation on Cluster", func() {
		It("Should install APOC plugin on Neo4jEnterpriseCluster", func() {
			ctx := context.Background()
			namespace := createUniqueNamespace()

			By("Creating namespace")
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating admin secret")
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			By("Creating Neo4jEnterpriseCluster")
			clusterName := "plugin-test-cluster"
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for cluster to be ready")
			Eventually(func() string {
				currentCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, currentCluster)
				if err != nil {
					return ""
				}
				return currentCluster.Status.Phase
			}, timeout, interval).Should(Equal("Ready"))

			By("Verifying server StatefulSet exists with correct name")
			serverSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}, serverSts)
			}, timeout, interval).Should(Succeed())
			Expect(*serverSts.Spec.Replicas).To(Equal(int32(2)))

			By("Creating APOC plugin for cluster")
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "apoc-plugin",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: clusterName,
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Source: &neo4jv1alpha1.PluginSource{
						Type: "official",
					},
					// APOC configuration in Neo4j 5.26+ is handled via environment variables only
					// These settings will be converted to NEO4J_APOC_EXPORT_FILE_ENABLED and NEO4J_APOC_IMPORT_FILE_ENABLED
					Config: map[string]string{
						"apoc.export.file.enabled": "true",
						"apoc.import.file.enabled": "true",
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Waiting for plugin status to be Ready")
			Eventually(func() string {
				currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "apoc-plugin",
					Namespace: namespace.Name,
				}, currentPlugin)
				if err != nil {
					return ""
				}
				return currentPlugin.Status.Phase
			}, timeout, interval).Should(Equal("Ready"))

			By("Verifying StatefulSet has NEO4J_PLUGINS environment variable")
			serverSts = &appsv1.StatefulSet{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}, serverSts)
				if err != nil {
					return false
				}

				// Find Neo4j container and check for NEO4J_PLUGINS env var
				for _, container := range serverSts.Spec.Template.Spec.Containers {
					if container.Name == "neo4j" {
						for _, env := range container.Env {
							if env.Name == "NEO4J_PLUGINS" && strings.Contains(env.Value, "apoc") {
								return true
							}
						}
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Verifying APOC configuration environment variables (Neo4j 5.26+ approach)")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}, serverSts)
				if err != nil {
					return false
				}

				// Check for APOC configuration env vars - this is the only way to configure APOC in Neo4j 5.26+
				// APOC settings are no longer supported in neo4j.conf, only via environment variables
				for _, container := range serverSts.Spec.Template.Spec.Containers {
					if container.Name == "neo4j" {
						hasExportEnabled := false
						hasImportEnabled := false
						for _, env := range container.Env {
							if env.Name == "NEO4J_APOC_EXPORT_FILE_ENABLED" && env.Value == "true" {
								hasExportEnabled = true
							}
							if env.Name == "NEO4J_APOC_IMPORT_FILE_ENABLED" && env.Value == "true" {
								hasImportEnabled = true
							}
						}
						return hasExportEnabled && hasImportEnabled
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, plugin)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, cluster)).Should(Succeed())
		})
	})

	Context("Plugin Installation on Standalone", func() {
		It("Should install GDS plugin on Neo4jEnterpriseStandalone", func() {
			ctx := context.Background()
			namespace := createUniqueNamespace()

			By("Creating namespace")
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating admin secret")
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			By("Creating Neo4jEnterpriseStandalone")
			standaloneName := "plugin-test-standalone"
			standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			By("Waiting for standalone to be ready")
			Eventually(func() string {
				currentStandalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, currentStandalone)
				if err != nil {
					return ""
				}
				return currentStandalone.Status.Phase
			}, timeout, interval).Should(Equal("Running"))

			By("Verifying standalone StatefulSet exists with correct name")
			standaloneSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, standaloneSts)
			}, timeout, interval).Should(Succeed())
			Expect(*standaloneSts.Spec.Replicas).To(Equal(int32(1)))

			By("Creating GDS plugin for standalone with dependency")
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gds-plugin",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: standaloneName,
					Name:       "graph-data-science",
					Version:    "2.10.0",
					Enabled:    true,
					Source: &neo4jv1alpha1.PluginSource{
						Type: "community",
					},
					Dependencies: []neo4jv1alpha1.PluginDependency{
						{
							Name:              "apoc",
							VersionConstraint: ">=5.26.0",
							Optional:          false,
						},
					},
					// GDS configuration goes through neo4j.conf (not environment variables like APOC)
					Config: map[string]string{
						"gds.enterprise.license_file": "/licenses/gds.license",
					},
					Security: &neo4jv1alpha1.PluginSecurity{
						AllowedProcedures: []string{"gds.*", "apoc.load.*"},
						Sandbox:           true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Waiting for plugin status to be Installing")
			Eventually(func() string {
				currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "gds-plugin",
					Namespace: namespace.Name,
				}, currentPlugin)
				if err != nil {
					return ""
				}
				return currentPlugin.Status.Phase
			}, timeout, interval).Should(SatisfyAny(
				Equal("Installing"),
				Equal("Ready"),
			))

			By("Verifying dependency plugins are included in NEO4J_PLUGINS")
			standaloneSts = &appsv1.StatefulSet{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, standaloneSts)
				if err != nil {
					return false
				}

				// Check that NEO4J_PLUGINS contains both gds and apoc (dependency)
				for _, container := range standaloneSts.Spec.Template.Spec.Containers {
					if container.Name == "neo4j" {
						for _, env := range container.Env {
							if env.Name == "NEO4J_PLUGINS" {
								return strings.Contains(env.Value, "graph-data-science") && strings.Contains(env.Value, "apoc")
							}
						}
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Verifying GDS procedure security settings are configured")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, standaloneSts)
				if err != nil {
					return false
				}

				// Check for GDS-specific security environment variables
				// GDS requires dbms.security.procedures.unrestricted=gds.*
				for _, container := range standaloneSts.Spec.Template.Spec.Containers {
					if container.Name == "neo4j" {
						hasUnrestricted := false
						for _, env := range container.Env {
							if env.Name == "NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED" && strings.Contains(env.Value, "gds.*") {
								hasUnrestricted = true
								break
							}
						}
						return hasUnrestricted
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, plugin)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, standalone)).Should(Succeed())
		})

		It("Should configure Bloom plugin with proper neo4j.conf settings", func() {
			ctx := context.Background()
			namespace := createUniqueNamespace()

			By("Creating namespace")
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating admin secret")
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			By("Creating Neo4jEnterpriseStandalone for Bloom test")
			standaloneName := "bloom-test-standalone"
			standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      standaloneName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Resources: getCIAppropriateResourceRequirements(),
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			By("Waiting for standalone to be ready")
			Eventually(func() string {
				currentStandalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, currentStandalone)
				if err != nil {
					return ""
				}
				return currentStandalone.Status.Phase
			}, timeout, interval).Should(Equal("Running"))

			By("Creating Bloom plugin with neo4j.conf configuration")
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bloom-plugin",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: standaloneName,
					Name:       "bloom",
					Version:    "2.15.0",
					Enabled:    true,
					Source: &neo4jv1alpha1.PluginSource{
						Type: "official",
					},
					// Bloom configuration goes through neo4j.conf (unlike APOC)
					Config: map[string]string{
						"dbms.bloom.license_file": "/licenses/bloom.license",
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Verifying Bloom procedure security settings are automatically configured")
			standaloneSts := &appsv1.StatefulSet{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, standaloneSts)
				if err != nil {
					return false
				}

				// Check for Bloom-specific security environment variables
				for _, container := range standaloneSts.Spec.Template.Spec.Containers {
					if container.Name == "neo4j" {
						hasUnrestricted := false
						hasHttpAuth := false
						hasUnmanagedExt := false

						for _, env := range container.Env {
							if env.Name == "NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED" && strings.Contains(env.Value, "bloom.*") {
								hasUnrestricted = true
							}
							if env.Name == "NEO4J_DBMS_SECURITY_HTTP_AUTH_ALLOWLIST" && strings.Contains(env.Value, "/bloom.*") {
								hasHttpAuth = true
							}
							if env.Name == "NEO4J_SERVER_UNMANAGED_EXTENSION_CLASSES" && strings.Contains(env.Value, "bloom.server=/bloom") {
								hasUnmanagedExt = true
							}
						}
						return hasUnrestricted && hasHttpAuth && hasUnmanagedExt
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, plugin)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, standalone)).Should(Succeed())
		})
	})

	Context("Plugin Error Handling", func() {
		It("Should handle non-existent deployment gracefully", func() {
			ctx := context.Background()
			namespace := createUniqueNamespace()

			By("Creating namespace")
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating plugin with non-existent clusterRef")
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphan-plugin",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "non-existent-deployment",
					Name:       "apoc",
					Version:    "5.26.0",
					Source: &neo4jv1alpha1.PluginSource{
						Type: "official",
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Verifying plugin status shows Failed")
			Eventually(func() string {
				currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "orphan-plugin",
					Namespace: namespace.Name,
				}, currentPlugin)
				if err != nil {
					return ""
				}
				return currentPlugin.Status.Phase
			}, timeout, interval).Should(Equal("Failed"))

			By("Verifying error message mentions deployment not found")
			currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "orphan-plugin",
				Namespace: namespace.Name,
			}, currentPlugin)
			Expect(err).NotTo(HaveOccurred())
			Expect(currentPlugin.Status.Message).To(ContainSubstring("not found"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, plugin)).Should(Succeed())
		})

		It("Should wait for deployment to be ready", func() {
			ctx := context.Background()
			namespace := createUniqueNamespace()

			By("Creating namespace")
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating admin secret")
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			By("Creating Neo4jEnterpriseCluster in Pending state")
			clusterName := "pending-cluster"
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			// Don't wait for it to be ready, leave it in Pending

			By("Creating plugin for pending cluster")
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "waiting-plugin",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: clusterName,
					Name:       "apoc",
					Version:    "5.26.0",
					Source: &neo4jv1alpha1.PluginSource{
						Type: "official",
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Verifying plugin status shows Waiting")
			Eventually(func() string {
				currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "waiting-plugin",
					Namespace: namespace.Name,
				}, currentPlugin)
				if err != nil {
					return ""
				}
				return currentPlugin.Status.Phase
			}, time.Second*30, interval).Should(Equal("Waiting"))

			By("Verifying status message mentions waiting for deployment")
			currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "waiting-plugin",
				Namespace: namespace.Name,
			}, currentPlugin)
			Expect(err).NotTo(HaveOccurred())
			Expect(currentPlugin.Status.Message).To(ContainSubstring("Waiting for"))
			Expect(currentPlugin.Status.Message).To(ContainSubstring("to be ready"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, plugin)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, cluster)).Should(Succeed())
		})
	})
})

// Helper function to create unique namespace
func createUniqueNamespace() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("plugin-test-%d", time.Now().UnixNano()),
		},
	}
}
