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

var _ = Describe("Restore API Integration Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	Context("When creating Neo4jRestore resources", func() {
		var testNamespace string

		BeforeEach(func() {
			By("Creating test namespace")
			testNamespace = createTestNamespace("restore-api")
		})

		It("Should create a restore from backup reference", func() {
			By("Creating restore from backup")
			restore := &neo4jv1alpha1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jRestoreSpec{
					TargetCluster: "test-cluster",
					DatabaseName:  "restoreddb",
					Source: neo4jv1alpha1.RestoreSource{
						Type:      "backup",
						BackupRef: "daily-backup-20250121",
					},
					Options: &neo4jv1alpha1.RestoreOptionsSpec{
						ReplaceExisting: true,
						VerifyBackup:    true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			By("Restore resource should be created")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      restore.Name,
					Namespace: restore.Namespace,
				}, restore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying restore configuration")
			Expect(restore.Spec.Source.Type).To(Equal("backup"))
			Expect(restore.Spec.Source.BackupRef).To(Equal("daily-backup-20250121"))
			Expect(restore.Spec.Options.ReplaceExisting).To(BeTrue())
		})

		It("Should create a restore with PITR", func() {
			By("Creating PITR restore")
			now := metav1.Now()
			restore := &neo4jv1alpha1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pitr-restore",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jRestoreSpec{
					TargetCluster: "prod-cluster",
					DatabaseName:  "pitrdb",
					Source: neo4jv1alpha1.RestoreSource{
						Type: "pitr",
						PITR: &neo4jv1alpha1.PITRConfig{
							BaseBackup: &neo4jv1alpha1.BaseBackupSource{
								Type:      "backup",
								BackupRef: "base-backup",
							},
							LogStorage: &neo4jv1alpha1.StorageLocation{
								Type:   "s3",
								Bucket: "logs-bucket",
								Path:   "/transaction-logs",
							},
							ValidateLogIntegrity: true,
							Compression: &neo4jv1alpha1.CompressionConfig{
								Enabled:   true,
								Algorithm: "gzip",
								Level:     6,
							},
						},
						PointInTime: &now,
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			By("Verifying PITR configuration")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      restore.Name,
					Namespace: restore.Namespace,
				}, restore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(restore.Spec.Source.Type).To(Equal("pitr"))
			Expect(restore.Spec.Source.PITR).NotTo(BeNil())
			Expect(restore.Spec.Source.PITR.BaseBackup.Type).To(Equal("backup"))
			Expect(restore.Spec.Source.PITR.Compression.Algorithm).To(Equal("gzip"))
		})

		It("Should create a restore with hooks", func() {
			By("Creating restore with pre/post hooks")
			restore := &neo4jv1alpha1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-hooks",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jRestoreSpec{
					TargetCluster: "staging-cluster",
					DatabaseName:  "testdb",
					Source: neo4jv1alpha1.RestoreSource{
						Type: "storage",
						Storage: &neo4jv1alpha1.StorageLocation{
							Type: "pvc",
							Path: "/backups/latest",
						},
					},
					Options: &neo4jv1alpha1.RestoreOptionsSpec{
						PreRestore: &neo4jv1alpha1.RestoreHooks{
							CypherStatements: []string{
								"STOP DATABASE testdb IF EXISTS",
								"DROP DATABASE testdb IF EXISTS",
							},
						},
						PostRestore: &neo4jv1alpha1.RestoreHooks{
							CypherStatements: []string{
								"CREATE INDEX idx_user_email IF NOT EXISTS FOR (u:User) ON (u.email)",
								"CALL db.awaitIndexes()",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			By("Verifying hooks configuration")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      restore.Name,
					Namespace: restore.Namespace,
				}, restore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(restore.Spec.Options.PreRestore).NotTo(BeNil())
			Expect(restore.Spec.Options.PreRestore.CypherStatements).To(HaveLen(2))
			Expect(restore.Spec.Options.PostRestore).NotTo(BeNil())
			Expect(restore.Spec.Options.PostRestore.CypherStatements).To(HaveLen(2))
		})
	})
})
