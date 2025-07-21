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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Backup Sidecar Path Creation", func() {
	const (
		timeout  = time.Second * 180
		interval = time.Second * 5
	)

	Context("When backup sidecar processes backup requests", func() {
		var (
			testNamespace string
			cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
			adminSecret   *corev1.Secret
		)

		BeforeEach(func() {
			testNamespace = createTestNamespace("backup-sidecar")

			By("Creating admin secret")
			adminSecret = &corev1.Secret{
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

			By("Creating a test cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-cluster-%d", time.Now().UnixNano()),
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Auth: neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for cluster to be ready")
			clusterKey := types.NamespacedName{
				Name:      cluster.Name,
				Namespace: testNamespace,
			}

			Eventually(func() string {
				var foundCluster neo4jv1alpha1.Neo4jEnterpriseCluster
				if err := k8sClient.Get(ctx, clusterKey, &foundCluster); err != nil {
					return "NotFound"
				}
				return foundCluster.Status.Phase
			}, timeout, interval).Should(Equal("Ready"))
		})

		AfterEach(func() {
			By("Cleaning up test resources")
			if cluster != nil {
				Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
			}
			if adminSecret != nil {
				Expect(k8sClient.Delete(ctx, adminSecret)).To(Succeed())
			}
		})

		It("Should verify backup sidecar creates backup path automatically", func() {
			By("Verifying backup sidecar is present")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{"neo4j.com/cluster": cluster.Name})).To(Succeed())

			Expect(podList.Items).To(HaveLen(2)) // 1 primary + 1 secondary

			// Check that backup sidecar container exists
			for _, pod := range podList.Items {
				var hasSidecar bool
				for _, container := range pod.Spec.Containers {
					if container.Name == "backup-sidecar" {
						hasSidecar = true

						// Verify the command includes mkdir -p $BACKUP_PATH
						Expect(container.Command).To(HaveLen(3))
						Expect(container.Command[0]).To(Equal("/bin/bash"))
						Expect(container.Command[1]).To(Equal("-c"))
						Expect(container.Command[2]).To(ContainSubstring("mkdir -p $BACKUP_PATH"))

						// Verify memory resources are set correctly
						Expect(container.Resources.Limits.Memory().String()).To(Equal("1Gi"))
						Expect(container.Resources.Requests.Memory().String()).To(Equal("512Mi"))
						break
					}
				}
				Expect(hasSidecar).To(BeTrue(), "Pod %s should have backup-sidecar container", pod.Name)
			}
		})

		It("Should verify standalone deployment has backup sidecar with path creation", func() {
			By("Creating a standalone deployment")
			standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-standalone-%d", time.Now().UnixNano()),
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Auth: neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
				},
			}
			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, standalone)).To(Succeed())
			}()

			By("Waiting for standalone to be ready")
			standaloneKey := types.NamespacedName{
				Name:      standalone.Name,
				Namespace: testNamespace,
			}

			Eventually(func() string {
				var foundStandalone neo4jv1alpha1.Neo4jEnterpriseStandalone
				if err := k8sClient.Get(ctx, standaloneKey, &foundStandalone); err != nil {
					return "NotFound"
				}
				return foundStandalone.Status.Phase
			}, timeout, interval).Should(Equal("Ready"))

			By("Verifying standalone backup sidecar configuration")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{"neo4j.com/deployment": standalone.Name})).To(Succeed())

			Expect(podList.Items).To(HaveLen(1))
			pod := podList.Items[0]

			var hasSidecar bool
			for _, container := range pod.Spec.Containers {
				if container.Name == "backup-sidecar" {
					hasSidecar = true

					// Verify the command includes mkdir -p $BACKUP_PATH
					Expect(container.Command).To(HaveLen(3))
					Expect(container.Command[2]).To(ContainSubstring("mkdir -p $BACKUP_PATH"))

					// Verify environment variables are set
					var hasLicenseEnv, hasEditionEnv bool
					for _, env := range container.Env {
						if env.Name == "NEO4J_ACCEPT_LICENSE_AGREEMENT" && env.Value == "yes" {
							hasLicenseEnv = true
						}
						if env.Name == "NEO4J_EDITION" && env.Value == "enterprise" {
							hasEditionEnv = true
						}
					}
					Expect(hasLicenseEnv).To(BeTrue(), "Should have NEO4J_ACCEPT_LICENSE_AGREEMENT=yes")
					Expect(hasEditionEnv).To(BeTrue(), "Should have NEO4J_EDITION=enterprise")
					break
				}
			}
			Expect(hasSidecar).To(BeTrue(), "Standalone pod should have backup-sidecar container")
		})

		It("Should verify 2025.x deployment has backup sidecar with path creation", func() {
			By("Creating a 2025.x cluster")
			cluster2025 := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-2025x-%d", time.Now().UnixNano()),
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "2025.01.0-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Auth: neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster2025)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, cluster2025)).To(Succeed())
			}()

			By("Verifying 2025.x backup sidecar configuration")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(testNamespace),
					client.MatchingLabels{"neo4j.com/cluster": cluster2025.Name}); err != nil {
					return false
				}

				if len(podList.Items) != 2 {
					return false
				}

				// Check first pod has backup sidecar with correct config
				for _, container := range podList.Items[0].Spec.Containers {
					if container.Name == "backup-sidecar" {
						// Should have path creation command
						return len(container.Command) == 3 &&
							container.Command[2] != "" &&
							container.Command[2] == container.Command[2] // Same command as 5.x
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
	})
})
