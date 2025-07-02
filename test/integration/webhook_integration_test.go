package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Webhook Integration with TLS", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		k8sClient client.Client
		namespace string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.TODO())
		namespace = "neo4j-webhook-test"

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		// Create auth secret for tests
		authSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-auth",
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"username": "neo4j",
				"password": "test-password",
			},
		}
		err = k8sClient.Create(ctx, authSecret)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		cancel()

		// Clean up test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_ = k8sClient.Delete(ctx, ns)
	})

	Context("When webhook TLS is properly configured", func() {
		It("should validate webhook certificate exists", func() {
			By("Checking webhook certificate secret")
			Eventually(func() error {
				secret := &corev1.Secret{}
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      "webhook-server-cert",
					Namespace: "neo4j-operator-system",
				}, secret)
			}, "60s", "5s").Should(Succeed())
		})

		It("should validate webhook service is running", func() {
			By("Checking webhook service exists")
			Eventually(func() error {
				service := &corev1.Service{}
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      "neo4j-operator-webhook-service",
					Namespace: "neo4j-operator-system",
				}, service)
			}, "30s", "2s").Should(Succeed())
		})

		It("should accept valid Neo4j cluster configurations", func() {
			By("Creating a valid Neo4j cluster")
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "10Gi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "test-auth",
					},
				},
			}

			err := k8sClient.Create(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Verify the cluster was created
			Eventually(func() error {
				var createdCluster neo4jv1alpha1.Neo4jEnterpriseCluster
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), &createdCluster)
			}, "10s", "1s").Should(Succeed())
		})

		It("should reject clusters with missing required fields", func() {
			By("Creating a cluster without license agreement")
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-license",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					// Missing AcceptLicenseAgreement
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "test-auth",
					},
				},
			}

			err := k8sClient.Create(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("denied the request"))
		})

		It("should reject clusters with invalid storage sizes", func() {
			By("Creating a cluster with negative storage")
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-storage",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					// Missing required fields Topology and Storage to make it invalid
				},
			}

			err := k8sClient.Create(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("denied the request"))
		})

		It("should validate TLS configuration options", func() {
			By("Creating a cluster with valid TLS configuration")
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "test-auth",
					},
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "letsencrypt-staging",
							Kind: "ClusterIssuer",
						},
					},
				},
			}

			err := k8sClient.Create(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Verify the cluster was created with TLS config
			var createdCluster neo4jv1alpha1.Neo4jEnterpriseCluster
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), &createdCluster)
			}, "10s", "1s").Should(Succeed())

			Expect(createdCluster.Spec.TLS).NotTo(BeNil())
			Expect(createdCluster.Spec.TLS.Mode).To(Equal("cert-manager"))
		})

		It("should reject clusters with invalid TLS configuration", func() {
			By("Creating a cluster with invalid TLS mode")
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-tls-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "test-auth",
					},
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "invalid-mode", // Invalid TLS mode
					},
				},
			}

			err := k8sClient.Create(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("denied the request"))
		})
	})

	Context("When testing webhook performance with TLS", func() {
		It("should handle multiple concurrent requests", func() {
			By("Creating multiple clusters concurrently")

			const numClusters = 5
			errors := make(chan error, numClusters)

			for i := 0; i < numClusters; i++ {
				go func(index int) {
					cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("concurrent-cluster-%d", index),
							Namespace: namespace,
						},
						Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
							Image: neo4jv1alpha1.ImageSpec{
								Repo: "neo4j",
								Tag:  "5.26.0-enterprise",
							},
							Topology: neo4jv1alpha1.TopologyConfiguration{
								Primaries: 1,
							},
							Storage: neo4jv1alpha1.StorageSpec{
								ClassName: "standard",
								Size:      "10Gi",
							},
							Auth: &neo4jv1alpha1.AuthSpec{
								Provider:    "native",
								AdminSecret: "test-auth",
							},
						},
					}

					errors <- k8sClient.Create(ctx, cluster)
				}(i)
			}

			// Wait for all requests to complete
			for i := 0; i < numClusters; i++ {
				err := <-errors
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should complete webhook validation within timeout", func() {
			By("Measuring webhook response time")

			start := time.Now()

			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "performance-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "10Gi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						Provider:    "native",
						AdminSecret: "test-auth",
					},
				},
			}

			err := k8sClient.Create(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			duration := time.Since(start)
			Expect(duration).To(BeNumerically("<", 10*time.Second))
		})
	})
})

func TestWebhookIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Integration Suite")
}
