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

// PVC-backed cluster restore via the v1.13 scope-based API, both scopes, through
// the in-cluster seed-proxy path (no cloud storage). Covers:
//   - single-database restore (spec.instanceRef + spec.database), and
//   - all-databases restore (spec.instanceRef + spec.allDatabases) — the
//     PVC-cluster all-databases path (#288 PVC-cluster part).
//
// Run locally:
//
//	NEO4J_VERSION=2025.12-enterprise ginkgo run -focus "PVC.*All-Databases" ./test/integration
var _ = Describe("PVC Cluster Restore — All-Databases and Single-Database (v1.13 API)", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout = 10 * time.Minute
		dbReadyTimeout      = 5 * time.Minute
		backupTimeout       = 10 * time.Minute
		restoreTimeout      = 12 * time.Minute
		pollInterval        = 5 * time.Second
		adminPass           = "password123"
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		dbInventory   *neo4jv1beta1.Neo4jDatabase
		dbCustomers   *neo4jv1beta1.Neo4jDatabase
		backup        *neo4jv1beta1.Neo4jBackup
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("pvc-all-db")
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
		for _, cr := range []client.Object{backup, dbInventory, dbCustomers, cluster} {
			if cr == nil {
				continue
			}
			if len(cr.GetFinalizers()) > 0 {
				cr.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, cr)
			}
			_ = k8sClient.Delete(ctx, cr)
		}
		cluster, dbInventory, dbCustomers, backup = nil, nil, nil, nil
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
	waitRestore := func(name string) *neo4jv1beta1.Neo4jRestore {
		r := &neo4jv1beta1.Neo4jRestore{}
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: testNamespace}, r)
			return r.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"%s should reach Completed; message=%q results=%+v", name, r.Status.Message, r.Status.DatabaseResults)
		return r
	}

	It("restores single-database and all-databases from a PVC backup via the seed proxy", func() {
		By("Creating a 2-server cluster")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-alldb-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:                   &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:               neo4jv1beta1.TopologyConfiguration{Servers: 2},
				Storage:                neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources:              getCIAppropriateResourceRequirements(),
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.Phase
		}, clusterReadyTimeout, pollInterval).Should(Equal("Ready"))

		By("Creating two user databases with data")
		dbInventory = &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory", Namespace: testNamespace},
			Spec:       neo4jv1beta1.Neo4jDatabaseSpec{ClusterRef: cluster.Name, Name: "inventory", Wait: true},
		}
		dbCustomers = &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "customers", Namespace: testNamespace},
			Spec:       neo4jv1beta1.Neo4jDatabaseSpec{ClusterRef: cluster.Name, Name: "customers", Wait: true},
		}
		Expect(k8sClient.Create(ctx, dbInventory)).To(Succeed())
		Expect(k8sClient.Create(ctx, dbCustomers)).To(Succeed())
		for _, db := range []*neo4jv1beta1.Neo4jDatabase{dbInventory, dbCustomers} {
			Eventually(func() string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(db), db)
				return db.Status.Phase
			}, dbReadyTimeout, pollInterval).Should(Equal("Ready"))
		}
		pod := fmt.Sprintf("%s-server-0", cluster.Name)
		cypher(pod, "inventory", "CREATE (:Item {sku:'A-100', count:42}) RETURN 1;")
		cypher(pod, "customers", "CREATE (:Customer {id:'C-1', tier:'gold'}) RETURN 1;")

		By("Backing up all databases to a PVC (spec.instanceRef + spec.allDatabases)")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-all-db-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				InstanceRef:  cluster.Name,
				AllDatabases: true,
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC:  &neo4jv1beta1.PVCSpec{Name: "pvc-backup-store", Size: "2Gi"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupTimeout, pollInterval).Should(Equal("Completed"))
		Expect(backup.Status.History[0].DatabaseArtifacts).ToNot(BeEmpty())

		By("Single-database PVC backup + restore via spec.instanceRef + spec.database (inventory)")
		// A single-database restore needs a single-database backup: a single-DB
		// cluster restore cannot seed from a kind:Cluster (all-databases) backup
		// (no single artifact). So take a dedicated single-DB backup of inventory.
		singleBackup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-single-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				InstanceRef: cluster.Name,
				Database:    "inventory",
				Storage:     neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "pvc-single-store", Size: "2Gi"}},
			},
		}
		Expect(k8sClient.Create(ctx, singleBackup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(singleBackup), singleBackup)
			return singleBackup.Status.Phase
		}, backupTimeout, pollInterval).Should(Equal("Completed"))

		cypher(pod, "inventory", "MATCH (i:Item) SET i.count = 999 RETURN 1;")
		single := &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-single-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				InstanceRef: cluster.Name,
				Database:    "inventory",
				Force:       true,
				Source:      neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: singleBackup.Name},
			},
		}
		Expect(k8sClient.Create(ctx, single)).To(Succeed())
		waitRestore("pvc-single-restore")
		Eventually(func() string {
			return readCypher(pod, "inventory", "MATCH (i:Item {sku:'A-100'}) RETURN i.count AS c;")
		},
			3*time.Minute, pollInterval).Should(ContainSubstring("42"))

		By("All-databases PVC restore via spec.instanceRef + spec.allDatabases")
		cypher(pod, "inventory", "MATCH (i:Item) SET i.count = 111 RETURN 1;")
		cypher(pod, "customers", "MATCH (c:Customer) SET c.tier = 'bronze' RETURN 1;")
		all := &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-all-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				InstanceRef:  cluster.Name,
				AllDatabases: true,
				Force:        true,
				Source:       neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: backup.Name},
			},
		}
		Expect(k8sClient.Create(ctx, all)).To(Succeed())
		r := waitRestore("pvc-all-restore")
		got := map[string]string{}
		for _, dr := range r.Status.DatabaseResults {
			got[dr.Database] = dr.Phase
		}
		Expect(got).To(HaveKeyWithValue("inventory", "Completed"))
		Expect(got).To(HaveKeyWithValue("customers", "Completed"))
		Eventually(func() string {
			return readCypher(pod, "inventory", "MATCH (i:Item {sku:'A-100'}) RETURN i.count AS c;")
		},
			3*time.Minute, pollInterval).Should(ContainSubstring("42"))
		Eventually(func() string {
			return readCypher(pod, "customers", "MATCH (c:Customer {id:'C-1'}) RETURN c.tier AS t;")
		},
			3*time.Minute, pollInterval).Should(ContainSubstring("gold"))
	})
})
