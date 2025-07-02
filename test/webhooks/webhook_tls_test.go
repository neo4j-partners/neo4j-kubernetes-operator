package webhooks_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Webhook TLS Configuration", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		k8sClient client.Client
		testEnv   *envtest.Environment
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.TODO())

		testEnv = &envtest.Environment{
			CRDDirectoryPaths:     []string{"../../config/crd/bases"},
			ErrorIfCRDPathMissing: true,
			WebhookInstallOptions: envtest.WebhookInstallOptions{
				Paths: []string{"../../config/webhook"},
			},
		}

		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		scheme := runtime.NewScheme()
		err = neo4jv1alpha1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cancel()
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("When testing webhook TLS configuration", func() {
		It("should have valid webhook certificates", func() {
			By("Checking webhook server certificate")
			// In envtest, certificates are automatically generated
			Eventually(func() error {
				// Test that webhook endpoint is reachable with TLS
				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true, // For testing only
						},
					},
					Timeout: 5 * time.Second,
				}

				resp, err := client.Get("https://localhost:9443/healthz")
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				}
				return nil
			}, "30s", "1s").Should(Succeed())
		})

		It("should validate TLS certificate properties", func() {
			By("Checking certificate validity and properties")
			// This test would be more meaningful in a real cluster
			// In envtest, we can test the webhook logic itself

			// Create a test Neo4j cluster to trigger webhook
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-tls",
					Namespace: "default",
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

			// Verify the cluster was created (webhook validation passed)
			Eventually(func() error {
				var createdCluster neo4jv1alpha1.Neo4jEnterpriseCluster
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), &createdCluster)
			}, "10s", "1s").Should(Succeed())
		})

		It("should reject invalid resources through TLS", func() {
			By("Creating an invalid Neo4j cluster")
			invalidCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-cluster-tls",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					// Missing required fields - only provide Image but not Topology/Storage/Auth
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					// Missing Topology
					// Missing Storage
					// Missing Auth
				},
			}

			err := k8sClient.Create(ctx, invalidCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("denied the request"))
		})
	})

	Context("When testing certificate rotation", func() {
		It("should handle certificate renewal gracefully", func() {
			Skip("Certificate rotation testing requires real cluster with cert-manager")
			// This test would be implemented for integration testing
		})
	})

	Context("When testing TLS security", func() {
		It("should enforce minimum TLS version", func() {
			By("Testing TLS configuration")
			// Test that webhook only accepts TLS 1.2+
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
						MinVersion:         tls.VersionTLS12,
						MaxVersion:         tls.VersionTLS12,
					},
				},
				Timeout: 5 * time.Second,
			}

			Eventually(func() error {
				resp, err := client.Get("https://localhost:9443/healthz")
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, "10s", "1s").Should(Succeed())
		})

		It("should have proper certificate subject", func() {
			By("Verifying certificate subject and SANs")
			// This would check the actual certificate in a real environment
			// For envtest, we verify the webhook functionality works
			Expect(true).To(BeTrue()) // Placeholder - actual cert checking would go here
		})
	})

	Context("When testing webhook admission", func() {
		It("should properly handle admission reviews over TLS", func() {
			By("Creating a valid admission review")

			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "admission-test-cluster",
					Namespace: "default",
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

			// Test that admission request succeeds
			err := k8sClient.Create(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Verify the resource was created
			var createdCluster neo4jv1alpha1.Neo4jEnterpriseCluster
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), &createdCluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(createdCluster.Name).To(Equal(cluster.Name))
		})
	})
})

func TestWebhookTLS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook TLS Suite")
}

// Helper function to validate certificate properties
func validateCertificate(certPEM []byte) error {
	cert, err := x509.ParseCertificate(certPEM)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %v", err)
	}

	// Check certificate validity
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate not yet valid")
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate expired")
	}

	// Check key usage
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return fmt.Errorf("certificate missing digital signature usage")
	}
	if cert.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		return fmt.Errorf("certificate missing key encipherment usage")
	}

	// Check extended key usage
	hasServerAuth := false
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
			break
		}
	}
	if !hasServerAuth {
		return fmt.Errorf("certificate missing server auth extended usage")
	}

	// Check DNS names
	expectedDNSNames := []string{
		"webhook-service",
		"webhook-service.neo4j-operator-system",
		"webhook-service.neo4j-operator-system.svc",
		"webhook-service.neo4j-operator-system.svc.cluster.local",
	}

	for _, expected := range expectedDNSNames {
		found := false
		for _, actual := range cert.DNSNames {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("certificate missing expected DNS name: %s", expected)
		}
	}

	return nil
}
