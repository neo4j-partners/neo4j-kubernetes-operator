package controller_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// hasFailedConditionMatching returns true when the restore reports
// status.phase=Failed AND at least one condition's message contains every
// substring in `needles` (case-insensitive). The cluster controller emits
// a single Ready=False condition with a free-form message rather than a
// typed ClusterNotFound/BackupNotFound condition, so we assert on message
// content to verify the failure reason without coupling the test to a
// specific condition Type that the controller does not actually set.
func hasFailedConditionMatching(restore *neo4jv1beta1.Neo4jRestore, needles ...string) bool {
	if restore.Status.Phase != "Failed" {
		return false
	}
	for _, cond := range restore.Status.Conditions {
		msg := strings.ToLower(cond.Message)
		ok := true
		for _, n := range needles {
			if !strings.Contains(msg, strings.ToLower(n)) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

var _ = Describe("Neo4jRestore Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 250
	)

	var (
		ctx             context.Context
		restore         *neo4jv1beta1.Neo4jRestore
		cluster         *neo4jv1beta1.Neo4jEnterpriseCluster
		backup          *neo4jv1beta1.Neo4jBackup
		restoreName     string
		clusterName     string
		backupName      string
		adminSecretName string
		namespaceName   string
	)

	BeforeEach(func() {
		// Fresh context per spec — the prior `if ctx == nil` guard worked by
		// accident on the first run (interface zero value) but conflated
		// "first run after suite setup" with "subsequent reconciles" on
		// retries. Unconditional assignment makes the intent explicit.
		ctx = context.Background()

		restoreName = fmt.Sprintf("test-restore-%d", time.Now().UnixNano())
		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
		backupName = fmt.Sprintf("test-backup-%d", time.Now().UnixNano())
		// Unique admin-secret name per spec to match the surrounding
		// convention (cluster/backup/restore are all UnixNano-suffixed)
		// and eliminate cleanup-leak collisions between specs.
		adminSecretName = fmt.Sprintf("neo4j-admin-secret-%d", time.Now().UnixNano())
		namespaceName = "default"

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
					Servers: 5,
				},
				Storage: neo4jv1beta1.StorageSpec{
					ClassName: "standard",
					Size:      "10Gi",
				},
				Auth: &neo4jv1beta1.AuthSpec{
					AdminSecret: adminSecretName,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

		// Create admin secret (unique per spec — see adminSecretName above).
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      adminSecretName,
				Namespace: namespaceName,
			},
			Data: map[string][]byte{
				"NEO4J_AUTH": []byte("neo4j/testpassword123"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

		// Patch cluster status to Ready so restore controller proceeds
		patch := client.MergeFrom(cluster.DeepCopy())
		cluster.Status.Conditions = []metav1.Condition{{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "TestReady",
			LastTransitionTime: metav1.Now(),
		}}
		Expect(k8sClient.Status().Patch(ctx, cluster, patch)).To(Succeed())

		// Create backup for successful restore test
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
						Name:             "backup-storage",
						StorageClassName: "standard",
						Size:             "10Gi",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

		// Patch backup status to Completed
		backupPatch := client.MergeFrom(backup.DeepCopy())
		backup.Status.Phase = "Completed"
		backup.Status.Conditions = []metav1.Condition{{
			Type:               "Completed",
			Status:             metav1.ConditionTrue,
			Reason:             "TestCompleted",
			LastTransitionTime: metav1.Now(),
		}}
		Expect(k8sClient.Status().Patch(ctx, backup, backupPatch)).To(Succeed())

		// Create basic restore spec
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      restoreName,
				Namespace: namespaceName,
			},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   clusterName,
				DatabaseName: "neo4j",
				Source: neo4jv1beta1.RestoreSource{
					Type:      "backup",
					BackupRef: backupName, // Use the created backup
				},
			},
		}
	})

	AfterEach(func() {
		// Clean up resources. Cleanup-warning output goes through GinkgoWriter
		// so Ginkgo can attribute it to the spec that produced it (and respect
		// verbosity flags) — fmt.Printf to stdout would be unattributed noise.
		//
		// NotFound is filtered out so finalizer cascades and tests that
		// delete-as-they-go don't produce noise on already-gone resources.
		// Matches the sibling pattern in neo4jbackup_controller_test.go.
		if restore != nil {
			if err := k8sClient.Delete(ctx, restore, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
				fmt.Fprintf(GinkgoWriter, "Warning: Failed to delete restore during cleanup: %v\n", err)
			}
		}
		if backup != nil {
			if err := k8sClient.Delete(ctx, backup, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
				fmt.Fprintf(GinkgoWriter, "Warning: Failed to delete backup during cleanup: %v\n", err)
			}
		}
		if cluster != nil {
			if err := k8sClient.Delete(ctx, cluster, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !errors.IsNotFound(err) {
				fmt.Fprintf(GinkgoWriter, "Warning: Failed to delete cluster during cleanup: %v\n", err)
			}
		}
		// Clean up admin secret (unique name per spec).
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      adminSecretName,
				Namespace: namespaceName,
			},
		}
		if err := k8sClient.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
			fmt.Fprintf(GinkgoWriter, "Warning: Failed to delete admin secret during cleanup: %v\n", err)
		}
	})

	Context("When creating a Neo4jRestore", func() {
		It("Should create restore successfully", func() {
			By("Creating a Neo4jRestore")
			Expect(k8sClient.Create(ctx, restore)).Should(Succeed())

			restoreLookupKey := types.NamespacedName{
				Name:      restore.Name,
				Namespace: restore.Namespace,
			}
			createdRestore := &neo4jv1beta1.Neo4jRestore{}

			// We'll need to wait and give the controller time to get the updated object.
			// The first reconciliation may add the finalizer.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Verify finalizer was added
			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				if err != nil {
					return false
				}
				return len(createdRestore.Finalizers) > 0
			}, timeout, interval).Should(BeTrue())

			// Verify the finalizer name
			Expect(createdRestore.Finalizers).Should(ContainElement("neo4j.com/restore-finalizer"))
		})

		It("Should fail when cluster doesn't exist", func() {
			By("Creating a Neo4jRestore with non-existent cluster")
			restoreWithNoCluster := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-no-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef:   "non-existent-cluster",
					DatabaseName: "neo4j",
					Source: neo4jv1beta1.RestoreSource{
						Type:      "backup",
						BackupRef: backupName, // Use existing backup
					},
				},
			}
			Expect(k8sClient.Create(ctx, restoreWithNoCluster)).Should(Succeed())

			restoreLookupKey := types.NamespacedName{
				Name:      restoreWithNoCluster.Name,
				Namespace: restoreWithNoCluster.Namespace,
			}
			createdRestore := &neo4jv1beta1.Neo4jRestore{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// A missing target is TRANSIENT (#218): kubectl apply -f dir/
			// commonly creates the restore before its target CR, so the
			// controller waits in Pending (message referencing the missing
			// target) instead of pinning a terminal Failed.
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, restoreLookupKey, createdRestore); err != nil {
					return false
				}
				return createdRestore.Status.Phase == "Pending" &&
					strings.Contains(createdRestore.Status.Message, "not found")
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, restoreWithNoCluster)).Should(Succeed())
		})

		It("Should fail when backup doesn't exist", func() {
			By("Creating a Neo4jRestore with non-existent backup")
			restoreWithNoBackup := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-no-backup",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef:   clusterName, // Use existing cluster
					DatabaseName: "neo4j",
					Source: neo4jv1beta1.RestoreSource{
						Type:      "backup",
						BackupRef: "non-existent-backup",
					},
				},
			}
			Expect(k8sClient.Create(ctx, restoreWithNoBackup)).Should(Succeed())

			restoreLookupKey := types.NamespacedName{
				Name:      restoreWithNoBackup.Name,
				Namespace: restoreWithNoBackup.Namespace,
			}
			createdRestore := &neo4jv1beta1.Neo4jRestore{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// The controller should reject this restore due to non-existent backup
			// — Phase=Failed plus a condition whose message references the missing
			// backup. The controller emits a Ready=False condition with the
			// "Validation failed: backup ... not found: ..." message, not a typed
			// BackupNotFound condition.
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, restoreLookupKey, createdRestore); err != nil {
					return false
				}
				return hasFailedConditionMatching(createdRestore, "backup", "non-existent-backup")
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, restoreWithNoBackup)).Should(Succeed())
		})

		It("Should handle storage-based restore", func() {
			By("Creating a Neo4jRestore with storage source")
			restoreWithStorage := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-storage",
					Namespace: "default",
				},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef:   clusterName, // Use existing cluster
					DatabaseName: "neo4j",
					Source: neo4jv1beta1.RestoreSource{
						Type: "storage",
						Storage: &neo4jv1beta1.StorageLocation{
							Type:   "pvc",
							Path:   "/backups",
							Bucket: "backup-bucket",
							PVC: &neo4jv1beta1.PVCSpec{
								Name:             "backup-storage",
								StorageClassName: "standard",
								Size:             "10Gi",
							},
						},
						BackupPath: "/backups/2024-01-01/",
					},
				},
			}
			Expect(k8sClient.Create(ctx, restoreWithStorage)).Should(Succeed())

			restoreLookupKey := types.NamespacedName{
				Name:      restoreWithStorage.Name,
				Namespace: restoreWithStorage.Namespace,
			}
			createdRestore := &neo4jv1beta1.Neo4jRestore{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Verify the controller observed the resource and started reconciling
			// it (first-pass adds the finalizer). Asserting deeper restore Job
			// semantics here would couple this unit-style test to the full backup
			// retrieval / Job creation pipeline that the controller-suite envtest
			// is not set up to exercise meaningfully — that surface belongs in
			// the integration-test suite.
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, restoreLookupKey, createdRestore); err != nil {
					return false
				}
				for _, f := range createdRestore.Finalizers {
					if f == "neo4j.com/restore-finalizer" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, restoreWithStorage)).Should(Succeed())
		})
	})

	Context("When updating a Neo4jRestore", func() {
		It("Should accept a custom databaseName in spec", func() {
			By("Creating a Neo4jRestore with different database name")

			// Create restore with a different database name from the start
			restore.Spec.DatabaseName = "testdb"
			Expect(k8sClient.Create(ctx, restore)).Should(Succeed())

			restoreLookupKey := types.NamespacedName{
				Name:      restore.Name,
				Namespace: restore.Namespace,
			}
			createdRestore := &neo4jv1beta1.Neo4jRestore{}

			// Wait for the restore to be created and potentially reconciled
			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Verify the spec was set correctly
			Expect(createdRestore.Spec.DatabaseName).To(Equal("testdb"))
		})
	})

	Context("When deleting a Neo4jRestore", func() {
		It("Should handle deletion correctly", func() {
			By("Creating and deleting a Neo4jRestore")
			Expect(k8sClient.Create(ctx, restore)).Should(Succeed())

			restoreLookupKey := types.NamespacedName{
				Name:      restore.Name,
				Namespace: restore.Namespace,
			}
			createdRestore := &neo4jv1beta1.Neo4jRestore{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Delete the restore
			Expect(k8sClient.Delete(ctx, restore)).Should(Succeed())

			// Verify it was deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, restoreLookupKey, createdRestore)
				return err != nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
