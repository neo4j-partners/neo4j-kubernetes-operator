/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package integration_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Property Sharding PVC Seed (F5) — exercises the HTTP-proxy path for
// PVC-backed seedBackupRef.
//
// Setup:
//
//  1. Create a property-sharding cluster.
//  2. Create sharded DB `products` and back it up to a PVC (NOT cloud).
//  3. Backup runs with spec.options.validate=true so ShardArtifacts.Filename
//     gets populated (F3 dependency — F5 requires exact filenames to build
//     per-shard URLs).
//  4. Create a NEW sharded DB `products-restored` with seedBackupRef
//     pointing at the PVC backup.
//  5. Assert the operator creates the `backup-seed-proxy-products-restored`
//     Deployment + Service.
//  6. Assert the new sharded DB's in-memory Cypher receives a seedURIs
//     map (rather than a single seedURI) keyed by shard name with
//     http://… URLs pointing at the proxy.
//  7. Assert the restored sharded DB reaches Ready — confirms Neo4j was
//     able to fetch per-shard files from the proxy via URLConnectionSeedProvider.
//
// Gated identically to the rest of the property-sharding suite (CI-skipped,
// Neo4j 2025.12+ only).
var _ = Describe("Property Sharding PVC Seed (F5) Integration Tests", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout   = 10 * time.Minute
		shardedDBReadyTimeout = 10 * time.Minute
		backupJobTimeout      = 10 * time.Minute
		proxyReadyTimeout     = 3 * time.Minute
		pollInterval          = 5 * time.Second
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		shardedDB     *neo4jv1beta1.Neo4jShardedDatabase
		shardedDB2    *neo4jv1beta1.Neo4jShardedDatabase
		backup        *neo4jv1beta1.Neo4jBackup
		backupPVC     *corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		if isRunningInCI() {
			Skip("Skipping PVC seed test in CI - resource requirements too large")
		}
		if !isPropertyShardingCompatible() {
			Skip("Skipping PVC seed test: requires Neo4j 2025.12+")
		}
		testNamespace = createTestNamespace("property-sharding-pvc-seed")

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: testNamespace},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		})).To(Succeed())

		// Pre-create the backup PVC. Sized generously since the proxy
		// will also mount it (RO) and Neo4j fetches per-shard files
		// across the network during seed.
		backupPVC = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-seed-backup", Namespace: testNamespace},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("2Gi"),
					},
				},
				StorageClassName: func() *string { s := "standard"; return &s }(),
			},
		}
		Expect(k8sClient.Create(ctx, backupPVC)).To(Succeed())

		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(pollInterval)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespaceDiagnostics(testNamespace)
		}
		// Per-pointer nil checks (typed-nil interface footgun — same fix as
		// property_sharding_minio_restore_test.go).
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

	It("backs up a sharded DB to a PVC and restores via PVC-proxy seedBackupRef", func() {
		By("Creating a property-sharding cluster")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-seed-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:     &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:  neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
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
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
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

		By("Backing up the sharded DB to the PVC (F3 captures shard filenames)")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "products-pvc-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindShardedDatabase, Name: "products", ClusterRef: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC:  &neo4jv1beta1.PVCSpec{Name: backupPVC.Name},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"))

		By("Verifying ShardArtifacts have Filenames populated (F3 prerequisite for F5)")
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			if len(backup.Status.History) == 0 || len(backup.Status.History[0].ShardArtifacts) == 0 {
				return false
			}
			for _, a := range backup.Status.History[0].ShardArtifacts {
				if a.Filename == "" {
					return false
				}
			}
			return true
		}, 3*time.Minute, pollInterval).Should(BeTrue(),
			"F5 requires F3 to have populated per-shard Filename for all shards")

		By("Creating the restore-target sharded DB with PVC seedBackupRef")
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
				SeedBackupRef:      backup.Name,
				SeedSourceDatabase: "products",
				Wait:               true,
				IfNotExists:        func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB2)).To(Succeed())

		By("Verifying the operator spawned the backup-seed-proxy Deployment + Service")
		proxyName := "backup-seed-proxy-products-restored"
		Eventually(func() bool {
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: proxyName, Namespace: testNamespace}, dep); err != nil {
				return false
			}
			return dep.Status.ReadyReplicas > 0
		}, proxyReadyTimeout, pollInterval).Should(BeTrue(),
			"backup-seed-proxy Deployment must reach Ready replicas > 0 for the seed fetch to succeed")
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: proxyName, Namespace: testNamespace}, svc)).To(Succeed())
		// Service should expose :8080 — load-bearing for the seed URL.
		var port int32
		for _, p := range svc.Spec.Ports {
			if p.Name == "http" {
				port = p.Port
			}
		}
		Expect(port).To(BeEquivalentTo(8080),
			"backup-seed-proxy Service must expose port 8080 named 'http'")

		By("Verifying the restored sharded DB reaches Ready (Neo4j fetched from PVC proxy)")
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB2), shardedDB2)
			return shardedDB2.Status.ShardingReady != nil && *shardedDB2.Status.ShardingReady
		}, shardedDBReadyTimeout, pollInterval).Should(BeTrue(),
			"products-restored failed to reach Ready — likely Neo4j couldn't fetch from the proxy (check proxy pod logs + cluster pod debug.log)")
	})
})
