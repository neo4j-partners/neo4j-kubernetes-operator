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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Backup API Integration Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	Context("When creating Neo4jBackup resources", func() {
		var testNamespace string

		BeforeEach(func() {
			By("Creating test namespace")
			testNamespace = createTestNamespace("backup-api")
		})

		It("Should create a backup with backup type options", func() {
			By("Creating a FULL backup")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-full-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						Path: "/backups/full",
					},
					Options: &neo4jv1alpha1.BackupOptions{
						BackupType: "FULL",
						Compress:   true,
						PageCache:  "2G",
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			By("Backup resource should be created")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      backup.Name,
					Namespace: backup.Namespace,
				}, backup)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying backup options")
			Expect(backup.Spec.Options).NotTo(BeNil())
			Expect(backup.Spec.Options.BackupType).To(Equal("FULL"))
			Expect(backup.Spec.Options.PageCache).To(Equal("2G"))
		})

		It("Should create a scheduled backup with retention", func() {
			By("Creating a scheduled backup")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-scheduled-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind:      "Database",
						Name:      "mydb",
						Namespace: testNamespace,
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						Path: "/backups/scheduled",
					},
					Schedule: "0 2 * * *",
					Retention: &neo4jv1alpha1.RetentionPolicy{
						MaxAge:       "7d",
						MaxCount:     7,
						DeletePolicy: "Delete",
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			By("Backup should have schedule and retention")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      backup.Name,
					Namespace: backup.Namespace,
				}, backup)
				return err == nil && backup.Spec.Schedule != ""
			}, timeout, interval).Should(BeTrue())

			Expect(backup.Spec.Retention).NotTo(BeNil())
			Expect(backup.Spec.Retention.MaxAge).To(Equal("7d"))
		})

		It("Should create a backup with cloud storage", func() {
			By("Creating S3 backup")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-s3-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "prod-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: "my-bucket",
						Path:   "/neo4j-backups/prod",
					},
					Cloud: &neo4jv1alpha1.CloudBlock{
						Provider: "aws",
					},
					Options: &neo4jv1alpha1.BackupOptions{
						BackupType: "AUTO",
						Encryption: &neo4jv1alpha1.EncryptionSpec{
							Enabled:   true,
							Algorithm: "AES256",
							KeySecret: "backup-encryption-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			By("Verifying cloud configuration")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      backup.Name,
					Namespace: backup.Namespace,
				}, backup)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(backup.Spec.Cloud).NotTo(BeNil())
			Expect(backup.Spec.Cloud.Provider).To(Equal("aws"))
			Expect(backup.Spec.Options.Encryption).NotTo(BeNil())
			Expect(backup.Spec.Options.Encryption.Enabled).To(BeTrue())
		})
	})
})
