package integration_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Rolling Upgrade Integration", func() {
	var (
		ctx         context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName string
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Skip unless the caller has specified a target upgrade version.
		// This avoids wasting CI resources when upgrade images are not pre-loaded.
		// Set NEO4J_UPGRADE_TARGET_VERSION=<enterprise-tag> to enable this test.
		if os.Getenv("NEO4J_UPGRADE_TARGET_VERSION") == "" {
			Skip("Rolling upgrade test skipped: set NEO4J_UPGRADE_TARGET_VERSION to enable")
		}

		namespaceName := createTestNamespace("rolling-upgrade")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}

		clusterName = fmt.Sprintf("rolling-upgrade-%d", time.Now().Unix())

		// Create admin secret for authentication
		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: namespaceName,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())
	})

	AfterEach(func() {
		if cluster != nil {
			By("Cleaning up cluster resource")
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			_ = k8sClient.Delete(ctx, cluster)
		}

		if namespace != nil {
			cleanupCustomResourcesInNamespace(namespace.Name)
			_ = k8sClient.Delete(ctx, namespace)
		}
	})

	Context("Leader-aware rolling upgrade on single StatefulSet", func() {
		It("performs a rolling upgrade to the target version", SpecTimeout(30*time.Minute), func(ctx SpecContext) {
			if !isOperatorRunning() {
				Skip("Operator must be running in the cluster for integration tests")
			}

			initialTag := getNeo4jImageTag()
			targetTag := os.Getenv("NEO4J_UPGRADE_TARGET_VERSION")

			serverCount := getCIAppropriateClusterSize(3)

			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  initialTag,
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AuthenticationProviders: []string{"native"},
						AdminSecret:             "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: serverCount,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Resources: getCIAppropriateResourceRequirements(),
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

			By("Creating Neo4jEnterpriseCluster with initial image")
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			serverKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-server", clusterName),
				Namespace: namespace.Name,
			}

			By("Waiting for server StatefulSet to be ready with the initial image")
			Eventually(func(g Gomega) {
				serverSts := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, serverKey, serverSts)).To(Succeed())
				g.Expect(serverSts.Spec.Replicas).NotTo(BeNil())
				g.Expect(*serverSts.Spec.Replicas).To(Equal(serverCount))
				g.Expect(serverSts.Status.ReadyReplicas).To(Equal(serverCount))
				g.Expect(serverSts.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				g.Expect(strings.Contains(serverSts.Spec.Template.Spec.Containers[0].Image, initialTag)).To(BeTrue())
			}, clusterTimeout, interval).Should(Succeed())

			By("Waiting for cluster to report Ready before upgrade")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return false
				}
				return cluster.Status.Phase == "Ready"
			}, clusterTimeout, interval).Should(BeTrue())

			By("Triggering rolling upgrade by updating image tag")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return err
				}
				cluster.Spec.Image.Tag = targetTag
				return k8sClient.Update(ctx, cluster)
			}, clusterTimeout, interval).Should(Succeed())

			By("Waiting for StatefulSet to roll to the target image")
			Eventually(func() bool {
				serverSts := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, serverKey, serverSts); err != nil {
					return false
				}
				if serverSts.Spec.Template.Spec.Containers == nil {
					return false
				}
				image := serverSts.Spec.Template.Spec.Containers[0].Image
				return strings.Contains(image, targetTag) &&
					serverSts.Status.ReadyReplicas == serverCount &&
					serverSts.Status.UpdatedReplicas == serverCount
			}, 2*clusterTimeout, interval).Should(BeTrue())

			By("Waiting for upgrade status to complete")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace.Name,
				}, cluster)
				if err != nil {
					return false
				}

				if cluster.Status.UpgradeStatus == nil {
					return false
				}

				return cluster.Status.UpgradeStatus.Phase == "Completed" &&
					cluster.Status.Version == targetTag &&
					cluster.Status.Phase == "Ready"
			}, 2*clusterTimeout, interval).Should(BeTrue())
		})
	})
})
