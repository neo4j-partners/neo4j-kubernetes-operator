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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Backup chain e2e — proves spec.chainFromBackup composes a mixed-cadence
// backup workflow (FULL + DIFF) without waiting for cron.
//
// Flow:
//  1. Create the cluster + a standard DB; write seed data.
//  2. Create `inventory-daily` (one-shot, backupType: FULL).
//     Wait for Completed.
//  3. Write more data so the DIFF has something to capture.
//  4. Create `inventory-hourly` with chainFromBackup: inventory-daily
//     (one-shot, backupType: DIFF). Wait for Completed.
//  5. Verify both runs report the same BackupsPath (the chain root —
//     "inventory-daily") and each captured a distinct ArtifactFilename.
//     Verify both Jobs carry the same `app.kubernetes.io/part-of`
//     label.
//  6. Restore from `inventory-hourly` (the DIFF leaf). CloudSeedProvider
//     scans the shared directory and applies the chain. Expect the
//     restored DB to contain BOTH the pre-FULL and post-FULL data
//     (proving the chain was applied — not just the FULL).
var _ = Describe("Backup Chain Integration Tests", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout = 10 * time.Minute
		dbReadyTimeout      = 5 * time.Minute
		backupJobTimeout    = 10 * time.Minute
		restoreTimeout      = 10 * time.Minute
		minioReadyTimeout   = 5 * time.Minute
		pollInterval        = 5 * time.Second

		minioAccessKey = "minioadmin"
		minioSecretKey = "minioadmin"
		minioBucket    = "neo4j-backups"
		adminPass      = "password123"
		dbName         = "inventory"
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		db            *neo4jv1beta1.Neo4jDatabase
		dailyBackup   *neo4jv1beta1.Neo4jBackup
		hourlyBackup  *neo4jv1beta1.Neo4jBackup
		restore       *neo4jv1beta1.Neo4jRestore
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("backup-chain")

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
		var toClean []client.Object
		if restore != nil {
			toClean = append(toClean, restore)
		}
		if hourlyBackup != nil {
			toClean = append(toClean, hourlyBackup)
		}
		if dailyBackup != nil {
			toClean = append(toClean, dailyBackup)
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
		cluster, db, dailyBackup, hourlyBackup, restore = nil, nil, nil, nil, nil
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	It("composes a daily FULL + hourly DIFF chain via spec.chainFromBackup", func() {
		By("Creating a cluster with MinIO seed-creds projected")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "chain-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:      &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:  neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:   neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources: getCIAppropriateResourceRequirements(),
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

		By("Creating the standard database and writing initial data")
		db = &neo4jv1beta1.Neo4jDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: dbName, Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jDatabaseSpec{
				ClusterRef: cluster.Name, Name: dbName, Wait: true,
			},
		}
		Expect(k8sClient.Create(ctx, db)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(db), db)
			return db.Status.Phase
		}, dbReadyTimeout, pollInterval).Should(Equal("Ready"))

		podName := fmt.Sprintf("%s-server-0", cluster.Name)
		runCypher := func(stmt string) {
			Eventually(func() error {
				cmd := exec.CommandContext(ctx, "kubectl", "exec",
					podName, "-n", testNamespace, "--",
					"cypher-shell", "--format", "plain", "--database", dbName,
					"-u", "neo4j", "-p", adminPass, stmt,
				)
				out, err := cmd.CombinedOutput()
				if err != nil {
					GinkgoWriter.Printf("cypher err=%v out=%s\n", err, string(out))
				}
				return err
			}, 2*time.Minute, pollInterval).Should(Succeed())
		}
		runCypher("CREATE (:Item {sku: 'pre-full', n: 1}) RETURN count(*);")

		By("Creating the daily FULL backup (one-shot)")
		dailyBackup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-daily", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindDatabase, Name: dbName, ClusterRef: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: minioBucket,
					Path:   "inventory",
					Cloud: &neo4jv1beta1.CloudBlock{
						Provider:             "aws",
						CredentialsSecretRef: "minio-creds",
						EndpointURL:          "http://minio:9000",
						ForcePathStyle:       true,
					},
				},
				Options: &neo4jv1beta1.BackupOptions{BackupType: "FULL"},
			},
		}
		Expect(k8sClient.Create(ctx, dailyBackup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(dailyBackup), dailyBackup)
			return dailyBackup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"))
		Expect(dailyBackup.Status.History).ToNot(BeEmpty())
		dailyRun := dailyBackup.Status.History[0]
		Expect(dailyRun.Status).To(Equal("Succeeded"))
		Expect(dailyRun.BackupsPath).To(Equal("inventory-daily"),
			"daily CR's BackupsPath should be the chain root (its own name)")

		By("Adding data AFTER the daily FULL so the DIFF has something to capture")
		runCypher("CREATE (:Item {sku: 'post-full', n: 2}) RETURN count(*);")

		By("Creating the hourly DIFF backup chained off the daily")
		hourlyBackup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-hourly", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindDatabase, Name: dbName, ClusterRef: cluster.Name,
				},
				Storage:         dailyBackup.Spec.Storage,
				ChainFromBackup: dailyBackup.Name,
				Options:         &neo4jv1beta1.BackupOptions{BackupType: "DIFF"},
			},
		}
		Expect(k8sClient.Create(ctx, hourlyBackup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(hourlyBackup), hourlyBackup)
			return hourlyBackup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"))
		Expect(hourlyBackup.Status.History).ToNot(BeEmpty())
		hourlyRun := hourlyBackup.Status.History[0]
		Expect(hourlyRun.Status).To(Equal("Succeeded"))
		Expect(hourlyRun.BackupsPath).To(Equal("inventory-daily"),
			"hourly CR's BackupsPath should be the chain root (parent's name), not its own")

		By("Verifying both runs captured distinct ArtifactFilenames in the shared directory")
		Expect(dailyRun.ArtifactFilename).ToNot(BeEmpty(),
			"daily run should have ArtifactFilename captured from Pod-log parsing")
		Expect(hourlyRun.ArtifactFilename).ToNot(BeEmpty(),
			"hourly run should have ArtifactFilename captured")
		Expect(dailyRun.ArtifactFilename).ToNot(Equal(hourlyRun.ArtifactFilename),
			"daily and hourly artifacts must be distinct files in the shared directory")

		By("Verifying both Jobs carry the same app.kubernetes.io/part-of label (chain root)")
		jobs := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, jobs, client.InNamespace(testNamespace), client.MatchingLabels{
			"app.kubernetes.io/managed-by": "neo4j-operator",
			"app.kubernetes.io/component":  "backup",
			"app.kubernetes.io/part-of":    "inventory-daily",
		})).To(Succeed())
		Expect(jobs.Items).To(HaveLen(2),
			"both daily and hourly backup Jobs must share `part-of: inventory-daily`")

		By("Restoring from the hourly DIFF — should apply the full chain")
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   cluster.Name,
				DatabaseName: dbName,
				Source: neo4jv1beta1.RestoreSource{
					Type:      "backup",
					BackupRef: hourlyBackup.Name,
				},
				Force: true,
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), restore)
			return restore.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"restore via chained DIFF should reach Completed; message=%q", restore.Status.Message)

		By("Verifying BOTH the pre-FULL and post-FULL items are present (chain was applied, not just the FULL)")
		// List skus so a chain miss is visible: "pre-full" only → DIFF
		// didn't apply; "post-full" only → FULL wasn't replayed; both → ok.
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass,
				"MATCH (i:Item) RETURN i.sku AS sku ORDER BY sku;",
			)
			out, err := cmd.CombinedOutput()
			outStr := string(out)
			GinkgoWriter.Printf("verify chain err=%v out=%s\n", err, outStr)
			return outStr
		}, 2*time.Minute, pollInterval).Should(SatisfyAll(
			ContainSubstring("pre-full"),
			ContainSubstring("post-full"),
		), "both pre-FULL and post-FULL items must be present after restoring from the DIFF — chain wasn't applied correctly")
	})
})
