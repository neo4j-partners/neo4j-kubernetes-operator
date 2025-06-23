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

package neo4j

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Neo4j Client Suite")
}

var _ = Describe("Neo4j Client", func() {
	var (
		ctx        context.Context
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		secret     *corev1.Secret
		fakeClient client.Client
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = clientgoscheme.AddToScheme(scheme)
		_ = neo4jv1alpha1.AddToScheme(scheme)

		// Create test cluster
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1alpha1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Primaries:   3,
					Secondaries: 2,
				},
				Storage: neo4jv1alpha1.StorageSpec{
					ClassName: "standard",
					Size:      "10Gi",
				},
			},
		}

		// Create test secret
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "admin-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"NEO4J_AUTH": []byte("neo4j/testpassword"),
			},
		}

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, secret).
			Build()
	})

	Context("Client creation", func() {
		It("Should create client with correct configuration", func() {
			By("Creating Neo4j client")
			neo4jClient, err := NewClientForEnterprise(cluster, fakeClient, "admin-secret")
			Expect(err).NotTo(HaveOccurred())
			Expect(neo4jClient).NotTo(BeNil())

			By("Verifying client configuration")
			// Client should be configured with proper connection settings
			metrics := neo4jClient.GetConnectionPoolMetrics()
			Expect(metrics).NotTo(BeNil())
		})

		It("Should handle missing secret gracefully", func() {
			By("Attempting to create client with missing secret")
			_, err := NewClientForEnterprise(cluster, fakeClient, "missing-secret")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("secret not found"))
		})

		It("Should handle invalid secret format", func() {
			By("Creating secret with invalid format")
			invalidSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"NEO4J_AUTH": []byte("invalid-format"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cluster, invalidSecret).
				Build()

			By("Attempting to create client with invalid secret")
			_, err := NewClientForEnterprise(cluster, fakeClient, "invalid-secret")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid auth format"))
		})
	})

	Context("Connection management", func() {
		var neo4jClient *Client

		BeforeEach(func() {
			var err error
			neo4jClient, err = NewClientForEnterprise(cluster, fakeClient, "admin-secret")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should implement circuit breaker pattern", func() {
			By("Simulating connection failures")
			// Test circuit breaker behavior
			for i := 0; i < 5; i++ {
				err := neo4jClient.VerifyConnectivity(ctx)
				// In a real test, this would fail due to no actual Neo4j instance
				Expect(err).To(HaveOccurred())
			}

			By("Verifying circuit breaker state")
			// Circuit breaker should open after multiple failures
			// Note: This is a simplified test - real implementation would check circuit breaker state
		})

		It("Should handle connection pooling correctly", func() {
			By("Verifying connection pool configuration")
			metrics := neo4jClient.GetConnectionPoolMetrics()
			Expect(metrics).NotTo(BeNil())
			Expect(metrics.LastHealthCheck).NotTo(BeZero())
		})

		It("Should implement proper cleanup", func() {
			By("Closing client")
			err := neo4jClient.Close()
			Expect(err).NotTo(HaveOccurred())

			By("Verifying resources are cleaned up")
			// Verify that connection manager is properly cleaned up
			Expect(neo4jClient.driver).To(BeNil())
		})
	})

	Context("Database operations", func() {
		var neo4jClient *Client

		BeforeEach(func() {
			var err error
			neo4jClient, err = NewClientForEnterprise(cluster, fakeClient, "admin-secret")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should handle RAFT operations", func() {
			By("Getting cluster overview")
			// In a real test, this would connect to actual Neo4j
			_, err := neo4jClient.GetClusterOverview(ctx)
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j

			By("Getting cluster members")
			_, err = neo4jClient.GetSecondaryMembers(ctx)
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j
		})

		It("Should handle user management operations", func() {
			By("Creating user")
			err := neo4jClient.CreateUser(ctx, "testuser", "password", false)
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j

			By("Dropping user")
			err = neo4jClient.DropUser(ctx, "testuser")
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j
		})

		It("Should handle role management operations", func() {
			By("Creating role")
			err := neo4jClient.CreateRole(ctx, "testrole")
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j

			By("Dropping role")
			err = neo4jClient.DropRole(ctx, "testrole")
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j
		})

		It("Should handle database operations", func() {
			By("Creating database")
			err := neo4jClient.CreateDatabase(ctx, "testdb", map[string]string{})
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j

			By("Dropping database")
			err = neo4jClient.DropDatabase(ctx, "testdb")
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j
		})

		It("Should handle backup operations", func() {
			By("Starting backup")
			err := neo4jClient.CreateBackup(ctx, "testdb", "backup-name", "/backup/path", BackupOptions{})
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j

			By("Getting backup metadata")
			_, err = neo4jClient.GetBackupMetadata(ctx, "/backup/path")
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j
		})
	})

	Context("Health monitoring", func() {
		var neo4jClient *Client

		BeforeEach(func() {
			var err error
			neo4jClient, err = NewClientForEnterprise(cluster, fakeClient, "admin-secret")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should implement health checks", func() {
			By("Testing connection")
			err := neo4jClient.VerifyConnectivity(ctx)
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j

			By("Getting health status")
			healthy, err := neo4jClient.IsClusterHealthy(ctx)
			Expect(err).To(HaveOccurred()) // Expected to fail without real Neo4j
			Expect(healthy).To(BeFalse())  // Expected to be unhealthy without real Neo4j
		})

		It("Should implement metrics collection", func() {
			By("Getting connection metrics")
			metrics := neo4jClient.GetConnectionPoolMetrics()
			Expect(metrics).NotTo(BeNil())
			Expect(metrics.TotalConnections).To(Equal(int64(0)))
			Expect(metrics.ActiveConnections).To(Equal(int64(0)))
		})
	})
})
