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

package webhooks

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Suite")
}

var _ = Describe("Neo4jEnterpriseCluster Webhook", func() {
	var (
		ctx     context.Context
		webhook *Neo4jEnterpriseClusterWebhook
		scheme  *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = clientgoscheme.AddToScheme(scheme)
		_ = neo4jv1alpha1.AddToScheme(scheme)

		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		webhook = &Neo4jEnterpriseClusterWebhook{
			Client: client,
		}
	})

	Context("Default validation", func() {
		It("Should set default values correctly", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
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

			err := webhook.Default(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Should set default edition
			Expect(cluster.Spec.Edition).To(Equal("enterprise"))
		})
	})

	Context("Validation webhook", func() {
		It("Should accept valid cluster configuration", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should reject even number of primaries", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   2, // Even number - should fail
						Secondaries: 2,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "10Gi",
					},
				},
			}

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("primary count must be odd"))
		})

		It("Should reject community edition", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "community-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "community", // Should fail
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-community",
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

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only enterprise edition"))
		})

		It("Should reject unsupported Neo4j version", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "old-version-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "4.4-enterprise", // Old version - should fail
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

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Neo4j version 5.26+"))
		})

		It("Should reject invalid TLS configuration", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-tls-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						// Missing IssuerRef - should fail
					},
				},
			}

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("issuerRef is required"))
		})

		It("Should reject invalid cloud identity configuration", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-cloud-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "gcp", // Mismatch - should fail
							},
						},
					},
				},
			}

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("provider mismatch"))
		})

		It("should validate cloud configuration", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider:       "aws",
								ServiceAccount: "neo4j-backup-sa",
							},
						},
					},
				},
			}

			_, err := webhook.ValidateCreate(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Update validation", func() {
		It("Should allow valid updates", func() {
			oldCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "update-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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

			newCluster := oldCluster.DeepCopy()
			newCluster.Spec.Image.Tag = "5.27-enterprise"
			newCluster.Spec.Topology.Secondaries = 3

			_, err := webhook.ValidateUpdate(ctx, oldCluster, newCluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should reject downgrade attempts", func() {
			oldCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "downgrade-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.27-enterprise",
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

			newCluster := oldCluster.DeepCopy()
			newCluster.Spec.Image.Tag = "5.26-enterprise" // Downgrade - should fail

			_, err := webhook.ValidateUpdate(ctx, oldCluster, newCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("downgrades are not supported"))
		})

		It("Should reject storage class changes", func() {
			oldCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-change-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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

			newCluster := oldCluster.DeepCopy()
			newCluster.Spec.Storage.ClassName = "premium" // Change storage class - should fail

			_, err := webhook.ValidateUpdate(ctx, oldCluster, newCluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("storage class cannot be changed"))
		})
	})
})
