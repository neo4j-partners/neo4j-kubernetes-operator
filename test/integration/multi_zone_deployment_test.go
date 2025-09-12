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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Multi-Zone Deployment", func() {
	const (
		timeout  = time.Second * 300
		interval = time.Second * 2
	)

	var (
		testNamespace string
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("multi-zone")
		cluster = nil
	})

	AfterEach(func() {
		// Clean up cluster if created
		if cluster != nil {
			By("Cleaning up multi-zone cluster")
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			_ = k8sClient.Delete(ctx, cluster)
			cluster = nil
		}

		// Clean up namespace resources
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	Context("Basic Multi-Zone Configuration", func() {
		It("should create a cluster with zone anti-affinity", func() {
			By("Creating a cluster with zone anti-affinity")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-zone-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						AntiAffinity: &neo4jv1alpha1.AntiAffinitySpec{
							Type:     "zone",
							Required: false,
							Weight:   100,
						},
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}

			// Create admin secret
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("testpassword123"),
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			// Create the cluster
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			// Verify StatefulSet has anti-affinity configuration
			Eventually(func() bool {
				sts := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "multi-zone-cluster-server",
					Namespace: testNamespace,
				}, sts)
				if err != nil {
					return false
				}

				// Check pod template has anti-affinity
				affinity := sts.Spec.Template.Spec.Affinity
				if affinity == nil || affinity.PodAntiAffinity == nil {
					return false
				}

				// Check for preferred anti-affinity with zone topology
				preferred := affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
				if len(preferred) == 0 {
					return false
				}

				for _, term := range preferred {
					if term.PodAffinityTerm.TopologyKey == "topology.kubernetes.io/zone" &&
						term.Weight == 100 {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "StatefulSet should have zone anti-affinity")
		})
	})

	Context("Server Groups with Tags", func() {
		It("should create a cluster with server groups and tags", func() {
			By("Creating a cluster with server groups")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-groups-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 6,
						ServerGroups: []neo4jv1alpha1.ServerGroup{
							{
								Name:       "primary-group",
								Count:      3,
								ServerTags: []string{"primary", "region-a", "tier1"},
								NodeSelector: map[string]string{
									"zone": "zone-a",
								},
								RoleHint: "PRIMARY_PREFERRED",
							},
							{
								Name:       "secondary-group",
								Count:      3,
								ServerTags: []string{"secondary", "region-b", "tier2"},
								NodeSelector: map[string]string{
									"zone": "zone-b",
								},
								RoleHint: "SECONDARY_PREFERRED",
							},
						},
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}

			// Create admin secret
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("testpassword123"),
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			// Create the cluster
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			// Verify ConfigMap contains server tags configuration
			Eventually(func() bool {
				cm := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "server-groups-cluster-config",
					Namespace: testNamespace,
				}, cm)
				if err != nil {
					return false
				}

				// Check startup script for server tags
				startupScript, exists := cm.Data["startup.sh"]
				if !exists {
					return false
				}

				// Verify server tags are configured
				hasServerTags := strings.Contains(startupScript, "SERVER_TAGS=") &&
					strings.Contains(startupScript, "server.tags=$SERVER_TAGS")

				// Verify role hints are configured
				hasRoleHints := strings.Contains(startupScript, "PRIMARY_PREFERRED") ||
					strings.Contains(startupScript, "SECONDARY_PREFERRED")

				return hasServerTags || hasRoleHints
			}, timeout, interval).Should(BeTrue(), "ConfigMap should contain server tags configuration")
		})
	})

	Context("Routing Policies", func() {
		It("should create a cluster with routing policies", func() {
			By("Creating a cluster with routing policies")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "routing-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Routing: &neo4jv1alpha1.RoutingSpec{
						LoadBalancingPolicy: "server_policies",
						Policies: map[string]string{
							"primary-only":  "tags(primary)->min(2);halt();",
							"read-replicas": "tags(secondary);all();",
						},
						DefaultPolicy:   "primary-only",
						CatchupStrategy: "CONNECT_RANDOMLY_TO_PRIMARY_SERVER",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}

			// Create admin secret
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("testpassword123"),
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			// Create the cluster
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			// Verify ConfigMap contains routing policies
			Eventually(func() bool {
				cm := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "routing-cluster-config",
					Namespace: testNamespace,
				}, cm)
				if err != nil {
					return false
				}

				neo4jConf, exists := cm.Data["neo4j.conf"]
				if !exists {
					return false
				}

				// Check for routing configuration
				hasLoadBalancing := strings.Contains(neo4jConf, "dbms.routing.load_balancing.plugin=server_policies")
				hasPolicies := strings.Contains(neo4jConf, "dbms.routing.load_balancing.config.server_policies.primary-only=") &&
					strings.Contains(neo4jConf, "dbms.routing.load_balancing.config.server_policies.read-replicas=")
				hasDefaultPolicy := strings.Contains(neo4jConf, "dbms.routing.default_policy=primary-only")
				hasCatchupStrategy := strings.Contains(neo4jConf, "server.cluster.catchup.upstream_strategy=CONNECT_RANDOMLY_TO_PRIMARY_SERVER")

				return hasLoadBalancing && hasPolicies && hasDefaultPolicy && hasCatchupStrategy
			}, timeout, interval).Should(BeTrue(), "ConfigMap should contain routing policies")
		})
	})

	Context("Standalone with Tags", func() {
		It("should create a standalone deployment with server tags", func() {
			By("Creating a standalone deployment with tags")
			standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tagged-standalone",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					ServerTags: []string{"edge", "region-a", "standalone"},
					Placement: &neo4jv1alpha1.StandalonePlacementSpec{
						Zone:   "zone-a",
						Region: "region-a",
						AntiAffinity: &neo4jv1alpha1.AntiAffinitySpec{
							Type:     "node",
							Required: false,
						},
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}

			// Create admin secret
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("testpassword123"),
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			// Create the standalone deployment
			Expect(k8sClient.Create(ctx, standalone)).Should(Succeed())

			// Verify ConfigMap contains server tags
			Eventually(func() bool {
				cm := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "tagged-standalone-config",
					Namespace: testNamespace,
				}, cm)
				if err != nil {
					return false
				}

				neo4jConf, exists := cm.Data["neo4j.conf"]
				if !exists {
					return false
				}

				// Check for server tags
				return strings.Contains(neo4jConf, "server.tags=edge,region-a,standalone")
			}, timeout, interval).Should(BeTrue(), "ConfigMap should contain server tags")

			// Clean up standalone
			if len(standalone.GetFinalizers()) > 0 {
				standalone.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, standalone)
			}
			_ = k8sClient.Delete(ctx, standalone)
		})
	})
})
