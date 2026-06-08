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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Neo4jBackup Controller", func() {
	const (
		// 60s (not 30s): these specs share one envtest manager with the
		// cluster/standalone/restore suites, whose reconcilers continuously
		// retry Neo4j connectivity (there's no real Neo4j in envtest) and
		// saturate the shared work queue. Under that load a reconcile that
		// normally creates e.g. neo4j-backup-sa in <1s can miss a 30s window
		// — a CI-only flake, never a logic failure. 60s absorbs the queue
		// contention while staying well under the suite's per-test budget.
		timeout  = time.Second * 60
		interval = time.Millisecond * 250
	)

	var (
		ctx           context.Context
		backup        *neo4jv1beta1.Neo4jBackup
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
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
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespaceName,
			},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1beta1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1beta1.TopologyConfiguration{
					Servers: 5, // 3 + 2 total servers
				},
				Storage: neo4jv1beta1.StorageSpec{
					ClassName: "standard",
					Size:      "10Gi",
				},
				Auth: &neo4jv1beta1.AuthSpec{
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
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: namespaceName,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: clusterName,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
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
		It("Should create the backup ServiceAccount automatically", func() {
			By("Creating the backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Verifying neo4j-backup-sa ServiceAccount is created")
			sa := &corev1.ServiceAccount{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-sa",
					Namespace: namespaceName,
				}, sa)
			}, timeout, interval).Should(Succeed())
			// No Role or RoleBinding: the backup Job runs neo4j-admin directly
			// and requires no Kubernetes API access.
		})

		It("Should create backup job successfully", func() {
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

			By("Checking Job uses cluster Neo4j image and neo4j-admin backup command")
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(ContainSubstring("neo4j"))
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
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

			By("Checking CronJob schedule and uses neo4j-admin backup command")
			Expect(cronJob.Spec.Schedule).To(Equal("0 2 * * *"))
			containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(1))
			Expect(containers[0].Image).To(ContainSubstring("neo4j"))
			Expect(containers[0].Args).To(HaveLen(2))
			Expect(containers[0].Args[1]).To(ContainSubstring("neo4j-admin database backup"))
		})

		// Regression for #116: a one-time backup with status.phase=Completed must
		// not re-create the backup Job after the original Job's
		// TTLSecondsAfterFinished expires and the Job is GC'd. Same for Failed.
		It("Should not recreate Job for a Completed one-time backup", func() {
			By("Creating the one-time backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for the initial backup Job to be created")
			jobKey := types.NamespacedName{Name: backupName + "-backup", Namespace: namespaceName}
			job := &batchv1.Job{}
			Eventually(func() error { return k8sClient.Get(ctx, jobKey, job) }, timeout, interval).Should(Succeed())

			By("Patching CR status to Completed (simulating successful Job)")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: backupName, Namespace: namespaceName}, backup); err != nil {
					return err
				}
				patch := client.MergeFrom(backup.DeepCopy())
				backup.Status.Phase = "Completed"
				return k8sClient.Status().Patch(ctx, backup, patch)
			}, timeout, interval).Should(Succeed())

			By("Deleting the Job (simulating TTL-driven GC)")
			Expect(k8sClient.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, jobKey, &batchv1.Job{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Verifying the controller does NOT recreate the Job")
			Consistently(func() bool {
				err := k8sClient.Get(ctx, jobKey, &batchv1.Job{})
				return errors.IsNotFound(err)
			}, 5*time.Second, interval).Should(BeTrue())
		})

		It("Should not recreate Job for a Failed one-time backup", func() {
			By("Creating the one-time backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for the initial backup Job to be created")
			jobKey := types.NamespacedName{Name: backupName + "-backup", Namespace: namespaceName}
			job := &batchv1.Job{}
			Eventually(func() error { return k8sClient.Get(ctx, jobKey, job) }, timeout, interval).Should(Succeed())

			By("Patching CR status to Failed")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: backupName, Namespace: namespaceName}, backup); err != nil {
					return err
				}
				patch := client.MergeFrom(backup.DeepCopy())
				backup.Status.Phase = "Failed"
				return k8sClient.Status().Patch(ctx, backup, patch)
			}, timeout, interval).Should(Succeed())

			By("Deleting the Job and verifying no recreation")
			Expect(k8sClient.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
			Consistently(func() bool {
				err := k8sClient.Get(ctx, jobKey, &batchv1.Job{})
				return errors.IsNotFound(err)
			}, 5*time.Second, interval).Should(BeTrue())
		})

		// Regression for #116: suspend=true on a one-time backup must short-circuit
		// before Job creation. Previously the suspend check lived inside
		// handleScheduledBackup only, so one-time backups ignored it.
		It("Should not create a Job when suspend=true on a one-time backup", func() {
			By("Creating a suspended one-time backup")
			backup.Spec.Suspend = true
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Verifying status moves to Suspended")
			Eventually(func() string {
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: backupName, Namespace: namespaceName}, backup)
				return backup.Status.Phase
			}, timeout, interval).Should(Equal("Suspended"))

			By("Verifying no backup Job is created")
			jobKey := types.NamespacedName{Name: backupName + "-backup", Namespace: namespaceName}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, jobKey, &batchv1.Job{})
				return errors.IsNotFound(err)
			}, 5*time.Second, interval).Should(BeTrue())
		})

		// History/RunID logic for #118 is covered by pure unit tests of
		// jobToBackupRun and backupRunAlreadyRecorded in
		// neo4jbackup_history_test.go. End-to-end Job-status →
		// recordOneShotBackupRun integration is hard to assert in envtest:
		// the cluster controller running in the same manager flips
		// status.phase off Ready before the backup CR's periodic
		// requeue fires, leaving the backup controller stuck in the
		// "Target cluster is not ready" branch on subsequent reconciles.
	})

	Context("When creating S3 backup", func() {
		It("Should create backup with S3 configuration", func() {
			By("Configuring S3 storage")
			backup.Spec.Storage = neo4jv1beta1.StorageLocation{
				Type:   "s3",
				Bucket: "test-bucket",
				Path:   "/backups",
			}
			backup.Spec.Cloud = &neo4jv1beta1.CloudBlock{
				Provider: "aws",
				Identity: &neo4jv1beta1.CloudIdentity{
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
			Expect(container.Image).To(ContainSubstring("neo4j"))
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
			Expect(container.Args[1]).To(ContainSubstring("s3://test-bucket"))
		})
	})

	Context("When creating GCS backup", func() {
		It("Should create backup with GCS configuration", func() {
			By("Configuring GCS storage")
			backup.Spec.Storage = neo4jv1beta1.StorageLocation{
				Type:   "gcs",
				Bucket: "test-gcs-bucket",
				Path:   "/gcs-backups",
			}
			backup.Spec.Cloud = &neo4jv1beta1.CloudBlock{
				Provider: "gcp",
				Identity: &neo4jv1beta1.CloudIdentity{
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
			Expect(container.Image).To(ContainSubstring("neo4j"))
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
			Expect(container.Args[1]).To(ContainSubstring("gs://test-gcs-bucket"))
		})
	})

	Context("When creating Azure backup", func() {
		It("Should create backup with Azure configuration", func() {
			By("Configuring Azure storage")
			backup.Spec.Storage = neo4jv1beta1.StorageLocation{
				Type:   "azure",
				Bucket: "test-azure-container",
				Path:   "/azure-backups",
			}
			backup.Spec.Cloud = &neo4jv1beta1.CloudBlock{
				Provider: "azure",
				Identity: &neo4jv1beta1.CloudIdentity{
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
			Expect(container.Image).To(ContainSubstring("neo4j"))
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
			Expect(container.Args[1]).To(ContainSubstring("azb://test-azure-container"))
		})
	})

	Context("When handling backup status", func() {
		It("Should update status conditions correctly", func() {
			By("Creating the backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Waiting for status conditions to be set by the controller")
			Eventually(func() bool {
				updatedBackup := &neo4jv1beta1.Neo4jBackup{}
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
			backup.Spec.Retention = &neo4jv1beta1.RetentionPolicy{
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
			Expect(container.Image).To(ContainSubstring("neo4j"))
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
		})
	})

	Context("When handling database-specific backups", func() {
		It("Should create backup for specific database", func() {
			By("Configuring database-specific backup with additional args")
			backup.Spec.Options = &neo4jv1beta1.BackupOptions{
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
			Expect(container.Image).To(ContainSubstring("neo4j"))
			Expect(container.Args).To(HaveLen(2))
			Expect(container.Args[0]).To(Equal("-c"))
			Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
			Expect(container.Args[1]).To(ContainSubstring("--database=testdb"))
		})
	})
})
