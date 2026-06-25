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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// Property Sharding Restore (MinIO) — Phase 2 E2E
//
// Exercises the seedBackupRef path against a real S3-compatible store:
//  1. Deploy MinIO into the test namespace, create the bucket via a mc Job.
//  2. Create a property-sharding cluster with the AWS creds Secret projected
//     via spec.extraEnvFrom and JAVA_TOOL_OPTIONS=-Daws.s3.forcePathStyle=true
//     in spec.env (MinIO requires path-style addressing).
//  3. Create a sharded DB `products`, back it up to MinIO.
//  4. Create a NEW sharded DB `products-restored` with spec.seedBackupRef
//     pointing at the backup. The operator resolves the reference into a
//     directory URI and injects it into CREATE DATABASE OPTIONS.
//  5. Verify the new sharded DB reaches Ready — confirming Neo4j was able
//     to reach MinIO, fetch the per-shard artifacts, and seed the new DB.
//
// Same gating as the rest of the property-sharding suite: CI-skipped, version
// gated to 2025.12+.
//
// Run locally:
//
//	NEO4J_VERSION=2025.12-enterprise ginkgo run -focus "Property Sharding Restore" ./test/integration
var _ = Describe("Property Sharding Restore (MinIO) Integration Tests", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout   = 10 * time.Minute
		shardedDBReadyTimeout = 10 * time.Minute
		backupJobTimeout      = 10 * time.Minute
		minioReadyTimeout     = 5 * time.Minute
		pollInterval          = 5 * time.Second

		minioAccessKey = "minioadmin"
		minioSecretKey = "minioadmin"
		minioBucket    = "neo4j-backups"
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		shardedDB     *neo4jv1beta1.Neo4jShardedDatabase
		shardedDB2    *neo4jv1beta1.Neo4jShardedDatabase
		backup        *neo4jv1beta1.Neo4jBackup
	)

	BeforeEach(func() {
		if isRunningInCI() {
			Skip("Skipping MinIO restore test in CI - resource requirements too large")
		}
		if !isPropertyShardingCompatible() {
			Skip("Skipping MinIO restore test: requires Neo4j 2025.12+")
		}
		testNamespace = createTestNamespace("property-sharding-minio")

		// Admin Secret for the Neo4j cluster.
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: testNamespace},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		})).To(Succeed())

		// MinIO admin Secret — same creds used by both MinIO server and the
		// backup/restore Job pods. Includes AWS_ENDPOINT_URL_S3 so the AWS SDK
		// running inside the Neo4j JVM (or neo4j-admin) routes S3 calls to
		// in-cluster MinIO instead of real AWS.
		minioCreds := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-creds", Namespace: testNamespace},
			Data: map[string][]byte{
				"AWS_ACCESS_KEY_ID":     []byte(minioAccessKey),
				"AWS_SECRET_ACCESS_KEY": []byte(minioSecretKey),
				"AWS_REGION":            []byte("us-east-1"),
				// In-namespace service hostname; MinIO is deployed in the SAME ns.
				"AWS_ENDPOINT_URL_S3": []byte("http://minio:9000"),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, minioCreds)).To(Succeed())

		// Deploy MinIO (single-pod, ephemeral storage). Acceptable for the
		// duration of one test; teardown happens via namespace deletion.
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
		// Build cleanup list dynamically to skip typed-nil pointers — a
		// `*Neo4jShardedDatabase(nil)` in a `[]client.Object` slice carries
		// a non-nil interface value (only the underlying pointer is nil),
		// so `cr != nil` is true and cr.GetFinalizers() then panics with
		// nil-pointer dereference. Building the slice from explicit
		// per-pointer checks avoids the interface-vs-pointer footgun.
		var toClean []client.Object
		if shardedDB2 != nil {
			toClean = append(toClean, shardedDB2)
		}
		if backup != nil {
			toClean = append(toClean, backup)
		}
		if shardedDB != nil {
			toClean = append(toClean, shardedDB)
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
		shardedDB, shardedDB2, backup, cluster = nil, nil, nil, nil
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	It("backs up a sharded DB to MinIO and restores into a new sharded DB via seedBackupRef", func() {
		By("Creating a property-sharding cluster with MinIO seed-creds projected")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-host-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:                   &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:               neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:                neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceCPU:    resource.MustParse("2000m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("8Gi"),
						corev1.ResourceCPU:    resource.MustParse("2000m"),
					},
				},
				PropertySharding: &neo4jv1beta1.PropertyShardingSpec{
					Enabled: true,
					Config: map[string]string{
						"internal.dbms.sharded_property_database.enabled":                     "true",
						"db.query.default_language":                                           "CYPHER_25",
						"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
					},
				},
				// Path-style addressing is required for MinIO; AWS SDK reads
				// it from the JAVA_TOOL_OPTIONS system property. The other
				// AWS creds come from the Secret via ExtraEnvFrom below.
				Env: []corev1.EnvVar{
					{Name: "JAVA_TOOL_OPTIONS", Value: "-Daws.s3.forcePathStyle=true"},
				},
				ExtraEnvFrom: []corev1.EnvFromSource{
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.Phase
		}, clusterReadyTimeout, pollInterval).Should(Equal("Ready"))
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
		}, clusterReadyTimeout, pollInterval).Should(BeTrue())

		By("Creating the source sharded DB 'products'")
		shardedDB = &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef:            cluster.Name,
				Name:                  "products",
				DefaultCypherLanguage: "25",
				PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
					PropertyShards: 2,
					GraphShard:     neo4jv1beta1.DatabaseTopology{Primaries: 1},
					PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{
						Replicas: 1,
					},
				},
				Wait:        true,
				IfNotExists: func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
			return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue())

		By("Writing test data to the source sharded DB so the restore is data-meaningful")
		// Without real data the test only proves seed PLUMBING; writing
		// nodes (with properties that land on the property shards) and
		// verifying them after restore proves the seedURI actually carried
		// the data across all shards.
		hostPod := fmt.Sprintf("%s-server-0", cluster.Name)
		writeCypher := "CREATE (:Item {sku: 'A-100', count: 42}), (:Item {sku: 'A-200', count: 13}) RETURN count(*) AS n;"
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				hostPod, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", "products",
				"-u", "neo4j", "-p", "password123",
				writeCypher,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("sharded data-write cypher-shell err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By("Backing up the sharded DB to MinIO via Neo4jBackup")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "products-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindShardedDatabase, Name: "products", ClusterRef: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: minioBucket,
					Path:   "products",
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

		By("Creating a NEW sharded DB 'products-restored' with spec.seedBackupRef pointing at the backup")
		shardedDB2 = &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products-restored", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef:            cluster.Name,
				Name:                  "products-restored",
				DefaultCypherLanguage: "25",
				PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
					PropertyShards:        2,
					GraphShard:            neo4jv1beta1.DatabaseTopology{Primaries: 1},
					PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{Replicas: 1},
				},
				SeedBackupRef:      "products-backup",
				SeedSourceDatabase: "products",
				Wait:               true,
				IfNotExists:        func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB2)).To(Succeed())

		By("Verifying the seedBackupRef resolved correctly + Neo4j fetched from MinIO + restored DB reached Ready")
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB2), shardedDB2)
			return shardedDB2.Status.ShardingReady != nil && *shardedDB2.Status.ShardingReady
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue(),
			"products-restored failed to reach Ready — likely Neo4j couldn't reach MinIO or auth failed; check cluster pod logs for `Unable to start database` + debug.log")

		By("Verifying the restored sharded DB actually contains the backed-up data (not just an empty Ready DB)")
		// Mirrors the standard-DB restore test: query the restored sharded
		// DB for the specific Item written to the source. This proves the
		// cloud directory seedURI carried the data across the graph +
		// property shards, not merely that CREATE … WAIT returned.
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				hostPod, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", "products-restored",
				"-u", "neo4j", "-p", "password123",
				"MATCH (i:Item {sku: 'A-100'}) RETURN i.count AS count;",
			)
			out, err := cmd.CombinedOutput()
			outStr := string(out)
			GinkgoWriter.Printf("sharded restore verify err=%v out=%s\n", err, outStr)
			return outStr
		}, 2*time.Minute, pollInterval).Should(ContainSubstring("42"),
			"restored sharded DB 'products-restored' must contain the Item{sku:'A-100',count:42} seeded from the source backup")
	})

	It("catalogues a sharded family in an all-databases backup and restores it from that backup", func() {
		By("Creating a property-sharding cluster with MinIO seed-creds projected")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-alldb-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:                   &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:               neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:                neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceCPU:    resource.MustParse("2000m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("8Gi"),
						corev1.ResourceCPU:    resource.MustParse("2000m"),
					},
				},
				PropertySharding: &neo4jv1beta1.PropertyShardingSpec{
					Enabled: true,
					Config: map[string]string{
						"internal.dbms.sharded_property_database.enabled":                     "true",
						"db.query.default_language":                                           "CYPHER_25",
						"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
					},
				},
				Env: []corev1.EnvVar{
					{Name: "JAVA_TOOL_OPTIONS", Value: "-Daws.s3.forcePathStyle=true"},
				},
				ExtraEnvFrom: []corev1.EnvFromSource{
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
		}, clusterReadyTimeout, pollInterval).Should(BeTrue())

		By("Creating the source sharded DB 'products' with data")
		shardedDB = &neo4jv1beta1.Neo4jShardedDatabase{
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
				Wait:        true,
				IfNotExists: func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
			return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue())

		hostPod := fmt.Sprintf("%s-server-0", cluster.Name)
		writeCypher := "CREATE (:Item {sku: 'A-100', count: 42}) RETURN count(*) AS n;"
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec", hostPod, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", "products",
				"-u", "neo4j", "-p", "password123", writeCypher)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("data-write err=%v out=%s\n", err, string(out))
			}
			return err
		}, 2*time.Minute, pollInterval).Should(Succeed())

		By("Taking an ALL-DATABASES backup (instanceRef + allDatabases) to MinIO")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "products-alldb-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				InstanceRef:  cluster.Name,
				AllDatabases: true,
				Storage: neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: minioBucket,
					Path:   "alldb",
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

		By("Verifying the all-databases backup CATALOGUED the 'products' sharded family")
		var fam *neo4jv1beta1.ShardedFamilyArtifacts
		for i := range backup.Status.History[0].ShardedFamilies {
			if backup.Status.History[0].ShardedFamilies[i].Family == "products" {
				fam = &backup.Status.History[0].ShardedFamilies[i]
				break
			}
		}
		Expect(fam).ToNot(BeNil(), "all-databases backup must catalogue the 'products' family in status.shardedFamilies")
		// 1 graph shard (g000) + 2 property shards (p000, p001).
		Expect(fam.ShardArtifacts).To(HaveLen(3))
		for _, a := range fam.ShardArtifacts {
			Expect(a.Filename).ToNot(BeEmpty(), "shard %s must have a captured filename for cloud per-shard seeding", a.ShardName)
		}

		By("Restoring the family from the ALL-DATABASES backup into 'products-restored' via seedBackupRef")
		shardedDB2 = &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products-restored", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef:            cluster.Name,
				Name:                  "products-restored",
				DefaultCypherLanguage: "25",
				PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
					PropertyShards:        2,
					GraphShard:            neo4jv1beta1.DatabaseTopology{Primaries: 1},
					PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{Replicas: 1},
				},
				SeedBackupRef:      "products-alldb-backup",
				SeedSourceDatabase: "products",
				Wait:               true,
				IfNotExists:        func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB2)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB2), shardedDB2)
			return shardedDB2.Status.ShardingReady != nil && *shardedDB2.Status.ShardingReady
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue(),
			"products-restored failed to reach Ready — the all-databases per-shard cloud seed didn't resolve; check operator + cluster pod logs")

		By("Verifying the restored DB contains the backed-up data (per-shard cloud seed carried it)")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec", hostPod, "-n", testNamespace, "--",
				"cypher-shell", "--format", "plain", "--database", "products-restored",
				"-u", "neo4j", "-p", "password123",
				"MATCH (i:Item {sku: 'A-100'}) RETURN i.count AS count;")
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, 2*time.Minute, pollInterval).Should(ContainSubstring("42"),
			"restored 'products-restored' must contain Item{sku:'A-100',count:42} seeded from the all-databases backup")
	})

	It("destructively replaces an existing sharded DB via replaceExisting+force", func() {
		By("Creating a property-sharding cluster with MinIO seed-creds projected")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-replace-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:                   &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology:               neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:                neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceCPU:    resource.MustParse("2000m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("8Gi"),
						corev1.ResourceCPU:    resource.MustParse("2000m"),
					},
				},
				PropertySharding: &neo4jv1beta1.PropertyShardingSpec{
					Enabled: true,
					Config: map[string]string{
						"internal.dbms.sharded_property_database.enabled":                     "true",
						"db.query.default_language":                                           "CYPHER_25",
						"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
					},
				},
				Env: []corev1.EnvVar{
					{Name: "JAVA_TOOL_OPTIONS", Value: "-Daws.s3.forcePathStyle=true"},
				},
				ExtraEnvFrom: []corev1.EnvFromSource{
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
		}, clusterReadyTimeout, pollInterval).Should(BeTrue())

		By("Creating the source sharded DB 'products' (no seed)")
		shardedDB = &neo4jv1beta1.Neo4jShardedDatabase{
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
				Wait:        true,
				IfNotExists: func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
			return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue())

		By("Backing up 'products' to MinIO")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "products-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindShardedDatabase, Name: "products", ClusterRef: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: minioBucket,
					Path:   "products-replace",
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

		By("Flipping 'products' to destructive-restore mode (replaceExisting+force+seedBackupRef)")
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB); err != nil {
				return err
			}
			shardedDB.Spec.ReplaceExisting = true
			shardedDB.Spec.Force = true
			ifn := false
			shardedDB.Spec.IfNotExists = &ifn
			shardedDB.Spec.SeedBackupRef = "products-backup"
			shardedDB.Spec.SeedSourceDatabase = "products"
			return k8sClient.Update(ctx, shardedDB)
		}, 30*time.Second, pollInterval).Should(Succeed(),
			"failed to update sharded DB CR for destructive restore")
		preReplaceGeneration := shardedDB.Generation

		By("Verifying the operator dropped + re-created from seed (Ready + LastDestructiveRestoreGeneration stamped)")
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
			return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady &&
				shardedDB.Status.LastDestructiveRestoreGeneration >= preReplaceGeneration
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue(),
			"destructive restore did not complete — expected LastDestructiveRestoreGeneration to be stamped and ShardingReady=true")
	})
})

