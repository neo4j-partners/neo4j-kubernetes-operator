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
	"os/exec"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jPlugin Integration Tests", func() {
	const (
		timeout  = time.Second * 300 // 5 minutes for initial resource creation / cluster ready
		interval = time.Second * 5
	)

	// pluginTimeout is longer than clusterTimeout because plugin installation on a cluster
	// involves a full rolling restart (NEO4J_PLUGINS env var change → all pods restart) which
	// can take 4-8 minutes on top of the cluster formation time.
	// CI sets clusterTimeout=20m so this is still bounded there.
	pluginTimeout := func() time.Duration {
		return clusterTimeout * 2
	}

	AfterEach(func() {
		// Critical: Clean up any plugin test resources to prevent CI resource exhaustion
		By("Cleaning up plugin test resources")

		// Clean up any clusters created by plugin tests
		clusterList := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
		if err := k8sClient.List(ctx, clusterList); err == nil {
			for i := range clusterList.Items {
				cluster := &clusterList.Items[i]
				if cluster.Name == "plugin-test-cluster" || strings.Contains(cluster.Namespace, "plugin") {
					if len(cluster.GetFinalizers()) > 0 {
						cluster.SetFinalizers([]string{})
						_ = k8sClient.Update(ctx, cluster)
					}
					_ = k8sClient.Delete(ctx, cluster)
				}
			}
		}

		// Clean up any standalones created by plugin tests
		standaloneList := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
		if err := k8sClient.List(ctx, standaloneList); err == nil {
			for i := range standaloneList.Items {
				standalone := &standaloneList.Items[i]
				if standalone.Name == "plugin-test-standalone" || strings.Contains(standalone.Namespace, "plugin") {
					if len(standalone.GetFinalizers()) > 0 {
						standalone.SetFinalizers([]string{})
						_ = k8sClient.Update(ctx, standalone)
					}
					_ = k8sClient.Delete(ctx, standalone)
				}
			}
		}

		// Clean up any plugins
		pluginList := &neo4jv1alpha1.Neo4jPluginList{}
		if err := k8sClient.List(ctx, pluginList); err == nil {
			for i := range pluginList.Items {
				plugin := &pluginList.Items[i]
				if len(plugin.GetFinalizers()) > 0 {
					plugin.SetFinalizers([]string{})
					_ = k8sClient.Update(ctx, plugin)
				}
				_ = k8sClient.Delete(ctx, plugin)
			}
		}
	})

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
						Servers: getCIAppropriateClusterSize(2),
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

			// Apply CI-specific optimizations
			applyCIOptimizations(cluster)

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
			}, clusterTimeout, interval).Should(Equal("Ready"))

			By("Verifying server StatefulSet exists with correct name")
			serverSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}, serverSts)
			}, clusterTimeout, interval).Should(Succeed())
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
			// Use pluginTimeout (2× clusterTimeout) because plugin installation on a cluster involves
			// a rolling restart of all pods (NEO4J_PLUGINS env change) which takes 4-8 min on top of
			// the cluster formation time already consumed above.
			Eventually(func() string {
				currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "apoc-plugin",
					Namespace: namespace.Name,
				}, currentPlugin)
				if err != nil {
					return ""
				}
				GinkgoWriter.Printf("Plugin phase: %s message: %s\n", currentPlugin.Status.Phase, currentPlugin.Status.Message)
				return currentPlugin.Status.Phase
			}, pluginTimeout(), interval).Should(Equal("Ready"))

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
			}, pluginTimeout(), interval).Should(BeTrue())

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
			}, pluginTimeout(), interval).Should(BeTrue())

			By("Waiting for StatefulSet rolling update to complete after plugin install")
			// The NEO4J_PLUGINS env var triggers a rolling restart; all pods must be
			// updated and ready before APOC procedures will be available.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}, serverSts)
				if err != nil {
					return false
				}
				replicas := int32(1)
				if serverSts.Spec.Replicas != nil {
					replicas = *serverSts.Spec.Replicas
				}
				GinkgoWriter.Printf("StatefulSet rolling update: ReadyReplicas=%d, UpdatedReplicas=%d, Replicas=%d\n",
					serverSts.Status.ReadyReplicas, serverSts.Status.UpdatedReplicas, replicas)
				return serverSts.Status.ReadyReplicas == replicas &&
					serverSts.Status.UpdatedReplicas == replicas
			}, pluginTimeout(), interval).Should(BeTrue(), "StatefulSet rolling update should complete")

			By("Verifying APOC procedures are available via cypher-shell")
			// NEO4J_PLUGINS=["apoc"] tells the Neo4j Docker entrypoint to load APOC from the
			// pre-bundled jar at /var/lib/neo4j/labs/apoc-*-core.jar (no internet access required).
			// The entrypoint copies it to /plugins/ (the operator's EmptyDir volume), and Neo4j loads
			// it because server.directories.plugins=/plugins is set in the base config.
			podName := fmt.Sprintf("%s-server-0", clusterName)
			Eventually(func() int {
				apocCheckCmd := exec.CommandContext(ctx, "kubectl", "exec",
					podName, "-n", namespace.Name, "--",
					"cypher-shell", "-u", "neo4j", "-p", "admin123",
					"SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc' RETURN count(*) AS n",
				)
				apocOut, apocErr := apocCheckCmd.CombinedOutput()
				if apocErr != nil {
					GinkgoWriter.Printf("cypher-shell not yet ready: %v\n", apocErr)
					return 0
				}
				outStr := string(apocOut)
				GinkgoWriter.Printf("cypher-shell output: %s\n", outStr)
				// Parse the count from output (format: "\nn\n<count>\n")
				lines := strings.Split(strings.TrimSpace(outStr), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" && line != "n" {
						if count, err := strconv.Atoi(line); err == nil {
							return count
						}
					}
				}
				return 0
			}, pluginTimeout(), interval).Should(BeNumerically(">", 0), "APOC procedures should be available: NEO4J_PLUGINS copies from pre-bundled /var/lib/neo4j/labs jar")

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
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
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
			}, clusterTimeout, interval).Should(Equal("Ready"))

			By("Verifying standalone StatefulSet exists with correct name")
			standaloneSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneName,
					Namespace: namespace.Name,
				}, standaloneSts)
			}, clusterTimeout, interval).Should(Succeed())
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
					// Note: GDS license file configuration removed for testing - production deployments need actual license
					Config: map[string]string{
						// Test basic GDS configuration without requiring license file
					},
					Security: &neo4jv1alpha1.PluginSecurity{
						AllowedProcedures: []string{"gds.*", "apoc.load.*"},
						Sandbox:           true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Waiting for plugin status to be Installing or Ready")
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
			}, clusterTimeout, interval).Should(SatisfyAny(
				Equal("Waiting"),
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
			}, clusterTimeout, interval).Should(BeTrue())

			By("Verifying GDS procedure security settings are configured in ConfigMap")
			configMapName := fmt.Sprintf("%s-config", standaloneName)
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: namespace.Name,
				}, configMap)
				if err != nil {
					return false
				}

				neo4jConf := configMap.Data["neo4j.conf"]
				// GDS plugin should have both automatic unrestricted settings and user-provided allowlist
				// Automatic: dbms.security.procedures.unrestricted=gds.*,apoc.load.*
				// User-provided (sandbox=true): dbms.security.procedures.allowlist=gds.*,apoc.load.*
				hasUnrestricted := strings.Contains(neo4jConf, "dbms.security.procedures.unrestricted") &&
					strings.Contains(neo4jConf, "gds.*")
				hasAllowlist := strings.Contains(neo4jConf, "dbms.security.procedures.allowlist") &&
					strings.Contains(neo4jConf, "gds.*")

				if !hasUnrestricted && !hasAllowlist {
					GinkgoWriter.Printf("GDS security settings not found in ConfigMap neo4j.conf. Content: %s\n", neo4jConf)
				}

				// Either automatic unrestricted OR user-provided allowlist should be present
				return hasUnrestricted || hasAllowlist
			}, clusterTimeout, interval).Should(BeTrue())

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
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
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
			}, clusterTimeout, interval).Should(Equal("Ready"))

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
					// Note: Bloom license file configuration removed for testing - production deployments need actual license
					Config: map[string]string{
						// Test basic Bloom configuration without requiring license file
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).Should(Succeed())

			By("Verifying Bloom procedure security settings are automatically configured")
			// Bloom settings go into neo4j.conf via ConfigMap, not environment variables
			configMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-config", standaloneName),
					Namespace: namespace.Name,
				}, configMap)
				if err != nil {
					GinkgoWriter.Printf("Failed to get ConfigMap: %v\n", err)
					return false
				}

				// Check for Bloom-specific settings in neo4j.conf
				neo4jConf, exists := configMap.Data["neo4j.conf"]
				if !exists {
					GinkgoWriter.Printf("neo4j.conf not found in ConfigMap\n")
					return false
				}

				// Verify Bloom security settings are present
				hasUnrestricted := strings.Contains(neo4jConf, "dbms.security.procedures.unrestricted=bloom.*")
				hasHttpAuth := strings.Contains(neo4jConf, "dbms.security.http_auth_allowlist=/,/browser.*,/bloom.*")
				hasUnmanagedExt := strings.Contains(neo4jConf, "server.unmanaged_extension_classes=com.neo4j.bloom.server=/bloom")

				if !hasUnrestricted {
					GinkgoWriter.Printf("Missing dbms.security.procedures.unrestricted=bloom.* in neo4j.conf\n")
				}
				if !hasHttpAuth {
					GinkgoWriter.Printf("Missing dbms.security.http_auth_allowlist=/,/browser.*,/bloom.* in neo4j.conf\n")
				}
				if !hasUnmanagedExt {
					GinkgoWriter.Printf("Missing server.unmanaged_extension_classes=com.neo4j.bloom.server=/bloom in neo4j.conf\n")
				}

				return hasUnrestricted && hasHttpAuth && hasUnmanagedExt
			}, clusterTimeout, interval).Should(BeTrue())

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
			}, clusterTimeout, interval).Should(Equal("Failed"))

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
						Servers: getCIAppropriateClusterSize(2),
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
			}, clusterTimeout, interval).Should(Equal("Waiting"))

			By("Verifying status message mentions waiting for deployment")
			currentPlugin := &neo4jv1alpha1.Neo4jPlugin{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "waiting-plugin",
				Namespace: namespace.Name,
			}, currentPlugin)
			Expect(err).NotTo(HaveOccurred())
			Expect(currentPlugin.Status.Message).To(ContainSubstring("Waiting for"))
			Expect(currentPlugin.Status.Message).To(ContainSubstring("to be functional"))

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
