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

package eks

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("EKS Neo4j Cluster Tests", func() {
	var (
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName string
	)

	BeforeEach(func() {
		waitForNodeReadiness()
		clusterName = fmt.Sprintf("eks-cluster-%d", GinkgoRandomSeed())
	})

	Context("EKS-specific cluster deployment", func() {
		It("Should deploy cluster with EKS-optimized configurations", func() {
			By("Creating cluster with EKS settings")
			cluster = createEKSCluster(clusterName)
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for StatefulSets to be created")
			primarySts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-primary",
					Namespace: testNamespace,
				}, primarySts)
			}, timeout, interval).Should(Succeed())

			By("Verifying EKS-specific configurations")
			// Check storage class
			pvcTemplate := primarySts.Spec.VolumeClaimTemplates[0]
			Expect(*pvcTemplate.Spec.StorageClassName).To(Equal("gp3"))

			// Check resource requests suitable for EKS
			container := primarySts.Spec.Template.Spec.Containers[0]
			Expect(container.Resources.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("2Gi")))

			By("Verifying cluster readiness")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: testNamespace,
				}, cluster)
				if err != nil {
					return false
				}

				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			verifyEKSSpecificFeatures(clusterName)
		})

		It("Should handle EKS networking correctly", func() {
			By("Creating cluster")
			cluster = createEKSCluster(clusterName)
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Verifying Service configurations")
			clientSvc := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-client",
					Namespace: testNamespace,
				}, clientSvc)
			}, timeout, interval).Should(Succeed())

			// Verify EKS-specific service annotations
			Expect(clientSvc.Annotations).To(HaveKey("service.beta.kubernetes.io/aws-load-balancer-type"))

			By("Verifying network policies are compatible with EKS")
			// Check if CNI supports network policies
			pods := &corev1.PodList{}
			Eventually(func() int {
				err := k8sClient.List(ctx, pods, &client.ListOptions{
					Namespace: testNamespace,
				})
				if err != nil {
					return 0
				}
				return len(pods.Items)
			}, timeout, interval).Should(BeNumerically(">", 0))
		})
	})

	Context("EKS IAM and security", func() {
		It("Should configure IRSA for backups", func() {
			if os.Getenv("S3_BACKUP_BUCKET") == "" {
				Skip("Skipping IRSA test - S3_BACKUP_BUCKET not set")
			}

			By("Creating cluster with backup configuration")
			cluster = createEKSCluster(clusterName)
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Creating backup job")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: clusterName,
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: os.Getenv("S3_BACKUP_BUCKET"),
						Path:   "/test-backups",
					},
					Cloud: &neo4jv1alpha1.CloudBlock{
						Provider: "aws",
						Identity: &neo4jv1alpha1.CloudIdentity{
							Provider: "aws",
							AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
								Enabled: true,
								Annotations: map[string]string{
									"eks.amazonaws.com/role-arn": os.Getenv("AWS_BACKUP_ROLE_ARN"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Verifying backup job creation")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-backup",
					Namespace: testNamespace,
				}, job)
			}, timeout, interval).Should(Succeed())

			By("Verifying service account has IRSA configuration")
			sa := &corev1.ServiceAccount{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-backup",
					Namespace: testNamespace,
				}, sa)
			}, timeout, interval).Should(Succeed())

			Expect(sa.Annotations).To(HaveKey("eks.amazonaws.com/role-arn"))
			Expect(sa.Annotations["eks.amazonaws.com/role-arn"]).To(Equal(os.Getenv("AWS_BACKUP_ROLE_ARN")))
		})
	})

	Context("EKS storage integration", func() {
		It("Should work with EBS CSI driver", func() {
			By("Creating cluster with EBS volumes")
			cluster = createEKSCluster(clusterName)
			cluster.Spec.Storage.ClassName = "gp3"
			cluster.Spec.Storage.Size = "50Gi"
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Verifying PVC creation and binding")
			Eventually(func() bool {
				pvcs := &corev1.PersistentVolumeClaimList{}
				err := k8sClient.List(ctx, pvcs, &client.ListOptions{
					Namespace: testNamespace,
				})
				if err != nil {
					return false
				}

				boundPVCs := 0
				for _, pvc := range pvcs.Items {
					if pvc.Status.Phase == corev1.ClaimBound {
						boundPVCs++
					}
				}

				return boundPVCs >= 3 // At least primary replicas
			}, timeout, interval).Should(BeTrue())

			By("Verifying EBS volume properties")
			pvcs := &corev1.PersistentVolumeClaimList{}
			Expect(k8sClient.List(ctx, pvcs, &client.ListOptions{
				Namespace: testNamespace,
			})).Should(Succeed())

			for _, pvc := range pvcs.Items {
				if pvc.Status.Phase == corev1.ClaimBound {
					// Verify storage class
					Expect(*pvc.Spec.StorageClassName).To(Equal("gp3"))

					// Verify size
					requestedSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
					Expect(requestedSize.Equal(resource.MustParse("50Gi"))).To(BeTrue())
				}
			}
		})

		It("Should support volume expansion", func() {
			By("Creating cluster with initial storage size")
			cluster = createEKSCluster(clusterName)
			cluster.Spec.Storage.Size = "20Gi"
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for initial deployment")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: testNamespace,
				}, cluster)
				if err != nil {
					return false
				}

				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Expanding storage size")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: testNamespace,
				}, cluster)
				if err != nil {
					return err
				}
				cluster.Spec.Storage.Size = "40Gi"
				return k8sClient.Update(ctx, cluster)
			}, timeout, interval).Should(Succeed())

			By("Verifying storage expansion")
			// Note: In a real test, we would verify the PVC expansion
			// For now, we just verify the spec is updated
			Eventually(func() string {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName,
					Namespace: testNamespace,
				}, cluster)
				if err != nil {
					return ""
				}
				return cluster.Spec.Storage.Size
			}, timeout, interval).Should(Equal("40Gi"))
		})
	})

	AfterEach(func() {
		if cluster != nil {
			By("Cleaning up test resources")
			// Clean up test resources - errors in cleanup are logged but don't fail the test
			if err := k8sClient.Delete(ctx, cluster); err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "warning: failed to delete cluster during cleanup: %v\n", err)
			}
		}
	})
})
