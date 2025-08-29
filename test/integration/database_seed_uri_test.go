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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jDatabase Seed URI Integration Tests", func() {
	const (
		timeout  = time.Second * 600 // Increased to 10 minutes for cluster formation + database creation
		interval = time.Second * 2
	)

	Context("Seed URI Validation Integration", func() {
		var (
			testNamespace string
			testCluster   *neo4jv1alpha1.Neo4jEnterpriseCluster
			testSecret    *corev1.Secret
		)

		BeforeEach(func() {
			// Create a unique namespace for each test using the standard helper
			testNamespace = createTestNamespace("seed-uri")

			// Create admin secret for Neo4j authentication
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("admin123"),
				},
			}
			Expect(k8sClient.Create(context.Background(), adminSecret)).To(Succeed())

			// Create a test cluster
			testCluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3, // Smaller cluster for integration tests (requires less memory)
					},
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "500Mi", // Minimal for integration tests
						ClassName: "standard",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), testCluster)).To(Succeed())

			// Create test credentials secret
			testSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-credentials",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"AWS_ACCESS_KEY_ID":     []byte("test-access-key"),
					"AWS_SECRET_ACCESS_KEY": []byte("test-secret-key"),
					"AWS_REGION":            []byte("us-west-2"),
				},
			}
			Expect(k8sClient.Create(context.Background(), testSecret)).To(Succeed())

			// Wait for cluster to be ready
			Eventually(func() bool {
				var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      testCluster.Name,
					Namespace: testNamespace,
				}, &cluster)
				if err != nil {
					GinkgoWriter.Printf("Failed to get cluster: %v\n", err)
					return false
				}

				// Check if cluster phase is Ready (more reliable than conditions)
				if cluster.Status.Phase == "Ready" {
					GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
						cluster.Status.Phase, cluster.Status.Message)
					return true
				}

				// Log current status for debugging
				GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
					cluster.Status.Phase, cluster.Status.Message)
				return false
			}, timeout, interval).Should(BeTrue())
		})

		AfterEach(func() {
			// Cleanup is handled by the test suite's cleanup of test namespaces
			// The createTestNamespace function already tracks namespaces for cleanup
		})

		It("should create database with valid S3 seed URI successfully", func() {
			ctx := context.Background()

			By("Creating a database with S3 seed URI")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-seeded-database",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "s3db",
					SeedURI:    "s3://demo-neo4j-backups/test-database.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
						SecretRef: testSecret.Name,
					},
					SeedConfig: &neo4jv1alpha1.SeedConfiguration{
						Config: map[string]string{
							"compression": "gzip",
							"validation":  "strict",
							"bufferSize":  "64MB",
						},
					},
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   2,
						Secondaries: 2,
					},
					Wait:        true,
					IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Waiting for database to be created and validated")
			Eventually(func() bool {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: testNamespace,
				}, &db)
				if err != nil {
					return false
				}

				// Check that validation passed (no ValidationFailed condition)
				for _, condition := range db.Status.Conditions {
					if condition.Type == "Ready" && condition.Reason == "ValidationFailed" {
						return false
					}
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("Verifying database configuration is accepted by operator")
			var finalDatabase neo4jv1alpha1.Neo4jDatabase
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      database.Name,
				Namespace: testNamespace,
			}, &finalDatabase)).To(Succeed())

			// Verify seed URI configuration is preserved
			Expect(finalDatabase.Spec.SeedURI).To(Equal("s3://demo-neo4j-backups/test-database.backup"))
			Expect(finalDatabase.Spec.SeedCredentials.SecretRef).To(Equal(testSecret.Name))
			Expect(finalDatabase.Spec.SeedConfig.Config["compression"]).To(Equal("gzip"))
		})

		It("should reject database with conflicting seed URI and initial data", func() {
			ctx := context.Background()

			By("Creating a database with both seed URI and initial data")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "conflicting-database",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "conflictdb",
					SeedURI:    "s3://test-bucket/backup.backup",
					InitialData: &neo4jv1alpha1.InitialDataSpec{
						Source: "cypher",
						CypherStatements: []string{
							"CREATE (:TestNode {name: 'test'})",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Verifying database is marked as validation failed")
			Eventually(func() bool {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: testNamespace,
				}, &db)
				if err != nil {
					return false
				}

				// Check for ValidationFailed condition
				for _, condition := range db.Status.Conditions {
					if condition.Type == "Ready" &&
						condition.Status == metav1.ConditionFalse &&
						condition.Reason == "ValidationFailed" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should validate database topology against cluster capacity", func() {
			ctx := context.Background()

			By("Creating a database with topology exceeding cluster capacity")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oversized-database",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "oversizeddb",
					SeedURI:    "s3://test-bucket/backup.backup",
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   3, // Total: 5 servers, but cluster only has 3
						Secondaries: 2,
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Verifying topology validation fails")
			Eventually(func() bool {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: testNamespace,
				}, &db)
				if err != nil {
					return false
				}

				// Check for ValidationFailed condition with topology error
				for _, condition := range db.Status.Conditions {
					if condition.Type == "Ready" &&
						condition.Status == metav1.ConditionFalse &&
						condition.Reason == "ValidationFailed" {
						// Check that the message mentions topology/servers
						return Contains(condition.Message, "topology") ||
							Contains(condition.Message, "servers")
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should handle missing credentials secret gracefully", func() {
			ctx := context.Background()

			By("Creating a database with non-existent credentials secret")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-secret-database",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "missingsecretdb",
					SeedURI:    "s3://test-bucket/backup.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
						SecretRef: "nonexistent-secret",
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Verifying validation fails for missing secret")
			Eventually(func() bool {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: testNamespace,
				}, &db)
				if err != nil {
					return false
				}

				// Check for ValidationFailed condition
				for _, condition := range db.Status.Conditions {
					if condition.Type == "Ready" &&
						condition.Status == metav1.ConditionFalse &&
						condition.Reason == "ValidationFailed" {
						return Contains(condition.Message, "Secret") &&
							Contains(condition.Message, "not found")
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should validate seed configuration options", func() {
			ctx := context.Background()

			By("Creating a database with invalid seed configuration")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-config-database",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "invalidconfigdb",
					SeedURI:    "gs://test-bucket/backup.backup",
					SeedConfig: &neo4jv1alpha1.SeedConfiguration{
						RestoreUntil: "not-a-valid-timestamp",
						Config: map[string]string{
							"compression": "invalid-compression-type",
							"validation":  "invalid-validation-mode",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Verifying seed configuration validation fails")
			Eventually(func() bool {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: testNamespace,
				}, &db)
				if err != nil {
					return false
				}

				// Check for ValidationFailed condition with configuration error
				for _, condition := range db.Status.Conditions {
					if condition.Type == "Ready" &&
						condition.Status == metav1.ConditionFalse &&
						condition.Reason == "ValidationFailed" {
						return Contains(condition.Message, "validation") ||
							Contains(condition.Message, "compression") ||
							Contains(condition.Message, "restoreUntil")
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should accept database with system-wide authentication (no explicit credentials)", func() {
			ctx := context.Background()

			By("Creating a database without explicit credentials")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "system-auth-database",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "systemauthdb",
					SeedURI:    "s3://demo-bucket/backup.backup",
					// No seedCredentials - relies on system-wide auth
					SeedConfig: &neo4jv1alpha1.SeedConfiguration{
						Config: map[string]string{
							"compression": "lz4",
							"validation":  "lenient",
						},
					},
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1,
					},
					Wait:        true,
					IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Verifying database is accepted with system-wide auth")
			Eventually(func() bool {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: testNamespace,
				}, &db)
				if err != nil {
					return false
				}

				// Ensure no ValidationFailed condition
				for _, condition := range db.Status.Conditions {
					if condition.Type == "Ready" && condition.Reason == "ValidationFailed" {
						return false
					}
				}
				return true
			}, timeout, interval).Should(BeTrue())
		})
	})
})

// Helper function to check if string contains substring
func Contains(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
