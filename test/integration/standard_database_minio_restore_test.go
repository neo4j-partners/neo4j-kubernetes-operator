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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Standard Database Restore (MinIO) — cluster Cypher restore E2E.
//
// Proves the architectural property that cluster Neo4jRestore targets use the
// Cypher path (`dbms.recreateDatabase` / `CREATE DATABASE OPTIONS{seedURI}`)
// per the Neo4j cluster restore docs, NOT a `neo4j-admin database restore`
// Job (which the docs flag as unsafe on clusters).
//
// Flow:
//  1. Deploy MinIO into the test namespace.
//  2. Create a 3-server cluster with MinIO creds projected via
//     spec.extraEnvFrom (CloudSeedProvider authenticates via the AWS SDK
//     default credential chain) + JAVA_TOOL_OPTIONS=-Daws.s3.forcePathStyle=true
//     for MinIO compatibility.
//  3. Create a standard database `inventory` via Neo4jDatabase CR.
//  4. Write some data via cypher-shell.
//  5. Back up `inventory` to MinIO via Neo4jBackup (target.kind=Database).
//  6. Verify backup succeeded + status.history populated.
//  7. Create a Neo4jRestore that points at the backup.
//  8. Assert: NO restore Job was created in the namespace (the test of
//     "cluster targets use Cypher, not Job" — rule 75).
//  9. Assert: Neo4jRestore reaches Completed state.
//  10. Verify the database is reachable via cypher-shell after restore.
//
// Run locally:
//
//	NEO4J_VERSION=2025.12-enterprise ginkgo run -focus "Standard Database Restore" ./test/integration
var _ = Describe("Standard Database Restore (MinIO) Integration Tests", Label("extended"), Serial, func() {
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
		backup        *neo4jv1beta1.Neo4jBackup
		restore       *neo4jv1beta1.Neo4jRestore
	)

	BeforeEach(func() {
		testNamespace = createTestNamespace("standard-db-cypher-restore")

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

	It("backs up a standard DB to MinIO and restores via cluster Cypher path (no Job)", func() {
		By("Creating a cluster with MinIO seed-creds projected")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cypher-restore-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:      &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:  neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:   neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources: getCIAppropriateResourceRequirements(),
				// MinIO needs path-style addressing; AWS SDK reads it from the
				// JAVA_TOOL_OPTIONS system property. The other creds come from
				// the Secret via ExtraEnvFrom below.
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

		By(fmt.Sprintf("Creating the standard database '%s' via Neo4jDatabase CR", dbName))
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

		By("Writing some test data via cypher-shell")
		podName := fmt.Sprintf("%s-server-0", cluster.Name)
		writeCypher := "CREATE (:Item {sku: 'A-100', count: 42}), (:Item {sku: 'A-200', count: 13}) RETURN count(*) AS n;"
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass,
				writeCypher,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("data-write cypher-shell err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By(fmt.Sprintf("Backing up '%s' to MinIO via Neo4jBackup", dbName))
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-backup", Namespace: testNamespace},
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
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"))
		Expect(backup.Status.History).ToNot(BeEmpty())
		Expect(backup.Status.History[0].Status).To(Equal("Succeeded"))

		By("Modifying data so the restore-then-verify check is meaningful")
		modifyCypher := "MATCH (i:Item) SET i.count = 999 RETURN count(i) AS n;"
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass,
				modifyCypher,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("post-backup modify err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By("Creating a Neo4jRestore CR pointing at the backup")
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-restore", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   cluster.Name,
				DatabaseName: dbName,
				Source: neo4jv1beta1.RestoreSource{
					Type:      "backup",
					BackupRef: backup.Name,
				},
				// stopCluster: false is the right setting for the cluster
				// Cypher path — the Cypher procedures handle atomic swap
				// without scaling down the StatefulSet.
				StopCluster: false,
				// Force=true because the database exists and we want
				// dbms.recreateDatabase to run against it (the operator's
				// non-force path would reject due to existence; force says
				// "yes, overwrite").
				Force: true,
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())

		By("Asserting NO restore Job is created in the namespace (cluster Cypher path — rule 75)")
		// Sleep briefly to let any erroneous Job creation race land, then
		// check. We do this before the Completed check because once Restore
		// is Completed there's no risk of Job creation anyway.
		Consistently(func() int {
			jobs := &batchv1.JobList{}
			_ = k8sClient.List(ctx, jobs, client.InNamespace(testNamespace), client.MatchingLabels{
				"app.kubernetes.io/instance":  restore.Name,
				"app.kubernetes.io/component": "restore",
			})
			return len(jobs.Items)
		}, 30*time.Second, pollInterval).Should(BeZero(),
			"cluster Cypher restore must NOT spawn a Job — rule 75")

		By("Verifying the Neo4jRestore reaches Completed")
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), restore)
			return restore.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"Neo4jRestore phase should reach Completed via the cluster Cypher path; status.message=%q",
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
			GinkgoWriter.Printf("verify cypher-shell err=%v out=%s\n", err, outStr)
			return outStr
		}, 2*time.Minute, pollInterval).Should(ContainSubstring("42"),
			"restored data should match the pre-backup state (count=42), not the post-backup modification (count=999)")

		// ── Second restore via source.type=storage (no Neo4jBackup CR) ──
		// Same cluster + same backup files, but this time we restore by
		// pointing directly at the storage location + exact .backup file —
		// the cross-cluster / disaster-recovery path where no Neo4jBackup CR
		// is available. Regression guard for the directory-vs-file fix on the
		// type=storage cluster cloud path.
		By("Capturing the backup's artifact filename for the type=storage restore")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)).To(Succeed())
		var artifact string
		for i := range backup.Status.History {
			if backup.Status.History[i].Status == "Succeeded" && backup.Status.History[i].ArtifactFilename != "" {
				artifact = backup.Status.History[i].ArtifactFilename
				break
			}
		}
		Expect(artifact).ToNot(BeEmpty(), "backup must record an ArtifactFilename for a type=storage cluster restore")

		By("Re-modifying data so the type=storage restore is meaningful (count=777)")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass,
				"MATCH (i:Item) SET i.count = 777 RETURN count(i) AS n;",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("pre-storage-restore modify err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By("Restoring AGAIN via source.type=storage pointing at the exact .backup file")
		restoreStorage := &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-restore-storage", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   cluster.Name,
				DatabaseName: dbName,
				StopCluster:  false,
				Force:        true,
				Source: neo4jv1beta1.RestoreSource{
					Type: "storage",
					Storage: &neo4jv1beta1.StorageLocation{
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
					// Exact .backup file path under storage.path: <chain-root>/<artifact>.
					// chain-root is the backup CR name (no spec.chainFromBackup here).
					BackupPath: fmt.Sprintf("%s/%s", backup.Name, artifact),
				},
			},
		}
		Expect(k8sClient.Create(ctx, restoreStorage)).To(Succeed())

		By("Verifying the type=storage restore reaches Completed (not Failed on a directory seedURI)")
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restoreStorage), restoreStorage)
			return restoreStorage.Status.Phase
		}, restoreTimeout, pollInterval).Should(Equal("Completed"),
			"type=storage cluster restore should reach Completed; status.message=%q", restoreStorage.Status.Message)

		By("Verifying the type=storage restore brought the data back to count=42 (not 777)")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", dbName,
				"-u", "neo4j", "-p", adminPass,
				"MATCH (i:Item {sku: 'A-100'}) RETURN i.count AS count;",
			)
			out, err := cmd.CombinedOutput()
			outStr := string(out)
			GinkgoWriter.Printf("type=storage verify cypher-shell err=%v out=%s\n", err, outStr)
			return outStr
		}, 2*time.Minute, pollInterval).Should(ContainSubstring("42"),
			"type=storage restore should match the backup state (count=42), not the post-restore modification (count=777)")
	})

	It("rejects sharded DB restores with an actionable error pointing at Neo4jShardedDatabase", func() {
		// This is a pure validation test — no cluster bootstrap needed. The
		// Neo4jRestore validator looks up Neo4jShardedDatabase by name in the
		// namespace and rejects the restore at validateRestore time, before
		// any cluster connectivity is attempted. Creating just the
		// Neo4jShardedDatabase CR shell is enough to trigger the lookup.
		//
		// We do still need a Neo4jEnterpriseCluster CR to exist (the
		// controller resolves spec.clusterRef before validation), but it
		// doesn't have to be Ready or even valid — the restore validator
		// runs BEFORE the cluster connection.
		By("Creating a minimal cluster CR (not bootstrapped — validator runs early)")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "sharded-reject-cluster", Namespace: testNamespace},
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

		By("Creating a Neo4jShardedDatabase CR shell (no validation, no bootstrap)")
		shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef:            cluster.Name,
				Name:                  "products",
				DefaultCypherLanguage: "25",
				PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
					PropertyShards:        2,
					GraphShard:            neo4jv1beta1.DatabaseTopology{Primaries: 1},
					PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{Replicas: 1},
				},
				IfNotExists: func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB)).To(Succeed())
		defer func() {
			if len(shardedDB.GetFinalizers()) > 0 {
				shardedDB.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, shardedDB)
			}
			_ = k8sClient.Delete(ctx, shardedDB)
		}()

		By("Creating a Neo4jRestore that targets the sharded DB — must be rejected by validateRestore")
		// Use source.type=storage with a stub StorageLocation so the
		// validator skips the "backup ref must exist" check and reaches
		// the sharded-DB-rejection check (which is the actual assertion
		// of this test). The storage values are placeholders — the
		// validator only verifies BackupPath is set, not that the data
		// exists.
		restore = &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "products-restore-attempt", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   cluster.Name,
				DatabaseName: "products",
				Source: neo4jv1beta1.RestoreSource{
					Type: "storage",
					Storage: &neo4jv1beta1.StorageLocation{
						Type: "s3", Bucket: "stub", Path: "stub",
					},
					BackupPath: "stub-path",
				},
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())

		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), restore)
			return restore.Status.Phase
		}, 2*time.Minute, pollInterval).Should(Equal("Failed"),
			"Neo4jRestore targeting a sharded DB must be rejected with status=Failed")
		Expect(restore.Status.Message).To(SatisfyAll(
			ContainSubstring("Neo4jShardedDatabase"),
			ContainSubstring("replaceExisting"),
		), "rejection message should point at the sharded restore path; got %q",
			strings.ToLower(restore.Status.Message))
	})
})
