package controller_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Database API Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	Context("When creating Neo4jDatabase resources", func() {
		var testNamespace string

		BeforeEach(func() {
			testNamespace = fmt.Sprintf("db-api-%d", time.Now().UnixNano())
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())
		})

		It("Should create a database resource with all new fields", func() {
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-database-full", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster", Name: "testdb",
					Wait: true, IfNotExists: true,
					Topology:              &neo4jv1beta1.DatabaseTopology{Primaries: 2, Secondaries: 1},
					DefaultCypherLanguage: "25",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: database.Name, Namespace: testNamespace}, database)
			}, timeout, interval).Should(Succeed())
			Expect(database.Spec.Wait).To(BeTrue())
			Expect(database.Spec.Topology.Primaries).To(Equal(int32(2)))
			Expect(database.Spec.DefaultCypherLanguage).To(Equal("25"))
		})

		It("Should create database with standalone reference", func() {
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-database-standalone", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-standalone", Name: "teststandalonedb",
					Wait: true, IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: database.Name, Namespace: testNamespace}, database)
			}, timeout, interval).Should(Succeed())
			Expect(database.Spec.ClusterRef).To(Equal("test-standalone"))
		})

		It("Should create a database with minimal configuration", func() {
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-database-minimal", Namespace: testNamespace},
				Spec:       neo4jv1beta1.Neo4jDatabaseSpec{ClusterRef: "test-cluster", Name: "simpledb"},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: database.Name, Namespace: testNamespace}, database)
			}, timeout, interval).Should(Succeed())
			Expect(database.Spec.Topology).To(BeNil())
			Expect(database.Spec.DefaultCypherLanguage).To(BeEmpty())
		})

		It("Should validate Cypher language version enum", func() {
			database := &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-database-cypher5", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef: "test-cluster", Name: "cypher5db",
					DefaultCypherLanguage: "5",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())
		})
	})
})
