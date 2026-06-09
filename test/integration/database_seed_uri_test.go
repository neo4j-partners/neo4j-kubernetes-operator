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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Neo4jDatabase Seed URI Integration Tests", Label("extended"), Ordered, func() {

	var (
		testNamespace string
		testCluster   *neo4jv1beta1.Neo4jEnterpriseCluster
		testSecret    *corev1.Secret
	)

	BeforeAll(func() {
		testNamespace = createTestNamespace("seed-uri")

		// Create admin secret
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

		// Create cluster — 2 servers is sufficient for validation tests
		testCluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Topology: neo4jv1beta1.TopologyConfiguration{
					Servers: 2,
				},
				Image: neo4jv1beta1.ImageSpec{
					Repo: "neo4j",
					Tag:  getNeo4jImageTag(),
				},
				Storage: neo4jv1beta1.StorageSpec{
					Size:      "500Mi",
					ClassName: "standard",
				},
				Resources: getCIAppropriateResourceRequirements(),
				Auth: &neo4jv1beta1.AuthSpec{
					AdminSecret: "neo4j-admin-secret",
				},
				TLS: &neo4jv1beta1.TLSSpec{
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
		applyCIOptimizations(testCluster)
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
			var cluster neo4jv1beta1.Neo4jEnterpriseCluster
			err := k8sClient.Get(context.Background(), types.NamespacedName{
				Name:      testCluster.Name,
				Namespace: testNamespace,
			}, &cluster)
			if err != nil {
				return false
			}
			if cluster.Status.Phase == "Ready" {
				GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
					cluster.Status.Phase, cluster.Status.Message)
				return true
			}
			GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
				cluster.Status.Phase, cluster.Status.Message)
			return false
		}, clusterTimeout, interval).Should(BeTrue())
	})

	AfterAll(func() {
		if testCluster != nil {
			// Re-fetch to get latest version
			var latest neo4jv1beta1.Neo4jEnterpriseCluster
			if err := k8sClient.Get(context.Background(), types.NamespacedName{
				Name: testCluster.Name, Namespace: testNamespace,
			}, &latest); err == nil {
				latest.SetFinalizers([]string{})
				_ = k8sClient.Update(context.Background(), &latest)
				_ = k8sClient.Delete(context.Background(), &latest)
			}
		}
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	// Helper to clean up a database CR after each test
	cleanupDatabase := func(name string) {
		var db neo4jv1beta1.Neo4jDatabase
		if err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name: name, Namespace: testNamespace,
		}, &db); err == nil {
			db.SetFinalizers([]string{})
			_ = k8sClient.Update(context.Background(), &db)
			_ = k8sClient.Delete(context.Background(), &db)
		}
	}

	It("should create database with valid S3 seed URI successfully", func() {
		ctx := context.Background()
		dbName := "s3-seeded-database"
		defer cleanupDatabase(dbName)

		By("Creating a database with S3 seed URI")
		database := &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbName,
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: testCluster.Name,
				Name:       "s3db",
				SeedURI:    "s3://demo-neo4j-backups/test-database.backup",
				SeedCredentials: &neo4jv1beta1.SeedCredentials{
					SecretRef: testSecret.Name,
				},
				SeedConfig: &neo4jv1beta1.SeedConfiguration{
					Config: map[string]string{
						"compression": "gzip",
						"validation":  "strict",
						"bufferSize":  "64MB",
					},
				},
				Topology: &neo4jv1beta1.DatabaseTopology{
					Primaries:   1,
					Secondaries: 1,
				},
				Wait:        true,
				IfNotExists: true,
			},
		}
		Expect(k8sClient.Create(ctx, database)).To(Succeed())

		By("Waiting for database to be created and validated")
		Eventually(func() bool {
			var db neo4jv1beta1.Neo4jDatabase
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: dbName, Namespace: testNamespace,
			}, &db)
			if err != nil {
				return false
			}
			for _, condition := range db.Status.Conditions {
				if condition.Type == "Ready" && condition.Reason == "ValidationFailed" {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue())

		By("Verifying database configuration is accepted by operator")
		var finalDatabase neo4jv1beta1.Neo4jDatabase
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: dbName, Namespace: testNamespace,
		}, &finalDatabase)).To(Succeed())
		Expect(finalDatabase.Spec.SeedURI).To(Equal("s3://demo-neo4j-backups/test-database.backup"))
		Expect(finalDatabase.Spec.SeedCredentials.SecretRef).To(Equal(testSecret.Name))
		Expect(finalDatabase.Spec.SeedConfig.Config["compression"]).To(Equal("gzip"))
	})

	It("should reject database with conflicting seed URI and initial data", func() {
		ctx := context.Background()
		dbName := "conflicting-database"
		defer cleanupDatabase(dbName)

		By("Creating a database with both seed URI and initial data")
		database := &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbName,
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: testCluster.Name,
				Name:       "conflictdb",
				SeedURI:    "s3://test-bucket/backup.backup",
				InitialData: &neo4jv1beta1.InitialDataSpec{
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
			var db neo4jv1beta1.Neo4jDatabase
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: dbName, Namespace: testNamespace,
			}, &db); err != nil {
				return false
			}
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
		dbName := "oversized-database"
		defer cleanupDatabase(dbName)

		By("Creating a database with topology exceeding cluster capacity")
		database := &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbName,
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: testCluster.Name,
				Name:       "oversizeddb",
				SeedURI:    "s3://test-bucket/backup.backup",
				Topology: &neo4jv1beta1.DatabaseTopology{
					Primaries:   3,
					Secondaries: 2,
				},
			},
		}
		Expect(k8sClient.Create(ctx, database)).To(Succeed())

		By("Verifying topology validation fails")
		Eventually(func() bool {
			var db neo4jv1beta1.Neo4jDatabase
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: dbName, Namespace: testNamespace,
			}, &db); err != nil {
				return false
			}
			for _, condition := range db.Status.Conditions {
				if condition.Type == "Ready" &&
					condition.Status == metav1.ConditionFalse &&
					condition.Reason == "ValidationFailed" {
					return Contains(condition.Message, "topology") ||
						Contains(condition.Message, "servers")
				}
			}
			return false
		}, timeout, interval).Should(BeTrue())
	})

	It("should handle missing credentials secret gracefully", func() {
		ctx := context.Background()
		dbName := "missing-secret-database"
		defer cleanupDatabase(dbName)

		By("Creating a database with non-existent credentials secret")
		database := &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbName,
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: testCluster.Name,
				Name:       "missingsecretdb",
				SeedURI:    "s3://test-bucket/backup.backup",
				SeedCredentials: &neo4jv1beta1.SeedCredentials{
					SecretRef: "nonexistent-secret",
				},
			},
		}
		Expect(k8sClient.Create(ctx, database)).To(Succeed())

		By("Verifying validation fails for missing secret")
		Eventually(func() bool {
			var db neo4jv1beta1.Neo4jDatabase
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: dbName, Namespace: testNamespace,
			}, &db); err != nil {
				return false
			}
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
		dbName := "invalid-config-database"
		defer cleanupDatabase(dbName)

		By("Creating a database with invalid seed configuration")
		database := &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbName,
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: testCluster.Name,
				Name:       "invalidconfigdb",
				SeedURI:    "gs://test-bucket/backup.backup",
				SeedConfig: &neo4jv1beta1.SeedConfiguration{
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
			var db neo4jv1beta1.Neo4jDatabase
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: dbName, Namespace: testNamespace,
			}, &db); err != nil {
				return false
			}
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
		dbName := "system-auth-database"
		defer cleanupDatabase(dbName)

		By("Creating a database without explicit credentials")
		database := &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbName,
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: testCluster.Name,
				Name:       "systemauthdb",
				SeedURI:    "s3://demo-bucket/backup.backup",
				SeedConfig: &neo4jv1beta1.SeedConfiguration{
					Config: map[string]string{
						"compression": "lz4",
						"validation":  "lenient",
					},
				},
				Topology: &neo4jv1beta1.DatabaseTopology{
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
			var db neo4jv1beta1.Neo4jDatabase
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: dbName, Namespace: testNamespace,
			}, &db); err != nil {
				return false
			}
			for _, condition := range db.Status.Conditions {
				if condition.Type == "Ready" && condition.Reason == "ValidationFailed" {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue())
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
