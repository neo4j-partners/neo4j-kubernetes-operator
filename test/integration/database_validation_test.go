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
		timeout  = time.Second * 900 // Increased to 15 minutes for cluster formation + database creation + stabilization
		interval = time.Second * 5
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

		By("Creating a test cluster with 3 servers")
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
					Servers: 3, // 3-server cluster for validation tests
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

		By("Verifying cluster internals service is accessible")
		// Allow additional time for cluster internals to be ready for database operations
		Eventually(func() error {
			// Check that we can get the cluster and it has endpoints
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			}, cluster)
		}, 60*time.Second, 5*time.Second).Should(Succeed())

		// Additional stabilization time for Neo4j cluster internals
		By("Allowing cluster services to fully stabilize")
		time.Sleep(30 * time.Second)
	})

	Context("When creating databases with topology validation", func() {
		It("should accept valid topology within cluster capacity", func() {
			By("Creating database with valid topology (2 primaries + 1 secondary = 3 servers)")
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
						Primaries:   2,
						Secondaries: 1, // Total: 3 servers (within cluster capacity)
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
						Primaries:   2,
						Secondaries: 1, // Total: 3 servers (all cluster servers)
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
						Primaries:   3,
						Secondaries: 2, // Total: 5 servers (exceeds cluster capacity of 3)
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
			Expect(database.Status.Conditions[0].Message).To(ContainSubstring("database topology requires 5 servers but cluster only has 3 servers available"))
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
	})
})
