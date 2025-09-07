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
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// isRunningInCI checks if tests are running in CI environment
func isRunningInCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}

// Property Sharding Integration Tests
// These tests are skipped in CI environments due to large resource requirements:
// - Neo4j 2025.07.1+ images are larger than standard versions
// - Property sharding requires minimum 5 servers for proper shard distribution
// - Each server needs 4Gi+ memory minimum for property sharding workloads (8Gi recommended)
// - Total cluster resource requirements: 20Gi minimum (40Gi recommended)
//
// To run these tests locally:
//
//	make test-integration FOCUS="Property Sharding"
//
// Or:
//
//	ginkgo run -focus "Property Sharding" ./test/integration
var _ = Describe("Property Sharding Integration Tests", Serial, func() {
	var (
		testNamespace string
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
		shardedDB     *neo4jv1alpha1.Neo4jShardedDatabase
	)

	BeforeEach(func() {
		// Skip property sharding tests in CI due to resource requirements
		if isRunningInCI() {
			Skip("Skipping property sharding tests in CI - resource requirements too large")
		}

		testNamespace = createTestNamespace("property-sharding")

		// Create admin secret for authentication
		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

		// Ensure test timeout
		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(5 * time.Second)
	})

	AfterEach(func() {
		// Critical: Clean up resources immediately to prevent CI resource exhaustion
		if shardedDB != nil {
			By("Cleaning up sharded database resource")
			if len(shardedDB.GetFinalizers()) > 0 {
				shardedDB.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, shardedDB)
			}
			_ = k8sClient.Delete(ctx, shardedDB)
			shardedDB = nil
		}

		if cluster != nil {
			By("Cleaning up cluster resource")
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			_ = k8sClient.Delete(ctx, cluster)
			cluster = nil
		}

		// Clean up any remaining resources in namespace
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	Describe("Property Sharding Cluster Configuration", func() {
		Context("when creating a cluster with property sharding enabled", func() {
			It("should validate property sharding configuration", func() {
				cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "property-sharding-cluster",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
						Image: neo4jv1alpha1.ImageSpec{
							Repo: "neo4j",
							Tag:  "2025.07.1-enterprise", // Property sharding requires 2025.07.1+
						},
						Auth: &neo4jv1alpha1.AuthSpec{
							AdminSecret: "neo4j-admin-secret",
						},
						Topology: neo4jv1alpha1.TopologyConfiguration{
							Servers: 5, // Property sharding test configuration
						},
						Storage: neo4jv1alpha1.StorageSpec{
							Size:      "1Gi",
							ClassName: "standard",
						},
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("4Gi"),   // Minimum for dev/test environments
								corev1.ResourceCPU:    resource.MustParse("2000m"), // 2 cores per server
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("8Gi"),   // Sufficient for property sharding
								corev1.ResourceCPU:    resource.MustParse("2000m"), // 2 cores per server
							},
						},
						PropertySharding: &neo4jv1alpha1.PropertyShardingSpec{
							Enabled: true,
							Config: map[string]string{
								"internal.dbms.sharded_property_database.enabled":                     "true",
								"db.query.default_language":                                           "CYPHER_25",
								"internal.dbms.cluster.experimental_protocol_version.dbms_enabled":    "true",
								"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
							},
						},
					},
				}

				By("Creating the cluster with property sharding enabled")
				Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

				By("Checking cluster reaches Ready phase")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
					if err != nil {
						return ""
					}
					return cluster.Status.Phase
				}).Should(Equal("Ready"))

				By("Verifying property sharding readiness")
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
					if err != nil {
						return false
					}
					return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
				}).Should(BeTrue())
			})
		})

		Context("when property sharding configuration is invalid", func() {
			It("should fail validation for insufficient servers", func() {
				cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "invalid-sharding-cluster",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
						Image: neo4jv1alpha1.ImageSpec{
							Repo: "neo4j",
							Tag:  "2025.07.1-enterprise",
						},
						Auth: &neo4jv1alpha1.AuthSpec{
							AdminSecret: "neo4j-admin-secret",
						},
						Topology: neo4jv1alpha1.TopologyConfiguration{
							Servers: 2, // Too few for property sharding
						},
						Storage: neo4jv1alpha1.StorageSpec{
							Size:      "1Gi",
							ClassName: "standard",
						},
						PropertySharding: &neo4jv1alpha1.PropertyShardingSpec{
							Enabled: true,
						},
					},
				}

				By("Creating the cluster with insufficient servers")
				Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

				By("Checking cluster fails validation")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
					if err != nil {
						return ""
					}
					return cluster.Status.Phase
				}).Should(Equal("Failed"))

				By("Checking failure message mentions server requirement")
				Expect(cluster.Status.Message).Should(ContainSubstring("minimum 5 servers"))
			})

			It("should fail validation for unsupported Neo4j version", func() {
				cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-version-cluster",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
						Image: neo4jv1alpha1.ImageSpec{
							Repo: "neo4j",
							Tag:  "5.26-enterprise", // Too old for property sharding
						},
						Auth: &neo4jv1alpha1.AuthSpec{
							AdminSecret: "neo4j-admin-secret",
						},
						Topology: neo4jv1alpha1.TopologyConfiguration{
							Servers: 5,
						},
						Storage: neo4jv1alpha1.StorageSpec{
							Size:      "1Gi",
							ClassName: "standard",
						},
						PropertySharding: &neo4jv1alpha1.PropertyShardingSpec{
							Enabled: true,
						},
					},
				}

				By("Creating the cluster with old Neo4j version")
				Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

				By("Checking cluster fails validation")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
					if err != nil {
						return ""
					}
					return cluster.Status.Phase
				}).Should(Equal("Failed"))

				By("Checking failure message mentions version requirement")
				Expect(cluster.Status.Message).Should(ContainSubstring("2025.07.1+"))
			})
		})
	})

	Describe("Neo4jShardedDatabase Lifecycle", func() {
		BeforeEach(func() {
			// Create a property sharding enabled cluster first
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sharding-host-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "2025.07.1-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 5, // Property sharding test configuration
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),   // Minimum for dev/test environments
							corev1.ResourceCPU:    resource.MustParse("2000m"), // 2 cores per server
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("8Gi"),   // Sufficient for property sharding
							corev1.ResourceCPU:    resource.MustParse("2000m"), // 2 cores per server
						},
					},
					PropertySharding: &neo4jv1alpha1.PropertyShardingSpec{
						Enabled: true,
						Config: map[string]string{
							"internal.dbms.sharded_property_database.enabled":                     "true",
							"db.query.default_language":                                           "CYPHER_25",
							"internal.dbms.cluster.experimental_protocol_version.dbms_enabled":    "true",
							"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
						},
					},
				},
			}

			By("Creating property sharding cluster")
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for cluster to be ready")
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				if err != nil {
					return ""
				}
				return cluster.Status.Phase
			}).Should(Equal("Ready"))

			By("Ensuring property sharding is ready")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				if err != nil {
					return false
				}
				return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
			}).Should(BeTrue())
		})

		Context("when creating a sharded database", func() {
			It("should successfully create and configure sharded database", func() {
				shardedDB = &neo4jv1alpha1.Neo4jShardedDatabase{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sharded-database",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jShardedDatabaseSpec{
						ClusterRef:            cluster.Name,
						Name:                  "products",
						DefaultCypherLanguage: "25",
						PropertySharding: neo4jv1alpha1.PropertyShardingConfiguration{
							PropertyShards: 3,
							HashFunction:   "murmur3",
							GraphShard: neo4jv1alpha1.DatabaseTopology{
								Primaries:   1,
								Secondaries: 1,
							},
							PropertyShardTopology: neo4jv1alpha1.DatabaseTopology{
								Primaries:   1,
								Secondaries: 0,
							},
						},
						Wait:        true,
						IfNotExists: true,
					},
				}

				By("Creating the sharded database")
				Expect(k8sClient.Create(ctx, shardedDB)).Should(Succeed())

				By("Checking sharded database reaches Ready phase")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
					if err != nil {
						return ""
					}
					return shardedDB.Status.Phase
				}).Should(Equal("Ready"))

				By("Verifying sharding readiness")
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
					if err != nil {
						return false
					}
					return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady
				}).Should(BeTrue())
			})

			It("should validate sharded database configuration", func() {
				shardedDB = &neo4jv1alpha1.Neo4jShardedDatabase{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "invalid-sharded-database",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jShardedDatabaseSpec{
						ClusterRef:            cluster.Name,
						Name:                  "invalid-db",
						DefaultCypherLanguage: "5", // Invalid for property sharding
						PropertySharding: neo4jv1alpha1.PropertyShardingConfiguration{
							PropertyShards: 0, // Invalid
							GraphShard: neo4jv1alpha1.DatabaseTopology{
								Primaries:   1,
								Secondaries: 0,
							},
							PropertyShardTopology: neo4jv1alpha1.DatabaseTopology{
								Primaries:   1,
								Secondaries: 0,
							},
						},
					},
				}

				By("Creating the invalid sharded database should fail validation")
				err := k8sClient.Create(ctx, shardedDB)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Invalid value"))
				// The resource should not be created due to CRD validation, so no need to check status
			})
		})

		Context("when cluster doesn't support property sharding", func() {
			var nonShardingCluster *neo4jv1alpha1.Neo4jEnterpriseCluster

			BeforeEach(func() {
				nonShardingCluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-sharding-cluster",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
						Image: neo4jv1alpha1.ImageSpec{
							Repo: "neo4j",
							Tag:  "2025.07.1-enterprise",
						},
						Auth: &neo4jv1alpha1.AuthSpec{
							AdminSecret: "neo4j-admin-secret",
						},
						Topology: neo4jv1alpha1.TopologyConfiguration{
							Servers: 3,
						},
						Storage: neo4jv1alpha1.StorageSpec{
							Size:      "1Gi",
							ClassName: "standard",
						},
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1.5Gi"),
								corev1.ResourceCPU:    resource.MustParse("500m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
								corev1.ResourceCPU:    resource.MustParse("1000m"),
							},
						},
						// No PropertySharding configuration
					},
				}

				By("Creating non-sharding cluster")
				Expect(k8sClient.Create(ctx, nonShardingCluster)).Should(Succeed())

				By("Waiting for non-sharding cluster to be ready")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(nonShardingCluster), nonShardingCluster)
					if err != nil {
						return ""
					}
					return nonShardingCluster.Status.Phase
				}).Should(Equal("Ready"))
			})

			AfterEach(func() {
				if nonShardingCluster != nil {
					if len(nonShardingCluster.GetFinalizers()) > 0 {
						nonShardingCluster.SetFinalizers([]string{})
						_ = k8sClient.Update(ctx, nonShardingCluster)
					}
					_ = k8sClient.Delete(ctx, nonShardingCluster)
				}
			})

			It("should fail when targeting non-sharding cluster", func() {
				shardedDB = &neo4jv1alpha1.Neo4jShardedDatabase{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "failed-sharded-database",
						Namespace: testNamespace,
					},
					Spec: neo4jv1alpha1.Neo4jShardedDatabaseSpec{
						ClusterRef:            nonShardingCluster.Name,
						Name:                  "products",
						DefaultCypherLanguage: "25",
						PropertySharding: neo4jv1alpha1.PropertyShardingConfiguration{
							PropertyShards: 3,
							GraphShard: neo4jv1alpha1.DatabaseTopology{
								Primaries:   1,
								Secondaries: 1,
							},
							PropertyShardTopology: neo4jv1alpha1.DatabaseTopology{
								Primaries:   1,
								Secondaries: 0,
							},
						},
					},
				}

				By("Creating sharded database targeting non-sharding cluster")
				Expect(k8sClient.Create(ctx, shardedDB)).Should(Succeed())

				By("Checking sharded database fails due to cluster not supporting sharding")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
					if err != nil {
						return ""
					}
					return shardedDB.Status.Phase
				}).Should(Equal("Failed"))

				By("Checking failure message mentions cluster sharding support")
				Eventually(func() string {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
					if err != nil {
						return ""
					}
					return shardedDB.Status.Message
				}).Should(ContainSubstring("does not support property sharding"))
			})
		})
	})
})
