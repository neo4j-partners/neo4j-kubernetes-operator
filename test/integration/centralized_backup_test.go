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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Centralized Backup Configuration", func() {
	const (
		timeout  = time.Second * 600 // Increased to 10 minutes for cluster formation + backup operations
		interval = time.Second * 5
	)

	Context("When centralized backup processes backup requests", func() {
		var (
			testNamespace string
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
		})

		AfterEach(func() {
			By("Cleaning up test resources")

			// Clean up any standalone resources
			standaloneList := &neo4jv1alpha1.Neo4jEnterpriseStandaloneList{}
			if err := k8sClient.List(ctx, standaloneList, client.InNamespace(testNamespace)); err == nil {
				for i := range standaloneList.Items {
					standalone := &standaloneList.Items[i]
					if len(standalone.GetFinalizers()) > 0 {
						standalone.SetFinalizers([]string{})
						_ = k8sClient.Update(ctx, standalone)
					}
					_ = k8sClient.Delete(ctx, standalone)
				}
			}

			// Clean up any cluster resources that might have been created in other tests
			clusterList := &neo4jv1alpha1.Neo4jEnterpriseClusterList{}
			if err := k8sClient.List(ctx, clusterList, client.InNamespace(testNamespace)); err == nil {
				for i := range clusterList.Items {
					cluster := &clusterList.Items[i]
					if len(cluster.GetFinalizers()) > 0 {
						cluster.SetFinalizers([]string{})
						_ = k8sClient.Update(ctx, cluster)
					}
					_ = k8sClient.Delete(ctx, cluster)
				}
			}

			// Clean up admin secret
			if adminSecret != nil {
				_ = k8sClient.Delete(ctx, adminSecret)
			}
		})

		It("Should verify cluster deployment has centralized backup StatefulSet", func() {
			By("Creating a cluster with backups enabled")
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-backup-cluster-%d", time.Now().UnixNano()),
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"), // Reduced for CI constraints
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"), // Reduced for CI constraints
							corev1.ResourceMemory: resource.MustParse("1Gi"),  // Neo4j Enterprise minimum requirement
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
					Backups: &neo4jv1alpha1.BackupsSpec{
						// Enable backups (just having the section enables centralized backup)
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
			defer func() {
				// Clean up cluster with finalizer removal if needed
				cleanupResource(cluster, testNamespace, "Neo4jEnterpriseCluster")
			}()

			// Log that we created the cluster
			GinkgoWriter.Printf("Created cluster %s in namespace %s with backups enabled\n", cluster.Name, testNamespace)

			By("Waiting for cluster to be ready")
			clusterKey := types.NamespacedName{
				Name:      cluster.Name,
				Namespace: testNamespace,
			}

			Eventually(func() bool {
				var foundCluster neo4jv1alpha1.Neo4jEnterpriseCluster
				if err := k8sClient.Get(ctx, clusterKey, &foundCluster); err != nil {
					GinkgoWriter.Printf("Failed to get cluster: %v\n", err)
					return false
				}

				// Log status for debugging
				GinkgoWriter.Printf("Cluster %s status: Phase=%s, Message=%s\n",
					foundCluster.Name, foundCluster.Status.Phase, foundCluster.Status.Message)

				for _, condition := range foundCluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Verifying centralized backup StatefulSet is created")
			backupStsKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-backup", cluster.Name),
				Namespace: testNamespace,
			}
			var backupSts appsv1.StatefulSet

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, backupStsKey, &backupSts); err != nil {
					GinkgoWriter.Printf("Backup StatefulSet not found: %v\n", err)
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue(), "Backup StatefulSet should be created")

			By("Verifying backup StatefulSet configuration")
			// Verify replica count (should be 1 for centralized backup)
			Expect(*backupSts.Spec.Replicas).To(Equal(int32(1)), "Backup StatefulSet should have 1 replica")

			// Verify labels
			Expect(backupSts.Labels["neo4j.com/cluster"]).To(Equal(cluster.Name))
			Expect(backupSts.Labels["neo4j.com/component"]).To(Equal("backup"))

			// Verify container configuration
			Expect(backupSts.Spec.Template.Spec.Containers).To(HaveLen(1))
			backupContainer := backupSts.Spec.Template.Spec.Containers[0]
			Expect(backupContainer.Name).To(Equal("backup"))

			// Verify environment variables are set correctly
			var hasLicenseEnv, hasEditionEnv, hasClusterEnv bool
			for _, env := range backupContainer.Env {
				if env.Name == "NEO4J_ACCEPT_LICENSE_AGREEMENT" && env.Value == "yes" {
					hasLicenseEnv = true
				}
				if env.Name == "NEO4J_EDITION" && env.Value == "enterprise" {
					hasEditionEnv = true
				}
				if env.Name == "NEO4J_CLUSTER_NAME" && env.Value == cluster.Name {
					hasClusterEnv = true
				}
			}
			Expect(hasLicenseEnv).To(BeTrue(), "Should have NEO4J_ACCEPT_LICENSE_AGREEMENT=yes")
			Expect(hasEditionEnv).To(BeTrue(), "Should have NEO4J_EDITION=enterprise")
			Expect(hasClusterEnv).To(BeTrue(), "Should have NEO4J_CLUSTER_NAME set")

			// Verify volume mounts
			var hasBackupStorage, hasBackupRequests bool
			for _, mount := range backupContainer.VolumeMounts {
				if mount.Name == "backup-storage" && mount.MountPath == "/backups" {
					hasBackupStorage = true
				}
				if mount.Name == "backup-requests" && mount.MountPath == "/backup-requests" {
					hasBackupRequests = true
				}
			}
			Expect(hasBackupStorage).To(BeTrue(), "Should have backup-storage volume mount")
			Expect(hasBackupRequests).To(BeTrue(), "Should have backup-requests volume mount")

			// Verify PVC template is created
			Expect(backupSts.Spec.VolumeClaimTemplates).To(HaveLen(1))
			pvcTemplate := backupSts.Spec.VolumeClaimTemplates[0]
			Expect(pvcTemplate.ObjectMeta.Name).To(Equal("backup-storage"))
		})

		It("Should verify 2025.x deployment has centralized backup StatefulSet", func() {
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
						Servers: 2,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"), // Reduced for CI constraints
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"), // Reduced for CI constraints
							corev1.ResourceMemory: resource.MustParse("1Gi"),  // Neo4j Enterprise minimum requirement
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
					Backups: &neo4jv1alpha1.BackupsSpec{
						// Enable backups for 2025.x
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster2025)).To(Succeed())
			defer func() {
				cleanupResource(cluster2025, testNamespace, "Neo4jEnterpriseCluster")
			}()

			By("Verifying 2025.x centralized backup configuration")
			// Wait for cluster to be ready first
			Eventually(func() bool {
				var foundCluster neo4jv1alpha1.Neo4jEnterpriseCluster
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: cluster2025.Name, Namespace: testNamespace}, &foundCluster); err != nil {
					return false
				}
				for _, condition := range foundCluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Verify backup StatefulSet exists for 2025.x
			backupStsKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-backup", cluster2025.Name),
				Namespace: testNamespace,
			}
			var backupSts appsv1.StatefulSet

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, backupStsKey, &backupSts); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue(), "2025.x cluster should have centralized backup StatefulSet")

			// Verify 2025.x backup configuration is same as 5.x (unified approach)
			Expect(*backupSts.Spec.Replicas).To(Equal(int32(1)), "2025.x backup should also be centralized")
			Expect(backupSts.Labels["neo4j.com/component"]).To(Equal("backup"))
		})
	})
})
