/*
Copyright 2025 Priyo Lahiri.

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
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// Standalone all-databases restore via the v1.13 scope-based API (#288). A
// Neo4jEnterpriseStandalone takes the offline `neo4j-admin database restore`
// Job path — distinct from the cluster in-place Cypher path — restoring every
// user database recorded in the backup's per-database artifact map (system
// excluded) in a single multi-database restore Job, then bringing each online
// and reporting per-database outcomes in status.databaseResults.
//
// Run locally:
//
//	NEO4J_VERSION=5.26-enterprise ginkgo run -focus "Standalone.*All-Databases" ./test/integration
var _ = Describe("Standalone All-Databases Restore (v1.13 API)", Label("extended"), Serial, func() {
	const (
		readyTimeout   = 10 * time.Minute
		dbReadyTimeout = 5 * time.Minute
		backupTimeout  = 10 * time.Minute
		restoreTimeout = 12 * time.Minute
		pollInterval   = 5 * time.Second
		adminPass      = "password123"
	)

	var (
		testNamespace string
		standalone    *neo4jv1beta1.Neo4jEnterpriseStandalone
		dbInventory   *neo4jv1beta1.Neo4jDatabase
		dbCustomers   *neo4jv1beta1.Neo4jDatabase
		backup        *neo4jv1beta1.Neo4jBackup
		restore       *neo4jv1beta1.Neo4jRestore
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("sa-all-db")
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: testNamespace},
			Data:       map[string][]byte{"username": []byte("neo4j"), "password": []byte(adminPass)},
			Type:       corev1.SecretTypeOpaque,
		})).To(Succeed())
		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(pollInterval)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespaceDiagnostics(testNamespace)
		}
		for _, cr := range []client.Object{restore, backup, dbInventory, dbCustomers, standalone} {
			if cr == nil {
				continue
			}
			if len(cr.GetFinalizers()) > 0 {
				cr.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, cr)
			}
			_ = k8sClient.Delete(ctx, cr)
		}
		standalone, dbInventory, dbCustomers, backup, restore = nil, nil, nil, nil, nil
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	cypher := func(pod, db, stmt string) {
		Eventually(func() error {
			out, err := exec.CommandContext(ctx, "kubectl", "exec", pod, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", db, "-u", "neo4j", "-p", adminPass, stmt).CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher (%s) err=%v out=%s\n", db, err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())
	}
	readCypher := func(pod, db, stmt string) string {
		out, _ := exec.CommandContext(ctx, "kubectl", "exec", pod, "-n", testNamespace, "--",
			"cypher-shell", "--format", "plain", "--database", db, "-u", "neo4j", "-p", adminPass, stmt).CombinedOutput()
		return string(out)
	}

	It("restores all user databases from a PVC backup via the offline Job path", func() {
		By("Creating a standalone")
		standalone = &neo4jv1beta1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
				AcceptLicenseAgreement: "eval",
				Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag(), PullPolicy: "IfNotPresent"},
				Storage:                neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "3Gi"},
				Auth:                   &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Resources:              getCIAppropriateResourceRequirements(),
				Env:                    []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
			},
		}
		Expect(k8sClient.Create(ctx, standalone)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(standalone), standalone)
			return standalone.Status.Phase
		}, readyTimeout, pollInterval).Should(Equal("Ready"))

		By("Creating two user databases with data")
		dbInventory = &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory", Namespace: testNamespace},
			Spec:       neo4jv1beta1.Neo4jDatabaseSpec{ClusterRef: standalone.Name, Name: "inventory", Wait: true},
		}
		dbCustomers = &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "customers", Namespace: testNamespace},
			Spec:       neo4jv1beta1.Neo4jDatabaseSpec{ClusterRef: standalone.Name, Name: "customers", Wait: true},
		}
		Expect(k8sClient.Create(ctx, dbInventory)).To(Succeed())
		Expect(k8sClient.Create(ctx, dbCustomers)).To(Succeed())
		for _, db := range []*neo4jv1beta1.Neo4jDatabase{dbInventory, dbCustomers} {
			Eventually(func() string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(db), db)
				return db.Status.Phase
			}, dbReadyTimeout, pollInterval).Should(Equal("Ready"))
		}
		pod := fmt.Sprintf("%s-0", standalone.Name)
		cypher(pod, "inventory", "CREATE (:Item {sku:'A-100', count:42}) RETURN 1;")
		cypher(pod, "customers", "CREATE (:Customer {id:'C-1', tier:'gold'}) RETURN 1;")

		By("Backing up all databases to a PVC (spec.instanceRef + spec.allDatabases)")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "sa-all-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				InstanceRef:  standalone.Name,
				AllDatabases: true,
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC:  &neo4jv1beta1.PVCSpec{Name: "sa-backup-store", Size: "2Gi"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupTimeout, pollInterval).Should(Equal("Completed"))
		Expect(backup.Status.History[0].DatabaseArtifacts).ToNot(BeEmpty())

		By("Mutating both databases, then restoring all (spec.allDatabases, stopCluster + force)")
		cypher(pod, "inventory", "MATCH (i:Item) SET i.count = 999 RETURN 1;")
		cypher(pod, "customers", "MATCH (c:Customer) SET c.tier = 'bronze' RETURN 1;")
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "sa-all-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				InstanceRef:  standalone.Name,
				AllDatabases: true,
				Options:      &neo4jv1beta1.RestoreOptionsSpec{ReplaceExisting: true},
				StopCluster:  true,
				Source:       neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: backup.Name},
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), restore)
			return restore.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"restore should reach Completed; message=%q results=%+v", restore.Status.Message, restore.Status.DatabaseResults)

		By("Per-database results record every user database online (system excluded)")
		got := map[string]string{}
		for _, dr := range restore.Status.DatabaseResults {
			got[dr.Database] = dr.Phase
		}
		Expect(got).To(HaveKeyWithValue("inventory", "Completed"))
		Expect(got).To(HaveKeyWithValue("customers", "Completed"))
		Expect(got).ToNot(HaveKey("system"))

		By("Restored data reflects the backed-up state, not the mutations")
		Eventually(func() string {
			return readCypher(pod, "inventory", "MATCH (i:Item {sku:'A-100'}) RETURN i.count AS c;")
		}, 3*time.Minute, pollInterval).Should(ContainSubstring("42"))
		Eventually(func() string {
			return readCypher(pod, "customers", "MATCH (c:Customer {id:'C-1'}) RETURN c.tier AS t;")
		}, 3*time.Minute, pollInterval).Should(ContainSubstring("gold"))
	})
})
