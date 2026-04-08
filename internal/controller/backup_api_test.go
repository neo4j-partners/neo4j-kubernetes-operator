package controller_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Backup API Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	Context("When creating Neo4jBackup resources", func() {
		var testNamespace string

		BeforeEach(func() {
			testNamespace = fmt.Sprintf("backup-api-%d", time.Now().UnixNano())
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())
		})

		It("Should create a backup with backup type options", func() {
			backup := &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-full-backup", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target:  neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
					Storage: neo4jv1beta1.StorageLocation{Type: "pvc", Path: "/backups/full"},
					Options: &neo4jv1beta1.BackupOptions{BackupType: "FULL", Compress: true, PageCache: "2G"},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: testNamespace}, backup)
			}, timeout, interval).Should(Succeed())
			Expect(backup.Spec.Options.BackupType).To(Equal("FULL"))
			Expect(backup.Spec.Options.PageCache).To(Equal("2G"))
		})

		It("Should create a scheduled backup with retention", func() {
			backup := &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-scheduled-backup", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target:    neo4jv1beta1.BackupTarget{Kind: "Database", Name: "mydb"},
					Storage:   neo4jv1beta1.StorageLocation{Type: "pvc", Path: "/backups/scheduled"},
					Schedule:  "0 2 * * *",
					Retention: &neo4jv1beta1.RetentionPolicy{MaxAge: "7d", MaxCount: 7, DeletePolicy: "Delete"},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: testNamespace}, backup)
			}, timeout, interval).Should(Succeed())
			Expect(backup.Spec.Retention.MaxAge).To(Equal("7d"))
		})

		It("Should create a backup with cloud storage", func() {
			backup := &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-s3-backup", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target:  neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "prod-cluster"},
					Storage: neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "my-bucket", Path: "/neo4j-backups/prod"},
					Cloud:   &neo4jv1beta1.CloudBlock{Provider: "aws"},
					Options: &neo4jv1beta1.BackupOptions{
						BackupType: "AUTO",
						Encryption: &neo4jv1beta1.EncryptionSpec{Enabled: true, Algorithm: "AES256", KeySecret: "backup-encryption-key"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: testNamespace}, backup)
			}, timeout, interval).Should(Succeed())
			Expect(backup.Spec.Cloud.Provider).To(Equal("aws"))
			Expect(backup.Spec.Options.Encryption.Enabled).To(BeTrue())
		})
	})
})
