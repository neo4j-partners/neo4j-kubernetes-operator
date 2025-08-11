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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Database Validation Integration Tests", func() {
	const (
		timeout  = time.Second * 1200 // Increased to 20 minutes for CI environment constraints (image pull + resource constraints)
		interval = time.Second * 10   // Increased polling interval to reduce API load in CI
	)

	var testNamespace string
	var cluster *neo4jv1alpha1.Neo4jEnterpriseCluster

	BeforeEach(func() {
		By("Creating test namespace")
		testNamespace = createTestNamespace("db-validation")

		By("Creating admin secret for Neo4j authentication")
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
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

		By("Creating a test cluster with 2 servers")
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "validation-cluster",
				Namespace: testNamespace,
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Edition: "enterprise",
				Image: neo4jv1alpha1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Servers: 2, // Reduced to 2-server cluster for CI resource constraints
				},
				Storage: neo4jv1alpha1.StorageSpec{
					ClassName: "standard",
					Size:      "500Mi", // Minimal for integration tests
				},
				Auth: &neo4jv1alpha1.AuthSpec{
					AdminSecret: "neo4j-admin-secret",
				},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"), // Minimal CPU
						corev1.ResourceMemory: resource.MustParse("1Gi"), // Minimum for Neo4j Enterprise
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1Gi"), // Prevent OOM
					},
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
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		By("Waiting for cluster to be ready")
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)
			if err != nil {
				return false
			}
			for _, condition := range cluster.Status.Conditions {
				if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, timeout, interval).Should(BeTrue())

		By("Verifying cluster services have endpoints")
		// Wait for service endpoints to exist (pods may not be ready yet, but endpoints should exist)
		// Increased timeout for CI environments with resource constraints and image pull delays
		Eventually(func() bool {
			// Check that the client service has endpoints
			endpoints := &corev1.Endpoints{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      cluster.Name + "-client",
				Namespace: cluster.Namespace,
			}, endpoints)
			if err != nil {
				return false
			}

			// Check if there are any addresses (ready or not ready) and the bolt port exists
			for _, subset := range endpoints.Subsets {
				totalAddresses := len(subset.Addresses) + len(subset.NotReadyAddresses)
				if totalAddresses > 0 && len(subset.Ports) > 0 {
					// Check if bolt port is configured
					for _, port := range subset.Ports {
						if port.Name == "bolt" && port.Port == 7687 {
							return true
						}
					}
				}
			}
			return false
		}, 600*time.Second, 10*time.Second).Should(BeTrue(), "Client service endpoints should exist")

		// Additional stabilization time for Neo4j cluster internals
		By("Allowing Neo4j internal services to fully initialize")
		time.Sleep(60 * time.Second) // Increased from 30s to ensure Neo4j is accepting connections
	})

	Context("When creating databases with topology validation", func() {
		It("should accept valid topology within cluster capacity", func() {
			By("Creating database with valid topology (1 primary + 1 secondary = 2 servers)")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-topology-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "validation-cluster",
					Name:        "validdb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1, // Total: 2 servers (within cluster capacity)
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database should be marked as ready")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: database.Namespace,
				}, database)
				if err != nil {
					return false
				}
				for _, condition := range database.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should emit warnings for suboptimal topologies", func() {
			By("Creating database that uses all cluster servers")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "warning-topology-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "validation-cluster",
					Name:        "warningdb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1, // Total: 2 servers (all cluster servers)
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database should still be created but with validation warnings")
			// Check that events contain validation warnings
			Eventually(func() []corev1.Event {
				eventList := &corev1.EventList{}
				err := k8sClient.List(ctx, eventList, &client.ListOptions{
					Namespace: testNamespace,
				})
				if err != nil {
					return nil
				}

				var dbEvents []corev1.Event
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == database.Name &&
						event.InvolvedObject.Kind == "Neo4jDatabase" {
						dbEvents = append(dbEvents, event)
					}
				}
				return dbEvents
			}, timeout, interval).Should(ContainElement(
				HaveField("Reason", "ValidationWarning")))
		})

		It("should reject topology that exceeds cluster capacity", func() {
			By("Creating database with topology exceeding cluster capacity")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-topology-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "validation-cluster",
					Name:        "invaliddb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   2,
						Secondaries: 2, // Total: 4 servers (exceeds cluster capacity of 2)
					},
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database should be marked as failed due to validation error")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: database.Namespace,
				}, database)
				if err != nil {
					return false
				}
				for _, condition := range database.Status.Conditions {
					if condition.Type == "Ready" &&
						condition.Status == metav1.ConditionFalse &&
						condition.Reason == "ValidationFailed" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Validation error should contain expected message")
			Expect(database.Status.Conditions[0].Message).To(ContainSubstring("database topology requires 4 servers but cluster only has 2 servers available"))
		})

		It("should validate Cypher language versions", func() {
			By("Creating database with invalid Cypher version should be rejected by CRD validation")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-cypher-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:            "validation-cluster",
					Name:                  "cypherdb",
					Wait:                  true,
					IfNotExists:           true,
					DefaultCypherLanguage: "4", // Invalid version
				},
			}

			By("CRD validation should reject the invalid Cypher version")
			err := k8sClient.Create(ctx, database)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value: \"4\": supported values: \"5\", \"25\""))
		})

		It("should validate database OPTIONS parameters at controller level", func() {
			By("Creating database with valid OPTIONS")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-options-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "validation-cluster",
					Name:        "validoptionsdb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1,
					},
					Options: map[string]string{
						"storeFormat": "block",
					},
				},
			}

			err := k8sClient.Create(ctx, database)
			Expect(err).NotTo(HaveOccurred(), "Database with valid OPTIONS should be accepted")

			By("Waiting for database to be processed (or fail with validation error)")
			Eventually(func() bool {
				// Check if database exists and get its status
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{Name: database.Name, Namespace: database.Namespace}, &db)
				if err != nil {
					return false
				}
				// If status shows ready or has error conditions, validation passed
				return db.Status.Phase != ""
			}, timeout, interval).Should(BeTrue(), "Database should be processed and show status")

			// Clean up the database after test
			defer func() {
				k8sClient.Delete(ctx, database)
			}()

			By("Creating database with invalid OPTIONS")
			invalidDatabase := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-options-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "validation-cluster",
					Name:        "invalidoptionsdb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1,
					},
					Options: map[string]string{
						"db.logs.query.enabled": "true", // Invalid - should be rejected by controller
					},
				},
			}

			err = k8sClient.Create(ctx, invalidDatabase)
			Expect(err).NotTo(HaveOccurred(), "Invalid OPTIONS database should be created but fail validation")

			By("Waiting for controller to process and reject invalid OPTIONS")
			Eventually(func() string {
				var db neo4jv1alpha1.Neo4jDatabase
				err := k8sClient.Get(ctx, types.NamespacedName{Name: invalidDatabase.Name, Namespace: invalidDatabase.Namespace}, &db)
				if err != nil {
					return ""
				}
				return db.Status.Message
			}, timeout, interval).Should(ContainSubstring("is not a valid CREATE DATABASE OPTIONS parameter"), "Controller should reject invalid OPTIONS")

			// Clean up
			defer func() {
				k8sClient.Delete(ctx, invalidDatabase)
			}()

			By("Creating database with valid but dotted key OPTIONS")
			dottedDatabase := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dotted-options-db",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "validation-cluster",
					Name:        "dottedoptionsdb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   1,
						Secondaries: 1,
					},
					Options: map[string]string{
						"txLogEnrichment": "OFF", // Valid OPTIONS parameter
					},
				},
			}

			err = k8sClient.Create(ctx, dottedDatabase)
			Expect(err).NotTo(HaveOccurred(), "Database with valid OPTIONS should be accepted")

			// Clean up
			defer func() {
				k8sClient.Delete(ctx, dottedDatabase)
			}()
		})
	})
})