// deployMinIO stamps a minimal single-pod MinIO into the namespace + a
// ClusterIP Service `minio:9000`. Storage is the pod's ephemeral disk —
// fine for one test run, gone with the namespace.
func deployMinIO(namespace, accessKey, secretKey string) {
	labels := map[string]string{"app": "minio"}
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "minio",
						Image: "minio/minio:latest",
						Args:  []string{"server", "/data", "--console-address", ":9001"},
						Env: []corev1.EnvVar{
							{Name: "MINIO_ROOT_USER", Value: accessKey},
							{Name: "MINIO_ROOT_PASSWORD", Value: secretKey},
						},
						Ports: []corev1.ContainerPort{
							{ContainerPort: 9000, Name: "s3"},
							{ContainerPort: 9001, Name: "console"},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/minio/health/ready",
									Port: intstr.FromInt(9000),
								},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       5,
						},
					}},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, dep)).To(Succeed())

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "s3", Port: 9000, TargetPort: intstr.FromInt(9000)},
			},
		},
	}
	Expect(k8sClient.Create(ctx, svc)).To(Succeed())
}

// waitForMinIOReady polls the minio Deployment until at least one replica
// reports Available.
func waitForMinIOReady(namespace string, timeout time.Duration) {
	Eventually(func() bool {
		dep := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: "minio", Namespace: namespace}, dep); err != nil {
			return false
		}
		return dep.Status.ReadyReplicas > 0
	}, timeout, 5*time.Second).Should(BeTrue(), "MinIO Deployment never became Ready")
}

// createMinIOBucket runs a one-shot Job using minio/mc to create the bucket.
// Returns when the Job reports Succeeded; fails the test if it doesn't.
func createMinIOBucket(namespace, bucket, accessKey, secretKey string, timeout time.Duration) {
	jobName := "mc-mkbucket"
	script := strings.Join([]string{
		// `mc alias set local http://minio:9000 <key> <secret>`
		// `mc mb --ignore-existing local/<bucket>`
		"mc alias set local http://minio:9000 " + accessKey + " " + secretKey,
		"mc mb --ignore-existing local/" + bucket,
	}, " && ")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: namespace},
		Spec: batchv1.JobSpec{
			BackoffLimit: func() *int32 { v := int32(2); return &v }(),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "mc",
						Image:   "minio/mc:latest",
						Command: []string{"/bin/sh", "-c", script},
					}},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, job)).To(Succeed())
	Eventually(func() int32 {
		_ = k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: namespace}, job)
		return job.Status.Succeeded
	}, timeout, 5*time.Second).Should(BeNumerically(">", 0),
		"mc mb Job never reached Succeeded — MinIO bucket creation failed")
}
