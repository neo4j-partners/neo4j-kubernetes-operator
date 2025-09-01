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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
)

var _ = Describe("Neo4jDatabase Controller", func() {
	Context("When reconciling a database with missing cluster reference", func() {
		It("should handle missing referenced cluster gracefully", func() {
			ctx := context.Background()

			By("Creating a database with non-existent cluster reference")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-orphan-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "non-existent-cluster",
					Name:       "orphandb",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Reconciling the database with missing cluster")
			controllerReconciler := &controller.Neo4jDatabaseReconciler{
				Client:            k8sClient,
				Scheme:            k8sClient.Scheme(),
				DatabaseValidator: validation.NewDatabaseValidator(k8sClient),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-orphan-db",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred()) // Should not error when cluster is missing

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})
	})

	Context("When reconciling a database with seed URI", func() {
		var testCluster *neo4jv1alpha1.Neo4jEnterpriseCluster
		var testSecret *corev1.Secret

		BeforeEach(func() {
			// Create a test cluster with unique name
			testClusterName := fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
			testCluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testClusterName,
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
				Status: neo4jv1alpha1.Neo4jEnterpriseClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), testCluster)).To(Succeed())

			// Create a test credentials secret with unique name
			testSecretName := fmt.Sprintf("test-credentials-%d", time.Now().UnixNano())
			testSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"AWS_ACCESS_KEY_ID":     []byte("test-key"),
					"AWS_SECRET_ACCESS_KEY": []byte("test-secret"),
					"AWS_REGION":            []byte("us-west-2"),
				},
			}
			Expect(k8sClient.Create(context.Background(), testSecret)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			Expect(k8sClient.Delete(context.Background(), testCluster)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), testSecret)).To(Succeed())
		})

		It("should validate seed URI configuration correctly", func() {
			ctx := context.Background()

			By("Creating a database with valid seed URI")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-seed-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "seeddb",
					SeedURI:    "s3://my-backups/database.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
						SecretRef: testSecret.Name,
					},
					SeedConfig: &neo4jv1alpha1.SeedConfiguration{
						Config: map[string]string{
							"compression": "gzip",
							"validation":  "strict",
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

			By("Validating the seed URI configuration")
			validator := validation.NewDatabaseValidator(k8sClient)
			result := validator.Validate(ctx, database)

			// Should have no validation errors for a properly configured seed URI database
			Expect(result.Errors).To(HaveLen(0))

			// Should have some warnings (e.g., about system-wide auth, missing optional keys)
			Expect(len(result.Warnings)).To(BeNumerically(">=", 1))

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})

		It("should reject conflicting seed URI and initial data", func() {
			ctx := context.Background()

			By("Creating a database with both seed URI and initial data")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-conflict-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "conflictdb",
					SeedURI:    "s3://my-backups/database.backup",
					InitialData: &neo4jv1alpha1.InitialDataSpec{
						Source: "cypher",
						CypherStatements: []string{
							"CREATE (:Test)",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Validating the conflicting configuration")
			validator := validation.NewDatabaseValidator(k8sClient)
			result := validator.Validate(ctx, database)

			// Should have validation errors for conflicting configuration
			Expect(result.Errors).To(HaveLen(1))
			Expect(result.Errors[0].Error()).To(ContainSubstring("seedURI and initialData cannot be specified together"))

			// Should also have a warning about the conflict
			Expect(len(result.Warnings)).To(BeNumerically(">=", 1))
			foundConflictWarning := false
			for _, warning := range result.Warnings {
				if Contains(warning, "seedURI and initialData") {
					foundConflictWarning = true
					break
				}
			}
			Expect(foundConflictWarning).To(BeTrue())

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})

		It("should validate invalid seed URI format", func() {
			ctx := context.Background()

			By("Creating a database with invalid seed URI")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-uri-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "invaliddb",
					SeedURI:    "invalid-uri-format",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Validating the invalid URI configuration")
			validator := validation.NewDatabaseValidator(k8sClient)
			result := validator.Validate(ctx, database)

			// Should have validation errors for invalid URI format
			Expect(result.Errors).To(HaveLen(1))
			Expect(result.Errors[0].Error()).To(ContainSubstring("supported values"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})

		It("should validate missing credentials secret", func() {
			ctx := context.Background()

			By("Creating a database with missing credentials secret")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-missing-secret-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "missingsecretdb",
					SeedURI:    "s3://my-backups/database.backup",
					SeedCredentials: &neo4jv1alpha1.SeedCredentials{
						SecretRef: "nonexistent-secret",
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Validating the missing secret configuration")
			validator := validation.NewDatabaseValidator(k8sClient)
			result := validator.Validate(ctx, database)

			// Should have validation errors for missing secret
			Expect(result.Errors).To(HaveLen(1))
			Expect(result.Errors[0].Error()).To(ContainSubstring("Secret nonexistent-secret not found"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})

		It("should validate seed configuration options", func() {
			ctx := context.Background()

			By("Creating a database with invalid seed configuration")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-config-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "invalidconfigdb",
					SeedURI:    "s3://my-backups/database.backup",
					SeedConfig: &neo4jv1alpha1.SeedConfiguration{
						RestoreUntil: "invalid-timestamp",
						Config: map[string]string{
							"compression": "invalid-compression",
							"validation":  "invalid-validation",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Validating the invalid configuration")
			validator := validation.NewDatabaseValidator(k8sClient)
			result := validator.Validate(ctx, database)

			// Should have multiple validation errors for invalid configuration
			Expect(len(result.Errors)).To(BeNumerically(">=", 3)) // restoreUntil + compression + validation

			// Check specific error messages
			errorMessages := make([]string, len(result.Errors))
			for i, err := range result.Errors {
				errorMessages[i] = err.Error()
			}

			foundRestoreUntilError := false
			foundCompressionError := false
			foundValidationError := false

			for _, msg := range errorMessages {
				if Contains(msg, "restoreUntil must be RFC3339 timestamp") {
					foundRestoreUntilError = true
				}
				if Contains(msg, "compression") && Contains(msg, "supported values") {
					foundCompressionError = true
				}
				if Contains(msg, "validation") && Contains(msg, "supported values") {
					foundValidationError = true
				}
			}

			Expect(foundRestoreUntilError).To(BeTrue())
			Expect(foundCompressionError).To(BeTrue())
			Expect(foundValidationError).To(BeTrue())

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})

		It("should reject database with invalid seed URI format", func() {
			ctx := context.Background()

			By("Creating a database with invalid seed URI format")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-uri-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: testCluster.Name,
					Name:       "invaliduridb",
					SeedURI:    "invalid-uri-format", // No scheme, no host
				},
			}

			By("Validating the invalid URI configuration")
			validator := validation.NewDatabaseValidator(k8sClient)
			result := validator.Validate(ctx, database)

			// Should have validation errors for invalid URI format
			Expect(result.Errors).To(HaveLen(1))
			Expect(result.Errors[0].Error()).To(ContainSubstring("supported values"))
		})
	})
})

// Helper function to check if a string contains a substring
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
