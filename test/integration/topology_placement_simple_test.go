package integration_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Topology Placement Simple", func() {
	var (
		namespace   *corev1.Namespace
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName string
	)

	BeforeEach(func() {
		// Create test namespace
		namespaceName := createTestNamespace("topology")
		clusterName = fmt.Sprintf("topology-test-%d", time.Now().Unix())

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
	})

	AfterEach(func() {
		if cluster != nil {
			// Remove finalizers and delete cluster
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			// Delete the resource
			err := k8sClient.Delete(ctx, cluster)
			if err != nil && !errors.IsNotFound(err) {
				By(fmt.Sprintf("Failed to delete cluster: %v", err))
			}
		}
		// Note: Namespace cleanup is handled by the test suite cleanup
	})

	Context("Topology Spread Constraints", func() {
		It("should apply topology spread constraints to StatefulSet", func() {
			// Create admin secret first
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("admin123"),
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						Placement: &neo4jv1alpha1.PlacementConfig{
							TopologySpread: &neo4jv1alpha1.TopologySpreadConfig{
								Enabled:           true,
								TopologyKey:       "topology.kubernetes.io/zone",
								MaxSkew:           1,
								WhenUnsatisfiable: "DoNotSchedule",
							},
						},
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
				},
			}

			// Create the cluster
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Wait for StatefulSet to be created
			Eventually(func() bool {
				sts := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespace.Name,
				}, sts)
				return err == nil
			}, 60*time.Second, 2*time.Second).Should(BeTrue())

			// Verify topology spread constraints are applied
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      clusterName + "-server",
				Namespace: namespace.Name,
			}, sts)).To(Succeed())

			// Check that topology spread constraints exist
			constraints := sts.Spec.Template.Spec.TopologySpreadConstraints
			Expect(constraints).To(HaveLen(1), "Should have exactly 1 topology spread constraint")

			constraint := constraints[0]
			Expect(constraint.TopologyKey).To(Equal("topology.kubernetes.io/zone"))
			Expect(constraint.MaxSkew).To(Equal(int32(1)))
			Expect(constraint.WhenUnsatisfiable).To(Equal(corev1.DoNotSchedule))

			// CRITICAL TEST: Verify correct label selector
			Expect(constraint.LabelSelector).NotTo(BeNil())
			expectedLabels := map[string]string{
				"app.kubernetes.io/name":      "neo4j",
				"app.kubernetes.io/instance":  clusterName,
				"app.kubernetes.io/component": "database", // MUST be "database", not "primary"
			}
			Expect(constraint.LabelSelector.MatchLabels).To(Equal(expectedLabels))
		})
	})
})
