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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Database API Integration Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	Context("When creating Neo4jDatabase resources", func() {
		var testNamespace string

		BeforeEach(func() {
			By("Creating test namespace")
			testNamespace = createTestNamespace("db-api")
		})

		It("Should create a database resource with all new fields", func() {
			By("Creating a database with all features")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-database-full",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "test-cluster",
					Name:        "testdb",
					Wait:        true,
					IfNotExists: true,
					Topology: &neo4jv1alpha1.DatabaseTopology{
						Primaries:   2,
						Secondaries: 1,
					},
					DefaultCypherLanguage: "25",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database resource should be created")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: database.Namespace,
				}, database)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying all fields are set correctly")
			Expect(database.Spec.Wait).To(BeTrue())
			Expect(database.Spec.IfNotExists).To(BeTrue())
			Expect(database.Spec.Topology).NotTo(BeNil())
			Expect(database.Spec.Topology.Primaries).To(Equal(int32(2)))
			Expect(database.Spec.Topology.Secondaries).To(Equal(int32(1)))
			Expect(database.Spec.DefaultCypherLanguage).To(Equal("25"))
		})

		It("Should create database with standalone reference", func() {
			By("Creating a database that references a Neo4jEnterpriseStandalone")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-database-standalone",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:  "test-standalone", // References standalone resource
					Name:        "teststandalonedb",
					Wait:        true,
					IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database resource should be created and accept standalone reference")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: database.Namespace,
				}, database)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying standalone reference is preserved")
			Expect(database.Spec.ClusterRef).To(Equal("test-standalone"))
		})

		It("Should create a database with minimal configuration", func() {
			By("Creating a simple database")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-database-minimal",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster",
					Name:       "simpledb",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Database should have default values")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      database.Name,
					Namespace: database.Namespace,
				}, database)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Default values should be applied by the webhook or controller
			Expect(database.Spec.Topology).To(BeNil())
			Expect(database.Spec.DefaultCypherLanguage).To(BeEmpty())
		})

		It("Should validate Cypher language version enum", func() {
			By("Creating database with valid Cypher version")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-database-cypher5",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef:            "test-cluster",
					Name:                  "cypher5db",
					DefaultCypherLanguage: "5",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())
		})
	})
})
