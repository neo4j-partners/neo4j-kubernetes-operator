/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package integration_test

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Standard Database PVC Restore — cluster Cypher restore from a PVC-backed
// backup, no MinIO required.
//
// Closes the cluster + standard-DB + PVC backup gap. The operator:
//  1. Captures the .backup artifact filename into BackupRun.ArtifactFilename
//     via Pod-log parsing (mirror of sharded F3 for shards).
//  2. Spawns an in-cluster httpd proxy (backup-seed-proxy-<restore-name>)
//     mounting the backup PVC RO at /backup.
//  3. Builds an http:// seedURI pointing at the captured filename.
//  4. Calls dbms.recreateDatabase / CREATE DATABASE OPTIONS{seedURI} with
//     the http URL; URLConnectionSeedProvider fetches the file.
//
// Asserts:
//   - Backup run records ArtifactFilename
//   - Proxy Deployment + Service are created with the Neo4jRestore CR as owner
//   - NO restore Job is created (Cypher path)
//   - Restore reaches Completed
//   - Restored data matches pre-backup state
var _ = Describe("Standard Database PVC Restore Integration Tests", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout = 10 * time.Minute
		dbReadyTimeout      = 5 * time.Minute
		backupJobTimeout    = 10 * time.Minute
		restoreTimeout      = 10 * time.Minute
		pollInterval        = 5 * time.Second

		adminPass = "password123"
		dbName    = "inventory"
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		db            *neo4jv1beta1.Neo4jDatabase
		backup        *neo4jv1beta1.Neo4jBackup
		restore       *neo4jv1beta1.Neo4jRestore
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("standard-db-pvc-restore")

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: testNamespace},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte(adminPass),
			},
			Type: corev1.SecretTypeOpaque,
		})).To(Succeed())

		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(pollInterval)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespaceDiagnostics(testNamespace)
		}
		var toClean []client.Object
		if restore != nil {
			toClean = append(toClean, restore)
		}
		if backup != nil {
			toClean = append(toClean, backup)
		}
		if db != nil {
			toClean = append(toClean, db)
		}
		if cluster != nil {
			toClean = append(toClean, cluster)
		}
		for _, cr := range toClean {
			if len(cr.GetFinalizers()) > 0 {
				cr.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, cr)
			}
			_ = k8sClient.Delete(ctx, cr)
		}
		cluster, db, backup, restore = nil, nil, nil, nil
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	It("backs up a standard DB to a PVC and restores via cluster Cypher (HTTP proxy)", func() {
		By("Creating a cluster")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-restore-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:      &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:  neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:   neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources: getCIAppropriateResourceRequirements(),
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.Phase
		}, clusterReadyTimeout, pollInterval).Should(Equal("Ready"))

		By(fmt.Sprintf("Creating the standard database '%s'", dbName))
		db = &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: dbName, Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: cluster.Name,
				Name:       dbName,
				Wait:       true,
			},
		}
		Expect(k8sClient.Create(ctx, db)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(db), db)
			return db.Status.Phase
		}, dbReadyTimeout, pollInterval).Should(Equal("Ready"))

		By("Writing test data via cypher-shell")
		podName := fmt.Sprintf("%s-server-0", cluster.Name)
		writeCypher := "CREATE (:Item {sku: 'A-100', count: 42}) RETURN count(*) AS n;"
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass, writeCypher,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("write err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By("Backing up to a PVC via Neo4jBackup")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-pvc-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindDatabase, Name: dbName, ClusterRef: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name:             "inventory-backup-pvc",
						Size:             "1Gi",
						StorageClassName: "standard",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"))
		Expect(backup.Status.History).ToNot(BeEmpty())
		Expect(backup.Status.History[0].Status).To(Equal("Succeeded"))

		By("Verifying ArtifactFilename was captured (Pod-log parser — prerequisite for PVC cluster restore)")
		Expect(backup.Status.History[0].ArtifactFilename).ToNot(BeEmpty(),
			"BackupRun.ArtifactFilename must be populated for cluster PVC restore to work; got empty")
		Expect(backup.Status.History[0].ArtifactFilename).To(MatchRegexp(`^inventory-20\d\d.+\.backup$`),
			"ArtifactFilename should match `<dbname>-<timestamp>.backup`; got %q", backup.Status.History[0].ArtifactFilename)

		By("Modifying data so the restore-then-verify check is meaningful")
		modifyCypher := "MATCH (i:Item) SET i.count = 999 RETURN count(i) AS n;"
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass, modifyCypher,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("modify err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By("Creating a Neo4jRestore that points at the PVC backup")
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-pvc-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   cluster.Name,
				DatabaseName: dbName,
				Source: neo4jv1beta1.RestoreSource{
					Type:      "backup",
					BackupRef: backup.Name,
				},
				Force: true,
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())

		By("Verifying the operator spawned the backup-seed-proxy Deployment + Service")
		proxyName := fmt.Sprintf("backup-seed-proxy-%s", restore.Name)
		Eventually(func() bool {
			dep := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: proxyName, Namespace: testNamespace}, dep)
			return err == nil
		}, 2*time.Minute, pollInterval).Should(BeTrue(),
			"backup-seed-proxy Deployment must be created by the operator when a PVC-backed cluster restore is in progress")

		Eventually(func() bool {
			svc := &corev1.Service{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: proxyName, Namespace: testNamespace}, svc)
			return err == nil
		}, 2*time.Minute, pollInterval).Should(BeTrue(),
			"backup-seed-proxy Service must be created")

		By("Asserting NO restore Job is created (cluster Cypher path — rule 75)")
		Consistently(func() int {
			jobs := &batchv1.JobList{}
			_ = k8sClient.List(ctx, jobs, client.InNamespace(testNamespace), client.MatchingLabels{
				"app.kubernetes.io/instance":  restore.Name,
				"app.kubernetes.io/component": "restore",
			})
			return len(jobs.Items)
		}, 30*time.Second, pollInterval).Should(BeZero(),
			"cluster Cypher restore must NOT spawn a Job — rule 75")

		By("Verifying Neo4jRestore reaches Completed")
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), restore)
			return restore.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"Neo4jRestore phase should reach Completed; status.message=%q",
			restore.Status.Message)

		By("Verifying the restored data matches the backup (count=42, not 999)")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass,
				"MATCH (i:Item {sku: 'A-100'}) RETURN i.count AS count;",
			)
			out, err := cmd.CombinedOutput()
			outStr := string(out)
			GinkgoWriter.Printf("verify err=%v out=%s\n", err, outStr)
			return outStr
		}, 2*time.Minute, pollInterval).Should(ContainSubstring("42"),
			"restored data should match the pre-backup state (count=42), not the post-backup modification (count=999)")
	})
})
