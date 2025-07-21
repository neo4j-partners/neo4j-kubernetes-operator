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

package controller_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jBackup Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 250
	)

	var (
		ctx           context.Context
		backup        *neo4jv1alpha1.Neo4jBackup
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
		adminSecret   *corev1.Secret
		backupName    string
		clusterName   string
		namespaceName string
	)

	BeforeEach(func() {
		// Ensure context is properly initialized
		if ctx == nil {
			ctx = context.Background()
		}

		backupName = fmt.Sprintf("test-backup-%d", time.Now().UnixNano())
		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
		namespaceName = "default"

		// Create admin secret first (if it doesn't exist)
		adminSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: namespaceName,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("testpassword"),
			},
		}
		err := k8sClient.Create(ctx, adminSecret)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		// Create cluster first
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespaceName,
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
				Auth: &neo4jv1alpha1.AuthSpec{
					AdminSecret: "neo4j-admin-secret",
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

		// Wait for cluster to be created and then patch its status
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      clusterName,
				Namespace: namespaceName,
			}, cluster)
		}, timeout, interval).Should(Succeed())

		// Patch cluster status to Ready so backup controller proceeds
		patch := client.MergeFrom(cluster.DeepCopy())
		cluster.Status.Phase = "Ready"
		cluster.Status.Conditions = []metav1.Condition{{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "TestReady",
			LastTransitionTime: metav1.Now(),
		}}
		Expect(k8sClient.Status().Patch(ctx, cluster, patch)).To(Succeed())

		// Create basic backup spec
		backup = &neo4jv1alpha1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: namespaceName,
			},
			Spec: neo4jv1alpha1.Neo4jBackupSpec{
				Target: neo4jv1alpha1.BackupTarget{
					Kind: "Cluster",
					Name: clusterName,
				},
				Storage: neo4jv1alpha1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1alpha1.PVCSpec{
						Name: "test-backup-pvc",
						Size: "10Gi",
					},
				},
			},
		}
	})

	AfterEach(func() {
		// Clean up resources
		if backup != nil {
			if err := k8sClient.Delete(ctx, backup, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				// Log the error but don't fail the test cleanup
				fmt.Printf("Warning: Failed to delete backup during cleanup: %v\n", err)
			}
		}
		if cluster != nil {
			if err := k8sClient.Delete(ctx, cluster, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				// Log the error but don't fail the test cleanup
				fmt.Printf("Warning: Failed to delete cluster during cleanup: %v\n", err)
			}
		}
		if adminSecret != nil {
			if err := k8sClient.Delete(ctx, adminSecret, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
				// Log the error but don't fail the test cleanup
				fmt.Printf("Warning: Failed to delete admin secret during cleanup: %v\n", err)
			}
		}
	})

	Context("When creating a PVC backup", func() {
		It("Should create backup RBAC resources automatically", func() {
			By("Creating the backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Verifying backup RBAC resources were created")
			// Check service account
			sa := &corev1.ServiceAccount{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-sa",
					Namespace: namespaceName,
				}, sa)
			}, timeout, interval).Should(Succeed())

			// Check role
			role := &rbacv1.Role{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-role",
					Namespace: namespaceName,
				}, role)
			}, timeout, interval).Should(Succeed())

			// Verify role has correct permissions
			Expect(role.Rules).To(HaveLen(3))
			Expect(role.Rules[0].APIGroups).To(Equal([]string{""}))
			Expect(role.Rules[0].Resources).To(Equal([]string{"pods"}))
			Expect(role.Rules[0].Verbs).To(ConsistOf("get", "list"))

			// Check role binding
			rb := &rbacv1.RoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-rolebinding",
					Namespace: namespaceName,
				}, rb)
			}, timeout, interval).Should(Succeed())

			// Verify role binding references correct resources
			Expect(rb.RoleRef.Name).To(Equal("neo4j-backup-role"))
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Name).To(Equal("neo4j-backup-sa"))
		})

		It("Should create backup job successfully", func() {
			By("Creating a mock pod for backup")
			mockPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-0",
					Namespace: namespaceName,
					Labels: map[string]string{
						"neo4j.com/cluster": clusterName,
						"neo4j.com/role":    "primary",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "neo4j",
						Image: "neo4j:5.26-enterprise",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			Expect(k8sClient.Create(ctx, mockPod)).Should(Succeed())

			By("Creating the backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for backup Job to be created by the controller")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup",
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			By("Checking Job specifications")
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring("neo4j"))

			By("Verifying job uses backup service account")
			Expect(job.Spec.Template.Spec.ServiceAccountName).To(Equal("neo4j-backup-sa"))

			// Clean up mock pod
			Expect(k8sClient.Delete(ctx, mockPod)).Should(Succeed())
		})

		It("Should handle scheduled backups", func() {
			By("Setting up scheduled backup")
			backup.Spec.Schedule = "0 2 * * *" // Daily at 2 AM
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for CronJob to be created by the controller")
			cronJob := &batchv1.CronJob{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup-cron",
					Namespace: namespaceName,
				}, cronJob)
			}, timeout, interval).Should(Succeed())

			By("Checking CronJob schedule")
			Expect(cronJob.Spec.Schedule).To(Equal("0 2 * * *"))
		})
	})

	Context("When creating S3 backup", func() {
		It("Should create backup with S3 configuration", func() {
			By("Configuring S3 storage")
			backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
				Type:   "s3",
				Bucket: "test-bucket",
				Path:   "/backups",
			}
			backup.Spec.Cloud = &neo4jv1alpha1.CloudBlock{
				Provider: "aws",
				Identity: &neo4jv1alpha1.CloudIdentity{
					Provider:       "aws",
					ServiceAccount: "backup-sa",
				},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for Job to be created with S3 configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup",
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "BACKUP_BUCKET",
				Value: "test-bucket",
			}))
		})
	})

	Context("When creating GCS backup", func() {
		It("Should create backup with GCS configuration", func() {
			By("Configuring GCS storage")
			backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
				Type:   "gcs",
				Bucket: "test-gcs-bucket",
				Path:   "/gcs-backups",
			}
			backup.Spec.Cloud = &neo4jv1alpha1.CloudBlock{
				Provider: "gcp",
				Identity: &neo4jv1alpha1.CloudIdentity{
					Provider:       "gcp",
					ServiceAccount: "gcs-backup-sa",
				},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for Job to be created with GCS configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup",
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "BACKUP_BUCKET",
				Value: "test-gcs-bucket",
			}))
		})
	})

	Context("When creating Azure backup", func() {
		It("Should create backup with Azure configuration", func() {
			By("Configuring Azure storage")
			backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
				Type:   "azure",
				Bucket: "test-azure-container",
				Path:   "/azure-backups",
			}
			backup.Spec.Cloud = &neo4jv1alpha1.CloudBlock{
				Provider: "azure",
				Identity: &neo4jv1alpha1.CloudIdentity{
					Provider:       "azure",
					ServiceAccount: "azure-backup-sa",
				},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for Job to be created with Azure configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup",
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "BACKUP_BUCKET",
				Value: "test-azure-container",
			}))
		})
	})

	Context("When handling backup status", func() {
		It("Should update status conditions correctly", func() {
			By("Creating the backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for status conditions to be set by the controller")
			Eventually(func() bool {
				updatedBackup := &neo4jv1alpha1.Neo4jBackup{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				}, updatedBackup)
				if err != nil {
					return false
				}
				return len(updatedBackup.Status.Conditions) > 0
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When handling retention policies", func() {
		It("Should apply retention policies", func() {
			By("Configuring retention policy")
			backup.Spec.Retention = &neo4jv1alpha1.RetentionPolicy{
				MaxAge:   "30d",
				MaxCount: 5,
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for Job to be created with retention configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup",
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			// Check that retention policy is set in the backup command
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("export BACKUP_MAX_AGE='30d'"))
		})
	})

	Context("When handling database-specific backups", func() {
		It("Should create backup for specific database", func() {
			By("Configuring database-specific backup")
			// Use AdditionalArgs to specify the database
			backup.Spec.Options = &neo4jv1alpha1.BackupOptions{
				AdditionalArgs: []string{"--database=testdb"},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for Job to be created with database specification")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName + "-backup",
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Args).To(ContainElement(ContainSubstring("--database=testdb")))
		})
	})
})
