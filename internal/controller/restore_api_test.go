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

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Restore API Tests", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	Context("When creating Neo4jRestore resources", func() {
		var testNamespace string

		BeforeEach(func() {
			testNamespace = fmt.Sprintf("restore-api-%d", time.Now().UnixNano())
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())
		})

		It("Should create a restore from backup reference", func() {
			restore := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-restore-backup", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef: "test-cluster", DatabaseName: "restoreddb",
					Source:  neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "daily-backup-20250121"},
					Options: &neo4jv1beta1.RestoreOptionsSpec{ReplaceExisting: true, VerifyBackup: true},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: restore.Name, Namespace: testNamespace}, restore)
			}, timeout, interval).Should(Succeed())
			Expect(restore.Spec.Source.Type).To(Equal("backup"))
			Expect(restore.Spec.Source.BackupRef).To(Equal("daily-backup-20250121"))
			Expect(restore.Spec.Options.ReplaceExisting).To(BeTrue())
		})

		It("Should create a restore with PITR", func() {
			now := metav1.Now()
			restore := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pitr-restore", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef: "prod-cluster", DatabaseName: "pitrdb",
					Source: neo4jv1beta1.RestoreSource{
						Type: "pitr",
						PITR: &neo4jv1beta1.PITRConfig{
							BaseBackup:           &neo4jv1beta1.BaseBackupSource{Type: "backup", BackupRef: "base-backup"},
							LogStorage:           &neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "logs-bucket", Path: "/transaction-logs"},
							ValidateLogIntegrity: true,
							Compression:          &neo4jv1beta1.CompressionConfig{Enabled: true, Algorithm: "gzip", Level: 6},
						},
						PointInTime: &now,
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: restore.Name, Namespace: testNamespace}, restore)
			}, timeout, interval).Should(Succeed())
			Expect(restore.Spec.Source.Type).To(Equal("pitr"))
			Expect(restore.Spec.Source.PITR.BaseBackup.Type).To(Equal("backup"))
			Expect(restore.Spec.Source.PITR.Compression.Algorithm).To(Equal("gzip"))
		})

		It("Should create a restore with hooks", func() {
			restore := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-restore-hooks", Namespace: testNamespace},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef: "staging-cluster", DatabaseName: "testdb",
					Source: neo4jv1beta1.RestoreSource{
						Type:    "storage",
						Storage: &neo4jv1beta1.StorageLocation{Type: "pvc", Path: "/backups/latest"},
					},
					Options: &neo4jv1beta1.RestoreOptionsSpec{
						PreRestore:  &neo4jv1beta1.RestoreHooks{CypherStatements: []string{"STOP DATABASE testdb IF EXISTS", "DROP DATABASE testdb IF EXISTS"}},
						PostRestore: &neo4jv1beta1.RestoreHooks{CypherStatements: []string{"CREATE INDEX idx IF NOT EXISTS FOR (u:User) ON (u.email)", "CALL db.awaitIndexes()"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: restore.Name, Namespace: testNamespace}, restore)
			}, timeout, interval).Should(Succeed())
			Expect(restore.Spec.Options.PreRestore.CypherStatements).To(HaveLen(2))
			Expect(restore.Spec.Options.PostRestore.CypherStatements).To(HaveLen(2))
		})
	})
})
