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

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
		reconciler    *Neo4jBackupReconciler
		backupName    string
		clusterName   string
		namespaceName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		backupName = fmt.Sprintf("test-backup-%d", time.Now().UnixNano())
		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
		namespaceName = "default"

		// Create cluster first
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespaceName,
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
		Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

		// Create reconciler
		reconciler = &Neo4jBackupReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(100),
		}

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
						Size:             "10Gi",
						StorageClassName: "standard",
					},
				},
			},
		}
	})

	AfterEach(func() {
		// Clean up resources
		if backup != nil {
			k8sClient.Delete(ctx, backup, client.PropagationPolicy(metav1.DeletePropagationForeground))
		}
		if cluster != nil {
			k8sClient.Delete(ctx, cluster, client.PropagationPolicy(metav1.DeletePropagationForeground))
		}
	})

	Context("When creating a PVC backup", func() {
		It("Should create backup job successfully", func() {
			By("Creating the backup resource")
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Reconciling the backup")
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			By("Checking that backup Job is created")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			By("Checking Job specifications")
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring("neo4j"))
		})

		It("Should handle scheduled backups", func() {
			By("Setting up scheduled backup")
			backup.Spec.Schedule = "0 2 * * *" // Daily at 2 AM
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Reconciling the backup")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that CronJob is created")
			cronJob := &batchv1.CronJob{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
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

			By("Reconciling the backup")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Job has S3 configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
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

			By("Reconciling the backup")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Job has GCS configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
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

			By("Reconciling the backup")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Job has Azure configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "BACKUP_CONTAINER",
				Value: "test-azure-container",
			}))
		})
	})

	Context("When handling backup status", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())
		})

		It("Should update status conditions correctly", func() {
			By("Reconciling and checking initial status")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking status conditions are set")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: backupName, Namespace: namespaceName}, backup)
				if err != nil {
					return false
				}
				return len(backup.Status.Conditions) > 0
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When handling retention policies", func() {
		It("Should apply retention policies", func() {
			By("Configuring retention policy")
			backup.Spec.Retention = &neo4jv1alpha1.RetentionPolicy{
				MaxAge:   "7d",
				MaxCount: 10,
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Reconciling the backup")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Job has retention configuration")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "RETENTION_MAX_AGE",
				Value: "7d",
			}))
		})
	})

	Context("When handling database-specific backups", func() {
		It("Should create backup for specific database", func() {
			By("Configuring database-specific backup")
			backup.Spec.Target = neo4jv1alpha1.BackupTarget{
				Kind: "Database",
				Name: "test-database",
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Reconciling the backup")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Job has database specification")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      backupName,
					Namespace: namespaceName,
				}, job)
			}, timeout, interval).Should(Succeed())

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name:  "BACKUP_DATABASE",
				Value: "test-database",
			}))
		})
	})
})
