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

// All-databases backup + cluster restore (#222), exercising the v1.13
// scope-based API end to end:
//   - back up EVERY user database with spec.instanceRef + spec.allDatabases,
//   - confirm the per-database artifact map (status.history[].databaseArtifacts),
//   - restore EVERY user database with spec.instanceRef + spec.allDatabases,
//   - confirm per-database status (status.databaseResults) and that each
//     database's data round-trips.
//
// Run locally (operator must be deployed to neo4j-operator-system):
//
//	NEO4J_VERSION=2025.12-enterprise ginkgo run -focus "All-Databases" ./test/integration
var _ = Describe("All-Databases Backup and Cluster Restore (MinIO) Integration Tests", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout = 10 * time.Minute
		dbReadyTimeout      = 5 * time.Minute
		backupJobTimeout    = 10 * time.Minute
		restoreTimeout      = 12 * time.Minute
		minioReadyTimeout   = 5 * time.Minute
		pollInterval        = 5 * time.Second

		minioAccessKey = "minioadmin"
		minioSecretKey = "minioadmin"
		minioBucket    = "neo4j-backups"
		adminPass      = "password123"
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		dbInventory   *neo4jv1beta1.Neo4jDatabase
		dbCustomers   *neo4jv1beta1.Neo4jDatabase
		backup        *neo4jv1beta1.Neo4jBackup
		restore       *neo4jv1beta1.Neo4jRestore
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("all-db-restore")

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: testNamespace},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte(adminPass),
			},
			Type: corev1.SecretTypeOpaque,
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-creds", Namespace: testNamespace},
			Data: map[string][]byte{
				"AWS_ACCESS_KEY_ID":     []byte(minioAccessKey),
				"AWS_SECRET_ACCESS_KEY": []byte(minioSecretKey),
				"AWS_REGION":            []byte("us-east-1"),
				"AWS_ENDPOINT_URL_S3":   []byte("http://minio:9000"),
			},
			Type: corev1.SecretTypeOpaque,
		})).To(Succeed())

		deployMinIO(testNamespace, minioAccessKey, minioSecretKey)
		waitForMinIOReady(testNamespace, minioReadyTimeout)
		createMinIOBucket(testNamespace, minioBucket, minioAccessKey, minioSecretKey, minioReadyTimeout)

		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(pollInterval)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespaceDiagnostics(testNamespace)
		}
		for _, cr := range []client.Object{restore, backup, dbInventory, dbCustomers, cluster} {
			if cr == nil {
				continue
			}
			if len(cr.GetFinalizers()) > 0 {
				cr.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, cr)
			}
			_ = k8sClient.Delete(ctx, cr)
		}
		cluster, dbInventory, dbCustomers, backup, restore = nil, nil, nil, nil, nil
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	writeCypher := func(podName, dbName, stmt string) {
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec", podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName, "-u", "neo4j", "-p", adminPass, stmt)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell (%s) err=%v out=%s\n", dbName, err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())
	}

	It("backs up and restores every user database via spec.allDatabases", func() {
		By("Creating a 2-server cluster with MinIO seed-creds projected")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "alldb-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:                   &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:               neo4jv1beta1.TopologyConfiguration{Servers: 2},
				Storage:                neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources:              getCIAppropriateResourceRequirements(),
				Env: []corev1.EnvVar{
					{Name: "JAVA_TOOL_OPTIONS", Value: "-Daws.s3.forcePathStyle=true"},
				},
				ExtraEnvFrom: []corev1.EnvFromSource{
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"}}},
				},
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.Phase
		}, clusterReadyTimeout, pollInterval).Should(Equal("Ready"))

		By("Creating two user databases (inventory, customers)")
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

		podName := fmt.Sprintf("%s-server-0", cluster.Name)
		By("Writing distinct data to each database")
		writeCypher(podName, "inventory", "CREATE (:Item {sku:'A-100', count:42}) RETURN 1;")
		writeCypher(podName, "customers", "CREATE (:Customer {id:'C-1', tier:'gold'}) RETURN 1;")

		By("Backing up EVERY database with spec.instanceRef + spec.allDatabases (v1.13 API)")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "all-db-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				InstanceRef:  cluster.Name,
				AllDatabases: true,
				Storage: neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: minioBucket,
					Path:   "all-db",
					Cloud: &neo4jv1beta1.CloudBlock{
						Provider: "aws", CredentialsSecretRef: "minio-creds",
						EndpointURL: "http://minio:9000", ForcePathStyle: true,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"))

		By("Confirming the per-database artifact map records inventory + customers")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)).To(Succeed())
		Expect(backup.Status.History).ToNot(BeEmpty())
		gotDBs := map[string]bool{}
		for _, a := range backup.Status.History[0].DatabaseArtifacts {
			gotDBs[a.Database] = true
		}
		Expect(gotDBs).To(HaveKey("inventory"))
		Expect(gotDBs).To(HaveKey("customers"))

		By("Modifying both databases so the restore is meaningful")
		writeCypher(podName, "inventory", "MATCH (i:Item) SET i.count = 999 RETURN 1;")
		writeCypher(podName, "customers", "MATCH (c:Customer) SET c.tier = 'bronze' RETURN 1;")

		By("Restoring EVERY database with spec.instanceRef + spec.allDatabases (v1.13 API)")
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "all-db-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				InstanceRef:  cluster.Name,
				AllDatabases: true,
				Force:        true, // databases exist — recreate from backup
				Source:       neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: backup.Name},
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())

		By("Verifying the all-databases restore reaches Completed with per-database results")
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), restore)
			return restore.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"all-databases restore should reach Completed; message=%q results=%+v",
			restore.Status.Message, restore.Status.DatabaseResults)

		gotResults := map[string]string{}
		for _, r := range restore.Status.DatabaseResults {
			gotResults[r.Database] = r.Phase
		}
		Expect(gotResults).To(HaveKeyWithValue("inventory", "Completed"))
		Expect(gotResults).To(HaveKeyWithValue("customers", "Completed"))

		By("Verifying both databases' data round-tripped (pre-backup values restored)")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec", podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", "inventory", "-u", "neo4j", "-p", adminPass,
				"MATCH (i:Item {sku:'A-100'}) RETURN i.count AS count;")
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, 3*time.Minute, pollInterval).Should(ContainSubstring("42"))
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec", podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", "customers", "-u", "neo4j", "-p", adminPass,
				"MATCH (c:Customer {id:'C-1'}) RETURN c.tier AS tier;")
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, 3*time.Minute, pollInterval).Should(ContainSubstring("gold"))
	})
})
